// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package hunt is the control plane's hunting brain: per-instance hourly budgets,
// processed-item memory, run lifecycle, schedule evaluation and the planner tick.
// Execution against *arr instances happens in the Rust worker.
package hunt

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// WarnRatio is the fraction of the hourly cap at which the UI shows a warning.
const WarnRatio = 0.8

// Grant is the outcome of a budget request.
type Grant struct {
	Granted   int
	Used      int
	Cap       int
	Remaining int
	ResetsAt  time.Time
}

// Budget debits per-instance hourly buckets. The cap counts items searched (newtarr's
// meaning), not HTTP requests, and is enforced atomically so N workers share one budget.
type Budget struct {
	st  *store.Store
	clk clock.Clock
}

// NewBudget wires the budget.
func NewBudget(st *store.Store, clk clock.Clock) *Budget {
	return &Budget{st: st, clk: clk}
}

// Window returns the start of the hourly bucket containing now.
func Window(now time.Time) time.Time {
	return now.UTC().Truncate(time.Hour)
}

// Acquire grants up to `requested` items from the current window without exceeding cap.
func (b *Budget) Acquire(ctx context.Context, instanceID uuid.UUID, capacity, requested int) (Grant, error) {
	now := b.clk.Now()
	window := Window(now)
	if requested < 0 || capacity <= 0 {
		return Grant{}, fmt.Errorf("hunt: budget: invalid request (capacity %d, requested %d)", capacity, requested)
	}
	var g Grant
	err := b.st.Tx(ctx, func(q *sqlcgen.Queries) error {
		key := sqlcgen.EnsureBucketParams{InstanceID: instanceID, WindowStart: window}
		if err := q.EnsureBucket(ctx, key); err != nil {
			return fmt.Errorf("ensure bucket: %w", err)
		}
		used, err := q.LockBucket(ctx, sqlcgen.LockBucketParams{InstanceID: instanceID, WindowStart: window})
		if err != nil {
			return fmt.Errorf("lock bucket: %w", err)
		}
		g = grant(int(used), capacity, requested, window)
		if g.Granted > 0 {
			err = q.SetBucketUsed(ctx, sqlcgen.SetBucketUsedParams{
				InstanceID: instanceID, WindowStart: window, Used: int32(g.Used), //nolint:gosec // bounded by cap <= 500
			})
			if err != nil {
				return fmt.Errorf("set bucket: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return Grant{}, fmt.Errorf("hunt: budget: %w", store.MapError(err))
	}
	return g, nil
}

// grant is the pure cap arithmetic.
func grant(used, capacity, requested int, window time.Time) Grant {
	remaining := max(capacity-used, 0)
	granted := min(requested, remaining)
	return Grant{
		Granted:   granted,
		Used:      used + granted,
		Cap:       capacity,
		Remaining: remaining - granted,
		ResetsAt:  window.Add(time.Hour),
	}
}

// Used reports the current window's consumption without debiting.
func (b *Budget) Used(ctx context.Context, instanceID uuid.UUID) (int, time.Time, error) {
	window := Window(b.clk.Now())
	used, err := b.st.Q().GetBucketUsed(ctx, sqlcgen.GetBucketUsedParams{InstanceID: instanceID, WindowStart: window})
	if err != nil {
		return 0, window, fmt.Errorf("hunt: budget used: %w", store.MapError(err))
	}
	return int(used), window.Add(time.Hour), nil
}
