// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package snatch

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"

	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

// spendStart is 30 s into a minute, so a grant 59.5 minutes later lands in the next hour's
// first minute while the first spend still counts.
var spendStart = time.Date(2026, 10, 10, 10, 0, 30, 0, time.UTC)

func budgetAt(st *store.Store, now time.Time) *Budget {
	return NewBudget(st, clockwork.NewFakeClockAt(now))
}

// grantConcurrently fires one request of 1 item per budget at once and returns the total
// granted.
func grantConcurrently(t *testing.T, budgets []*Budget, instances []uuid.UUID, capacity, globalCap int) int64 {
	t.Helper()
	var granted atomic.Int64
	var wg sync.WaitGroup
	release := make(chan struct{})
	for i, b := range budgets {
		instanceID := instances[i%len(instances)]
		wg.Go(func() {
			<-release
			g, err := b.Acquire(t.Context(), instanceID, capacity, globalCap, 1)
			if err != nil {
				t.Errorf("Acquire() = %v", err)
				return
			}
			granted.Add(int64(g.Granted))
		})
	}
	close(release)
	wg.Wait()
	return granted.Load()
}

// straddling returns n budgets whose clocks alternate between one second before and one
// second after the minute boundary 59.5 minutes after spendStart.
func straddling(st *store.Store, n int) []*Budget {
	out := make([]*Budget, n)
	for i := range out {
		offset := 59*time.Minute + 29*time.Second // 10:59:59
		if i%2 == 1 {
			offset += 2 * time.Second // 11:00:01
		}
		out[i] = budgetAt(st, spendStart.Add(offset))
	}
	return out
}

// Concurrent grants on either side of a minute boundary never exceed the cap within 60
// minutes; the hourly-caps view reports the numbers the grant enforced; the first spend
// comes back once its bucket leaves the span.
func TestRollingCapAcrossBoundary(t *testing.T) {
	t.Parallel()
	const capacity = 10
	st := storetest.New(t)
	instanceID := newInstance(t, st)
	if g, err := budgetAt(st, spendStart).Acquire(t.Context(), instanceID, capacity, 0, 4); err != nil || g.Granted != 4 {
		t.Fatalf("first Acquire() = %+v, %v; want 4 granted", g, err)
	}
	if got := grantConcurrently(t, straddling(st, 12), []uuid.UUID{instanceID}, capacity, 0); got != capacity-4 {
		t.Fatalf("12 concurrent requests across the boundary granted %d, want %d", got, capacity-4)
	}
	used, resets, err := budgetAt(st, spendStart.Add(59*time.Minute+31*time.Second)).Used(t.Context(), instanceID)
	if want := FreesAt(Minute(spendStart)); err != nil || used != capacity || !resets.Equal(want) {
		t.Fatalf("Used() = %d, %v, %v; want %d, %v", used, resets, err, capacity, want)
	}
	later := budgetAt(st, spendStart.Add(60*time.Minute+30*time.Second)) // 11:01:00: the 10:00 bucket left the span
	if g, err := later.Acquire(t.Context(), instanceID, capacity, 0, capacity); err != nil || g.Granted != 4 {
		t.Fatalf("Acquire() after the first spend expired = %+v, %v; want 4 granted", g, err)
	}
}

// The global stamina follows the same rule across instances. Each request uses its own
// instance, so only the global lock stands between them.
func TestRollingGlobalCapAcrossBoundary(t *testing.T) {
	t.Parallel()
	const capacity, globalCap, requests = 10, 5, 10
	st := storetest.New(t)
	instances := make([]uuid.UUID, requests)
	for i := range instances {
		instances[i] = newInstance(t, st)
	}
	if g, err := budgetAt(st, spendStart).Acquire(t.Context(), instances[0], capacity, globalCap, 2); err != nil || g.Granted != 2 {
		t.Fatalf("first Acquire() = %+v, %v; want 2 granted", g, err)
	}
	if got := grantConcurrently(t, straddling(st, requests), instances, capacity, globalCap); got != globalCap-2 {
		t.Fatalf("%d concurrent requests on %d instances granted %d, want %d", requests, requests, got, globalCap-2)
	}
	used, _, err := budgetAt(st, spendStart.Add(59*time.Minute+31*time.Second)).GlobalUsed(t.Context())
	if err != nil || used != globalCap {
		t.Fatalf("GlobalUsed() = %d, %v; want %d", used, err, globalCap)
	}
}

// newInstance creates an instance with storetest at spendStart.
func newInstance(t *testing.T, st *store.Store) uuid.UUID {
	t.Helper()
	return storetest.Instance(t, st, spendStart)
}
