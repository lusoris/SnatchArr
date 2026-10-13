// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package metricstest builds metrics.Metrics on a registry of the test's own, so parallel tests
// never share a series, and reads one series back.
package metricstest

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/lusoris/SnatchArr/api/internal/metrics"
)

const (
	// awaitBound is how long Await waits for a series before it fails the test (HISS-02:
	// Await polls at most awaitBound/pollEvery times).
	awaitBound = 5 * time.Second
	// pollEvery is how often Await reads the registry.
	pollEvery = 10 * time.Millisecond
)

// New returns Metrics on a fresh registry and that registry.
func New(t *testing.T) (*metrics.Metrics, *prometheus.Registry) {
	t.Helper()
	reg := prometheus.NewRegistry()
	m, err := metrics.New(reg)
	if err != nil {
		t.Fatalf("metrics.New() = %v", err)
	}
	return m, reg
}

// Value returns the value of the series name{label=value} in reg and whether it exists; an
// empty label reads the first series of name, the only one of a collector without labels.
func Value(t *testing.T, reg prometheus.Gatherer, name, label, value string) (float64, bool) {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() = %v", err)
	}
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
		for _, m := range f.GetMetric() {
			if label != "" && !hasLabel(m.GetLabel(), label, value) {
				continue
			}
			switch {
			case m.GetCounter() != nil:
				return m.GetCounter().GetValue(), true
			case m.GetGauge() != nil:
				return m.GetGauge().GetValue(), true
			}
		}
	}
	return 0, false
}

// Await waits until the series name{label=value} in reg holds want, for a loop that records
// from its own goroutine, and fails the test when it does not in time.
func Await(t *testing.T, reg prometheus.Gatherer, name, label, value string, want float64) {
	t.Helper()
	poll := time.NewTicker(pollEvery)
	defer poll.Stop()
	for range int(awaitBound / pollEvery) {
		if got, ok := Value(t, reg, name, label, value); ok && got == want {
			return
		}
		<-poll.C
	}
	got, ok := Value(t, reg, name, label, value)
	if !ok || got != want {
		t.Fatalf("%s{%s=%q} = %v (present %t), want %v", name, label, value, got, ok, want)
	}
}

func hasLabel[L interface {
	GetName() string
	GetValue() string
}](labels []L, name, value string) bool {
	for _, l := range labels {
		if l.GetName() == name && l.GetValue() == value {
			return true
		}
	}
	return false
}
