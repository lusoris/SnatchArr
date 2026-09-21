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
package leaderx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/leader"
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

// Gate starts and stops a set of loops as leadership comes and goes.
type Gate struct {
	loops  []Loop
	clk    clock.Clock
	logger *slog.Logger

	mu      sync.Mutex
	running bool
	started []Loop
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
	if elect == nil {
		g.startAll(ctx)
		<-ctx.Done()
		g.stopAll(ctx)
		return nil
	}
	if retry <= 0 {
		retry = DefaultRetry
	}
	defer g.stopAll(ctx)
	for range maxElections {
		err := elect(ctx, g.Callbacks(ctx))
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

// startAll starts every loop once; a loop that fails to start is logged and skipped, so
// one broken job never keeps the others from running.
func (g *Gate) startAll(ctx context.Context) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.running {
		return
	}
	for _, l := range g.loops {
		if err := l.Start(ctx); err != nil {
			g.logger.ErrorContext(ctx, "leader: loop start failed",
				slog.String("loop", fmt.Sprintf("%T", l)), slog.String("error", err.Error()))
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
				slog.String("loop", fmt.Sprintf("%T", v)), slog.String("error", err.Error()))
		}
	}
	g.started = nil
	g.running = false
	g.logger.InfoContext(ctx, "leader: loops stopped")
}
