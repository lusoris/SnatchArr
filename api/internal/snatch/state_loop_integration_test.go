// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package snatch

import (
	"context"
	"log/slog"
	"testing"

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
	if _, err = f.runs.Complete(t.Context(), r1.ID, "worker-1", domain.RunDone, 5, ""); err != nil {
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
