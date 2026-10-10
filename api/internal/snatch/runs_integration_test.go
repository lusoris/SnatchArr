// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package snatch

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

func newRunsFixture(t *testing.T) (*Runs, *clockwork.FakeClock, uuid.UUID) {
	t.Helper()
	st := storetest.New(t)
	clk := clock.NewFake()
	return NewRuns(st, clk, id.New()), clk, storetest.Instance(t, st, clk.Now())
}

func enqueueRun(t *testing.T, runs *Runs, instanceID uuid.UUID) domain.Run {
	t.Helper()
	run, ok, err := runs.Enqueue(t.Context(), instanceID, domain.SnatchMissing)
	if err != nil || !ok {
		t.Fatalf("Enqueue() = %v, %v", ok, err)
	}
	return run
}

func mustLease(t *testing.T, runs *Runs, worker string, want uuid.UUID) {
	t.Helper()
	got, err := runs.Lease(t.Context(), worker)
	if err != nil || got.ID != want {
		t.Fatalf("Lease(%s) = %v, %v; want run %s", worker, got.ID, err, want)
	}
}

// A run whose lease expires MaxLeases times ends failed and is never leased again.
func TestLeaseExhaustedRunEndsFailed(t *testing.T) {
	t.Parallel()
	runs, clk, instanceID := newRunsFixture(t)
	ctx := t.Context()
	run := enqueueRun(t, runs, instanceID)
	for lease := 1; lease <= MaxLeases; lease++ {
		mustLease(t, runs, "worker", run.ID)
		clk.Advance(LeaseTTL + time.Second)
		if lease < MaxLeases {
			if failed, err := runs.FailExhausted(ctx); err != nil || len(failed) != 0 {
				t.Fatalf("FailExhausted() after lease %d = %d run(s), %v; want none", lease, len(failed), err)
			}
		}
	}
	if _, err := runs.Lease(ctx, "other"); !errors.Is(err, ErrNoRun) {
		t.Fatalf("Lease() after %d expiries = %v, want ErrNoRun", MaxLeases, err)
	}
	failed, err := runs.FailExhausted(ctx)
	if err != nil || len(failed) != 1 || failed[0].ID != run.ID {
		t.Fatalf("FailExhausted() = %v, %v; want run %s", failed, err, run.ID)
	}
	got, err := runs.Get(ctx, run.ID)
	if err != nil || got.Status != domain.RunFailed || got.Error != ExhaustedReason {
		t.Fatalf("Get() = status %q error %q, %v; want failed %q", got.Status, got.Error, err, ExhaustedReason)
	}
	if again, err := runs.FailExhausted(ctx); err != nil || len(again) != 0 {
		t.Fatalf("second FailExhausted() = %d run(s), %v; want none", len(again), err)
	}
	if _, err := runs.Lease(ctx, "other"); !errors.Is(err, ErrNoRun) {
		t.Fatalf("Lease() of a failed run = %v, want ErrNoRun", err)
	}
}

// Below the limit an expired lease is handed to the next worker.
func TestExpiredLeaseWithinLimitIsLeasedAgain(t *testing.T) {
	t.Parallel()
	runs, clk, instanceID := newRunsFixture(t)
	run := enqueueRun(t, runs, instanceID)
	mustLease(t, runs, "first", run.ID)
	clk.Advance(LeaseTTL + time.Second)
	mustLease(t, runs, "second", run.ID)
}

// A run at the limit whose lease has not expired yet is neither failed nor re-leased.
func TestLiveLeaseAtLimitIsKept(t *testing.T) {
	t.Parallel()
	runs, clk, instanceID := newRunsFixture(t)
	ctx := t.Context()
	run := enqueueRun(t, runs, instanceID)
	for lease := 1; lease <= MaxLeases; lease++ {
		mustLease(t, runs, "worker", run.ID)
		if lease < MaxLeases {
			clk.Advance(LeaseTTL + time.Second)
		}
	}
	clk.Advance(LeaseTTL - time.Second)
	if failed, err := runs.FailExhausted(ctx); err != nil || len(failed) != 0 {
		t.Fatalf("FailExhausted() before expiry = %d run(s), %v; want none", len(failed), err)
	}
	got, err := runs.Get(ctx, run.ID)
	if err != nil || got.Status != domain.RunLeased {
		t.Fatalf("Get() = %q, %v; want leased", got.Status, err)
	}
}

// Concurrent workers never lease the same run (FOR UPDATE SKIP LOCKED): every queued run goes
// to exactly one worker, the surplus workers get ErrNoRun, and a live lease is not handed out
// again.
func TestConcurrentLeasesNeverShareARun(t *testing.T) {
	t.Parallel()
	const instances, workers = 4, 16
	st := storetest.New(t)
	clk := clock.NewFake()
	runs := NewRuns(st, clk, id.New())
	queued := map[uuid.UUID]bool{}
	for range instances {
		instanceID := storetest.Instance(t, st, clk.Now())
		for _, kind := range []domain.SnatchKind{domain.SnatchMissing, domain.SnatchUpgrade} {
			run, ok, err := runs.Enqueue(t.Context(), instanceID, kind)
			if err != nil || !ok {
				t.Fatalf("Enqueue() = %v, %v", ok, err)
			}
			queued[run.ID] = true
		}
	}
	leased, empty := leaseConcurrently(t, runs, workers)
	if len(leased) != len(queued) || empty != workers-len(queued) {
		t.Fatalf("%d runs leased, %d workers empty-handed; want %d and %d", len(leased), empty, len(queued), workers-len(queued))
	}
	for runID, n := range leased {
		if n != 1 || !queued[runID] {
			t.Fatalf("run %s leased %d times (queued: %v)", runID, n, queued[runID])
		}
	}
	if _, err := runs.Lease(t.Context(), "late"); !errors.Is(err, ErrNoRun) {
		t.Fatalf("Lease() while every lease is live = %v, want ErrNoRun", err)
	}
}

// leaseConcurrently starts n Lease calls at once and counts how often each run was leased
// and how many calls got ErrNoRun.
func leaseConcurrently(t *testing.T, runs *Runs, n int) (map[uuid.UUID]int, int) {
	t.Helper()
	var mu sync.Mutex
	leased, empty := map[uuid.UUID]int{}, 0
	var wg sync.WaitGroup
	release := make(chan struct{})
	for w := range n {
		wg.Go(func() {
			<-release
			run, err := runs.Lease(t.Context(), fmt.Sprintf("worker-%d", w))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case errors.Is(err, ErrNoRun):
				empty++
			case err != nil:
				t.Errorf("Lease() = %v", err)
			default:
				leased[run.ID]++
			}
		})
	}
	close(release)
	wg.Wait()
	return leased, empty
}
