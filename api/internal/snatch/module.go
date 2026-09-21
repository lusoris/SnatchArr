// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package snatch

import (
	"context"
	"log/slog"
	"time"

	"go.uber.org/fx"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/leaderx"
)

// GateGroup is the fx value group other modules add Gate implementations to.
const GateGroup = `group:"snatch.gates"`

type plannerParams struct {
	fx.In
	Gates []Gate `group:"snatch.gates"`
}

func newPlanner(st storeParam, clk clock.Clock, runs *Runs, rec *Recorder, logger *slog.Logger, p plannerParams) *Planner {
	gates := p.Gates
	if len(gates) == 0 {
		gates = []Gate{AllowAll{}}
	}
	return NewPlanner(st.Store, clk, runs, rec, logger, gates...)
}

// Ticker runs the planner on a fixed interval for the process lifetime. Until leader
// election lands, run exactly one API replica.
type Ticker struct {
	planner  *Planner
	clk      clock.Clock
	logger   *slog.Logger
	interval time.Duration
	cancel   context.CancelFunc
	done     chan struct{}
}

// NewTicker builds a ticker with the default interval.
func NewTicker(planner *Planner, clk clock.Clock, logger *slog.Logger) *Ticker {
	return &Ticker{planner: planner, clk: clk, logger: logger, interval: TickInterval}
}

// Start launches the loop.
func (t *Ticker) Start(parent context.Context) error {
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	t.cancel = cancel
	t.done = make(chan struct{})
	go t.loop(ctx)
	return nil
}

// Stop ends the loop and waits for the in-flight tick.
func (t *Ticker) Stop(ctx context.Context) error {
	if t.cancel != nil {
		t.cancel()
	}
	select {
	case <-t.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (t *Ticker) loop(ctx context.Context) {
	defer close(t.done)
	timer := t.clk.NewTicker(t.interval)
	defer timer.Stop()
	t.tick(ctx)
	for range maxTicks {
		select {
		case <-ctx.Done():
			return
		case <-timer.Chan():
			t.tick(ctx)
		}
	}
}

func (t *Ticker) tick(ctx context.Context) {
	tctx, cancel := context.WithTimeout(ctx, t.interval)
	defer cancel()
	n, err := t.planner.Tick(tctx)
	if err != nil {
		t.logger.ErrorContext(ctx, "snatch: planner tick", slog.String("error", err.Error()))
		return
	}
	if n > 0 {
		t.logger.InfoContext(ctx, "snatch: runs queued", slog.Int("count", n))
	}
}

// Module provides the snatch services and starts the planner ticker.
var Module = fx.Module("snatcharr.snatch",
	fx.Provide(NewBudget, NewMemory, NewRuns, NewRecorder, NewSchedules, newPlanner, NewTicker),
	// The planner runs on the leader only (internal/leaderx).
	fx.Provide(leaderx.Provide[*Ticker]()),
)
