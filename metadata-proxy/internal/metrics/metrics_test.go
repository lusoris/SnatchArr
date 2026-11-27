// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package metrics_test

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
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

const maxHISSFuncLines = 60

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
	if err = rec.RecordTunnel(ctx, "sonarr", "skyhook.sonarr.tv", metrics.ResultOK, config.ModeLog, 128, 256, 20*time.Millisecond); err != nil {
		t.Fatalf("RecordTunnel allowlisted: %v", err)
	}
	if err = rec.RecordTunnel(ctx, "sonarr", "unlisted.example.org", metrics.ResultOK, config.ModeLog, 64, 32, 10*time.Millisecond); err != nil {
		t.Fatalf("RecordTunnel unlisted: %v", err)
	}
	if err = rec.RecordRefusal(ctx, "sonarr", metricdef.OtherValue, metrics.ResultRefusedHost, config.ModeLog, 5*time.Millisecond); err != nil {
		t.Fatalf("RecordRefusal: %v", err)
	}
	if err = rec.RecordDialFailure(ctx, "sonarr", "skyhook.sonarr.tv", metrics.ResultDialError, config.ModeLog, 15*time.Millisecond); err != nil {
		t.Fatalf("RecordDialFailure: %v", err)
	}

	if count := testutil.CollectAndCount(reg, metrics.MetricTunnelsTotal); count != 4 {
		t.Fatalf("CollectAndCount(%s) = %d, want 4 series", metrics.MetricTunnelsTotal, count)
	}
	assertFoldedUnlistedHost(t, reg, "unlisted.example.org", 4)
}

func assertFoldedUnlistedHost(t *testing.T, reg prometheus.Gatherer, unlistedHost string, wantFamilies int) {
	t.Helper()
	if err := verifyFoldedUnlistedHost(reg, unlistedHost, wantFamilies); err != nil {
		t.Fatal(err)
	}
}

func verifyFoldedUnlistedHost(reg prometheus.Gatherer, unlistedHost string, wantFamilies int) error {
	families, err := reg.Gather()
	if err != nil {
		return fmt.Errorf("gather: %w", err)
	}
	if wantFamilies > 0 && len(families) != wantFamilies {
		return fmt.Errorf("gathered %d families, want %d", len(families), wantFamilies)
	}
	foundFolded := false
	for _, fam := range families {
		for _, m := range fam.GetMetric() {
			var hostVal, resultVal string
			for _, lp := range m.GetLabel() {
				if lp.GetValue() == unlistedHost {
					return fmt.Errorf("family %s leaked raw unlisted label %s=%q", fam.GetName(), lp.GetName(), lp.GetValue())
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
				foundFolded = true
			}
		}
	}
	if !foundFolded {
		return fmt.Errorf("%s missing folded series host=%q result=%q", metrics.MetricTunnelsTotal, metricdef.OtherValue, metrics.ResultOK)
	}
	return nil
}

func TestVerifyFoldedUnlistedHostHelper(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		setup   func(*prometheus.Registry)
		wantErr string
	}{
		{
			name: "positive: folded other ok present and raw host absent",
			setup: func(reg *prometheus.Registry) {
				cv := prometheus.NewCounterVec(prometheus.CounterOpts{Name: metrics.MetricTunnelsTotal, Help: "h"}, []string{"host", "result"})
				reg.MustRegister(cv)
				cv.WithLabelValues(metricdef.OtherValue, metrics.ResultOK).Inc()
			},
		},
		{
			name: "negative: raw unlisted host leaked into series",
			setup: func(reg *prometheus.Registry) {
				cv := prometheus.NewCounterVec(prometheus.CounterOpts{Name: metrics.MetricTunnelsTotal, Help: "h"}, []string{"host", "result"})
				reg.MustRegister(cv)
				cv.WithLabelValues("unlisted.example.org", metrics.ResultOK).Inc()
			},
			wantErr: "leaked raw unlisted label",
		},
		{
			name: "boundary: empty scrape missing folded series",
			setup: func(*prometheus.Registry) {
			},
			wantErr: "missing folded series",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reg := prometheus.NewRegistry()
			tc.setup(reg)
			err := verifyFoldedUnlistedHost(reg, "unlisted.example.org", 0)
			if tc.wantErr == "" && err != nil {
				t.Fatalf("verifyFoldedUnlistedHost unexpected error: %v", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("verifyFoldedUnlistedHost error = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestFunctionLineSpanWithinHISSLimit(t *testing.T) {
	t.Parallel()

	buildSyntheticGo := func(totalLines int) string {
		var b strings.Builder
		b.WriteString("package synthetic\nfunc sample() {\n")
		for range totalLines - 2 {
			b.WriteString("\t_ = 1\n")
		}
		b.WriteString("}\n")
		return b.String()
	}

	cases := []struct {
		name    string
		path    string
		src     string
		wantErr bool
	}{
		{name: "positive: metrics.go functions <= 60 lines", path: "metrics.go"},
		{name: "positive: metrics_test.go functions <= 60 lines", path: "metrics_test.go"},
		{name: "boundary: 60-line function passes", path: "boundary.go", src: buildSyntheticGo(maxHISSFuncLines)},
		{name: "negative: 61-line function fails", path: "over.go", src: buildSyntheticGo(maxHISSFuncLines + 1), wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := checkFileFuncLengths(tc.path, tc.src, maxHISSFuncLines)
			if tc.wantErr && err == nil {
				t.Fatalf("checkFileFuncLengths(%s) = nil, want error", tc.path)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("checkFileFuncLengths(%s): %v", tc.path, err)
			}
		})
	}
}

func checkFileFuncLengths(filename, inlineSrc string, maxLines int) error {
	fset := token.NewFileSet()
	var (
		file *ast.File
		err  error
	)
	if inlineSrc != "" {
		file, err = parser.ParseFile(fset, filename, inlineSrc, 0)
	} else {
		file, err = parser.ParseFile(fset, filename, nil, 0)
	}
	if err != nil {
		return fmt.Errorf("parse %s: %w", filename, err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		span := fset.Position(fn.End()).Line - fset.Position(fn.Pos()).Line + 1
		if span > maxLines {
			return fmt.Errorf("%s:%d: func %s spans %d lines (max %d)", filename, fset.Position(fn.Pos()).Line, fn.Name.Name, span, maxLines)
		}
	}
	return nil
}

func TestReservedOtherHostRejected(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		allowHosts []string
		wantErr    error
	}{
		{
			name:       "positive: provider hosts accepted",
			allowHosts: []string{"skyhook.sonarr.tv", ".radarr.video"},
		},
		{
			name:       "positive: empty allow_hosts in log mode accepted",
			allowHosts: nil,
		},
		{
			name:       "boundary: leading-dot .other suffix does not collide with exact other",
			allowHosts: []string{".other"},
		},
		{
			name:       "negative: exact other rejected",
			allowHosts: []string{"other"},
			wantErr:    metrics.ErrReservedHost,
		},
		{
			name:       "negative: mixed-case trailing-dot OTHER. rejected",
			allowHosts: []string{"skyhook.sonarr.tv", "OTHER."},
			wantErr:    metrics.ErrReservedHost,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertReservedHostCase(t, tc.allowHosts, tc.wantErr)
		})
	}
}

func assertReservedHostCase(t *testing.T, allowHosts []string, wantErr error) {
	t.Helper()
	_, catErr := metrics.NewCatalog([]string{"sonarr"}, allowHosts)
	if wantErr == nil && catErr != nil {
		t.Fatalf("NewCatalog(%v) unexpected error: %v", allowHosts, catErr)
	}
	if wantErr != nil && !errors.Is(catErr, wantErr) {
		t.Fatalf("NewCatalog(%v) error = %v, want errors.Is(_, %v)", allowHosts, catErr, wantErr)
	}

	opts := config.DefaultOptions()
	opts.Listeners = []config.Listener{{App: "sonarr", Port: 8081}}
	opts.AllowHosts = allowHosts
	compiled, err := config.Compile(opts)
	if err != nil {
		t.Fatalf("config.Compile(%v): %v", allowHosts, err)
	}
	_, recErr := metrics.New(compiled, prometheus.NewRegistry())
	if wantErr == nil && recErr != nil {
		t.Fatalf("metrics.New(%v) unexpected error: %v", allowHosts, recErr)
	}
	if wantErr != nil && !errors.Is(recErr, wantErr) {
		t.Fatalf("metrics.New(%v) error = %v, want errors.Is(_, %v)", allowHosts, recErr, wantErr)
	}
}

func TestTunnelDurationBucketsCoverLongLivedTunnels(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		dur        time.Duration
		lowerBound float64
		upperBound float64
	}{
		{
			name:       "positive: subsecond refusal lands in subsecond finite bucket",
			dur:        20 * time.Millisecond,
			lowerBound: 0.01,
			upperBound: 0.025,
		},
		{
			name:       "positive: 20s tunnel lands in 30s bucket above 10s",
			dur:        20 * time.Second,
			lowerBound: 10,
			upperBound: 30,
		},
		{
			name:       "positive: 120s idle tunnel lands in 120s bucket separate from 20s",
			dur:        120 * time.Second,
			lowerBound: 60,
			upperBound: 120,
		},
		{
			name:       "boundary: 30m default max_tunnel lands in 1800s finite bucket",
			dur:        30 * time.Minute,
			lowerBound: 600,
			upperBound: 1800,
		},
		{
			name:       "negative: 2h duration exceeds highest finite bucket",
			dur:        2 * time.Hour,
			lowerBound: 3600,
			upperBound: -1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertHistogramBucketPlacement(t, tc.dur, tc.lowerBound, tc.upperBound)
		})
	}
}

func assertHistogramBucketPlacement(t *testing.T, dur time.Duration, lowerBound, upperBound float64) {
	t.Helper()
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
	if err = rec.RecordTunnel(t.Context(), "sonarr", "skyhook.sonarr.tv", metrics.ResultOK, config.ModeLog, 1, 1, dur); err != nil {
		t.Fatalf("RecordTunnel(%s): %v", dur, err)
	}
	buckets := readDurationBuckets(t, reg)
	if gotLow, ok := buckets[lowerBound]; !ok || gotLow != 0 {
		t.Fatalf("dur=%s bucket le=%v count=(%d, exists=%v), want (0, true)", dur, lowerBound, gotLow, ok)
	}
	if upperBound < 0 {
		return
	}
	if gotHigh, ok := buckets[upperBound]; !ok || gotHigh != 1 {
		t.Fatalf("dur=%s bucket le=%v count=(%d, exists=%v), want (1, true)", dur, upperBound, gotHigh, ok)
	}
}

func readDurationBuckets(t *testing.T, reg *prometheus.Registry) map[float64]uint64 {
	t.Helper()
	fams, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	out := make(map[float64]uint64)
	for _, fam := range fams {
		if fam.GetName() != metrics.MetricTunnelDurationSeconds {
			continue
		}
		for _, m := range fam.GetMetric() {
			for _, b := range m.GetHistogram().GetBucket() {
				out[b.GetUpperBound()] = b.GetCumulativeCount()
			}
		}
	}
	return out
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
				return rec.RecordTunnel(t.Context(), "sonarr", "skyhook.sonarr.tv", metrics.ResultOK, config.ModeLog, 10, 20, 5*time.Millisecond)
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

type tunnelRecordCase struct {
	name       string
	result     string
	mode       config.Mode
	up         int64
	down       int64
	dur        time.Duration
	wantResult string
	wantUp     float64
	wantDown   float64
}

func TestTransferErrorCatalogAndRecordTunnel(t *testing.T) {
	t.Parallel()
	assertTransferErrorInAllResultsAndCatalog(t)

	cases := []tunnelRecordCase{
		{
			name:       "positive: transfer_error increments tunnel counter duration and partial up bytes",
			result:     metrics.ResultTransferError,
			mode:       config.ModeEnforce,
			up:         48,
			down:       0,
			dur:        25 * time.Millisecond,
			wantResult: metrics.ResultTransferError,
			wantUp:     48,
			wantDown:   0,
		},
		{
			name:       "negative: ok tunnel records ok result distinct from transfer_error",
			result:     metrics.ResultOK,
			mode:       config.ModeLog,
			up:         16,
			down:       32,
			dur:        15 * time.Millisecond,
			wantResult: metrics.ResultOK,
			wantUp:     16,
			wantDown:   32,
		},
		{
			name:       "boundary: transfer_error with zero bytes and non-positive duration clamps to zero",
			result:     metrics.ResultTransferError,
			mode:       config.ModeLog,
			up:         0,
			down:       0,
			dur:        -5 * time.Millisecond,
			wantResult: metrics.ResultTransferError,
			wantUp:     0,
			wantDown:   0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertRecordedTunnelCase(t, tc)
		})
	}
}

func assertTransferErrorInAllResultsAndCatalog(t *testing.T) {
	t.Helper()
	got := metrics.AllResults()
	want := []string{
		metrics.ResultOK,
		metrics.ResultRefusedMethod,
		metrics.ResultRefusedTarget,
		metrics.ResultRefusedHost,
		metrics.ResultRefusedPort,
		metrics.ResultRefusedIP,
		metrics.ResultRefusedLimit,
		metrics.ResultDialTimeout,
		metrics.ResultDialError,
		metrics.ResultTransferError,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("AllResults() = %v, want %v", got, want)
	}
	cat, err := metrics.NewCatalog([]string{"sonarr"}, []string{"skyhook.sonarr.tv"})
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	for _, d := range cat.Defs() {
		if lim, hasResult := d.Limits["result"]; hasResult && !slices.Equal(lim.Allow, want) {
			t.Fatalf("metric %s result Allow = %v, want %v", d.Name, lim.Allow, want)
		}
	}
}

func assertRecordedTunnelCase(t *testing.T, tc tunnelRecordCase) {
	t.Helper()
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
	if err = rec.RecordTunnel(t.Context(), "sonarr", "skyhook.sonarr.tv", tc.result, tc.mode, tc.up, tc.down, tc.dur); err != nil {
		t.Fatalf("RecordTunnel: %v", err)
	}
	wantTunnels := fmt.Sprintf("# HELP %s Total CONNECT requests handled by the metadata proxy, partitioned by outcome.\n# TYPE %s counter\n%s{app=\"sonarr\",host=\"skyhook.sonarr.tv\",mode=%q,result=%q} 1\n",
		metrics.MetricTunnelsTotal, metrics.MetricTunnelsTotal, metrics.MetricTunnelsTotal, tc.mode, tc.wantResult)
	if err = testutil.GatherAndCompare(reg, strings.NewReader(wantTunnels), metrics.MetricTunnelsTotal); err != nil {
		t.Fatalf("GatherAndCompare(%s): %v", metrics.MetricTunnelsTotal, err)
	}
	wantBytes := fmt.Sprintf("# HELP %s Total bytes copied through established CONNECT tunnels.\n# TYPE %s counter\n%s{app=\"sonarr\",direction=\"down\",host=\"skyhook.sonarr.tv\"} %g\n%s{app=\"sonarr\",direction=\"up\",host=\"skyhook.sonarr.tv\"} %g\n",
		metrics.MetricTunnelBytesTotal, metrics.MetricTunnelBytesTotal, metrics.MetricTunnelBytesTotal, tc.wantDown, metrics.MetricTunnelBytesTotal, tc.wantUp)
	if err = testutil.GatherAndCompare(reg, strings.NewReader(wantBytes), metrics.MetricTunnelBytesTotal); err != nil {
		t.Fatalf("GatherAndCompare(%s): %v", metrics.MetricTunnelBytesTotal, err)
	}
	if count := testutil.CollectAndCount(reg, metrics.MetricRefusedTunnelsTotal); count != 0 {
		t.Fatalf("CollectAndCount(%s) = %d, want 0", metrics.MetricRefusedTunnelsTotal, count)
	}
	assertDurationResultRecorded(t, reg, tc.wantResult)
}

func assertDurationResultRecorded(t *testing.T, reg *prometheus.Registry, wantResult string) {
	t.Helper()
	fams, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	for _, fam := range fams {
		if fam.GetName() != metrics.MetricTunnelDurationSeconds {
			continue
		}
		for _, m := range fam.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "result" && lp.GetValue() == wantResult && m.GetHistogram().GetSampleCount() == 1 {
					return
				}
			}
		}
	}
	t.Fatalf("%s missing sample_count=1 series with result=%q", metrics.MetricTunnelDurationSeconds, wantResult)
}
