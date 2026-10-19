// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package metrics_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil/promlint"
	dto "github.com/prometheus/client_model/go"

	"github.com/lusoris/SnatchArr/api/internal/metrics"
)

// errNoPrefix reports a metric outside the snatcharr_ namespace.
var errNoPrefix = errors.New("metric name lacks the snatcharr_ prefix")

func lintSnatcharrPrefix(mf *dto.MetricFamily) []error {
	if !strings.HasPrefix(mf.GetName(), "snatcharr_") {
		return []error{errNoPrefix}
	}
	return nil
}

// lintReservedLabels refuses labels colliding with Prometheus target labels (instance, job).
func lintReservedLabels(mf *dto.MetricFamily) []error {
	var errs []error
	seen := make(map[string]bool)
	for _, m := range mf.GetMetric() {
		for _, lp := range m.GetLabel() {
			name := lp.GetName()
			if (name == "instance" || name == "job") && !seen[name] {
				seen[name] = true
				errs = append(errs, fmt.Errorf("reserved target label %q", name))
			}
		}
	}
	return errs
}

// lintRegistry runs Prometheus' own metric-name linter, plus custom rules, over every
// family reg gathers.
func lintRegistry(reg *prometheus.Registry) ([]promlint.Problem, error) {
	mfs, err := reg.Gather()
	if err != nil {
		return nil, fmt.Errorf("gather: %w", err)
	}
	l := promlint.NewWithMetricFamilies(mfs)
	l.AddCustomValidations(lintSnatcharrPrefix, lintReservedLabels)
	problems, err := l.Lint()
	if err != nil {
		return nil, fmt.Errorf("lint: %w", err)
	}
	return problems, nil
}

// Positive: every metric the API registers passes promlint and carries the namespace.
func TestNames_PassPromlint(t *testing.T) {
	t.Parallel()
	reg := prometheus.NewRegistry()
	m, err := metrics.New(reg)
	if err != nil {
		t.Fatalf("metrics.New = %v", err)
	}
	populateRegistry(m)
	problems, err := lintRegistry(reg)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range problems {
		t.Errorf("%s: %s", p.Metric, p.Text)
	}
}

// Negative and boundary: planted names prove the lint refuses what the API must never
// register.
func TestNames_PlantedNegatives(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		want      string
		collector func() prometheus.Collector
	}{
		{name: "counter without _total", want: "counter metrics should have \"_total\" suffix", collector: func() prometheus.Collector {
			c := prometheus.NewCounter(prometheus.CounterOpts{Name: "snatcharr_searches", Help: "h"})
			c.Inc()
			return c
		}},
		{name: "camel case", want: "camelCase", collector: func() prometheus.Collector {
			g := prometheus.NewGauge(prometheus.GaugeOpts{Name: "snatcharr_runsQueued", Help: "h"})
			g.Set(1)
			return g
		}},
		{name: "outside the namespace", want: errNoPrefix.Error(), collector: func() prometheus.Collector {
			g := prometheus.NewGauge(prometheus.GaugeOpts{Name: "runs", Help: "h"})
			g.Set(1)
			return g
		}},
		// Boundary: the prefix alone is a namespace, not a name.
		{name: "prefix without a name", want: errNoPrefix.Error(), collector: func() prometheus.Collector {
			g := prometheus.NewGauge(prometheus.GaugeOpts{Name: "snatcharr", Help: "h"})
			g.Set(1)
			return g
		}},
		{name: "reserved target label instance", want: `reserved target label "instance"`, collector: func() prometheus.Collector {
			g := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "snatcharr_test_reserved", Help: "h"}, []string{"instance"})
			g.WithLabelValues("val").Set(1)
			return g
		}},
		// Boundary: arr_instance contains instance as substring but is not reserved.
		{name: "boundary arr_instance passes", want: "", collector: func() prometheus.Collector {
			g := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "snatcharr_test_arr_instance", Help: "h"}, []string{"arr_instance"})
			g.WithLabelValues("val").Set(1)
			return g
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reg := prometheus.NewRegistry()
			reg.MustRegister(tc.collector())
			problems, err := lintRegistry(reg)
			if err != nil {
				t.Fatal(err)
			}
			assertLintProblems(t, problems, tc.want)
		})
	}
}

func assertLintProblems(t *testing.T, problems []promlint.Problem, want string) {
	t.Helper()
	if want == "" {
		if len(problems) > 0 {
			t.Fatalf("problems = %+v, want none", problems)
		}
		return
	}
	for _, p := range problems {
		if strings.Contains(p.Text, want) {
			return
		}
	}
	t.Fatalf("problems = %+v, want one containing %q", problems, want)
}
