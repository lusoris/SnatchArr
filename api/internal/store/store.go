// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package store is the persistence layer: sqlc-generated queries over the golusoris
// pgx pool, plus converters between rows and domain types. Migrations are embedded and
// applied by golusoris db/migrate at start-up.
package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

//go:embed migrations/postgres/*.sql
var migrationsFS embed.FS

// MigrationsFS returns the Postgres migration files rooted for golang-migrate's iofs
// source.
func MigrationsFS() (fs.FS, error) {
	sub, err := fs.Sub(migrationsFS, "migrations/postgres")
	if err != nil {
		return nil, fmt.Errorf("store: migrations fs: %w", err)
	}
	return sub, nil
}

// Store bundles the pool and the generated queries.
type Store struct {
	pool *pgxpool.Pool
	q    *sqlcgen.Queries
}

// New builds a Store over an existing pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: sqlcgen.New(pool)}
}

// Q returns the auto-committing query set.
func (s *Store) Q() *sqlcgen.Queries { return s.q }

// Pool exposes the pool for health checks and advisory locks.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// Tx runs fn inside a transaction and commits when it returns nil.
func (s *Store) Tx(ctx context.Context, fn func(q *sqlcgen.Queries) error) (err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer func() {
		if err == nil {
			return
		}
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			err = errors.Join(err, fmt.Errorf("store: rollback: %w", rbErr))
		}
	}()
	if err = fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}

// MapError translates driver errors to domain sentinels so callers never import pgx.
func MapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		switch pgErr.Code {
		case "23505": // unique_violation
			return fmt.Errorf("%w: %s", domain.ErrConflict, pgErr.ConstraintName)
		case "23514", "22P02": // check_violation, invalid_text_representation
			return fmt.Errorf("%w: %s", domain.ErrInvalid, pgErr.Message)
		}
	}
	return err
}

// Module provides *Store from the golusoris pool.
var Module = fx.Module("snatcharr.store", fx.Provide(New))
