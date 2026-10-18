// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package leaderx_test

import (
	"context"
	"fmt"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jonboulle/clockwork"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/leader"
	pgleader "github.com/golusoris/golusoris/leader/pg"

	"github.com/lusoris/SnatchArr/api/internal/leaderx"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

const defaultTestRetry = 200 * time.Millisecond

// electionBound covers the first election, which also opens the pool connections; a loaded
// race run (the pre-push gate runs every package at once) needs seconds, not one retry.
const electionBound = 10 * time.Second

// failoverBound caps a takeover or a loop stop at failoverRetries retry periods: ten in the plain
// build, fifty under the race detector. The measured takeover is logged; the bound only has to
// separate "bounded failover" from "no failover" under load.
const failoverBound = failoverRetries * defaultTestRetry

// maxSamples bounds the sampler loop: 2000 ticks of 10 ms = 20 s, far above every test bound.
const maxSamples = 2000

func openTestPool(t *testing.T, base *pgxpool.Pool) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	cfg := base.Config().Copy()
	cfg.AfterRelease = func(*pgx.Conn) bool {
		return false
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open test pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func pollUntil(t *testing.T, clk clock.Clock, check func() bool, bound time.Duration, what string) time.Duration {
	t.Helper()
	deadline := clk.Now().Add(bound)
	start := clk.Now()
	for clk.Now().Before(deadline) {
		if check() {
			return clk.Since(start)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if check() {
		return clk.Since(start)
	}
	t.Fatalf("timed out waiting for %s after %v", what, bound)
	return 0
}

func makeElect(pool *pgxpool.Pool, lockName, identity string, clk clock.Clock) leaderx.Elect {
	opts := pgleader.Options{
		Enabled:  true,
		Name:     lockName,
		Identity: identity,
		PG:       pgleader.BackendOptions{Retry: defaultTestRetry},
	}
	return func(ctx context.Context, cb leader.Callbacks) error {
		return pgleader.Run(ctx, pool, opts, clk, cb)
	}
}

func makeElectPair(poolA, poolB *pgxpool.Pool, nameA, nameB string, clk clock.Clock) (leaderx.Elect, leaderx.Elect) {
	return makeElect(poolA, nameA, "replica-a", clk),
		makeElect(poolB, nameB, "replica-b", clk)
}

func startGateRunner(t *testing.T, g *leaderx.Gate, elect leaderx.Elect) context.CancelFunc {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := g.Run(ctx, elect, defaultTestRetry); err != nil {
			t.Errorf("gate run: %v", err)
		}
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return cancel
}

type gateSample struct {
	at       time.Time
	runningA bool
	runningB bool
}

type gateSampler struct {
	stop chan struct{}
	done chan struct{}
	mu   sync.Mutex
	data []gateSample
}

func startSampler(clk clock.Clock, gA, gB *leaderx.Gate) *gateSampler {
	s := &gateSampler{
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for range maxSamples {
			select {
			case <-s.stop:
				return
			case <-ticker.C:
				s.mu.Lock()
				s.data = append(s.data, gateSample{
					at:       clk.Now(),
					runningA: gA.Running(),
					runningB: gB.Running(),
				})
				s.mu.Unlock()
			}
		}
	}()
	return s
}

func (s *gateSampler) stopSampling() []gateSample {
	close(s.stop)
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.data)
}

func checkMutualLoopCounts(t *testing.T, runA bool, loopA, loopB *fakeLoop) {
	t.Helper()
	sA, stopA := loopA.counts()
	sB, stopB := loopB.counts()
	if runA {
		if sA != 1 || stopA != 0 || sB != 0 || stopB != 0 {
			t.Fatalf("counts: gateA=%d/%d, gateB=%d/%d, want 1/0 and 0/0", sA, stopA, sB, stopB)
		}
		return
	}
	if sB != 1 || stopB != 0 || sA != 0 || stopA != 0 {
		t.Fatalf("counts: gateB=%d/%d, gateA=%d/%d, want 1/0 and 0/0", sB, stopB, sA, stopA)
	}
}

// Positive: exactly one gate runs its loops after both start; both are healthy.
func TestLeaderFailover_MutualExclusionAndHealth(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	poolA := openTestPool(t, st.Pool())
	poolB := openTestPool(t, poolA)

	clk := clockwork.NewRealClock()
	lockName := fmt.Sprintf("lock-%d-%s", os.Getpid(), t.Name())

	loopA := newFakeLoop(nil)
	gateA := leaderx.New(clk, quiet(), loopA)
	loopB := newFakeLoop(nil)
	gateB := leaderx.New(clk, quiet(), loopB)

	electA, electB := makeElectPair(poolA, poolB, lockName, lockName, clk)
	startGateRunner(t, gateA, electA)
	startGateRunner(t, gateB, electB)

	pollUntil(t, clk, func() bool {
		return gateA.Running() || gateB.Running()
	}, electionBound, "one gate to become leader")

	time.Sleep(50 * time.Millisecond)

	runA, runB := gateA.Running(), gateB.Running()
	if runA == runB {
		t.Fatalf("running mismatch: gateA=%v, gateB=%v, want exactly one", runA, runB)
	}
	checkMutualLoopCounts(t, runA, loopA, loopB)

	if err := gateA.Check("gateA").Fn(t.Context()); err != nil {
		t.Fatalf("gateA Check: %v", err)
	}
	if err := gateB.Check("gateB").Fn(t.Context()); err != nil {
		t.Fatalf("gateB Check: %v", err)
	}
}

// Positive: canceling leader Run ctx stops leader loops; follower takes over within bound.
func TestLeaderFailover_CleanShutdown(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	poolA := openTestPool(t, st.Pool())
	poolB := openTestPool(t, poolA)

	clk := clockwork.NewRealClock()
	lockName := fmt.Sprintf("lock-%d-%s", os.Getpid(), t.Name())

	loopA, loopB := newFakeLoop(nil), newFakeLoop(nil)
	gateA, gateB := leaderx.New(clk, quiet(), loopA), leaderx.New(clk, quiet(), loopB)

	electA, electB := makeElectPair(poolA, poolB, lockName, lockName, clk)
	cancelA := startGateRunner(t, gateA, electA)
	cancelB := startGateRunner(t, gateB, electB)

	pollUntil(t, clk, func() bool {
		return gateA.Running() || gateB.Running()
	}, electionBound, "initial leader election")

	leaderGate, followerGate := gateA, gateB
	leaderLoop, followerLoop := loopA, loopB
	cancelLeader := cancelA
	if gateB.Running() {
		leaderGate, followerGate = gateB, gateA
		leaderLoop, followerLoop = loopB, loopA
		cancelLeader = cancelB
	}

	cancelLeader()

	pollUntil(t, clk, func() bool {
		starts, stops := leaderLoop.counts()
		return starts > 0 && starts == stops && !leaderGate.Running()
	}, failoverBound, "leader loops to stop")

	takeover := pollUntil(t, clk, followerGate.Running, failoverBound, "follower loops to start")
	t.Logf("clean shutdown failover takeover: %v", takeover)

	fStarts, fStops := followerLoop.counts()
	if fStarts != 1 || fStops != 0 {
		t.Fatalf("follower loop: starts=%d, stops=%d, want 1/0", fStarts, fStops)
	}
}

// Positive: killing the lock session ends the old leader's term without a context cancel
// (golusoris leader/pg probes the session every retry period since v0.17.0), and one replica leads
// again within the bound. Either replica may win the new term: the old leader re-campaigns on a
// fresh connection at once and can take the lock before the follower's next retry. From the moment
// the old leader's stop is seen, the two never run together.
func TestLeaderFailover_SessionLoss(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	poolA := openTestPool(t, st.Pool())
	poolB := openTestPool(t, poolA)
	adminPool := openTestPool(t, poolA)

	clk := clockwork.NewRealClock()
	lockName := fmt.Sprintf("lock-%d-%s", os.Getpid(), t.Name())

	loopA, loopB := newFakeLoop(nil), newFakeLoop(nil)
	gateA, gateB := leaderx.New(clk, quiet(), loopA), leaderx.New(clk, quiet(), loopB)

	electA, electB := makeElectPair(poolA, poolB, lockName, lockName, clk)
	startGateRunner(t, gateA, electA)
	startGateRunner(t, gateB, electB)

	pollUntil(t, clk, func() bool {
		return gateA.Running() || gateB.Running()
	}, electionBound, "initial leader election")

	leaderLoop := loopA
	if gateB.Running() {
		leaderLoop = loopB
	}

	sampler := startSampler(clk, gateA, gateB)
	terminatedAt := clk.Now()
	terminateLockSession(t, adminPool)

	stopping := pollUntil(t, clk, func() bool {
		_, stops := leaderLoop.counts()
		return stops > 0
	}, failoverBound, "old leader to observe the session loss")
	stoppedAt := clk.Now()
	t.Logf("old leader stopped its loops %v after the backend kill", stopping)

	takeover := pollUntil(t, clk, func() bool {
		return gateA.Running() != gateB.Running()
	}, failoverBound, "one replica to lead the new term")
	t.Logf("new term %v after the old leader stopped (replica-a leads: %v)", takeover, gateA.Running())
	time.Sleep(2 * defaultTestRetry)

	samples := sampler.stopSampling()
	t.Logf("split-brain window before the old leader observed the loss: %v", overlapWindow(samples, terminatedAt))
	assertNeverBothAfter(t, samples, stoppedAt)
}

// terminateLockSession kills the backend holding the advisory lock in the test database.
func terminateLockSession(t *testing.T, adminPool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	var terminated bool
	err := adminPool.QueryRow(ctx,
		"SELECT pg_terminate_backend(pid) FROM pg_locks WHERE locktype='advisory' AND granted AND database = (SELECT oid FROM pg_database WHERE datname = current_database())",
	).Scan(&terminated)
	if err != nil || !terminated {
		t.Fatalf("terminate advisory lock backend: terminated=%v, err=%v", terminated, err)
	}
}

// Failover B note on the overlap window: a backend kill drops the advisory lock at once, but the
// old leader only learns of it at its next session probe, up to one retry period later. The
// follower can win the lock in that window, so a short overlap after the kill is expected and
// logged, not asserted. The "never both" rule holds from the old leader's stop on.

// assertNeverBothAfter fails when a sample taken after from shows both gates running, and when no
// sample was taken after from, so the rule is never passed without being checked.
func assertNeverBothAfter(t *testing.T, samples []gateSample, from time.Time) {
	t.Helper()
	checked := 0
	for _, s := range samples {
		if !s.at.After(from) {
			continue
		}
		checked++
		if s.runningA && s.runningB {
			t.Fatalf("both gates running after the old leader stopped, at sample %v", s.at)
		}
	}
	if checked == 0 {
		t.Fatalf("no sample taken after the old leader stopped at %v", from)
	}
}

// overlapWindow measures how long both gates reported running after the backend kill. A non-zero
// value is the known gap documented above, not a test failure: it is logged so the window is
// visible in every run.
func overlapWindow(samples []gateSample, from time.Time) time.Duration {
	var first, last time.Time
	for _, s := range samples {
		if s.at.Before(from) || !s.runningA || !s.runningB {
			continue
		}
		if first.IsZero() {
			first = s.at
		}
		last = s.at
	}
	return last.Sub(first)
}

// Negative: two gates with DIFFERENT lock names both run (proves lock name separates them).
func TestLeaderFailover_DistinctLockNames(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	poolA := openTestPool(t, st.Pool())
	poolB := openTestPool(t, poolA)

	clk := clockwork.NewRealClock()

	nameA := fmt.Sprintf("distinct-lock-a-%d-%s", os.Getpid(), t.Name())
	nameB := fmt.Sprintf("distinct-lock-b-%d-%s", os.Getpid(), t.Name())

	loopA, loopB := newFakeLoop(nil), newFakeLoop(nil)
	gateA, gateB := leaderx.New(clk, quiet(), loopA), leaderx.New(clk, quiet(), loopB)

	electA, electB := makeElectPair(poolA, poolB, nameA, nameB, clk)
	startGateRunner(t, gateA, electA)
	startGateRunner(t, gateB, electB)

	pollUntil(t, clk, func() bool {
		return gateA.Running() && gateB.Running()
	}, electionBound, "both gates to run on distinct lock names")

	sA, _ := loopA.counts()
	sB, _ := loopB.counts()
	if sA != 1 || sB != 1 {
		t.Fatalf("expected both loops started: sA=%d, sB=%d", sA, sB)
	}
}

// Positive: Callbacks().OnStoppedLeading stops loops and clears running flag on leadership loss.
func TestLeaderFailover_CallbacksStopOnLoss(t *testing.T) {
	t.Parallel()
	clk := clockwork.NewRealClock()
	loop := newFakeLoop(nil)
	gate := leaderx.New(clk, quiet(), loop)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	cb := gate.Callbacks(ctx)
	cb.OnStartedLeading(ctx)

	if !gate.Running() {
		t.Fatal("gate should be running after OnStartedLeading")
	}
	starts, stops := loop.counts()
	if starts != 1 || stops != 0 {
		t.Fatalf("loop counts before stop: starts=%d, stops=%d, want 1/0", starts, stops)
	}

	cb.OnStoppedLeading()

	if gate.Running() {
		t.Fatal("gate should not be running after OnStoppedLeading")
	}
	starts, stops = loop.counts()
	if starts != 1 || stops != 1 {
		t.Fatalf("loop counts after stop: starts=%d, stops=%d, want 1/1", starts, stops)
	}
}
