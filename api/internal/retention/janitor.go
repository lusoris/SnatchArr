// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package retention trims the tables that grow with every snatch: events and finished runs
// past history_retention_days, afterglow rows past snatch.PurgeGrace, spent rate buckets
// past snatch.BucketKeep and expired sessions. The janitor runs on the elected replica only
// (internal/leaderx).
package retention

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.uber.org/fx"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/auth"
	"github.com/lusoris/SnatchArr/api/internal/leaderx"
	"github.com/lusoris/SnatchArr/api/internal/settings"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
)

const (
	// Interval is how often the janitor runs a pass while this replica leads.
	Interval = time.Hour
	// Batch is how many rows one DELETE removes.
	Batch int32 = 1000
	// MaxBatches bounds one table per pass (HISS-02): at most MaxBatches*Batch rows go, the
	// rest waits for the next pass.
	MaxBatches = 100
	// opTimeout bounds one batch and the settings read.
	opTimeout = 30 * time.Second
	// maxTicks bounds the loop (HISS-02): 2^20 hours is about 120 years.
	maxTicks = 1 << 20
)

// Counts are the rows one pass deleted, per table.
type Counts struct {
	Events, Runs, Afterglow, Buckets, Sessions int64
}

// Total is the sum over every table.
func (c Counts) Total() int64 { return c.Events + c.Runs + c.Afterglow + c.Buckets + c.Sessions }

// Deps are the janitor's collaborators; each owns the purge of its table.
type Deps struct {
	fx.In

	Settings *settings.Service
	Recorder *snatch.Recorder
	Runs     *snatch.Runs
	Memory   *snatch.Memory
	Budget   *snatch.Budget
	Sessions *auth.PgSessionStore
	Clock    clock.Clock
	Logger   *slog.Logger
}

// Janitor runs retention passes on a timer. It implements leaderx.Loop.
type Janitor struct {
	deps     Deps
	interval time.Duration
	batch    int32
	cancel   context.CancelFunc
	done     chan struct{}
}

// New builds a janitor with the default interval and batch size.
func New(d Deps) *Janitor {
	return &Janitor{deps: d, interval: Interval, batch: Batch}
}

// step is one table's purge: it deletes at most limit rows and reports how many went.
type step struct {
	name  string
	count *int64
	purge func(ctx context.Context, limit int32) (int64, error)
}

// Pass deletes everything past retention, table by table, in bounded batches. A failing
// table does not stop the others; the errors are joined.
func (j *Janitor) Pass(ctx context.Context) (Counts, error) {
	sctx, cancel := context.WithTimeout(ctx, opTimeout)
	cfg, err := j.deps.Settings.Get(sctx)
	cancel()
	if err != nil {
		return Counts{}, fmt.Errorf("retention: %w", err)
	}
	before := j.deps.Clock.Now().Add(-time.Duration(cfg.HistoryRetentionDays) * 24 * time.Hour)
	var c Counts
	// Events go before runs: deleting a run clears run_id on its remaining events.
	steps := []step{
		{"events", &c.Events, func(ctx context.Context, n int32) (int64, error) {
			return j.deps.Recorder.Purge(ctx, before, n)
		}},
		{"runs", &c.Runs, func(ctx context.Context, n int32) (int64, error) {
			return j.deps.Runs.Purge(ctx, before, n)
		}},
		{"afterglow", &c.Afterglow, j.deps.Memory.Purge},
		{"buckets", &c.Buckets, j.deps.Budget.Purge},
		{"sessions", &c.Sessions, j.deps.Sessions.PurgeExpired},
	}
	var errs []error
	for _, s := range steps {
		n, purgeErr := drain(ctx, j.batch, s.purge)
		*s.count = n
		if purgeErr != nil {
			errs = append(errs, fmt.Errorf("retention: %s: %w", s.name, purgeErr))
		}
	}
	return c, errors.Join(errs...)
}

// drain repeats purge until a batch comes back short, at most MaxBatches times.
func drain(ctx context.Context, batch int32, purge func(context.Context, int32) (int64, error)) (int64, error) {
	var total int64
	for range MaxBatches {
		bctx, cancel := context.WithTimeout(ctx, opTimeout)
		n, err := purge(bctx, batch)
		cancel()
		total += n
		if err != nil {
			return total, err
		}
		if n < int64(batch) {
			return total, nil
		}
	}
	return total, nil
}

// Start launches the loop; the first pass runs at once.
func (j *Janitor) Start(parent context.Context) error {
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	j.cancel = cancel
	j.done = make(chan struct{})
	go j.loop(ctx)
	return nil
}

// Stop ends the loop and waits for the pass in flight.
func (j *Janitor) Stop(ctx context.Context) error {
	if j.cancel != nil {
		j.cancel()
	}
	select {
	case <-j.done:
	case <-ctx.Done():
		return fmt.Errorf("retention: stop: %w", ctx.Err())
	}
	return nil
}

func (j *Janitor) loop(ctx context.Context) {
	defer close(j.done)
	timer := j.deps.Clock.NewTicker(j.interval)
	defer timer.Stop()
	j.tick(ctx)
	for range maxTicks {
		select {
		case <-ctx.Done():
			return
		case <-timer.Chan():
			j.tick(ctx)
		}
	}
}

// tick runs one pass and logs one summary line with the row counts.
func (j *Janitor) tick(ctx context.Context) {
	tctx, cancel := context.WithTimeout(ctx, j.interval)
	defer cancel()
	c, err := j.Pass(tctx)
	attrs := []any{
		slog.Int64("events", c.Events), slog.Int64("runs", c.Runs), slog.Int64("afterglow", c.Afterglow),
		slog.Int64("buckets", c.Buckets), slog.Int64("sessions", c.Sessions),
	}
	switch {
	case err != nil:
		j.deps.Logger.ErrorContext(tctx, "retention: purge pass", append(attrs, slog.String("error", err.Error()))...)
	case c.Total() > 0:
		j.deps.Logger.InfoContext(tctx, "retention: purge pass", attrs...)
	default:
		j.deps.Logger.DebugContext(tctx, "retention: purge pass", attrs...)
	}
}

// Module provides the janitor and registers it as a leader-gated loop.
var Module = fx.Module("snatcharr.retention",
	fx.Provide(New),
	// The janitor runs on the leader only (internal/leaderx).
	fx.Provide(leaderx.Provide[*Janitor]()),
)
