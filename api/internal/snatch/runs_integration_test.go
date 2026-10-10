// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package snatch

import (
	"errors"
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
