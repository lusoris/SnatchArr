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

// failoverBound caps a takeover or a loop stop at ten retry periods. The measured takeover is
// logged; the bound only has to separate "bounded failover" from "no failover" under load.
const failoverBound = 10 * defaultTestRetry

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

// Failover B Note on overlap window and session loss:
// In golusoris leader/pg (v0.16.4), pgleader.Run acquires an advisory lock on a single
// dedicated session and delegates to hook.Lead, which blocks solely on <-ctx.Done(). There
// is no active heartbeat / ping query on the session connection while leading.
// When the lock session is terminated from outside via pg_terminate_backend, PostgreSQL drops
// the advisory lock immediately. The follower retries acquisition and takes over within 3*Retry.
// However, the old leader does not perform I/O on the dead session, so it cannot observe
// server-side session termination until its Run context is canceled (e.g. by watchdog,
// health probe failure, or container teardown). Strict "never both" on backend kill without
// context cancel is therefore impossible by design with session-scoped advisory locks without
// in-session heartbeats. Follower takeover succeeds within the 3*Retry bound; once the old
// leader context is canceled to signal loss, it stops its loops and never runs concurrently.
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
	cancelA := startGateRunner(t, gateA, electA)
	cancelB := startGateRunner(t, gateB, electB)

	pollUntil(t, clk, func() bool {
		return gateA.Running() || gateB.Running()
	}, electionBound, "initial leader election")

	leaderGate, followerGate := gateA, gateB
	cancelLeader := cancelA
	leaderIsA := true
	if gateB.Running() {
		leaderGate, followerGate = gateB, gateA
		cancelLeader = cancelB
		leaderIsA = false
	}

	sampler := startSampler(clk, gateA, gateB)
	terminatedAt := clk.Now()

	terminateCtx, terminateCancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer terminateCancel()

	var terminated bool
	err := adminPool.QueryRow(terminateCtx,
		"SELECT pg_terminate_backend(pid) FROM pg_locks WHERE locktype='advisory' AND granted AND database = (SELECT oid FROM pg_database WHERE datname = current_database())",
	).Scan(&terminated)
	if err != nil || !terminated {
		t.Fatalf("terminate advisory lock backend: terminated=%v, err=%v", terminated, err)
	}

	takeover := pollUntil(t, clk, followerGate.Running, failoverBound, "follower takeover")
	t.Logf("session loss failover takeover: %v", takeover)

	// Old leader does not learn of server-side session termination on an idle socket. Hold for
	// two retry periods so the sampler records the overlap, then cancel its context to simulate
	// watchdog/supervisor termination of the severed replica.
	time.Sleep(2 * defaultTestRetry)
	cancelLeader()
	lossObservedAt := clk.Now()

	pollUntil(t, clk, func() bool {
		return !leaderGate.Running()
	}, failoverBound, "old leader to stop loops after loss observation")

	samples := sampler.stopSampling()
	overlap := overlapWindow(samples, terminatedAt)
	t.Logf("split-brain window after backend kill (old leader not yet cancelled): %v", overlap)
	for _, s := range samples {
		if s.at.After(lossObservedAt) {
			leaderRunning := s.runningA
			followerRunning := s.runningB
			if !leaderIsA {
				leaderRunning, followerRunning = s.runningB, s.runningA
			}
			if leaderRunning && followerRunning {
				t.Fatalf("both gates running after loss observed at sample %v", s.at)
			}
		}
	}
}

// overlapWindow measures how long both gates reported running after the backend kill. A non-zero
// value is the known gap documented above, not a test failure: it is logged so the window is
// visible in every run and the test can turn strict once the upstream elector notices session loss.
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
