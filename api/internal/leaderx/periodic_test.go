// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package leaderx_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/leaderx"
	"github.com/lusoris/SnatchArr/api/internal/leaderx/leaderxtest"
)

const (
	periodicInterval = time.Minute
	// tickBuffer is more ticks than any test here produces, so a tick never blocks the loop.
	tickBuffer = 16
	// stopBound bounds a stop that is expected to succeed (HISS-02).
	stopBound = 5 * time.Second
	// windDown is how long the tick in flight takes to end once it is cancelled: long enough
	// that a Stop which does not wait returns first.
	windDown = 50 * time.Millisecond
)

// recordTicks is a tick function that hands each call's context to the test.
func recordTicks() (<-chan context.Context, func(context.Context)) {
	calls := make(chan context.Context, tickBuffer)
	return calls, func(ctx context.Context) { calls <- ctx }
}

func nextTick(t *testing.T, calls <-chan context.Context) context.Context {
	t.Helper()
	select {
	case ctx := <-calls:
		return ctx
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a tick")
		return nil
	}
}

func mustStart(t *testing.T, p *leaderx.Periodic, parent context.Context) {
	t.Helper()
	if err := p.Start(parent); err != nil {
		t.Fatalf("Start() = %v", err)
	}
}

func mustStop(t *testing.T, p *leaderx.Periodic) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), stopBound)
	defer cancel()
	if err := p.Stop(ctx); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
	if p.Running() {
		t.Fatal("the loop still runs after Stop")
	}
}

// Positive: the first tick runs at once, then one per interval, each under a deadline of one
// interval; after Stop no tick follows.
func TestPeriodic_TicksAtOnceAndPerInterval(t *testing.T) {
	t.Parallel()
	clk := clock.NewFake()
	calls, tick := recordTicks()
	p := leaderx.NewPeriodic("job", periodicInterval, clk, tick)
	if p.Running() {
		t.Fatal("a new loop runs before Start")
	}
	mustStart(t, p, t.Context())
	if !p.Running() {
		t.Fatal("the loop does not run after Start")
	}
	first := nextTick(t, calls)
	deadline, ok := first.Deadline()
	if left := time.Until(deadline); !ok || left <= 0 || left > periodicInterval {
		t.Fatalf("tick deadline in %v (set: %v), want within one interval of %v", left, ok, periodicInterval)
	}
	for range 3 {
		clk.Advance(periodicInterval)
		nextTick(t, calls)
	}
	mustStop(t, p)
	clk.Advance(periodicInterval)
	if extra := len(calls); extra != 0 {
		t.Fatalf("%d ticks nobody asked for: want one at the start and one per interval", extra)
	}
}

// Positive: the gate starts a loop again when the replica wins the lease back.
func TestPeriodic_StartsAgainAfterStop(t *testing.T) {
	t.Parallel()
	calls, tick := recordTicks()
	p := leaderx.NewPeriodic("job", periodicInterval, clock.NewFake(), tick)
	for term := range 3 {
		mustStart(t, p, t.Context())
		nextTick(t, calls)
		mustStop(t, p)
		if extra := len(calls); extra != 0 {
			t.Fatalf("term %d: %d extra ticks, want one tick per start", term, extra)
		}
	}
}

// Negative: a second start while the loop runs is refused and leaves the running loop alone.
func TestPeriodic_SecondStartIsRefused(t *testing.T) {
	t.Parallel()
	clk := clock.NewFake()
	calls, tick := recordTicks()
	p := leaderx.NewPeriodic("job", periodicInterval, clk, tick)
	mustStart(t, p, t.Context())
	nextTick(t, calls)
	if err := p.Start(t.Context()); err == nil || !strings.HasPrefix(err.Error(), "job: ") {
		t.Fatalf("second Start() = %v, want an error that names the job", err)
	}
	clk.Advance(periodicInterval)
	nextTick(t, calls)
	mustStop(t, p)
	if extra := len(calls); extra != 0 {
		t.Fatalf("%d extra ticks: the refused start launched a second loop", extra)
	}
}

// Negative: a tick that ignores its context outlives the stop's deadline; Stop reports that
// with the job's name and the context's error.
func TestPeriodic_StopDeadlineExpires(t *testing.T) {
	t.Parallel()
	entered, release := make(chan struct{}), make(chan struct{})
	p := leaderx.NewPeriodic("job", periodicInterval, clock.NewFake(), func(context.Context) {
		close(entered)
		<-release
	})
	t.Cleanup(func() { close(release) })
	mustStart(t, p, t.Context())
	leaderxtest.Await(t, entered, "the tick")
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	err := p.Stop(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || !strings.HasPrefix(err.Error(), "job: stop: ") {
		t.Fatalf("Stop() = %v, want \"job: stop: \" wrapping the deadline error", err)
	}
}

// Boundary: stopping a loop that never started, or stopping twice, is a no-op that does not
// wait for the context.
func TestPeriodic_StopWithoutRunningLoop(t *testing.T) {
	t.Parallel()
	calls, tick := recordTicks()
	p := leaderx.NewPeriodic("job", periodicInterval, clock.NewFake(), tick)
	ended, cancel := context.WithCancel(t.Context())
	cancel()
	if err := p.Stop(ended); err != nil {
		t.Fatalf("Stop() before Start = %v, want nil", err)
	}
	mustStart(t, p, t.Context())
	nextTick(t, calls)
	mustStop(t, p)
	if err := p.Stop(ended); err != nil {
		t.Fatalf("second Stop() = %v, want nil", err)
	}
}

// Boundary: the loop outlives the context it was started with (the gate's is often cancelled
// by the time the loop works), and Stop cancels the tick in flight and waits for it.
func TestPeriodic_StopCancelsTheTickInFlight(t *testing.T) {
	t.Parallel()
	entered, cancelled, left := make(chan struct{}), make(chan struct{}), make(chan struct{})
	p := leaderx.NewPeriodic("job", periodicInterval, clock.NewFake(), func(ctx context.Context) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		time.Sleep(windDown)
		close(left)
	})
	parent, cancel := context.WithCancel(t.Context())
	mustStart(t, p, parent)
	leaderxtest.Await(t, entered, "the tick")
	cancel()
	select {
	case <-cancelled:
		t.Fatal("the tick was cancelled with the start context, want it to run until Stop")
	case <-time.After(20 * time.Millisecond):
	}
	mustStop(t, p)
	select {
	case <-left:
	default:
		t.Fatal("Stop returned before the tick in flight ended")
	}
}
