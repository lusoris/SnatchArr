// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package snatch

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jonboulle/clockwork"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/events"
	"github.com/lusoris/SnatchArr/api/internal/leaderx/leaderxtest"
	"github.com/lusoris/SnatchArr/api/internal/metrics"
	"github.com/lusoris/SnatchArr/api/internal/metrics/metricstest"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

const (
	// listErrPrefix is the context Tick adds to a failed instance listing.
	listErrPrefix = "snatch: planner list instances: "
	// askedBuffer is more gate calls than the ticker test produces, so the gate never blocks.
	askedBuffer = 16
	// stopBound bounds the ticker's stop (HISS-02).
	stopBound = 5 * time.Second
)

// refuse is a gate that vetoes every snatch.
type refuse struct{}

func (refuse) Allow(context.Context, domain.Instance, domain.SnatchKind) (bool, string, error) {
	return false, "refused by the test", nil
}

// signalGate reports each time the planner asks it and vetoes the snatch, so that every tick
// asks again.
type signalGate struct{ asked chan struct{} }

func (g signalGate) Allow(context.Context, domain.Instance, domain.SnatchKind) (bool, string, error) {
	select {
	case g.asked <- struct{}{}:
	default:
	}
	return false, "refused by the test", nil
}

type plannerFixture struct {
	st      *store.Store
	clk     *clockwork.FakeClock
	planner *Planner
	// logged is what the planner logged. Read it only while no tick runs.
	logged *bytes.Buffer
	// m and reg are the fixture's own metrics and the registry that holds them.
	m   *metrics.Metrics
	reg *prometheus.Registry
}

func newPlannerFixture(t *testing.T, gate Gate) plannerFixture {
	t.Helper()
	st := storetest.New(t)
	clk := clock.NewFake()
	logged := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logged, nil))
	rec := NewRecorder(st, clk, events.New(logger))
	m, reg := metricstest.New(t)
	return plannerFixture{
		st: st, clk: clk, planner: NewPlanner(st, clk, NewRuns(st, clk, id.New()), rec, logger, m, gate),
		logged: logged, m: m, reg: reg,
	}
}

// pauseAlways inserts a global pause window that covers every minute of every day.
func pauseAlways(t *testing.T, f plannerFixture) {
	t.Helper()
	_, err := f.st.Q().CreateSchedule(t.Context(), sqlcgen.CreateScheduleParams{
		ID: uuid.New(), Name: "always paused", DaysMask: 0b1111111, Tz: "UTC", Action: string(ActionPause), Enabled: true,
		StartTime: pgtype.Time{Valid: true},
		EndTime:   pgtype.Time{Microseconds: (24 * time.Hour).Microseconds(), Valid: true},
		CreatedAt: f.clk.Now(),
	})
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}
}

// count returns the single number a query yields.
func count(t *testing.T, st *store.Store, query string) int {
	t.Helper()
	var n int
	if err := st.Pool().QueryRow(t.Context(), query).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// assertQueued checks what one tick left in the store: one queued run and one run_queued
// event per queued snatch.
func assertQueued(t *testing.T, st *store.Store, want int) {
	t.Helper()
	if got := count(t, st, `SELECT count(*) FROM snatch_runs WHERE status = 'queued'`); got != want {
		t.Errorf("queued runs in the store = %d, want %d", got, want)
	}
	if got := count(t, st, `SELECT count(*) FROM snatch_events WHERE type = 'run_queued'`); got != want {
		t.Errorf("run_queued events in the store = %d, want %d", got, want)
	}
}

type tickCase struct {
	name      string
	instances int
	gate      Gate
	paused    bool
	cancelled bool
	want      int
	wantErr   error
}

func (tc tickCase) run(t *testing.T) {
	t.Helper()
	f := newPlannerFixture(t, tc.gate)
	for range tc.instances {
		storetest.Instance(t, f.st, f.clk.Now())
	}
	if tc.paused {
		pauseAlways(t, f)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if tc.cancelled {
		cancel()
	}
	queued, err := f.planner.Tick(ctx)
	if !errors.Is(err, tc.wantErr) {
		t.Fatalf("Tick() error = %v, want %v", err, tc.wantErr)
	}
	if queued != tc.want {
		t.Errorf("Tick() = %d, want %d", queued, tc.want)
	}
	assertQueued(t, f.st, tc.want)
	if tc.wantErr != nil {
		if !strings.HasPrefix(err.Error(), listErrPrefix) {
			t.Errorf("Tick() error = %q, want the prefix %q", err, listErrPrefix)
		}
		return
	}
	// A queued run is still active, so the next tick queues nothing for its instance.
	again, err := f.planner.Tick(ctx)
	if err != nil || again != 0 {
		t.Errorf("second Tick() = %d, %v, want 0, <nil>", again, err)
	}
	assertQueued(t, f.st, tc.want)
	// A tick that plans every instance skips none, and a skipped instance is a warning.
	if warned := strings.Count(f.logged.String(), "level=WARN"); warned != 0 {
		t.Errorf("%d warnings logged, want none:\n%s", warned, firstLines(f.logged.String()))
	}
}

// firstLines keeps a failure message readable when every instance logged a line.
func firstLines(logged string) string {
	const keep = 3
	lines := strings.SplitAfterN(logged, "\n", keep+1)
	if len(lines) > keep {
		lines = lines[:keep]
	}
	return strings.Join(lines, "")
}

func TestPlanner_Tick(t *testing.T) {
	t.Parallel()
	cases := []tickCase{
		// Positive: an instance that never ran is due; the default policy snatches missing only.
		{name: "due instances queue one run each", instances: 2, gate: AllowAll{}, want: 2},
		// Negative.
		{name: "a gate refuses", instances: 2, gate: refuse{}, want: 0},
		{name: "a pause window covers now", instances: 2, gate: AllowAll{}, paused: true, want: 0},
		{name: "the store fails", instances: 2, gate: AllowAll{}, cancelled: true, want: 0, wantErr: context.Canceled},
		// Boundary.
		{name: "no instance", gate: AllowAll{}, want: 0},
		{name: "exactly the bound", instances: MaxInstancesPerTick, gate: AllowAll{}, want: MaxInstancesPerTick},
		{name: "more than the bound", instances: MaxInstancesPerTick + 5, gate: AllowAll{}, want: MaxInstancesPerTick},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// Positive: the ticker hands Planner.Tick to the leader loop, which runs it at once and
// again after one TickInterval.
func TestNewTicker_RunsThePlannerPerInterval(t *testing.T) {
	t.Parallel()
	gate := signalGate{asked: make(chan struct{}, askedBuffer)}
	f := newPlannerFixture(t, gate)
	storetest.Instance(t, f.st, f.clk.Now())
	startTicker(t, f)

	leaderxtest.Await(t, gate.asked, "the first planner tick")
	f.clk.Advance(TickInterval)
	leaderxtest.Await(t, gate.asked, "the planner tick after one interval")
}

// startTicker starts the planner's leader loop and stops it when the test ends.
func startTicker(t *testing.T, f plannerFixture) {
	t.Helper()
	ticker := NewTicker(f.planner, f.clk, slog.New(slog.DiscardHandler), f.m)
	if err := ticker.Start(t.Context()); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), stopBound)
		defer cancel()
		if err := ticker.Stop(ctx); err != nil {
			t.Errorf("Stop() = %v", err)
		}
	})
}

// Positive: a planner tick that succeeds reaches the loop metrics as the last success at the
// clock's time, with no failure (gap audit A1-04).
func TestNewTicker_RecordsTheLastSuccess(t *testing.T) {
	t.Parallel()
	f := newPlannerFixture(t, AllowAll{})
	storetest.Instance(t, f.st, f.clk.Now())
	startTicker(t, f)
	metricstest.Await(t, f.reg, "snatcharr_loop_last_success_timestamp_seconds", "loop", "snatch_planner",
		float64(f.clk.Now().UnixNano())/float64(time.Second))
	metricstest.Await(t, f.reg, "snatcharr_loop_failures_total", "loop", "snatch_planner", 0)
}

// Negative: a planner tick that cannot list the instances counts as a failed tick.
func TestNewTicker_CountsAFailedTick(t *testing.T) {
	t.Parallel()
	f := newPlannerFixture(t, AllowAll{})
	if _, err := f.st.Pool().Exec(t.Context(), `DROP TABLE instances CASCADE`); err != nil {
		t.Fatalf("drop instances: %v", err)
	}
	startTicker(t, f)
	metricstest.Await(t, f.reg, "snatcharr_loop_failures_total", "loop", "snatch_planner", 1)
	if got, ok := metricstest.Value(t, f.reg, "snatcharr_loop_last_success_timestamp_seconds", "loop", "snatch_planner"); ok {
		t.Errorf("last success = %v after a failed tick only, want no series", got)
	}
}
