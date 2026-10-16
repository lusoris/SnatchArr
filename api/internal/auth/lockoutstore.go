// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/golusoris/golusoris/auth/lockout"
	gerr "github.com/golusoris/golusoris/core/errors"

	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

var _ lockout.Store = (*PgLockoutStore)(nil)

// PgLockoutStore implements golusoris auth/lockout.Store in Postgres.
type PgLockoutStore struct {
	st *store.Store
}

// NewPgLockoutStore builds the store.
func NewPgLockoutStore(st *store.Store) *PgLockoutStore {
	return &PgLockoutStore{st: st}
}

// Get returns the lockout state for key or gerr.CodeNotFound when absent.
func (p *PgLockoutStore) Get(ctx context.Context, key string) (lockout.State, error) {
	row, err := p.st.Q().GetLoginLockout(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockout.State{}, gerr.NotFound("lockout: not found")
	}
	if err != nil {
		return lockout.State{}, fmt.Errorf("auth: get lockout: %w", err)
	}
	st := lockout.State{
		Fails:       int(row.Fails),
		FirstFailAt: row.FirstFailAt,
	}
	if row.LockedUntil != nil {
		st.LockedUntil = *row.LockedUntil
	}
	return st, nil
}

// RecordFailure atomically resets stale windows, increments, and locks at threshold.
func (p *PgLockoutStore) RecordFailure(ctx context.Context, key string, now time.Time, opts lockout.Options) error {
	params := sqlcgen.RecordLoginLockoutFailureParams{
		Key:       key,
		Now:       now,
		MaxFails:  int32(opts.MaxFails), // #nosec G115 -- policy constant bounded by opts.MaxFails
		CooldownS: opts.Cooldown.Seconds(),
		WindowS:   opts.Window.Seconds(),
	}
	if err := p.st.Q().RecordLoginLockoutFailure(ctx, params); err != nil {
		return fmt.Errorf("auth: record lockout failure: %w", err)
	}
	return nil
}

// Delete removes the lockout state for key.
func (p *PgLockoutStore) Delete(ctx context.Context, key string) error {
	if err := p.st.Q().DeleteLoginLockout(ctx, key); err != nil {
		return fmt.Errorf("auth: delete lockout: %w", err)
	}
	return nil
}

// PurgeExpired removes up to limit rows whose lock has expired and whose window has passed.
func (p *PgLockoutStore) PurgeExpired(ctx context.Context, now time.Time, window time.Duration, limit int32) (int64, error) {
	n, err := p.st.Q().PurgeExpiredLoginLockouts(ctx, sqlcgen.PurgeExpiredLoginLockoutsParams{
		Now:     now,
		WindowS: window.Seconds(),
		Batch:   limit,
	})
	if err != nil {
		return 0, fmt.Errorf("auth: purge login lockouts: %w", err)
	}
	return n, nil
}
