// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package snatch

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/events"
	"github.com/lusoris/SnatchArr/api/internal/metrics"
	"github.com/lusoris/SnatchArr/api/internal/metrics/metricstest"
	"github.com/lusoris/SnatchArr/api/internal/settings"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

type stateLoopFixture struct {
	st       *store.Store
	clk      *clockwork.FakeClock
	budget   *Budget
	planner  *Planner
	settings *settings.Service
	runs     *Runs
	m        *metrics.Metrics
	reg      *prometheus.Registry
	loop     *StateLoop
}

func newStateLoopFixture(t *testing.T) stateLoopFixture {
	t.Helper()
	st := storetest.New(t)
	clk := clock.NewFake()
	logger := slog.New(slog.DiscardHandler)
	m, reg := metricstest.New(t)
	runs := NewRuns(st, clk, id.New())
	rec := NewRecorder(st, clk, events.New(logger))
	planner := NewPlanner(st, clk, runs, rec, logger, m)
	b := NewBudget(st, clk)
	set := settings.New(st, clk)
	loop := &StateLoop{
		st: st, clk: clk, budget: b, planner: planner,
		settings: set, logger: logger, m: m,
	}
	return stateLoopFixture{
		st: st, clk: clk, budget: b, planner: planner,
		settings: set, runs: runs, m: m, reg: reg, loop: loop,
	}
}

// Positive: state loop exports run counts by status and zeroes out missing ones.
func TestStateLoop_RunsByStatus(t *testing.T) {
	t.Parallel()
	f := newStateLoopFixture(t)
	instID := storetest.Instance(t, f.st, f.clk.Now())

	// Enqueue 2 runs
	r1, _, err := f.runs.Enqueue(t.Context(), instID, domain.SnatchMissing)
	if err != nil {
		t.Fatalf("Enqueue() = %v", err)
	}
	// Complete r1 as done
	if _, err = f.runs.Lease(t.Context(), "worker-1"); err != nil {
		t.Fatalf("Lease() = %v", err)
	}
	if _, err = f.runs.Complete(t.Context(), r1.ID, "worker-1", domain.RunDone, 5, "", nil); err != nil {
		t.Fatalf("Complete() = %v", err)
	}

	// Enqueue another run and leave it queued
	if _, _, err = f.runs.Enqueue(t.Context(), instID, domain.SnatchUpgrade); err != nil {
		t.Fatalf("Enqueue() = %v", err)
	}

	if err := f.loop.Tick(t.Context()); err != nil {
		t.Fatalf("Tick() = %v", err)
	}

	for _, tc := range []struct {
		status domain.RunStatus
		want   float64
	}{
		{domain.RunQueued, 1},
		{domain.RunLeased, 0},
		{domain.RunDone, 1},
		{domain.RunFailed, 0},
		{domain.RunCancelled, 0},
	} {
		if got, ok := metricstest.Value(t, f.reg, "snatcharr_runs", "status", string(tc.status)); !ok || got != tc.want {
			t.Errorf("snatcharr_runs{%s} = %v (present %t), want %v", tc.status, got, ok, tc.want)
		}
	}
}

// Positive: state loop exports stamina used and remaining for instances and global.
func TestStateLoop_Stamina(t *testing.T) {
	t.Parallel()
	f := newStateLoopFixture(t)
	instID := storetest.Instance(t, f.st, f.clk.Now())

	// Update policy hourly cap to 50
	_, err := f.st.Pool().Exec(t.Context(), `
		INSERT INTO snatch_policies (instance_id, hourly_cap, updated_at)
		VALUES ($1, 50, $2)
		ON CONFLICT (instance_id) DO UPDATE SET hourly_cap = 50
	`, instID, f.clk.Now())
	if err != nil {
		t.Fatalf("set hourly cap: %v", err)
	}

	// Configure global hourly cap = 100
	if _, err = f.settings.Update(t.Context(), domain.Settings{
		HistoryRetentionDays:   30,
		UserAgent:              "agent",
		GlobalHourlyCap:        100,
		SearchPriorityPreset:   domain.MediaFirstPreset(),
		DownloadPriorityPreset: domain.MediaFirstPreset(),
	}); err != nil {
		t.Fatalf("settings.Update: %v", err)
	}

	// Acquire 20 items
	if _, err = f.budget.Acquire(t.Context(), instID, 50, 100, 20); err != nil {
		t.Fatalf("budget.Acquire: %v", err)
	}

	if err = f.loop.Tick(t.Context()); err != nil {
		t.Fatalf("Tick() = %v", err)
	}

	instRow, err := f.st.Q().GetInstance(t.Context(), instID)
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}

	// Instance stamina: 20 used, 30 remaining
	if got, ok := metricstest.Value(t, f.reg, "snatcharr_stamina_used_items", "arr_instance", instRow.Name); !ok || got != 20 {
		t.Errorf("stamina_used{%s} = %v (present %t), want 20", instRow.Name, got, ok)
	}
	if got, ok := metricstest.Value(t, f.reg, "snatcharr_stamina_remaining_items", "arr_instance", instRow.Name); !ok || got != 30 {
		t.Errorf("stamina_remaining{%s} = %v (present %t), want 30", instRow.Name, got, ok)
	}

	// Global stamina: 20 used, 80 remaining
	if got, ok := metricstest.Value(t, f.reg, "snatcharr_stamina_used_items", "arr_instance", "_global"); !ok || got != 20 {
		t.Errorf("stamina_used{_global} = %v (present %t), want 20", got, ok)
	}
	if got, ok := metricstest.Value(t, f.reg, "snatcharr_stamina_remaining_items", "arr_instance", "_global"); !ok || got != 80 {
		t.Errorf("stamina_remaining{_global} = %v (present %t), want 80", got, ok)
	}
}

// Positive: state loop exports pending Seerr requests sum.
func TestStateLoop_SeerrPending(t *testing.T) {
	t.Parallel()
	f := newStateLoopFixture(t)

	// Create enabled Seerr link
	linkID := uuid.New()
	_, err := f.st.Q().CreateSeerrLink(t.Context(), sqlcgen.CreateSeerrLinkParams{
		ID: linkID, Name: "seerr-main", BaseUrl: "http://seerr.invalid", ApiKeyEnc: []byte("enc"), Enabled: true, CreatedAt: f.clk.Now(),
	})
	if err != nil {
		t.Fatalf("CreateSeerrLink: %v", err)
	}

	// Insert 3 requests
	now := f.clk.Now()
	for i := range 3 {
		err := f.st.Q().UpsertSeerrRequest(t.Context(), sqlcgen.UpsertSeerrRequestParams{
			LinkID: linkID, RequestID: int32(i + 1), MediaType: "movie", TmdbID: int32(100 + i),
			Title: "Test Movie", RequestStatus: 1, MediaStatus: 1, RequestedAt: &now, LastSeenAt: now,
			Seasons: []int32{},
		})
		if err != nil {
			t.Fatalf("UpsertSeerrRequest: %v", err)
		}
	}

	if err := f.loop.Tick(t.Context()); err != nil {
		t.Fatalf("Tick() = %v", err)
	}

	if got, ok := metricstest.Value(t, f.reg, "snatcharr_seerr_requests_pending", "", ""); !ok || got != 3 {
		t.Errorf("snatcharr_seerr_requests_pending = %v (present %t), want 3", got, ok)
	}
}

// mockGate vetoes snatch with a specific refusal.
type mockGate struct {
	allowed bool
	code    string
	reason  string
}

func (g mockGate) Allow(context.Context, domain.Instance, domain.SnatchKind) (bool, GateRefusal, error) {
	return g.allowed, GateRefusal{Code: g.code, Message: g.reason}, nil
}

// Positive: planner records deferrals for gates, schedule closed, and already running.
func TestPlanner_RecordsDeferrals(t *testing.T) {
	t.Parallel()

	t.Run("gate refusal", func(t *testing.T) {
		t.Parallel()
		f := newPlannerFixture(t, mockGate{allowed: false, code: GateClientUnreachable, reason: "download client q is unreachable"})
		storetest.Instance(t, f.st, f.clk.Now())

		if _, err := f.planner.Tick(t.Context()); err != nil {
			t.Fatalf("Tick() = %v", err)
		}

		if got, ok := metricstest.Value(t, f.reg, "snatcharr_planner_deferrals_total", "reason", GateClientUnreachable); !ok || got != 2 {
			t.Errorf("deferrals{client_unreachable} = %v (present %t), want 2", got, ok)
		}
	})

	t.Run("schedule closed", func(t *testing.T) {
		t.Parallel()
		f := newPlannerFixture(t, AllowAll{})
		storetest.Instance(t, f.st, f.clk.Now())
		pauseAlways(t, f)

		if _, err := f.planner.Tick(t.Context()); err != nil {
			t.Fatalf("Tick() = %v", err)
		}

		if got, ok := metricstest.Value(t, f.reg, "snatcharr_planner_deferrals_total", "reason", metrics.DeferralScheduleClosed); !ok || got != 2 {
			t.Errorf("deferrals{schedule_closed} = %v (present %t), want 2", got, ok)
		}
	})

	t.Run("already running", func(t *testing.T) {
		t.Parallel()
		f := newPlannerFixture(t, AllowAll{})
		instID := storetest.Instance(t, f.st, f.clk.Now())

		// Pre-enqueue a run so Enqueue returns !created
		if _, _, err := f.planner.runs.Enqueue(t.Context(), instID, domain.SnatchMissing); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}

		if _, err := f.planner.Tick(t.Context()); err != nil {
			t.Fatalf("Tick() = %v", err)
		}

		if got, ok := metricstest.Value(t, f.reg, "snatcharr_planner_deferrals_total", "reason", metrics.DeferralAlreadyRunning); !ok || got != 1 {
			t.Errorf("deferrals{already_running} = %v (present %t), want 1", got, ok)
		}
	})
}

// F4: Stamina refresh on an instance without a policy row must be read-only and create no policy row.
func TestStateLoop_RefreshInstanceStamina_CreatesNoPolicyRow(t *testing.T) {
	t.Parallel()
	f := newStateLoopFixture(t)
	instID := storetest.Instance(t, f.st, f.clk.Now())

	if _, err := f.st.Q().GetPolicy(t.Context(), instID); err == nil {
		t.Fatal("GetPolicy before tick succeeded, want ErrNoRows")
	}

	if err := f.loop.Tick(t.Context()); err != nil {
		t.Fatalf("Tick() = %v", err)
	}

	if _, err := f.st.Q().GetPolicy(t.Context(), instID); err == nil {
		t.Fatal("GetPolicy after tick succeeded: metrics loop must be read-only and not create a policy row")
	}
}

// F5: Stale series: stamina gauges must remove series for instances disabled between ticks.
func TestStateLoop_RefreshStamina_RemovesSeriesForDisabledInstance(t *testing.T) {
	t.Parallel()
	f := newStateLoopFixture(t)
	instID := storetest.Instance(t, f.st, f.clk.Now())
	inst, err := f.st.Q().GetInstance(t.Context(), instID)
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}

	if err = f.loop.Tick(t.Context()); err != nil {
		t.Fatalf("Tick 1 = %v", err)
	}
	if _, ok := metricstest.Value(t, f.reg, "snatcharr_stamina_remaining_items", "arr_instance", inst.Name); !ok {
		t.Fatalf("snatcharr_stamina_remaining_items{%s} not found after tick 1", inst.Name)
	}

	_, err = f.st.Q().UpdateInstance(t.Context(), sqlcgen.UpdateInstanceParams{
		ID:        instID,
		Name:      inst.Name,
		Kind:      inst.Kind,
		BaseUrl:   inst.BaseUrl,
		ApiKeyEnc: inst.ApiKeyEnc,
		Enabled:   false,
		UpdatedAt: f.clk.Now(),
	})
	if err != nil {
		t.Fatalf("UpdateInstance disabled: %v", err)
	}

	if err := f.loop.Tick(t.Context()); err != nil {
		t.Fatalf("Tick 2 = %v", err)
	}
	if _, ok := metricstest.Value(t, f.reg, "snatcharr_stamina_remaining_items", "arr_instance", inst.Name); ok {
		t.Errorf("snatcharr_stamina_remaining_items{%s} still present after instance was disabled", inst.Name)
	}
	if _, ok := metricstest.Value(t, f.reg, "snatcharr_stamina_used_items", "arr_instance", inst.Name); ok {
		t.Errorf("snatcharr_stamina_used_items{%s} still present after instance was disabled", inst.Name)
	}
}

const (
	lastRunMetric  = "snatcharr_instance_last_run_finished_timestamp_seconds"
	intervalMetric = "snatcharr_instance_run_interval_seconds"
)

func TestStateLoop_InstanceRuns(t *testing.T) {
	t.Parallel()
	t.Run("positive and restart-proof", testInstanceRunsPositiveAndRestart)
	t.Run("negative", testInstanceRunsNegative)
	t.Run("boundary", testInstanceRunsBoundary)
	t.Run("schedule closed skips interval", testInstanceRunsScheduleClosed)
}

func TestStateLoop_InstanceRuns_StashExcluded(t *testing.T) {
	t.Parallel()
	f := newStateLoopFixture(t)
	stashID := uuid.New()
	_, err := f.st.Pool().Exec(t.Context(), `
		INSERT INTO instances (id, kind, name, base_url, api_key_enc, enabled, source, created_at, updated_at)
		VALUES ($1, 'stash', $2, 'http://stash.invalid', 'placeholder', true, 'manual', $3, $3)
	`, stashID, "stash-"+stashID.String(), f.clk.Now())
	if err != nil {
		t.Fatalf("insert stash instance: %v", err)
	}
	name := "stash-" + stashID.String()
	insertTestPolicy(t, f.st, stashID, 1, 900, f.clk.Now())
	insertTestRun(t, f.st, uuid.New(), stashID, "done", f.clk.Now())

	if err := f.loop.Tick(t.Context()); err != nil {
		t.Fatalf("Tick() = %v", err)
	}
	assertNoMetric(t, f.reg, lastRunMetric, map[string]string{"arr_instance": name, "outcome": "done"})
	assertNoMetric(t, f.reg, intervalMetric, map[string]string{"arr_instance": name})
}

func testInstanceRunsPositiveAndRestart(t *testing.T) {
	t.Parallel()
	f := newStateLoopFixture(t)
	instID := storetest.Instance(t, f.st, f.clk.Now())
	inst, err := f.st.Q().GetInstance(t.Context(), instID)
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}

	tDone := f.clk.Now().Add(-10 * time.Minute)
	tFailed := f.clk.Now().Add(-5 * time.Minute)

	insertTestPolicy(t, f.st, instID, 1, 1800, f.clk.Now())
	insertTestRun(t, f.st, uuid.New(), instID, "done", tDone)
	insertTestRun(t, f.st, uuid.New(), instID, "failed", tFailed)

	if err := f.loop.Tick(t.Context()); err != nil {
		t.Fatalf("Tick() = %v", err)
	}

	cases := []metricTestCase{
		{"done run", lastRunMetric, map[string]string{"arr_instance": inst.Name, "outcome": "done"}, float64(tDone.Unix())},
		{"failed run", lastRunMetric, map[string]string{"arr_instance": inst.Name, "outcome": "failed"}, float64(tFailed.Unix())},
		{"interval", intervalMetric, map[string]string{"arr_instance": inst.Name}, 1800},
	}
	assertMetricValues(t, f.reg, cases)

	// Restart-proof: second fresh StateLoop with new registry, same DB, Tick, same value.
	m2, reg2 := metricstest.New(t)
	loop2 := &StateLoop{
		st: f.st, clk: f.clk, budget: f.budget, planner: f.planner,
		settings: f.settings, logger: f.loop.logger, m: m2,
	}
	if err := loop2.Tick(t.Context()); err != nil {
		t.Fatalf("loop2.Tick() = %v", err)
	}
	assertMetricValues(t, reg2, cases)
}

func testInstanceRunsNegative(t *testing.T) {
	t.Parallel()
	f := newStateLoopFixture(t)
	instID := storetest.Instance(t, f.st, f.clk.Now())
	inst, err := f.st.Q().GetInstance(t.Context(), instID)
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}

	insertTestPolicy(t, f.st, instID, 1, 900, f.clk.Now())
	insertTestRun(t, f.st, uuid.New(), instID, "done", f.clk.Now())

	if err = f.loop.Tick(t.Context()); err != nil {
		t.Fatalf("Tick 1: %v", err)
	}
	if _, ok := metricstest.ValueLabels(t, f.reg, lastRunMetric, map[string]string{"arr_instance": inst.Name, "outcome": "done"}); !ok {
		t.Fatal("lastRunMetric missing before disable")
	}

	// Disabled instance has no series after Tick (series set before disable vanishes).
	setInstanceEnabled(t, f.st, instID, false, f.clk.Now())
	if err = f.loop.Tick(t.Context()); err != nil {
		t.Fatalf("Tick 2: %v", err)
	}
	assertNoMetric(t, f.reg, lastRunMetric, map[string]string{"arr_instance": inst.Name, "outcome": "done"})
	assertNoMetric(t, f.reg, intervalMetric, map[string]string{"arr_instance": inst.Name})

	// Queued and leased runs (finished_at NULL) give no series.
	instQID := storetest.Instance(t, f.st, f.clk.Now())
	instQ, err := f.st.Q().GetInstance(t.Context(), instQID)
	if err != nil {
		t.Fatalf("GetInstance instQ: %v", err)
	}
	insertTestRunNullFinished(t, f.st, uuid.New(), instQID, "queued")
	insertTestRunNullFinished(t, f.st, uuid.New(), instQID, "leased")

	// Policy with missing_per_cycle=0 and upgrade_per_cycle=0 gives no interval series.
	instZeroID := storetest.Instance(t, f.st, f.clk.Now())
	instZero, err := f.st.Q().GetInstance(t.Context(), instZeroID)
	if err != nil {
		t.Fatalf("GetInstance instZero: %v", err)
	}
	insertTestPolicy(t, f.st, instZeroID, 0, 1200, f.clk.Now())

	if err = f.loop.Tick(t.Context()); err != nil {
		t.Fatalf("Tick 3: %v", err)
	}
	assertNoMetric(t, f.reg, lastRunMetric, map[string]string{"arr_instance": instQ.Name, "outcome": "queued"})
	assertNoMetric(t, f.reg, lastRunMetric, map[string]string{"arr_instance": instQ.Name, "outcome": "leased"})
	assertNoMetric(t, f.reg, intervalMetric, map[string]string{"arr_instance": instZero.Name})
}

func testInstanceRunsBoundary(t *testing.T) {
	t.Parallel()
	f := newStateLoopFixture(t)

	// Instance with no finished run has interval series but no last-run series.
	instID := storetest.Instance(t, f.st, f.clk.Now())
	inst, err := f.st.Q().GetInstance(t.Context(), instID)
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}
	insertTestPolicy(t, f.st, instID, 2, 600, f.clk.Now())

	if err := f.loop.Tick(t.Context()); err != nil {
		t.Fatalf("Tick 1: %v", err)
	}
	if got, ok := metricstest.ValueLabels(t, f.reg, intervalMetric, map[string]string{"arr_instance": inst.Name}); !ok || got != 600 {
		t.Errorf("intervalMetric = %v (present %t), want 600", got, ok)
	}
	assertNoMetric(t, f.reg, lastRunMetric, map[string]string{"arr_instance": inst.Name, "outcome": "done"})

	// Latest of two done runs wins.
	t1 := f.clk.Now().Add(-20 * time.Minute)
	t2 := f.clk.Now().Add(-5 * time.Minute)
	insertTestRun(t, f.st, uuid.New(), instID, "done", t1)
	insertTestRun(t, f.st, uuid.New(), instID, "done", t2)

	if err := f.loop.Tick(t.Context()); err != nil {
		t.Fatalf("Tick 2: %v", err)
	}
	if got, ok := metricstest.ValueLabels(t, f.reg, lastRunMetric, map[string]string{"arr_instance": inst.Name, "outcome": "done"}); !ok || got != float64(t2.Unix()) {
		t.Errorf("latest done run = %v (present %t), want %v", got, ok, float64(t2.Unix()))
	}
}

func testInstanceRunsScheduleClosed(t *testing.T) {
	t.Parallel()
	f := newStateLoopFixture(t)
	instID := storetest.Instance(t, f.st, f.clk.Now())
	inst, err := f.st.Q().GetInstance(t.Context(), instID)
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}
	insertTestPolicy(t, f.st, instID, 1, 900, f.clk.Now())
	pauseStateLoopAlways(t, f.st, f.clk.Now())

	if err := f.loop.Tick(t.Context()); err != nil {
		t.Fatalf("Tick(): %v", err)
	}
	assertNoMetric(t, f.reg, intervalMetric, map[string]string{"arr_instance": inst.Name})
}

func pauseStateLoopAlways(t *testing.T, st *store.Store, now time.Time) {
	t.Helper()
	_, err := st.Pool().Exec(t.Context(), `
		INSERT INTO schedules (id, name, days_mask, start_time, end_time, tz, action, enabled, created_at, updated_at)
		VALUES ($1, 'always paused', 127, '00:00:00'::time, '24:00:00'::time, 'UTC', 'pause', true, $2, $2)
	`, uuid.New(), now)
	if err != nil {
		t.Fatalf("create pause schedule: %v", err)
	}
}

type metricTestCase struct {
	name   string
	metric string
	labels map[string]string
	want   float64
}

func assertMetricValues(t *testing.T, reg prometheus.Gatherer, cases []metricTestCase) {
	t.Helper()
	for _, tc := range cases {
		got, ok := metricstest.ValueLabels(t, reg, tc.metric, tc.labels)
		if !ok || got != tc.want {
			t.Errorf("%s: ValueLabels(%s, %v) = %v (present %t), want %v", tc.name, tc.metric, tc.labels, got, ok, tc.want)
		}
	}
}

func assertNoMetric(t *testing.T, reg prometheus.Gatherer, metric string, labels map[string]string) {
	t.Helper()
	if _, ok := metricstest.ValueLabels(t, reg, metric, labels); ok {
		t.Errorf("metric %s%v unexpectedly present", metric, labels)
	}
}

func insertTestPolicy(t *testing.T, st *store.Store, instID uuid.UUID, missing, interval int, now time.Time) {
	t.Helper()
	_, err := st.Pool().Exec(t.Context(), `
		INSERT INTO snatch_policies (instance_id, missing_per_cycle, upgrade_per_cycle, cycle_interval_s, updated_at)
		VALUES ($1, $2, 0, $3, $4)
		ON CONFLICT (instance_id) DO UPDATE
		SET missing_per_cycle = $2, upgrade_per_cycle = 0, cycle_interval_s = $3, updated_at = $4
	`, instID, missing, interval, now)
	if err != nil {
		t.Fatalf("insert policy: %v", err)
	}
}

func insertTestRun(t *testing.T, st *store.Store, runID, instID uuid.UUID, status string, finishedAt time.Time) {
	t.Helper()
	_, err := st.Pool().Exec(t.Context(), `
		INSERT INTO snatch_runs (id, instance_id, kind, status, queued_at, finished_at)
		VALUES ($1, $2, 'missing', $3, $4, $5)
	`, runID, instID, status, finishedAt, finishedAt)
	if err != nil {
		t.Fatalf("insert run: %v", err)
	}
}

func insertTestRunNullFinished(t *testing.T, st *store.Store, runID, instID uuid.UUID, status string) {
	t.Helper()
	_, err := st.Pool().Exec(t.Context(), `
		INSERT INTO snatch_runs (id, instance_id, kind, status, queued_at, finished_at)
		VALUES ($1, $2, 'missing', $3, NOW(), NULL)
	`, runID, instID, status)
	if err != nil {
		t.Fatalf("insert run with null finished: %v", err)
	}
}

func setInstanceEnabled(t *testing.T, st *store.Store, instID uuid.UUID, enabled bool, now time.Time) {
	t.Helper()
	_, err := st.Pool().Exec(t.Context(), `
		UPDATE instances SET enabled = $2, updated_at = $3 WHERE id = $1
	`, instID, enabled, now)
	if err != nil {
		t.Fatalf("update instance enabled: %v", err)
	}
}
