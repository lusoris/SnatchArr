// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package metrics_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/golusoris/golusoris/observability/metricdef"

	"github.com/lusoris/SnatchArr/metadata-proxy/internal/config"
	"github.com/lusoris/SnatchArr/metadata-proxy/internal/metrics"
)

func TestCatalogShapeAndLabelLimits(t *testing.T) {
	t.Parallel()

	cat, err := metrics.NewCatalog([]string{"sonarr", "radarr"}, []string{"skyhook.sonarr.tv", ".radarr.video"})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}

	defs := cat.Defs()
	if len(defs) != 4 {
		t.Fatalf("len(Defs()) = %d, want 4", len(defs))
	}

	wantNames := []string{
		metrics.MetricRefusedTunnelsTotal,
		metrics.MetricTunnelBytesTotal,
		metrics.MetricTunnelDurationSeconds,
		metrics.MetricTunnelsTotal,
	}
	gotNames := make([]string, 0, len(defs))
	for _, d := range defs {
		gotNames = append(gotNames, d.Name)
		for _, label := range d.Labels {
			lim, ok := d.Limits[label]
			if !ok || len(lim.Allow) == 0 {
				t.Fatalf("metric %s label %s missing Allow limit: %+v", d.Name, label, lim)
			}
		}
	}
	if !slices.Equal(gotNames, wantNames) {
		t.Fatalf("metric names = %v, want %v", gotNames, wantNames)
	}
}

func TestRecorderFoldsUnlistedLabelsToOther(t *testing.T) {
	t.Parallel()

	opts := config.DefaultOptions()
	opts.Listeners = []config.Listener{{App: "sonarr", Port: 8081}}
	opts.AllowHosts = []string{"skyhook.sonarr.tv"}
	compiled, err := config.Compile(opts)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	reg := prometheus.NewRegistry()
	rec, err := metrics.New(compiled, reg)
	if err != nil {
		t.Fatalf("metrics.New: %v", err)
	}

	ctx := t.Context()
	if err = rec.RecordTunnel(ctx, "sonarr", "skyhook.sonarr.tv", config.ModeLog, 128, 256, 20*time.Millisecond); err != nil {
		t.Fatalf("RecordTunnel allowlisted: %v", err)
	}
	if err = rec.RecordTunnel(ctx, "sonarr", "unlisted.example.org", config.ModeLog, 64, 32, 10*time.Millisecond); err != nil {
		t.Fatalf("RecordTunnel unlisted: %v", err)
	}
	if err = rec.RecordRefusal(ctx, "sonarr", metricdef.OtherValue, metrics.ResultRefusedHost, config.ModeLog, 5*time.Millisecond); err != nil {
		t.Fatalf("RecordRefusal: %v", err)
	}
	if err = rec.RecordDialFailure(ctx, "sonarr", "skyhook.sonarr.tv", metrics.ResultDialError, config.ModeLog, 15*time.Millisecond); err != nil {
		t.Fatalf("RecordDialFailure: %v", err)
	}

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	if len(families) != 4 {
		t.Fatalf("gathered %d families, want 4", len(families))
	}

	count := testutil.CollectAndCount(reg, metrics.MetricTunnelsTotal)
	if count != 4 {
		t.Fatalf("CollectAndCount(%s) = %d, want 4 series", metrics.MetricTunnelsTotal, count)
	}

	foundFoldedUnlisted := false
	for _, fam := range families {
		for _, m := range fam.GetMetric() {
			var hostVal, resultVal string
			for _, lp := range m.GetLabel() {
				if lp.GetValue() == "unlisted.example.org" {
					t.Fatalf("family %s leaked raw unlisted label %s=%q", fam.GetName(), lp.GetName(), lp.GetValue())
				}
				switch lp.GetName() {
				case "host":
					hostVal = lp.GetValue()
				case "result":
					resultVal = lp.GetValue()
				}
			}
			if fam.GetName() == metrics.MetricTunnelsTotal &&
				hostVal == metricdef.OtherValue &&
				resultVal == metrics.ResultOK {
				foundFoldedUnlisted = true
			}
		}
	}
	if !foundFoldedUnlisted {
		t.Fatalf("%s missing folded series host=%q result=%q", metrics.MetricTunnelsTotal, metricdef.OtherValue, metrics.ResultOK)
	}
}

type mismatchRegisterer struct{}

func (mismatchRegisterer) Register(c prometheus.Collector) error {
	switch c.(type) {
	case *prometheus.CounterVec:
		return prometheus.AlreadyRegisteredError{
			ExistingCollector: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "stub_total", Help: "stub"}, nil),
			NewCollector:      c,
		}
	case *prometheus.HistogramVec:
		return prometheus.AlreadyRegisteredError{
			ExistingCollector: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "stub_seconds", Help: "stub"}, nil),
			NewCollector:      c,
		}
	default:
		return nil
	}
}

func (m mismatchRegisterer) MustRegister(cs ...prometheus.Collector) {
	for _, c := range cs {
		if err := m.Register(c); err != nil {
			panic(err)
		}
	}
}

func (mismatchRegisterer) Unregister(prometheus.Collector) bool { return false }

func TestRecorderSurfacesMetricdefErrors(t *testing.T) {
	t.Parallel()

	opts := config.DefaultOptions()
	opts.Listeners = []config.Listener{{App: "sonarr", Port: 8081}}
	opts.AllowHosts = []string{"skyhook.sonarr.tv"}
	compiled, err := config.Compile(opts)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	rec, err := metrics.New(compiled, mismatchRegisterer{})
	if err != nil {
		t.Fatalf("metrics.New: %v", err)
	}

	cases := []struct {
		name        string
		call        func() error
		wantMetrics []string
	}{
		{
			name: "negative: RecordRefusal joins and returns label-count mismatch errors",
			call: func() error {
				return rec.RecordRefusal(t.Context(), "sonarr", "skyhook.sonarr.tv", metrics.ResultRefusedHost, config.ModeLog, 5*time.Millisecond)
			},
			wantMetrics: []string{metrics.MetricRefusedTunnelsTotal, metrics.MetricTunnelsTotal, metrics.MetricTunnelDurationSeconds},
		},
		{
			name: "negative: RecordDialFailure joins and returns label-count mismatch errors",
			call: func() error {
				return rec.RecordDialFailure(t.Context(), "sonarr", "skyhook.sonarr.tv", metrics.ResultDialTimeout, config.ModeLog, 5*time.Millisecond)
			},
			wantMetrics: []string{metrics.MetricTunnelsTotal, metrics.MetricTunnelDurationSeconds},
		},
		{
			name: "negative: RecordTunnel joins and returns label-count mismatch errors",
			call: func() error {
				return rec.RecordTunnel(t.Context(), "sonarr", "skyhook.sonarr.tv", config.ModeLog, 10, 20, 5*time.Millisecond)
			},
			wantMetrics: []string{metrics.MetricTunnelBytesTotal, metrics.MetricTunnelsTotal, metrics.MetricTunnelDurationSeconds},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertJoinedMetricError(t, tc.call(), tc.wantMetrics)
		})
	}
}

func assertJoinedMetricError(t *testing.T, gotErr error, wantMetrics []string) {
	t.Helper()
	if gotErr == nil {
		t.Fatal("expected label-count mismatch error, got nil")
	}
	msg := gotErr.Error()
	if !strings.Contains(msg, "inconsistent label cardinality") {
		t.Fatalf("error %q missing label cardinality message", msg)
	}
	for _, m := range wantMetrics {
		if !strings.Contains(msg, m) {
			t.Fatalf("joined error %q missing metric %q", msg, m)
		}
	}
}
