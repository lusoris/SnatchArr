// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package metrics holds every Prometheus collector the control plane records, so that one
// file lists what an alert rule can watch. The collectors are served on /metrics with the
// framework's own (internal/app mounts it).
package metrics

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/fx"
)

// Metrics records the outcomes the logs alone used to carry (gap audit A1-02..A1-10).
type Metrics struct {
	loopFailures    *prometheus.CounterVec
	loopLastSuccess *prometheus.GaugeVec
	loopInterval    *prometheus.GaugeVec
	planFailures    *prometheus.CounterVec
}

// New registers the collectors on reg. A collector another instance already registered there
// (a second fx app in one test binary) is reused, so both record into the same series.
func New(reg prometheus.Registerer) (*Metrics, error) {
	m := &Metrics{
		loopFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "snatcharr_loop_failures_total",
			Help: "Ticks of a leader loop that returned an error, by loop.",
		}, []string{"loop"}),
		loopLastSuccess: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "snatcharr_loop_last_success_timestamp_seconds",
			Help: "Unix time of the last tick of a leader loop that returned no error, by loop.",
		}, []string{"loop"}),
		loopInterval: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "snatcharr_loop_interval_seconds",
			Help: "Interval between two ticks of a leader loop, by loop.",
		}, []string{"loop"}),
		planFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "snatcharr_planner_instance_failures_total",
			Help: "Planner passes that skipped an instance because planning it failed, by instance name.",
		}, []string{"instance"}),
	}
	var err error
	if m.loopFailures, err = register(reg, m.loopFailures); err != nil {
		return nil, err
	}
	if m.loopLastSuccess, err = register(reg, m.loopLastSuccess); err != nil {
		return nil, err
	}
	if m.loopInterval, err = register(reg, m.loopInterval); err != nil {
		return nil, err
	}
	if m.planFailures, err = register(reg, m.planFailures); err != nil {
		return nil, err
	}
	return m, nil
}

// register adds c to reg, or returns the collector of the same description reg already holds.
func register[C prometheus.Collector](reg prometheus.Registerer, c C) (C, error) {
	err := reg.Register(c)
	if err == nil {
		return c, nil
	}
	if are, ok := errors.AsType[prometheus.AlreadyRegisteredError](err); ok {
		if existing, ok := are.ExistingCollector.(C); ok {
			return existing, nil
		}
	}
	var zero C
	return zero, fmt.Errorf("metrics: register: %w", err)
}

// LoopTicked records one tick of the leader loop called name (internal/leaderx.Periodic).
// The failure series exists from the first tick on (WithLabelValues creates it at zero), so
// an alert on its increase sees a zero before the first failure.
func (m *Metrics) LoopTicked(name string, interval time.Duration, at time.Time, err error) {
	loop := LoopLabel(name)
	m.loopInterval.WithLabelValues(loop).Set(interval.Seconds())
	failures := m.loopFailures.WithLabelValues(loop)
	if err != nil {
		failures.Inc()
		return
	}
	m.loopLastSuccess.WithLabelValues(loop).Set(float64(at.UnixNano()) / float64(time.Second))
}

// PlanFailed records a planner pass that skipped the instance called instance.
func (m *Metrics) PlanFailed(instance string) {
	m.planFailures.WithLabelValues(instance).Inc()
}

// LoopLabel turns a loop name such as "snatch: planner" into the label value
// "snatch_planner": lower case, every run of other characters than a-z and 0-9 one "_".
func LoopLabel(name string) string {
	var b strings.Builder
	gap := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if gap && b.Len() > 0 {
				b.WriteByte('_')
			}
			gap = false
			b.WriteRune(r)
			continue
		}
		gap = true
	}
	return b.String()
}

// Module provides Metrics on the default registerer, the one /metrics serves.
var Module = fx.Module("snatcharr.metrics",
	fx.Provide(func() (*Metrics, error) { return New(prometheus.DefaultRegisterer) }),
)
