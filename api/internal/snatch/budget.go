// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package snatch is the control plane's snatching brain: per-instance hourly budgets,
// processed-item memory, run lifecycle, schedule evaluation and the planner tick.
// Execution against *arr instances happens in the Rust worker.
package snatch

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

// Acquire grants up to `requested` items from the current window without exceeding the
// instance cap nor, when globalCap > 0, the stamina shared by every instance.
func (b *Budget) Acquire(ctx context.Context, instanceID uuid.UUID, capacity, globalCap, requested int) (Grant, error) {
	window := Window(b.clk.Now())
	if requested < 0 || capacity <= 0 || globalCap < 0 {
		return Grant{}, fmt.Errorf("snatch: budget: invalid request (capacity %d, global %d, requested %d)", capacity, globalCap, requested)
	}
	var g Grant
	err := b.st.Tx(ctx, func(q *sqlcgen.Queries) error {
		var txErr error
		g, txErr = acquireTx(ctx, q, instanceID, capacity, globalCap, requested, window)
		return txErr
	})
	if err != nil {
		return Grant{}, fmt.Errorf("snatch: budget: %w", store.MapError(err))
	}
	return g, nil
}

// acquireTx locks the instance bucket, computes the grant, trims it to the global stamina
// and debits both inside the caller's transaction.
func acquireTx(ctx context.Context, q *sqlcgen.Queries, instanceID uuid.UUID, capacity, globalCap, requested int, window time.Time) (Grant, error) {
	if err := q.EnsureBucket(ctx, sqlcgen.EnsureBucketParams{InstanceID: instanceID, WindowStart: window}); err != nil {
		return Grant{}, fmt.Errorf("ensure bucket: %w", err)
	}
	used, err := q.LockBucket(ctx, sqlcgen.LockBucketParams{InstanceID: instanceID, WindowStart: window})
	if err != nil {
		return Grant{}, fmt.Errorf("lock bucket: %w", err)
	}
	g := grant(int(used), capacity, requested, window)
	if globalCap > 0 {
		if g, err = applyGlobal(ctx, q, g, globalCap, window); err != nil {
			return Grant{}, err
		}
	}
	if g.Granted > 0 {
		err = q.SetBucketUsed(ctx, sqlcgen.SetBucketUsedParams{
			InstanceID: instanceID, WindowStart: window, Used: int32(g.Used), // #nosec G115 -- bounded by cap <= 500
		})
		if err != nil {
			return Grant{}, fmt.Errorf("set bucket: %w", err)
		}
	}
	return g, nil
}

// applyGlobal trims an instance grant to the shared global stamina and debits it.
func applyGlobal(ctx context.Context, q *sqlcgen.Queries, g Grant, globalCap int, window time.Time) (Grant, error) {
	if err := q.EnsureGlobalBucket(ctx, window); err != nil {
		return g, fmt.Errorf("ensure global bucket: %w", err)
	}
	used, err := q.LockGlobalBucket(ctx, window)
	if err != nil {
		return g, fmt.Errorf("lock global bucket: %w", err)
	}
	g = trimToGlobal(g, int(used), globalCap)
	if g.Granted > 0 {
		err = q.SetGlobalBucketUsed(ctx, sqlcgen.SetGlobalBucketUsedParams{
			WindowStart: window, Used: int32(int(used) + g.Granted), // #nosec G115 -- bounded by cap <= 5000
		})
		if err != nil {
			return g, fmt.Errorf("set global bucket: %w", err)
		}
	}
	return g, nil
}

// trimToGlobal is the pure part of applyGlobal: the grant never exceeds what the global
// window has left, and Remaining reports the tighter of the two budgets.
func trimToGlobal(g Grant, globalUsed, globalCap int) Grant {
	globalLeft := max(globalCap-globalUsed, 0)
	if g.Granted > globalLeft {
		g.Used -= g.Granted - globalLeft
		g.Granted = globalLeft
	}
	g.Remaining = min(g.Remaining, globalLeft-g.Granted)
	return g
}

// GlobalUsed reports the shared window's consumption without debiting.
func (b *Budget) GlobalUsed(ctx context.Context) (int, time.Time, error) {
	window := Window(b.clk.Now())
	used, err := b.st.Q().GetGlobalBucketUsed(ctx, window)
	if err != nil {
		return 0, window, fmt.Errorf("snatch: global budget used: %w", store.MapError(err))
	}
	return int(used), window.Add(time.Hour), nil
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
		return 0, window, fmt.Errorf("snatch: budget used: %w", store.MapError(err))
	}
	return int(used), window.Add(time.Hour), nil
}
