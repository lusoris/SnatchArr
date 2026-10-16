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

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// Metrics records the outcomes the logs alone used to carry (gap audit A1-02..A1-10).
type Metrics struct {
	loopFailures       *prometheus.CounterVec
	loopLastSuccess    *prometheus.GaugeVec
	loopInterval       *prometheus.GaugeVec
	planFailures       *prometheus.CounterVec
	runsQueued         *prometheus.CounterVec
	runsLeased         *prometheus.CounterVec
	runsFinished       *prometheus.CounterVec
	workerEvents       *prometheus.CounterVec
	unconfirmed        *prometheus.CounterVec
	budgetRequests     *prometheus.CounterVec
	searchesDispatched *prometheus.CounterVec
	staminaUsed        *prometheus.GaugeVec
	staminaRemaining   *prometheus.GaugeVec
	runsCount          *prometheus.GaugeVec
	seerrPending       prometheus.Gauge
	runLeaseWait       *prometheus.HistogramVec
	plannerDeferrals   *prometheus.CounterVec
}

// Run outcomes the finished-runs counter carries next to the worker's own (done, failed,
// cancelled).
const (
	// OutcomeLeaseExhausted is a run the control plane failed because its lease expired
	// snatch.MaxLeases times.
	OutcomeLeaseExhausted = "lease_exhausted"
)

// Budget request results.
const (
	BudgetGranted = "granted" // everything asked for
	BudgetPartial = "partial" // less than asked for, more than nothing
	BudgetDenied  = "denied"  // nothing, the cap is spent
	BudgetError   = "error"   // the request failed
)

// Planner deferral reasons.
const (
	DeferralScheduleClosed     = "schedule_closed"
	DeferralAlreadyRunning     = "already_running"
	DeferralClientUnreachable  = "client_unreachable"
	DeferralClientPaused       = "client_paused"
	DeferralClientActiveLimit  = "client_active_limit"
	DeferralBandwidthSaturated = "bandwidth_saturated"
	DeferralGateRefusal        = "gate_refusal"
	DeferralIndexerLimit       = "indexer_limit"
)

// AllDeferralReasons lists every fixed deferral reason.
var AllDeferralReasons = []string{
	DeferralScheduleClosed,
	DeferralAlreadyRunning,
	DeferralClientUnreachable,
	DeferralClientPaused,
	DeferralClientActiveLimit,
	DeferralBandwidthSaturated,
	DeferralGateRefusal,
	DeferralIndexerLimit,
}

var leaseWaitBuckets = []float64{1, 5, 15, 30, 60, 120, 300, 600, 1800, 3600}

// New registers the collectors on reg. A collector another instance already registered there
// (a second fx app in one test binary) is reused, so both record into the same series.
func New(reg prometheus.Registerer) (*Metrics, error) {
	b := &builder{reg: reg}
	m := &Metrics{
		loopFailures:       b.counter("snatcharr_loop_failures_total", "Ticks of a leader loop that returned an error, by loop.", "loop"),
		loopLastSuccess:    b.gauge("snatcharr_loop_last_success_timestamp_seconds", "Unix time of the last tick of a leader loop that returned no error, by loop.", "loop"),
		loopInterval:       b.gauge("snatcharr_loop_interval_seconds", "Interval between two ticks of a leader loop, by loop.", "loop"),
		planFailures:       b.counter("snatcharr_planner_instance_failures_total", "Planner passes that skipped an instance because planning it failed, by instance name.", "instance"),
		runsQueued:         b.counter("snatcharr_runs_queued_total", "Runs the planner queued."),
		runsLeased:         b.counter("snatcharr_runs_leased_total", "Runs a worker leased, by snatch kind.", "kind"),
		runsFinished:       b.counter("snatcharr_runs_finished_total", "Runs that ended, by outcome.", "outcome"),
		workerEvents:       b.counter("snatcharr_worker_events_total", "Events workers reported, by event type.", "type"),
		unconfirmed:        b.counter("snatcharr_worker_events_unconfirmed_total", "Events workers emitted for a completed run that ReportEvents did not confirm."),
		budgetRequests:     b.counter("snatcharr_budget_requests_total", "Budget requests of workers, by result.", "result"),
		searchesDispatched: b.counter("snatcharr_searches_dispatched_total", "Items dispatched to *arr instances, by instance name and snatch kind.", "instance", "kind"),
		staminaUsed:        b.gauge("snatcharr_stamina_used_items", "Stamina spent in the current rolling hourly span, by instance name.", "instance"),
		staminaRemaining:   b.gauge("snatcharr_stamina_remaining_items", "Stamina remaining in the current rolling hourly span under effective cap, by instance name.", "instance"),
		runsCount:          b.gauge("snatcharr_runs", "Current count of runs, by lifecycle status.", "status"),
		seerrPending:       b.gaugeScalar("snatcharr_seerr_requests_pending", "Current count of active/pending processing Seerr requests across enabled links."),
		runLeaseWait:       b.histogram("snatcharr_run_lease_wait_seconds", "Wait time between run enqueued and leased by worker, by snatch kind.", leaseWaitBuckets, "kind"),
		plannerDeferrals:   b.counter("snatcharr_planner_deferrals_total", "Planner passes that decided not to enqueue a run, by reason.", "reason"),
	}
	if b.err != nil {
		return nil, b.err
	}
	initAlertSeries(m)
	return m, nil
}

func initAlertSeries(m *Metrics) {
	m.runsQueued.WithLabelValues()
	m.unconfirmed.WithLabelValues()
	for _, outcome := range []string{string(domain.RunDone), string(domain.RunFailed), string(domain.RunCancelled), OutcomeLeaseExhausted} {
		m.runsFinished.WithLabelValues(outcome)
	}
	for _, result := range []string{BudgetGranted, BudgetPartial, BudgetDenied, BudgetError} {
		m.budgetRequests.WithLabelValues(result)
	}
	for _, reason := range AllDeferralReasons {
		m.plannerDeferrals.WithLabelValues(reason)
	}
	for _, status := range []string{
		string(domain.RunQueued),
		string(domain.RunLeased),
		string(domain.RunDone),
		string(domain.RunFailed),
		string(domain.RunCancelled),
	} {
		m.runsCount.WithLabelValues(status)
	}
	m.seerrPending.Set(0)
	for _, kind := range []string{string(domain.SnatchMissing), string(domain.SnatchUpgrade)} {
		m.runLeaseWait.WithLabelValues(kind)
	}
}

// builder registers collectors and keeps the first error.
type builder struct {
	reg prometheus.Registerer
	err error
}

func (b *builder) counter(name, help string, labels ...string) *prometheus.CounterVec {
	return use(b, prometheus.NewCounterVec(prometheus.CounterOpts{Name: name, Help: help}, labels))
}

func (b *builder) gauge(name, help string, labels ...string) *prometheus.GaugeVec {
	return use(b, prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: name, Help: help}, labels))
}

func (b *builder) gaugeScalar(name, help string) prometheus.Gauge {
	return use(b, prometheus.NewGauge(prometheus.GaugeOpts{Name: name, Help: help}))
}

func (b *builder) histogram(name, help string, buckets []float64, labels ...string) *prometheus.HistogramVec {
	return use(b, prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: name, Help: help, Buckets: buckets}, labels))
}

// use registers c unless an earlier collector failed, and returns the collector to record into.
func use[C prometheus.Collector](b *builder, c C) C {
	if b.err != nil {
		return c
	}
	got, err := register(b.reg, c)
	if err != nil {
		b.err = err
		return c
	}
	return got
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

// RunsQueued records n runs the planner queued.
func (m *Metrics) RunsQueued(n int) {
	m.runsQueued.WithLabelValues().Add(float64(n))
}

// RunLeased records a run of kind a worker leased.
func (m *Metrics) RunLeased(kind string) {
	m.runsLeased.WithLabelValues(kind).Inc()
}

// RunFinished records a run that ended with outcome.
func (m *Metrics) RunFinished(outcome string) {
	m.runsFinished.WithLabelValues(outcome).Inc()
}

// WorkerEvent records an event of eventType a worker reported.
func (m *Metrics) WorkerEvent(eventType string) {
	m.workerEvents.WithLabelValues(eventType).Inc()
}

// WorkerEventTypes creates the event series at zero, so an alert on the increase of one
// (search_failed, instance_unreachable) sees a zero before the first event.
func (m *Metrics) WorkerEventTypes(eventTypes ...string) {
	for _, t := range eventTypes {
		m.workerEvents.WithLabelValues(t)
	}
}

// EventsUnconfirmed records n events a worker emitted for a run but could not confirm.
func (m *Metrics) EventsUnconfirmed(n uint32) {
	m.unconfirmed.WithLabelValues().Add(float64(n))
}

// BudgetRequested records a budget request that ended with result (BudgetGranted, ...).
func (m *Metrics) BudgetRequested(result string) {
	m.budgetRequests.WithLabelValues(result).Inc()
}

// SearchesDispatched records n items dispatched to instance of kind.
func (m *Metrics) SearchesDispatched(instance, kind string, n int) {
	if n <= 0 {
		n = 1
	}
	m.searchesDispatched.WithLabelValues(instance, kind).Add(float64(n))
}

// SetStaminaUsed records the used hourly stamina for instance.
func (m *Metrics) SetStaminaUsed(instance string, items float64) {
	m.staminaUsed.WithLabelValues(instance).Set(items)
}

// SetStaminaRemaining records the remaining hourly stamina for instance.
func (m *Metrics) SetStaminaRemaining(instance string, items float64) {
	m.staminaRemaining.WithLabelValues(instance).Set(items)
}

// ResetStamina clears all stamina series so deleted or disabled instances do not linger.
func (m *Metrics) ResetStamina() {
	m.staminaUsed.Reset()
	m.staminaRemaining.Reset()
}

// SetRuns records the current count of runs in status.
func (m *Metrics) SetRuns(status string, count float64) {
	m.runsCount.WithLabelValues(status).Set(count)
}

// SetSeerrRequestsPending records the active/pending processing Seerr requests.
func (m *Metrics) SetSeerrRequestsPending(count float64) {
	m.seerrPending.Set(count)
}

// RunLeaseWait records how long a run of kind waited between queue and lease.
func (m *Metrics) RunLeaseWait(kind string, seconds float64) {
	if seconds < 0 {
		seconds = 0
	}
	m.runLeaseWait.WithLabelValues(kind).Observe(seconds)
}

// PlannerDeferred records a planner pass that did not enqueue a run for reason.
func (m *Metrics) PlannerDeferred(reason string) {
	m.plannerDeferrals.WithLabelValues(reason).Inc()
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
