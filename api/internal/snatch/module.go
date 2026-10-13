// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package snatch

import (
	"context"
	"log/slog"

	"go.uber.org/fx"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/leaderx"
	"github.com/lusoris/SnatchArr/api/internal/metrics"
)

// GateGroup is the fx value group other modules add Gate implementations to.
const GateGroup = `group:"snatch.gates"`

type plannerParams struct {
	fx.In
	Metrics *metrics.Metrics
	Gates   []Gate `group:"snatch.gates"`
}

func newPlanner(st storeParam, clk clock.Clock, runs *Runs, rec *Recorder, logger *slog.Logger, p plannerParams) *Planner {
	gates := p.Gates
	if len(gates) == 0 {
		gates = []Gate{AllowAll{}}
	}
	return NewPlanner(st.Store, clk, runs, rec, logger, p.Metrics, gates...)
}

// Ticker runs the planner once per TickInterval while this replica leads.
type Ticker struct{ *leaderx.Periodic }

// NewTicker builds the planner's leader loop.
func NewTicker(planner *Planner, clk clock.Clock, logger *slog.Logger, m *metrics.Metrics) *Ticker {
	return &Ticker{leaderx.NewPeriodic("snatch: planner", TickInterval, clk, m, func(ctx context.Context) error {
		n, err := planner.Tick(ctx)
		if err != nil {
			logger.ErrorContext(ctx, "snatch: planner tick", slog.String("error", err.Error()))
			return err
		}
		if n > 0 {
			logger.InfoContext(ctx, "snatch: runs queued", slog.Int("count", n))
		}
		return nil
	})}
}

// Module provides the snatch services and registers the planner ticker.
var Module = fx.Module("snatcharr.snatch",
	fx.Provide(NewBudget, NewMemory, NewRuns, NewWorkers, NewRecorder, NewSchedules, newPlanner, NewTicker),
	// The planner runs on the leader only (internal/leaderx).
	fx.Provide(leaderx.Provide[*Ticker]()),
)
