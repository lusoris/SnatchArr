// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package snatch is the control plane's snatching brain: per-instance rolling budgets,
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

// Span is the rolling window stamina is counted over (#16): in no 60 minutes does an
// instance get more items than its hourly cap, and the global stamina follows the same rule.
const Span = time.Hour

// BucketKeep is how long spent one-minute buckets are kept: one span plus a margin.
const BucketKeep = 2 * time.Hour

// Grant is the outcome of a budget request.
type Grant struct {
	Granted   int
	Used      int
	Cap       int
	Remaining int
	// ResetsAt is when stamina next frees up: when the oldest spend still counted leaves
	// the span, or now when nothing is spent.
	ResetsAt time.Time
}

// Budget debits per-instance stamina in one-minute buckets summed over a rolling Span. The
// cap counts items searched (newtarr's meaning), not HTTP requests, and is enforced
// atomically so N workers share one budget.
type Budget struct {
	st  *store.Store
	clk clock.Clock
}

// NewBudget wires the budget.
func NewBudget(st *store.Store, clk clock.Clock) *Budget {
	return &Budget{st: st, clk: clk}
}

// Minute returns the one-minute bucket a grant at now debits.
func Minute(now time.Time) time.Time {
	return now.UTC().Truncate(time.Minute)
}

// SpanStart returns the oldest bucket a request at now counts. Buckets are a minute wide, so
// a request counts 61 of them: two grants less than 60 minutes apart always see each other,
// and spent stamina comes back 60 to 61 minutes later.
func SpanStart(now time.Time) time.Time {
	return Minute(now).Add(-Span)
}

// FreesAt returns when the stamina spent in the bucket starting at bucket stops counting.
func FreesAt(bucket time.Time) time.Time {
	return bucket.Add(Span + time.Minute)
}

// usage is the stamina counted at one moment.
type usage struct {
	used   int
	oldest time.Time // oldest bucket in the span that holds stamina; unset when used is 0
}

// debit returns the usage after n more items are granted at now.
func (u usage) debit(n int, now time.Time) usage {
	if n <= 0 {
		return u
	}
	if u.used <= 0 {
		u.oldest = Minute(now)
	}
	u.used += n
	return u
}

// resetsAt is when stamina next frees up: when the oldest counted bucket leaves the span,
// or now when nothing is spent.
func (u usage) resetsAt(now time.Time) time.Time {
	if u.used <= 0 {
		return now
	}
	return FreesAt(u.oldest)
}

// Purge drops up to limit instance and up to limit global buckets older than BucketKeep (one
// batch of the retention janitor) and returns how many rows went.
func (b *Budget) Purge(ctx context.Context, limit int32) (int64, error) {
	before := Minute(b.clk.Now()).Add(-BucketKeep)
	n, err := b.st.Q().PurgeBucketsBefore(ctx, sqlcgen.PurgeBucketsBeforeParams{Before: before, Batch: limit})
	if err != nil {
		return 0, fmt.Errorf("snatch: purge buckets: %w", store.MapError(err))
	}
	global, err := b.st.Q().PurgeGlobalBucketsBefore(ctx, sqlcgen.PurgeGlobalBucketsBeforeParams{Before: before, Batch: limit})
	if err != nil {
		return n, fmt.Errorf("snatch: purge global buckets: %w", store.MapError(err))
	}
	return n + global, nil
}

// Acquire grants up to `requested` items without exceeding the instance cap within the
// rolling span nor, when globalCap > 0, the stamina shared by every instance.
func (b *Budget) Acquire(ctx context.Context, instanceID uuid.UUID, capacity, globalCap, requested int) (Grant, error) {
	now := b.clk.Now()
	if requested < 0 || capacity <= 0 || globalCap < 0 {
		return Grant{}, fmt.Errorf("snatch: budget: invalid request (capacity %d, global %d, requested %d)", capacity, globalCap, requested)
	}
	var g Grant
	err := b.st.Tx(ctx, func(q *sqlcgen.Queries) error {
		var txErr error
		g, txErr = acquireTx(ctx, q, instanceID, capacity, globalCap, requested, now)
		return txErr
	})
	if err != nil {
		return Grant{}, fmt.Errorf("snatch: budget: %w", store.MapError(err))
	}
	return g, nil
}

// acquireTx takes the instance lock, sums the span, trims the grant to the global stamina
// and debits the current minute, all inside the caller's transaction.
func acquireTx(ctx context.Context, q *sqlcgen.Queries, instanceID uuid.UUID, capacity, globalCap, requested int, now time.Time) (Grant, error) {
	if err := q.LockInstanceBudget(ctx, instanceID); err != nil {
		return Grant{}, fmt.Errorf("lock instance budget: %w", err)
	}
	row, err := q.BucketUsage(ctx, sqlcgen.BucketUsageParams{InstanceID: instanceID, Since: SpanStart(now)})
	if err != nil {
		return Grant{}, fmt.Errorf("instance usage: %w", err)
	}
	inst := usage{used: int(row.Used), oldest: row.Oldest}
	g := grant(inst, capacity, requested, now)
	if globalCap > 0 {
		if g, err = applyGlobal(ctx, q, g, inst, globalCap, now); err != nil {
			return Grant{}, err
		}
	}
	if g.Granted > 0 {
		addErr := q.AddBucketUsed(ctx, sqlcgen.AddBucketUsedParams{
			InstanceID: instanceID, WindowStart: Minute(now), Used: int32(g.Granted), // #nosec G115 -- bounded by cap <= 500
		})
		if addErr != nil {
			return Grant{}, fmt.Errorf("debit bucket: %w", addErr)
		}
	}
	return g, nil
}

// applyGlobal takes the global lock (always after the instance lock), trims the grant to
// the global stamina and debits it.
func applyGlobal(ctx context.Context, q *sqlcgen.Queries, g Grant, inst usage, globalCap int, now time.Time) (Grant, error) {
	if err := q.LockGlobalBudget(ctx); err != nil {
		return g, fmt.Errorf("lock global budget: %w", err)
	}
	row, err := q.GlobalBucketUsage(ctx, SpanStart(now))
	if err != nil {
		return g, fmt.Errorf("global usage: %w", err)
	}
	g = trimToGlobal(g, inst, usage{used: int(row.Used), oldest: row.Oldest}, globalCap, now)
	if g.Granted > 0 {
		err = q.AddGlobalBucketUsed(ctx, sqlcgen.AddGlobalBucketUsedParams{
			WindowStart: Minute(now), Used: int32(g.Granted), // #nosec G115 -- bounded by cap <= 5000
		})
		if err != nil {
			return g, fmt.Errorf("debit global bucket: %w", err)
		}
	}
	return g, nil
}

// trimToGlobal is the pure part of applyGlobal: the grant never exceeds what the global
// span has left, and Remaining and ResetsAt follow the tighter of the two budgets.
func trimToGlobal(g Grant, inst, global usage, globalCap int, now time.Time) Grant {
	globalLeft := max(globalCap-global.used, 0)
	g.Granted = min(g.Granted, globalLeft)
	after := inst.debit(g.Granted, now)
	g.Used, g.Remaining, g.ResetsAt = after.used, max(g.Cap-after.used, 0), after.resetsAt(now)
	if left := globalLeft - g.Granted; left < g.Remaining {
		g.Remaining, g.ResetsAt = left, global.debit(g.Granted, now).resetsAt(now)
	}
	return g
}

// grant is the pure cap arithmetic for one instance.
func grant(u usage, capacity, requested int, now time.Time) Grant {
	remaining := max(capacity-u.used, 0)
	granted := min(requested, remaining)
	after := u.debit(granted, now)
	return Grant{
		Granted:   granted,
		Used:      after.used,
		Cap:       capacity,
		Remaining: remaining - granted,
		ResetsAt:  after.resetsAt(now),
	}
}

// Used reports the instance's stamina spent within the span, without debiting, and when it
// next frees up: the same arithmetic Acquire enforces.
func (b *Budget) Used(ctx context.Context, instanceID uuid.UUID) (int, time.Time, error) {
	now := b.clk.Now()
	row, err := b.st.Q().BucketUsage(ctx, sqlcgen.BucketUsageParams{InstanceID: instanceID, Since: SpanStart(now)})
	if err != nil {
		return 0, now, fmt.Errorf("snatch: budget used: %w", store.MapError(err))
	}
	u := usage{used: int(row.Used), oldest: row.Oldest}
	return u.used, u.resetsAt(now), nil
}

// GlobalUsed reports the shared stamina spent within the span, without debiting.
func (b *Budget) GlobalUsed(ctx context.Context) (int, time.Time, error) {
	now := b.clk.Now()
	row, err := b.st.Q().GlobalBucketUsage(ctx, SpanStart(now))
	if err != nil {
		return 0, now, fmt.Errorf("snatch: global budget used: %w", store.MapError(err))
	}
	u := usage{used: int(row.Used), oldest: row.Oldest}
	return u.used, u.resetsAt(now), nil
}
