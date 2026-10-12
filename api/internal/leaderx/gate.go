// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package leaderx runs the API's periodic loops on exactly one replica.
//
// Every loop that must not run twice (the snatch planner, the Seerr sync, the Configarr
// poll) registers itself in the "leader_loops" fx value group instead of starting from
// its own module. With leader.enabled=false (APP_LEADER_ENABLED, the default) the loops
// start with the process, which is right for a single replica. When enabled, a Postgres
// advisory lock (golusoris leader/pg) elects one replica: its loops start when it wins
// the lock and stop when it loses it; the others idle until they win.
//
// The gate's readiness check (Gate.Check) fails while the loops cannot run: before the
// gate runs or after it stopped, while a loop that failed to start is missing from the
// current term, and while the election itself fails. A replica that only waits for the
// lock is ready.
package leaderx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/k8s/health"
	"github.com/golusoris/golusoris/leader"
	"github.com/golusoris/golusoris/observability/statuspage"
)

// Loop is a periodic job with the lifecycle shape fx hooks use.
type Loop interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// Elect drives one election: it blocks while this replica competes and leads, invokes
// the callbacks on transitions, and returns when ctx ends or the backend fails.
type Elect func(ctx context.Context, cb leader.Callbacks) error

// StopTimeout bounds how long a lost lease waits for in-flight loop work.
const StopTimeout = 30 * time.Second

// DefaultRetry is the pause between failed elections when the backend gives none.
const DefaultRetry = 2 * time.Second

// maxElections bounds the retry loop (HISS-02): 2^20 attempts at the retry interval.
const maxElections = 1 << 20

// ErrExhausted reports that the election retry budget ran out.
var ErrExhausted = errors.New("leaderx: election attempts exhausted")

// ErrNotRunning is the health of a gate that does not run: not yet started, or stopped.
var ErrNotRunning = errors.New("leaderx: gate is not running")

// Gate starts and stops a set of loops as leadership comes and goes.
type Gate struct {
	loops  []Loop
	clk    clock.Clock
	logger *slog.Logger

	mu      sync.Mutex
	running bool
	started []Loop
	failed  []string // loops whose start failed in the current term

	alive    bool          // Run drives the gate
	retry    time.Duration // pause between failed elections
	attempt  time.Time     // start of the election in flight; zero between elections
	electErr error         // last failed election; a new term clears it
}

// New builds a gate over loops.
func New(clk clock.Clock, logger *slog.Logger, loops ...Loop) *Gate {
	return &Gate{loops: loops, clk: clk, logger: logger}
}

// Callbacks adapts the gate to an elector: loops start on leadership and stop on loss.
// parent scopes the stop (its values only; it is usually cancelled by then).
func (g *Gate) Callbacks(parent context.Context) leader.Callbacks {
	return leader.Callbacks{
		OnStartedLeading: g.startAll,
		OnStoppedLeading: func() { g.stopAll(parent) },
		OnNewLeader: func(identity string) {
			g.logger.InfoContext(parent, "leader: elected", slog.String("identity", identity))
		},
	}
}

// Running reports whether the loops are currently started.
func (g *Gate) Running() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.running
}

// Run drives the gate until ctx ends. Without an elector the loops run for the whole
// process lifetime; with one they follow the lease, and a failed election is retried
// after retry (DefaultRetry when retry is not positive).
func (g *Gate) Run(ctx context.Context, elect Elect, retry time.Duration) error {
	if retry <= 0 {
		retry = DefaultRetry
	}
	g.setAlive(true, retry)
	defer g.setAlive(false, retry)
	if elect == nil {
		g.startAll(ctx)
		<-ctx.Done()
		g.stopAll(ctx)
		return nil
	}
	defer g.stopAll(ctx)
	for range maxElections {
		err := g.elect(ctx, elect)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			g.logger.ErrorContext(ctx, "leader: election failed; retrying",
				slog.String("error", err.Error()), slog.Duration("retry", retry))
		}
		select {
		case <-ctx.Done():
			return nil
		case <-g.clk.After(retry):
		}
	}
	return ErrExhausted
}

// elect runs one election and records when it started and how it ended.
func (g *Gate) elect(ctx context.Context, elect Elect) error {
	g.mu.Lock()
	g.attempt = g.clk.Now()
	g.mu.Unlock()
	err := elect(ctx, g.Callbacks(ctx))
	g.mu.Lock()
	defer g.mu.Unlock()
	g.attempt = time.Time{}
	if err != nil {
		g.electErr = err
	}
	return err
}

func (g *Gate) setAlive(alive bool, retry time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.alive, g.retry = alive, retry
}

// Health reports why the loops cannot run, or nil. An election that failed counts until
// the next one has run for one retry interval without failing, or until a term starts.
func (g *Gate) Health() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	switch {
	case !g.alive:
		return ErrNotRunning
	case len(g.failed) > 0:
		return fmt.Errorf("leaderx: loops failed to start: %s", strings.Join(g.failed, ", "))
	case g.electErr != nil && (g.attempt.IsZero() || g.clk.Since(g.attempt) < g.retry):
		return fmt.Errorf("leaderx: election failing: %w", g.electErr)
	}
	return nil
}

// Check is the gate's readiness check for the statuspage registry.
func (g *Gate) Check(name string) statuspage.Check {
	return statuspage.Check{
		Name: name,
		Tags: []string{health.TagReadiness},
		Fn:   func(context.Context) error { return g.Health() },
	}
}

// startAll starts every loop once; a loop that fails to start is logged and skipped, so
// one broken job never keeps the others from running.
func (g *Gate) startAll(ctx context.Context) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.running {
		return
	}
	g.electErr = nil
	for _, l := range g.loops {
		if err := l.Start(ctx); err != nil {
			g.logger.ErrorContext(ctx, "leader: loop start failed",
				slog.String("loop", loopName(l)), slog.String("error", err.Error()))
			g.failed = append(g.failed, loopName(l))
			continue
		}
		g.started = append(g.started, l)
	}
	g.running = true
	g.logger.InfoContext(ctx, "leader: loops started", slog.Int("count", len(g.started)))
}

// stopAll stops the started loops in reverse order, bounded by StopTimeout. The parent
// is usually already cancelled (lease lost, shutdown), so only its values carry over.
func (g *Gate) stopAll(parent context.Context) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.running {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), StopTimeout)
	defer cancel()
	for _, v := range slices.Backward(g.started) {
		if err := v.Stop(ctx); err != nil {
			g.logger.ErrorContext(ctx, "leader: loop stop failed",
				slog.String("loop", loopName(v)), slog.String("error", err.Error()))
		}
	}
	g.started, g.failed = nil, nil
	g.running = false
	g.logger.InfoContext(ctx, "leader: loops stopped")
}

// loopName names a loop in logs and in the readiness message: its own name when it has
// one (Periodic does), else its type.
func loopName(l Loop) string {
	if n, ok := l.(interface{ Name() string }); ok {
		return n.Name()
	}
	return fmt.Sprintf("%T", l)
}
