// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package leaderx_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/leader"

	"github.com/lusoris/SnatchArr/api/internal/leaderx"
	"github.com/lusoris/SnatchArr/api/internal/leaderx/leaderxtest"
)

type fakeLoop struct {
	mu       sync.Mutex
	starts   int
	stops    int
	startErr error
	started  chan struct{}
}

func newFakeLoop(startErr error) *fakeLoop {
	return &fakeLoop{startErr: startErr, started: make(chan struct{}, 1)}
}

func (f *fakeLoop) Start(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts++
	select {
	case f.started <- struct{}{}:
	default:
	}
	return f.startErr
}

func (f *fakeLoop) Stop(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	return nil
}

func (f *fakeLoop) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.starts, f.stops
}

func quiet() *slog.Logger { return slog.New(slog.DiscardHandler) }

// Positive: without an elector the loops run from start until the context ends.
func TestRun_NoElector(t *testing.T) {
	t.Parallel()
	loop := newFakeLoop(nil)
	g := leaderx.New(clock.NewFake(), quiet(), loop)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := g.Run(ctx, nil, 0); err != nil {
			t.Errorf("Run: %v", err)
		}
	}()
	leaderxtest.Await(t, loop.started, "loop start")
	if !g.Running() {
		t.Fatal("gate should be running")
	}
	cancel()
	leaderxtest.Await(t, done, "Run to return")
	if starts, stops := loop.counts(); starts != 1 || stops != 1 {
		t.Fatalf("starts=%d stops=%d, want 1/1", starts, stops)
	}
	if g.Running() {
		t.Fatal("gate should have stopped")
	}
}

// Negative: a loop that fails to start is skipped, never stopped, and does not block others.
func TestRun_LoopStartError(t *testing.T) {
	t.Parallel()
	broken := newFakeLoop(errors.New("boom"))
	fine := newFakeLoop(nil)
	g := leaderx.New(clock.NewFake(), quiet(), broken, fine)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = g.Run(ctx, nil, 0)
	}()
	leaderxtest.Await(t, fine.started, "second loop start")
	cancel()
	leaderxtest.Await(t, done, "Run to return")
	if _, stops := broken.counts(); stops != 0 {
		t.Fatalf("broken loop stopped %d times, want 0", stops)
	}
	if starts, stops := fine.counts(); starts != 1 || stops != 1 {
		t.Fatalf("fine loop starts=%d stops=%d, want 1/1", starts, stops)
	}
}

// Boundary: with an elector the loops follow the lease exactly once per term.
func TestRun_FollowsLease(t *testing.T) {
	t.Parallel()
	loop := newFakeLoop(nil)
	g := leaderx.New(clock.NewFake(), quiet(), loop)
	elect := func(ctx context.Context, cb leader.Callbacks) error {
		cb.OnNewLeader("me")
		cb.OnStartedLeading(ctx)
		<-ctx.Done()
		cb.OnStoppedLeading()
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := g.Run(ctx, elect, time.Second); err != nil {
			t.Errorf("Run: %v", err)
		}
	}()
	leaderxtest.Await(t, loop.started, "leadership")
	cancel()
	leaderxtest.Await(t, done, "Run to return")
	if starts, stops := loop.counts(); starts != 1 || stops != 1 {
		t.Fatalf("starts=%d stops=%d, want 1/1 (stopAll must be idempotent)", starts, stops)
	}
}

// Boundary: a failed election is retried after the retry interval, never in a tight loop.
func TestRun_RetriesAfterElectionError(t *testing.T) {
	t.Parallel()
	clk := clock.NewFake()
	loop := newFakeLoop(nil)
	g := leaderx.New(clk, quiet(), loop)
	attempts := make(chan int, 4)
	n := 0
	elect := func(ctx context.Context, _ leader.Callbacks) error {
		n++
		attempts <- n
		if n == 1 {
			return errors.New("pg down")
		}
		<-ctx.Done()
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = g.Run(ctx, elect, time.Second)
	}()
	if got := <-attempts; got != 1 {
		t.Fatalf("first attempt = %d", got)
	}
	if err := clk.BlockUntilContext(ctx, 1); err != nil {
		t.Fatalf("BlockUntilContext: %v", err)
	}
	clk.Advance(time.Second)
	if got := <-attempts; got != 2 {
		t.Fatalf("second attempt = %d", got)
	}
	cancel()
	leaderxtest.Await(t, done, "Run to return")
	if starts, _ := loop.counts(); starts != 0 {
		t.Fatalf("loops started %d times without leadership", starts)
	}
}

// lossProbe is a Periodic whose tick hands its context to the test and holds it until it is
// cancelled, so the test sees what a lost lease does to a loop in flight.
type lossProbe struct {
	loop  *leaderx.Periodic
	ticks <-chan context.Context
	left  chan struct{}
}

func newLossProbe() *lossProbe {
	calls := make(chan context.Context, tickBuffer)
	probe := &lossProbe{ticks: calls, left: make(chan struct{}, tickBuffer)}
	probe.loop = leaderx.NewPeriodic("job", periodicInterval, clock.NewFake(), &observed{}, func(ctx context.Context) error {
		calls <- ctx
		<-ctx.Done()
		probe.left <- struct{}{}
		return ctx.Err()
	})
	return probe
}

// Positive, negative, boundary: losing the lease cancels the context of the loop in flight
// and waits for it, even while the lease context itself is still live; a loss without a term
// is a no-op; a new term runs the loop again under a fresh context.
func TestCallbacks_LossCancelsTheLoopContext(t *testing.T) {
	t.Parallel()
	probe := newLossProbe()
	g := leaderx.New(clock.NewFake(), quiet(), probe.loop)
	cb := g.Callbacks(t.Context())

	// Negative: a loss before any term stops nothing.
	cb.OnStoppedLeading()
	if g.Running() || probe.loop.Running() {
		t.Fatal("a loss without a term started the loops")
	}

	lease, cancelLease := context.WithCancel(t.Context())
	defer cancelLease()
	cb.OnStartedLeading(lease)
	first := nextTick(t, probe.ticks)
	cb.OnStoppedLeading()
	select {
	case <-first.Done():
	default:
		t.Fatal("the loop context is still live after the lease was lost")
	}
	select {
	case <-probe.left:
	default:
		t.Fatal("OnStoppedLeading returned before the loop in flight ended")
	}
	if g.Running() || probe.loop.Running() {
		t.Fatalf("gate running=%v loop running=%v after the loss, want both stopped", g.Running(), probe.loop.Running())
	}

	// Boundary: the next term starts the loop again under a context that is live.
	cb.OnStartedLeading(lease)
	second := nextTick(t, probe.ticks)
	if second.Err() != nil {
		t.Fatalf("the new term's loop context is already done: %v", second.Err())
	}
	cb.OnStoppedLeading()
	if second.Err() == nil {
		t.Fatal("the second term's loop context outlived its loss")
	}
}
