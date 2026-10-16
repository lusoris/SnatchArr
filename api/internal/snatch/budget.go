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

// BudgetItem is one search target asking for stamina (#142).
type BudgetItem struct {
	// Cost is the stamina the target spends when it is searched whole.
	Cost int
	// Atomic marks a target that cannot be split: one command for a whole season, series,
	// artist or author. It is granted its full cost or nothing.
	Atomic bool
}

// grantFrom is what the item gets while left stamina remains: its cost when that fits,
// otherwise what is left for a divisible item and nothing for an atomic one.
func (i BudgetItem) grantFrom(left int) int {
	switch {
	case i.Cost <= left:
		return i.Cost
	case i.Atomic:
		return 0
	default:
		return left
	}
}

// ItemsGrant is the outcome of a budget request that names its items.
type ItemsGrant struct {
	Grant
	// GrantedItems holds the stamina granted to each item, in request order.
	GrantedItems []int
}

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
// cap counts searches the *arr sends to its indexers (one per episode, season, movie, album
// or book, one per artist or author search; #180), not HTTP requests, and is enforced
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
// rolling span nor, when globalCap > 0, the stamina shared by every instance. It is
// AcquireItems for one divisible item.
func (b *Budget) Acquire(ctx context.Context, instanceID uuid.UUID, capacity, globalCap, requested int) (Grant, error) {
	ig, err := b.AcquireItems(ctx, instanceID, capacity, globalCap, []BudgetItem{{Cost: requested}})
	return ig.Grant, err
}

// Quote grants up to `requested` items without debiting the stamina buckets.
func (b *Budget) Quote(ctx context.Context, instanceID uuid.UUID, capacity, globalCap, requested int) (Grant, error) {
	ig, err := b.QuoteItems(ctx, instanceID, capacity, globalCap, []BudgetItem{{Cost: requested}})
	return ig.Grant, err
}

// AcquireItems grants stamina item by item, in order, and debits only what it grants, so a
// target that has to wait costs nothing (#142). An item that fits gets its cost; one that
// does not gets what is left when it is divisible and nothing when it is atomic, and a
// later, smaller item may still fit.
func (b *Budget) AcquireItems(ctx context.Context, instanceID uuid.UUID, capacity, globalCap int, items []BudgetItem) (ItemsGrant, error) {
	return b.acquireItemsInternal(ctx, instanceID, capacity, globalCap, items, false)
}

// QuoteItems computes the items grant without debiting the stamina buckets.
func (b *Budget) QuoteItems(ctx context.Context, instanceID uuid.UUID, capacity, globalCap int, items []BudgetItem) (ItemsGrant, error) {
	return b.acquireItemsInternal(ctx, instanceID, capacity, globalCap, items, true)
}

func (b *Budget) acquireItemsInternal(ctx context.Context, instanceID uuid.UUID, capacity, globalCap int, items []BudgetItem, quote bool) (ItemsGrant, error) {
	now := b.clk.Now()
	if capacity <= 0 || globalCap < 0 {
		return ItemsGrant{}, fmt.Errorf("snatch: budget: invalid request (capacity %d, global %d)", capacity, globalCap)
	}
	for _, item := range items {
		if item.Cost < 0 {
			return ItemsGrant{}, fmt.Errorf("snatch: budget: invalid request (item cost %d)", item.Cost)
		}
	}
	var ig ItemsGrant
	err := b.st.Tx(ctx, func(q *sqlcgen.Queries) error {
		var txErr error
		ig, txErr = acquireTx(ctx, q, instanceID, capacity, globalCap, items, quote, now)
		return txErr
	})
	if err != nil {
		return ItemsGrant{}, fmt.Errorf("snatch: budget: %w", store.MapError(err))
	}
	return ig, nil
}

// acquireTx takes the instance lock and, when a global cap applies, the global lock (always
// after the instance lock), sums both spans, grants the items and debits the current
// minute, all inside the caller's transaction.
func acquireTx(ctx context.Context, q *sqlcgen.Queries, instanceID uuid.UUID, capacity, globalCap int, items []BudgetItem, quote bool, now time.Time) (ItemsGrant, error) {
	if err := q.LockInstanceBudget(ctx, instanceID); err != nil {
		return ItemsGrant{}, fmt.Errorf("lock instance budget: %w", err)
	}
	row, err := q.BucketUsage(ctx, sqlcgen.BucketUsageParams{InstanceID: instanceID, Since: SpanStart(now)})
	if err != nil {
		return ItemsGrant{}, fmt.Errorf("instance usage: %w", err)
	}
	var global usage
	if globalCap > 0 {
		if global, err = lockedGlobalUsage(ctx, q, now); err != nil {
			return ItemsGrant{}, err
		}
	}
	ig := grantItems(usage{used: int(row.Used), oldest: row.Oldest}, global, capacity, globalCap, items, now)
	if ig.Granted > 0 && !quote {
		if err = debit(ctx, q, instanceID, globalCap, ig.Granted, now); err != nil {
			return ItemsGrant{}, err
		}
	}
	return ig, nil
}

// lockedGlobalUsage takes the global lock and sums the shared span.
func lockedGlobalUsage(ctx context.Context, q *sqlcgen.Queries, now time.Time) (usage, error) {
	if err := q.LockGlobalBudget(ctx); err != nil {
		return usage{}, fmt.Errorf("lock global budget: %w", err)
	}
	row, err := q.GlobalBucketUsage(ctx, SpanStart(now))
	if err != nil {
		return usage{}, fmt.Errorf("global usage: %w", err)
	}
	return usage{used: int(row.Used), oldest: row.Oldest}, nil
}

// debit books granted items on the instance's current minute and, when a global cap
// applies, on the shared one.
func debit(ctx context.Context, q *sqlcgen.Queries, instanceID uuid.UUID, globalCap, granted int, now time.Time) error {
	err := q.AddBucketUsed(ctx, sqlcgen.AddBucketUsedParams{
		InstanceID: instanceID, WindowStart: Minute(now), Used: int32(granted), // #nosec G115 -- bounded by cap <= 500
	})
	if err != nil {
		return fmt.Errorf("debit bucket: %w", err)
	}
	if globalCap <= 0 {
		return nil
	}
	err = q.AddGlobalBucketUsed(ctx, sqlcgen.AddGlobalBucketUsedParams{
		WindowStart: Minute(now), Used: int32(granted), // #nosec G115 -- bounded by cap <= 500
	})
	if err != nil {
		return fmt.Errorf("debit global bucket: %w", err)
	}
	return nil
}

// grantItems is the pure cap arithmetic: the items are served in order from the tighter of
// what the instance and, when globalCap > 0, the shared stamina have left. Remaining and
// ResetsAt follow the tighter of the two budgets after the grant.
func grantItems(inst, global usage, capacity, globalCap int, items []BudgetItem, now time.Time) ItemsGrant {
	left := max(capacity-inst.used, 0)
	if globalCap > 0 {
		left = min(left, max(globalCap-global.used, 0))
	}
	grants := make([]int, len(items))
	total := 0
	for i, item := range items {
		grants[i] = item.grantFrom(left)
		left -= grants[i]
		total += grants[i]
	}
	after := inst.debit(total, now)
	g := Grant{Granted: total, Used: after.used, Cap: capacity, Remaining: max(capacity-after.used, 0), ResetsAt: after.resetsAt(now)}
	if globalCap > 0 {
		shared := global.debit(total, now)
		if rest := max(globalCap-shared.used, 0); rest < g.Remaining {
			g.Remaining, g.ResetsAt = rest, shared.resetsAt(now)
		}
	}
	return ItemsGrant{Grant: g, GrantedItems: grants}
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
