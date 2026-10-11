// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package snatch

import (
	"maps"
	"slices"
	"testing"
	"testing/quick"
	"time"
)

// t0 is a minute-aligned instant the arithmetic tests count from.
var t0 = time.Date(2026, 9, 21, 14, 0, 0, 0, time.UTC)

func TestGrant(t *testing.T) {
	t.Parallel()
	now := t0.Add(30 * time.Second)
	spent := usage{oldest: t0.Add(-10 * time.Minute)}
	tests := []struct {
		name          string
		used, cap     int
		requested     int
		wantGranted   int
		wantUsed      int
		wantRemaining int
		wantResets    time.Time
	}{
		{"positive fresh span", 0, 20, 5, 5, 5, 15, FreesAt(t0)},
		{"positive partial", 18, 20, 5, 2, 20, 0, FreesAt(spent.oldest)},
		{"boundary exactly at cap", 20, 20, 1, 0, 20, 0, FreesAt(spent.oldest)},
		{"boundary over cap (cap lowered mid-span)", 25, 20, 3, 0, 25, 0, FreesAt(spent.oldest)},
		{"boundary zero request on nothing spent", 0, 20, 0, 0, 0, 20, now},
		{"negative-ish huge request", 0, 20, 1000, 20, 20, 0, FreesAt(t0)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u := spent
			u.used = tc.used
			if tc.used == 0 {
				u = usage{}
			}
			g := grant(u, tc.cap, tc.requested, now)
			if g.Granted != tc.wantGranted || g.Used != tc.wantUsed || g.Remaining != tc.wantRemaining {
				t.Fatalf("grant = %+v", g)
			}
			if !g.ResetsAt.Equal(tc.wantResets) {
				t.Fatalf("resets at %v, want %v", g.ResetsAt, tc.wantResets)
			}
		})
	}
}

func TestMinuteAndSpan(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 21, 16, 59, 59, 0, time.FixedZone("plus2", 2*3600))
	if got, want := Minute(now), time.Date(2026, 9, 21, 14, 59, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("Minute = %v, want %v", got, want)
	}
	if got, want := SpanStart(now), time.Date(2026, 9, 21, 13, 59, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("SpanStart = %v, want %v", got, want)
	}
}

// Boundary at exactly 60 minutes: a spend still counts 60 minutes later (the span is
// conservative by up to one minute) and is gone 61 minutes later.
func TestSpanBoundary(t *testing.T) {
	t.Parallel()
	ledger := newLedger(20)
	if g := ledger.request(t0, 20); g != 20 {
		t.Fatalf("first request granted %d, want 20", g)
	}
	for _, tc := range []struct {
		after time.Duration
		want  int
	}{
		{59*time.Minute + 59*time.Second, 0},
		{60 * time.Minute, 0},
		{60*time.Minute + 59*time.Second, 0},
		{61 * time.Minute, 20},
	} {
		probe := ledger.clone()
		if g := probe.request(t0.Add(tc.after), 20); g != tc.want {
			t.Errorf("request %v after a full spend granted %d, want %d", tc.after, g, tc.want)
		}
	}
}

// grant is the arithmetic for a request without items: one divisible item, no global cap.
func grant(u usage, capacity, requested int, now time.Time) Grant {
	return grantItems(u, usage{}, capacity, 0, []BudgetItem{{Cost: requested}}, now).Grant
}

func TestGrantUnderGlobalCap(t *testing.T) {
	t.Parallel()
	now := t0.Add(30 * time.Second)
	inst := usage{used: 5, oldest: t0.Add(-5 * time.Minute)} // instance: 15 of 20 left
	globalOldest := t0.Add(-20 * time.Minute)
	cases := []struct {
		name                     string
		globalUsed, globalCap    int
		granted, used, remaining int
		resets                   time.Time
	}{
		{"global has room", 0, 100, 10, 15, 5, FreesAt(inst.oldest)},
		{"global tighter than instance", 95, 100, 5, 10, 0, FreesAt(globalOldest)},
		{"global exhausted", 100, 100, 0, 5, 0, FreesAt(globalOldest)},
		{"global over its cap (cap lowered mid-span)", 120, 100, 0, 5, 0, FreesAt(globalOldest)},
		{"global remaining is the binding one", 90, 100, 10, 15, 0, FreesAt(globalOldest)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			global := usage{used: tc.globalUsed, oldest: globalOldest}
			if tc.globalUsed == 0 {
				global = usage{}
			}
			out := grantItems(inst, global, 20, tc.globalCap, []BudgetItem{{Cost: 10}}, now)
			if out.Granted != tc.granted || out.Used != tc.used || out.Remaining != tc.remaining || !out.ResetsAt.Equal(tc.resets) {
				t.Fatalf("got granted=%d used=%d remaining=%d resets=%v", out.Granted, out.Used, out.Remaining, out.ResetsAt)
			}
		})
	}
}

// Per-item grants (#142): positive, negative and boundary, instance cap 10 with 4 spent
// (6 left) unless the case says otherwise.
func TestGrantItems(t *testing.T) {
	t.Parallel()
	now := t0.Add(30 * time.Second)
	whole := func(cost int) BudgetItem { return BudgetItem{Cost: cost, Atomic: true} }
	part := func(cost int) BudgetItem { return BudgetItem{Cost: cost} }
	cases := []struct {
		name                  string
		globalUsed, globalCap int
		items                 []BudgetItem
		want                  []int
		used, remaining       int
	}{
		{"positive all fit", 0, 0, []BudgetItem{whole(2), part(3)}, []int{2, 3}, 9, 1},
		{"positive divisible takes what is left", 0, 0, []BudgetItem{part(9)}, []int{6}, 10, 0},
		{"negative atomic that does not fit costs nothing", 0, 0, []BudgetItem{whole(7)}, []int{0}, 4, 6},
		{"negative every atomic too big", 0, 0, []BudgetItem{whole(7), whole(8)}, []int{0, 0}, 4, 6},
		{"positive smaller item after a waiting atomic one", 0, 0, []BudgetItem{whole(7), whole(2), part(9)}, []int{0, 2, 4}, 10, 0},
		{"negative nothing left after a divisible item", 0, 0, []BudgetItem{part(9), whole(1), part(1)}, []int{6, 0, 0}, 10, 0},
		{"boundary atomic exactly what is left", 0, 0, []BudgetItem{whole(6)}, []int{6}, 10, 0},
		{"boundary atomic one over what is left", 0, 0, []BudgetItem{whole(5), whole(2)}, []int{5, 0}, 9, 1},
		{"boundary no items", 0, 0, nil, []int{}, 4, 6},
		{"boundary zero cost", 0, 0, []BudgetItem{whole(0), part(0)}, []int{0, 0}, 4, 6},
		{"negative atomic fits the instance but not the global", 47, 50, []BudgetItem{whole(4), part(5)}, []int{0, 3}, 7, 0},
		{"boundary atomic exactly the global rest", 47, 50, []BudgetItem{whole(3), whole(1)}, []int{3, 0}, 7, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			inst := usage{used: 4, oldest: t0.Add(-5 * time.Minute)}
			global := usage{used: tc.globalUsed, oldest: t0.Add(-20 * time.Minute)}
			out := grantItems(inst, global, 10, tc.globalCap, tc.items, now)
			if !slices.Equal(out.GrantedItems, tc.want) {
				t.Fatalf("granted items = %v, want %v", out.GrantedItems, tc.want)
			}
			if out.Used != tc.used || out.Remaining != tc.remaining || out.Granted != tc.used-4 {
				t.Fatalf("got granted=%d used=%d remaining=%d, want used=%d remaining=%d", out.Granted, out.Used, out.Remaining, tc.used, tc.remaining)
			}
		})
	}
}

// budgetCase is one generated request: usage and caps of both budgets and the items.
type budgetCase struct {
	instUsed, capacity    int
	globalUsed, globalCap int
	items                 []BudgetItem
}

// newBudgetCase folds raw random values into a small case; globalCap 0 means no global cap.
func newBudgetCase(instUsed, capacity, globalUsed, globalCap uint8, costs []uint8, atomicMask uint64) budgetCase {
	c := budgetCase{
		instUsed: int(instUsed % 40), capacity: int(capacity%40) + 1,
		globalUsed: int(globalUsed % 60), globalCap: int(globalCap % 60),
	}
	for i, cost := range costs {
		c.items = append(c.items, BudgetItem{Cost: int(cost % 12), Atomic: atomicMask&(1<<(i%64)) != 0})
	}
	return c
}

// left is the stamina the case has before any item is served.
func (c budgetCase) left() int {
	left := max(c.capacity-c.instUsed, 0)
	if c.globalCap > 0 {
		left = min(left, max(c.globalCap-c.globalUsed, 0))
	}
	return left
}

// holds states the grant rule item by item, without the code under test: an item that fits
// gets its cost, a divisible one that does not gets everything left, an atomic one that
// does not gets nothing; the total is the sum and is all that is debited.
func (c budgetCase) holds(out ItemsGrant) bool {
	if len(out.GrantedItems) != len(c.items) {
		return false
	}
	left, sum := c.left(), 0
	for i, item := range c.items {
		got := out.GrantedItems[i]
		fits := item.Cost <= left
		switch {
		case fits && got != item.Cost,
			!fits && item.Atomic && got != 0,
			!fits && !item.Atomic && got != left:
			return false
		}
		left -= got
		sum += got
	}
	return left >= 0 && out.Granted == sum && out.Used == c.instUsed+sum && out.Remaining == left
}

// Property: for any usage, caps and items, every grant follows the rule in order, an atomic
// item is never granted in part, and nothing beyond the sum of the grants is debited.
func TestGrantItemsProperty(t *testing.T) {
	t.Parallel()
	now := t0.Add(30 * time.Second)
	prop := func(instUsed, capacity, globalUsed, globalCap uint8, costs []uint8, atomicMask uint64) bool {
		c := newBudgetCase(instUsed, capacity, globalUsed, globalCap, costs, atomicMask)
		inst := usage{used: c.instUsed, oldest: t0.Add(-5 * time.Minute)}
		global := usage{used: c.globalUsed, oldest: t0.Add(-20 * time.Minute)}
		return c.holds(grantItems(inst, global, c.capacity, c.globalCap, c.items, now))
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 2000}); err != nil {
		t.Fatal(err)
	}
}

// ledger replays the grant arithmetic in memory over one-minute buckets, the way the
// queries do in Postgres.
type ledger struct {
	capacity int
	buckets  map[time.Time]int
	grants   []spend
}

type spend struct {
	at time.Time
	n  int
}

func newLedger(capacity int) *ledger {
	return &ledger{capacity: capacity, buckets: map[time.Time]int{}}
}

func (l *ledger) clone() *ledger {
	c := newLedger(l.capacity)
	maps.Copy(c.buckets, l.buckets)
	c.grants = append(c.grants, l.grants...)
	return c
}

func (l *ledger) request(now time.Time, n int) int {
	var u usage
	for bucket, used := range l.buckets {
		if !bucket.Before(SpanStart(now)) && used > 0 {
			if u.used == 0 || bucket.Before(u.oldest) {
				u.oldest = bucket
			}
			u.used += used
		}
	}
	g := grant(u, l.capacity, n, now)
	l.buckets[Minute(now)] += g.Granted
	l.grants = append(l.grants, spend{at: now, n: g.Granted})
	return g.Granted
}

// maxInAnySpan is the most granted within any 60 minutes: the busiest span starts at a grant.
func (l *ledger) maxInAnySpan() int {
	most := 0
	for _, start := range l.grants {
		sum := 0
		for _, s := range l.grants {
			if !s.at.Before(start.at) && s.at.Sub(start.at) < Span {
				sum += s.n
			}
		}
		most = max(most, sum)
	}
	return most
}

// Property: whatever the request pattern, no 60-minute span grants more than the cap
// (negative), and after 61 quiet minutes the full cap is available again (positive).
func TestSpanNeverExceedsCap(t *testing.T) {
	t.Parallel()
	const capacity = 7
	// Each step packs the gap to the previous request (up to 30 min) and its size (0 to 9).
	prop := func(steps []uint32) bool {
		l := newLedger(capacity)
		now := t0
		for _, step := range steps {
			now = now.Add(time.Duration((step>>8)%1800) * time.Second)
			l.request(now, int(step&0xff)%10)
		}
		if l.maxInAnySpan() > capacity {
			return false
		}
		return l.request(now.Add(Span+time.Minute), capacity) == capacity
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 300}); err != nil {
		t.Fatal(err)
	}
}
