// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package metrics_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/lusoris/SnatchArr/api/internal/metrics"
	"github.com/lusoris/SnatchArr/api/internal/metrics/metricstest"
)

const (
	failures    = "snatcharr_loop_failures_total"
	lastSuccess = "snatcharr_loop_last_success_timestamp_seconds"
	interval    = "snatcharr_loop_interval_seconds"
	planFailed  = "snatcharr_planner_instance_failures_total"
)

func TestLoopLabel(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, in, want string }{
		// Positive.
		{"the planner", "snatch: planner", "snatch_planner"},
		{"the Seerr sync", "seerr: sync", "seerr_sync"},
		{"one word", "retention", "retention"},
		// Negative: upper case and other characters never reach the label.
		{"upper case", "Seerr Sync", "seerr_sync"},
		{"a run of other characters", "a -- b", "a_b"},
		// Boundary.
		{"empty", "", ""},
		{"only other characters", ": -", ""},
		{"leading and trailing others", "  job 2 ", "job_2"},
		{"non-ASCII letters", "größe", "gr_e"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := metrics.LoopLabel(tc.in); got != tc.want {
				t.Errorf("LoopLabel(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// at is a tick time with a fraction of a second, which the gauge keeps.
var at = time.Date(2026, 10, 13, 12, 0, 0, 500_000_000, time.UTC)

func value(t *testing.T, reg prometheus.Gatherer, name, label, val string) (float64, bool) {
	t.Helper()
	return metricstest.Value(t, reg, name, label, val)
}

// Positive: a tick without error sets the last success and the interval and creates the
// failure series at zero, so an alert on its increase has a base.
func TestLoopTicked_Success(t *testing.T) {
	t.Parallel()
	m, reg := metricstest.New(t)
	m.LoopTicked("snatch: planner", time.Minute, at, nil)
	if got, ok := value(t, reg, failures, "loop", "snatch_planner"); !ok || got != 0 {
		t.Errorf("%s = %v (present %t), want 0", failures, got, ok)
	}
	if got, _ := value(t, reg, lastSuccess, "loop", "snatch_planner"); got != 1_791_892_800.5 {
		t.Errorf("%s = %v, want 1791892800.5", lastSuccess, got)
	}
	if got, _ := value(t, reg, interval, "loop", "snatch_planner"); got != 60 {
		t.Errorf("%s = %v, want 60", interval, got)
	}
}

// Negative: a failed tick counts one failure and leaves the last success where it was.
func TestLoopTicked_Failure(t *testing.T) {
	t.Parallel()
	m, reg := metricstest.New(t)
	m.LoopTicked("retention", time.Hour, at, nil)
	m.LoopTicked("retention", time.Hour, at.Add(time.Hour), errors.New("purge failed"))
	m.LoopTicked("retention", time.Hour, at.Add(2*time.Hour), errors.New("purge failed"))
	if got, _ := value(t, reg, failures, "loop", "retention"); got != 2 {
		t.Errorf("%s = %v, want 2", failures, got)
	}
	if got, _ := value(t, reg, lastSuccess, "loop", "retention"); got != float64(at.UnixNano())/float64(time.Second) {
		t.Errorf("%s = %v, want the first tick's time", lastSuccess, got)
	}
}

// Boundary: a loop that never succeeded has no last-success series, and loops never share one.
func TestLoopTicked_FirstTickFails(t *testing.T) {
	t.Parallel()
	m, reg := metricstest.New(t)
	m.LoopTicked("seerr: sync", 5*time.Minute, at, errors.New("list links"))
	m.LoopTicked("retention", time.Hour, at, nil)
	if got, ok := value(t, reg, lastSuccess, "loop", "seerr_sync"); ok {
		t.Errorf("%s{seerr_sync} = %v, want no series", lastSuccess, got)
	}
	if got, _ := value(t, reg, failures, "loop", "seerr_sync"); got != 1 {
		t.Errorf("%s{seerr_sync} = %v, want 1", failures, got)
	}
	if got, _ := value(t, reg, failures, "loop", "retention"); got != 0 {
		t.Errorf("%s{retention} = %v, want 0", failures, got)
	}
}

func TestPlanFailed(t *testing.T) {
	t.Parallel()
	m, reg := metricstest.New(t)
	m.PlanFailed("sonarr-4k")
	m.PlanFailed("sonarr-4k")
	m.PlanFailed("radarr")
	if got, _ := value(t, reg, planFailed, "arr_instance", "sonarr-4k"); got != 2 {
		t.Errorf("%s{sonarr-4k} = %v, want 2", planFailed, got)
	}
	if got, _ := value(t, reg, planFailed, "arr_instance", "radarr"); got != 1 {
		t.Errorf("%s{radarr} = %v, want 1", planFailed, got)
	}
	if got, ok := value(t, reg, planFailed, "arr_instance", "lidarr"); ok {
		t.Errorf("%s{lidarr} = %v, want no series", planFailed, got)
	}
}

// Positive: a second New on the same registry (a second fx app in one test binary) reuses
// the collectors, so both instances record into one series.
func TestNew_ReusesRegisteredCollectors(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	first, err := metrics.New(reg)
	if err != nil {
		t.Fatalf("first New() = %v", err)
	}
	second, err := metrics.New(reg)
	if err != nil {
		t.Fatalf("second New() = %v", err)
	}
	first.PlanFailed("sonarr")
	second.PlanFailed("sonarr")
	if got, _ := value(t, reg, planFailed, "arr_instance", "sonarr"); got != 2 {
		t.Errorf("%s{sonarr} = %v, want 2 from both instances", planFailed, got)
	}
}

// Negative: a collector of the same name but another shape is a conflict New reports.
func TestNew_RefusesAConflictingCollector(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	reg.MustRegister(prometheus.NewGauge(prometheus.GaugeOpts{Name: planFailed, Help: "not a counter"}))
	m, err := metrics.New(reg)
	if err == nil || m != nil {
		t.Fatalf("New() = %v, %v, want nil and an error", m, err)
	}
	if !strings.HasPrefix(err.Error(), "metrics: register: ") {
		t.Errorf("New() error = %q, want the prefix \"metrics: register: \"", err)
	}
}

// Positive: New creates every series an alert watches for an increase at zero.
func TestNew_CreatesTheAlertSeriesAtZero(t *testing.T) {
	t.Parallel()
	_, reg := metricstest.New(t)
	zero := map[string][2]string{
		"queued":                 {"snatcharr_runs_queued_total", ""},
		"unconfirmed":            {"snatcharr_worker_events_unconfirmed_total", ""},
		"done":                   {"snatcharr_runs_finished_total", "done"},
		"failed":                 {"snatcharr_runs_finished_total", "failed"},
		"cancelled":              {"snatcharr_runs_finished_total", "cancelled"},
		"lease exhausted":        {"snatcharr_runs_finished_total", metrics.OutcomeLeaseExhausted},
		"granted":                {"snatcharr_budget_requests_total", metrics.BudgetGranted},
		"partial":                {"snatcharr_budget_requests_total", metrics.BudgetPartial},
		"denied":                 {"snatcharr_budget_requests_total", metrics.BudgetDenied},
		"error":                  {"snatcharr_budget_requests_total", metrics.BudgetError},
		"seerr pending":          {"snatcharr_seerr_requests_pending", ""},
		"deferral schedule":      {"snatcharr_planner_deferrals_total", metrics.DeferralScheduleClosed},
		"deferral running":       {"snatcharr_planner_deferrals_total", metrics.DeferralAlreadyRunning},
		"deferral unreach":       {"snatcharr_planner_deferrals_total", metrics.DeferralClientUnreachable},
		"deferral paused":        {"snatcharr_planner_deferrals_total", metrics.DeferralClientPaused},
		"deferral limit":         {"snatcharr_planner_deferrals_total", metrics.DeferralClientActiveLimit},
		"deferral queued limit":  {"snatcharr_planner_deferrals_total", metrics.DeferralClientQueuedLimit},
		"deferral bandwidth":     {"snatcharr_planner_deferrals_total", metrics.DeferralBandwidthSaturated},
		"deferral gate":          {"snatcharr_planner_deferrals_total", metrics.DeferralGateRefusal},
		"deferral indexer limit": {"snatcharr_planner_deferrals_total", metrics.DeferralIndexerLimit},
		"runs queued":            {"snatcharr_runs", "queued"},
		"runs leased":            {"snatcharr_runs", "leased"},
		"runs done":              {"snatcharr_runs", "done"},
		"runs failed":            {"snatcharr_runs", "failed"},
		"runs cancelled":         {"snatcharr_runs", "cancelled"},
		"lease wait missing":     {"snatcharr_run_lease_wait_seconds", "missing"},
		"lease wait upgrade":     {"snatcharr_run_lease_wait_seconds", "upgrade"},
	}
	labelFor := map[string]string{
		"snatcharr_runs_finished_total":     "outcome",
		"snatcharr_budget_requests_total":   "result",
		"snatcharr_planner_deferrals_total": "reason",
		"snatcharr_runs":                    "status",
		"snatcharr_run_lease_wait_seconds":  "kind",
	}
	for name, series := range zero {
		label := labelFor[series[0]]
		if got, ok := value(t, reg, series[0], label, series[1]); !ok || got != 0 {
			t.Errorf("%s: %s{%s} = %v (present %t), want 0", name, series[0], series[1], got, ok)
		}
	}
}

// The worker-protocol series count what they are told, each label on its own.
func TestRunAndWorkerSeries(t *testing.T) {
	t.Parallel()
	m, reg := metricstest.New(t)
	m.RunsQueued(3)
	m.RunsQueued(0)
	m.RunLeased("missing")
	m.RunLeased("missing")
	m.RunLeased("cutoff")
	m.RunFinished("failed")
	m.WorkerEventTypes("search_failed", "run_started")
	m.WorkerEvent("search_failed")
	m.BudgetRequested(metrics.BudgetDenied)
	m.EventsUnconfirmed(3)
	m.EventsUnconfirmed(0)
	cases := []struct {
		name, series, label, value string
		want                       float64
	}{
		// Positive.
		{"queued", "snatcharr_runs_queued_total", "", "", 3},
		{"leased missing", "snatcharr_runs_leased_total", "kind", "missing", 2},
		{"leased cutoff", "snatcharr_runs_leased_total", "kind", "cutoff", 1},
		{"finished failed", "snatcharr_runs_finished_total", "outcome", "failed", 1},
		{"event search_failed", "snatcharr_worker_events_total", "type", "search_failed", 1},
		{"budget denied", "snatcharr_budget_requests_total", "result", metrics.BudgetDenied, 1},
		{"unconfirmed events, a zero adds nothing", "snatcharr_worker_events_unconfirmed_total", "", "", 3},
		// Negative: another label of the same series is untouched.
		{"finished done", "snatcharr_runs_finished_total", "outcome", "done", 0},
		{"budget granted", "snatcharr_budget_requests_total", "result", metrics.BudgetGranted, 0},
		// Boundary: an event type created at zero and never reported stays zero.
		{"event run_started", "snatcharr_worker_events_total", "type", "run_started", 0},
	}
	for _, tc := range cases {
		if got, ok := value(t, reg, tc.series, tc.label, tc.value); !ok || got != tc.want {
			t.Errorf("%s: %s{%s} = %v (present %t), want %v", tc.name, tc.series, tc.value, got, ok, tc.want)
		}
	}
	if got, ok := value(t, reg, "snatcharr_worker_events_total", "type", "run_finished"); ok {
		t.Errorf("worker events{run_finished} = %v, want no series without WorkerEventTypes", got)
	}
}

func TestSearchesDispatched(t *testing.T) {
	t.Parallel()
	m, reg := metricstest.New(t)
	m.SearchesDispatched("sonarr-4k", "missing", 5)
	m.SearchesDispatched("sonarr-4k", "missing", 0) // boundary <= 0 defaults to 1
	m.SearchesDispatched("radarr", "upgrade", 2)

	cases := []struct {
		name     string
		labels   map[string]string
		want     float64
		expected bool
	}{
		{"positive sonarr missing", map[string]string{"arr_instance": "sonarr-4k", "kind": "missing"}, 6, true},
		{"positive radarr upgrade", map[string]string{"arr_instance": "radarr", "kind": "upgrade"}, 2, true},
		{"negative lidarr missing", map[string]string{"arr_instance": "lidarr", "kind": "missing"}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := metricstest.ValueLabels(t, reg, "snatcharr_searches_dispatched_total", tc.labels)
			if ok != tc.expected {
				t.Fatalf("ValueLabels() present = %t, want %t", ok, tc.expected)
			}
			if ok && got != tc.want {
				t.Errorf("searches dispatched = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestStaminaGauges(t *testing.T) {
	t.Parallel()
	m, reg := metricstest.New(t)
	m.SetStaminaUsed("sonarr", 15)
	m.SetStaminaRemaining("sonarr", 5)
	m.SetStaminaUsed("_global", 50)
	m.SetStaminaRemaining("_global", 50)

	cases := []struct {
		name, series, label, value string
		want                       float64
	}{
		{"positive used sonarr", "snatcharr_stamina_used_items", "arr_instance", "sonarr", 15},
		{"positive remaining sonarr", "snatcharr_stamina_remaining_items", "arr_instance", "sonarr", 5},
		{"positive used global", "snatcharr_stamina_used_items", "arr_instance", "_global", 50},
		{"positive remaining global", "snatcharr_stamina_remaining_items", "arr_instance", "_global", 50},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := value(t, reg, tc.series, tc.label, tc.value)
			if !ok || got != tc.want {
				t.Errorf("%s = %v (present %t), want %v", tc.series, got, ok, tc.want)
			}
		})
	}
}

func TestRunsGauge(t *testing.T) {
	t.Parallel()
	m, reg := metricstest.New(t)
	m.SetRuns("queued", 3)
	m.SetRuns("leased", 1)
	m.SetRuns("done", 10)

	cases := []struct {
		name, status string
		want         float64
	}{
		{"queued", "queued", 3},
		{"leased", "leased", 1},
		{"done", "done", 10},
		{"failed zero-filled", "failed", 0},
		{"cancelled zero-filled", "cancelled", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := value(t, reg, "snatcharr_runs", "status", tc.status)
			if !ok || got != tc.want {
				t.Errorf("runs{%s} = %v (present %t), want %v", tc.status, got, ok, tc.want)
			}
		})
	}
}

func TestSeerrRequestsPending(t *testing.T) {
	t.Parallel()
	m, reg := metricstest.New(t)
	if got, ok := value(t, reg, "snatcharr_seerr_requests_pending", "", ""); !ok || got != 0 {
		t.Fatalf("initial seerr pending = %v (present %t), want 0", got, ok)
	}
	m.SetSeerrRequestsPending(42)
	if got, ok := value(t, reg, "snatcharr_seerr_requests_pending", "", ""); !ok || got != 42 {
		t.Errorf("seerr pending = %v (present %t), want 42", got, ok)
	}
}

func TestRunLeaseWait(t *testing.T) {
	t.Parallel()
	m, reg := metricstest.New(t)
	m.RunLeaseWait("missing", 2.5)
	m.RunLeaseWait("missing", 10.0)
	m.RunLeaseWait("upgrade", -1.0) // negative clamped to 0

	if got, ok := value(t, reg, "snatcharr_run_lease_wait_seconds", "kind", "missing"); !ok || got != 2 {
		t.Errorf("missing wait count = %v (present %t), want 2", got, ok)
	}
	if got, ok := value(t, reg, "snatcharr_run_lease_wait_seconds", "kind", "upgrade"); !ok || got != 1 {
		t.Errorf("upgrade wait count = %v (present %t), want 1", got, ok)
	}
}

func TestPlannerDeferred(t *testing.T) {
	t.Parallel()
	m, reg := metricstest.New(t)
	m.PlannerDeferred(metrics.DeferralScheduleClosed)
	m.PlannerDeferred(metrics.DeferralScheduleClosed)
	m.PlannerDeferred(metrics.DeferralClientPaused)

	cases := []struct {
		reason string
		want   float64
	}{
		{metrics.DeferralScheduleClosed, 2},
		{metrics.DeferralClientPaused, 1},
		{metrics.DeferralAlreadyRunning, 0},
		{metrics.DeferralClientUnreachable, 0},
	}
	for _, tc := range cases {
		t.Run(tc.reason, func(t *testing.T) {
			t.Parallel()
			got, ok := value(t, reg, "snatcharr_planner_deferrals_total", "reason", tc.reason)
			if !ok || got != tc.want {
				t.Errorf("deferral{%s} = %v (present %t), want %v", tc.reason, got, ok, tc.want)
			}
		})
	}
}

func TestSearchDispatchedAt(t *testing.T) {
	t.Parallel()
	m, reg := metricstest.New(t)
	const metric = "snatcharr_search_last_dispatched_timestamp_seconds"
	t1 := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 10, 19, 11, 30, 0, 0, time.UTC)

	cases := []struct {
		name     string
		instance string
		at       time.Time
		want     float64
	}{
		{"positive first instance", "sonarr-main", t1, float64(t1.Unix())},
		{"positive second instance", "radarr-4k", t2, float64(t2.Unix())},
		{"update existing instance", "sonarr-main", t2, float64(t2.Unix())},
	}
	for _, tc := range cases {
		m.SearchDispatchedAt(tc.instance, tc.at)
		got, ok := value(t, reg, metric, "arr_instance", tc.instance)
		if !ok || got != tc.want {
			t.Errorf("%s{%s} = %v (present %t), want %v", metric, tc.instance, got, ok, tc.want)
		}
	}

	// Boundary: nil receiver does not panic.
	var nilM *metrics.Metrics
	nilM.SearchDispatchedAt("sonarr-main", t1)
}
