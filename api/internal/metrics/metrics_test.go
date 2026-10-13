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
	if got, _ := value(t, reg, planFailed, "instance", "sonarr-4k"); got != 2 {
		t.Errorf("%s{sonarr-4k} = %v, want 2", planFailed, got)
	}
	if got, _ := value(t, reg, planFailed, "instance", "radarr"); got != 1 {
		t.Errorf("%s{radarr} = %v, want 1", planFailed, got)
	}
	if got, ok := value(t, reg, planFailed, "instance", "lidarr"); ok {
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
	if got, _ := value(t, reg, planFailed, "instance", "sonarr"); got != 2 {
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
