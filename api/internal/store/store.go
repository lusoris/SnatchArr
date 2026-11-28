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
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	dbmigrate "github.com/golusoris/golusoris/db/migrate"

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

const (
	snapshotStepTimeout = 10 * time.Second
	rollbackTimeout     = 5 * time.Second
)

type rwTxBeginner func(ctx context.Context) (pgx.Tx, error)

type txBeginner func(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)

type txLabels struct {
	begin    string
	commit   string
	rollback string
}

// Tx runs fn inside a transaction and commits when it returns nil.
func (s *Store) Tx(ctx context.Context, fn func(q *sqlcgen.Queries) error) error {
	return runTx(ctx, s.pool.Begin, s.q, fn)
}

func runTx(
	ctx context.Context,
	begin rwTxBeginner,
	queries *sqlcgen.Queries,
	fn func(q *sqlcgen.Queries) error,
) error {
	return executeTx(
		ctx,
		begin,
		func(parent context.Context, tx pgx.Tx) error { return tx.Commit(parent) },
		queries,
		txLabels{begin: "store: begin", commit: "store: commit", rollback: "store: rollback"},
		fn,
	)
}

// SnapshotReadTx runs fn inside a repeatable-read, read-only transaction so multi-batch
// reads observe a single consistent database snapshot without blocking concurrent writers.
func (s *Store) SnapshotReadTx(ctx context.Context, fn func(q *sqlcgen.Queries) error) error {
	return runSnapshotReadTx(ctx, s.pool.BeginTx, s.q, fn)
}

func runSnapshotReadTx(
	ctx context.Context,
	begin txBeginner,
	queries *sqlcgen.Queries,
	fn func(q *sqlcgen.Queries) error,
) error {
	return executeTx(
		ctx,
		func(parent context.Context) (pgx.Tx, error) {
			beginCtx, beginCancel := context.WithTimeout(parent, snapshotStepTimeout)
			defer beginCancel()
			return begin(beginCtx, pgx.TxOptions{
				IsoLevel:   pgx.RepeatableRead,
				AccessMode: pgx.ReadOnly,
			})
		},
		func(parent context.Context, tx pgx.Tx) error {
			commitCtx, commitCancel := context.WithTimeout(parent, snapshotStepTimeout)
			defer commitCancel()
			return tx.Commit(commitCtx)
		},
		queries,
		txLabels{
			begin:    "store: begin snapshot tx",
			commit:   "store: commit snapshot tx",
			rollback: "store: rollback snapshot tx",
		},
		fn,
	)
}

func executeTx(
	ctx context.Context,
	begin rwTxBeginner,
	commit func(context.Context, pgx.Tx) error,
	queries *sqlcgen.Queries,
	labels txLabels,
	fn func(q *sqlcgen.Queries) error,
) (err error) {
	tx, err := begin(ctx)
	if err != nil {
		return fmt.Errorf("%s: %w", labels.begin, err)
	}
	committed := false
	defer func() {
		if rbErr := rollbackTx(ctx, tx, labels.rollback); !committed && rbErr != nil {
			err = errors.Join(err, rbErr)
		}
	}()
	var q *sqlcgen.Queries
	if queries != nil {
		q = queries.WithTx(tx)
	}
	if err = fn(q); err != nil {
		return err
	}
	if commitErr := commit(ctx, tx); commitErr != nil {
		return fmt.Errorf("%s: %w", labels.commit, commitErr)
	}
	committed = true
	return nil
}

func rollbackTx(ctx context.Context, tx pgx.Tx, prefix string) error {
	rbCtx, rbCancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
	defer rbCancel()
	if err := tx.Rollback(rbCtx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		return fmt.Errorf("%s: %w", prefix, err)
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

// Module provides *Store from the golusoris pool, and only once the migrator exists: fx runs
// start hooks in the order they were appended, so every store user (leader loops, gRPC,
// HTTP handlers) appends its hook after the migrator's Up and starts on a migrated schema.
// Without this, leader/gate's child-module invoke started the loops before the root invoke
// built the migrator (#124).
var Module = fx.Module("snatcharr.store", fx.Provide(newAfterMigrations))

// newAfterMigrations is New for fx; the migrator parameter only orders construction.
func newAfterMigrations(pool *pgxpool.Pool, _ *dbmigrate.Migrator) *Store {
	return New(pool)
}
