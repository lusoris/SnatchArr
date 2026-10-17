// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package snatch

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/metrics"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// TickInterval is how often the planner evaluates every instance.
const TickInterval = 60 * time.Second

// MaxInstancesPerTick bounds one tick (HISS-02).
const MaxInstancesPerTick = 500

// Gate refusal codes. They double as the planner deferral reasons in package metrics,
// so one name space serves the log, the refusal and the metric label.
const (
	GateClientUnreachable  = metrics.DeferralClientUnreachable
	GateClientPaused       = metrics.DeferralClientPaused
	GateClientActiveLimit  = metrics.DeferralClientActiveLimit
	GateBandwidthSaturated = metrics.DeferralBandwidthSaturated
	GateGenericRefusal     = metrics.DeferralGateRefusal
	GateIndexerLimit       = metrics.DeferralIndexerLimit
)

// GateRefusal describes why a gate vetoed a snatch.
type GateRefusal struct {
	Code    string
	Message string
}

// Gate lets other subsystems veto a snatch for an instance (download-client backpressure,
// Cleanuparr signals).
type Gate interface {
	Allow(ctx context.Context, inst domain.Instance, kind domain.SnatchKind) (allowed bool, refusal GateRefusal, err error)
}

// AllowAll is the gate used until download-client backpressure lands.
type AllowAll struct{}

// Allow always allows.
func (AllowAll) Allow(context.Context, domain.Instance, domain.SnatchKind) (bool, GateRefusal, error) {
	return true, GateRefusal{}, nil
}

// Planner decides when runs are due and enqueues them.
type Planner struct {
	st     *store.Store
	clk    clock.Clock
	runs   *Runs
	gates  []Gate
	rec    *Recorder
	logger *slog.Logger
	m      *metrics.Metrics
}

// NewPlanner wires the planner. m counts the instances a tick skips.
func NewPlanner(st *store.Store, clk clock.Clock, runs *Runs, rec *Recorder, logger *slog.Logger, m *metrics.Metrics, gates ...Gate) *Planner {
	return &Planner{st: st, clk: clk, runs: runs, rec: rec, logger: logger, m: m, gates: gates}
}

// Tick evaluates every enabled instance once. It never returns early on a single
// instance's failure; errors are logged and the instance is skipped.
func (p *Planner) Tick(ctx context.Context) (int, error) {
	rows, err := p.st.Q().ListInstancesToPlan(ctx)
	if err != nil {
		return 0, fmt.Errorf("snatch: planner list instances: %w", store.MapError(err))
	}
	rows = p.bound(ctx, rows)
	queued := 0
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
		inst := store.InstanceFromRow(row)
		n, planErr := p.planInstance(ctx, inst)
		if planErr != nil {
			p.logger.WarnContext(ctx, "snatch: planner skipped instance", slog.String("instance", inst.Name), slog.String("error", planErr.Error()))
			p.m.PlanFailed(inst.Name)
			continue
		}
		queued += n
	}
	err = p.st.Q().MarkInstancesPlanned(ctx, sqlcgen.MarkInstancesPlannedParams{PlannedAt: p.clk.Now(), Ids: ids})
	if err != nil {
		return queued, fmt.Errorf("snatch: planner mark instances: %w", store.MapError(err))
	}
	return queued, nil
}

// bound keeps the first MaxInstancesPerTick instances. The ones past it wait for the next
// tick, which lists them first because it evaluated them longest ago (#176).
func (p *Planner) bound(ctx context.Context, rows []sqlcgen.Instance) []sqlcgen.Instance {
	if len(rows) <= MaxInstancesPerTick {
		return rows
	}
	deferred := len(rows) - MaxInstancesPerTick
	p.logger.WarnContext(ctx, "snatch: planner deferred instances past the tick bound",
		slog.Int("deferred", deferred), slog.Int("bound", MaxInstancesPerTick))
	for range deferred {
		p.m.PlannerDeferred(metrics.DeferralTickBound)
	}
	return rows[:MaxInstancesPerTick]
}

func (p *Planner) planInstance(ctx context.Context, inst domain.Instance) (int, error) {
	policyRow, err := p.st.Q().EnsureDefaultPolicy(ctx, sqlcgen.EnsureDefaultPolicyParams{InstanceID: inst.ID, UpdatedAt: p.clk.Now()})
	if err != nil {
		return 0, fmt.Errorf("policy: %w", store.MapError(err))
	}
	policy := store.PolicyFromRow(policyRow)
	decision, err := p.decision(ctx, inst.ID)
	if err != nil {
		return 0, err
	}
	if decision.Paused {
		for range dueKinds(policy) {
			p.m.PlannerDeferred(metrics.DeferralScheduleClosed)
		}
		return 0, nil
	}
	queued := 0
	for _, kind := range dueKinds(policy) {
		ok, err := p.maybeEnqueue(ctx, inst, policy, kind)
		if err != nil {
			return queued, err
		}
		if ok {
			queued++
		}
	}
	return queued, nil
}

func dueKinds(policy domain.Policy) []domain.SnatchKind {
	kinds := make([]domain.SnatchKind, 0, 2)
	if policy.MissingPerCycle > 0 {
		kinds = append(kinds, domain.SnatchMissing)
	}
	if policy.UpgradePerCycle > 0 {
		kinds = append(kinds, domain.SnatchUpgrade)
	}
	return kinds
}

func (p *Planner) maybeEnqueue(ctx context.Context, inst domain.Instance, policy domain.Policy, kind domain.SnatchKind) (bool, error) {
	last, err := p.runs.LastFinished(ctx, inst.ID, kind)
	if err != nil {
		return false, err
	}
	failures, err := p.runs.TrailingFailures(ctx, inst.ID, kind)
	if err != nil {
		return false, err
	}
	if due := DueAt(inst.ID, kind, last, policy.CycleInterval, failures); p.clk.Now().Before(due) {
		return false, nil
	}
	for _, g := range p.gates {
		allowed, refusal, gateErr := g.Allow(ctx, inst, kind)
		if gateErr != nil {
			return false, fmt.Errorf("gate: %w", gateErr)
		}
		if !allowed {
			p.logger.InfoContext(ctx, "snatch: gated", slog.String("instance", inst.Name), slog.String("kind", string(kind)), slog.String("reason", refusal.Message))
			code := refusal.Code
			switch code {
			case GateClientUnreachable, GateClientPaused, GateClientActiveLimit, GateBandwidthSaturated, GateIndexerLimit:
			default:
				code = GateGenericRefusal
			}
			p.m.PlannerDeferred(code)
			return false, nil
		}
	}
	run, created, err := p.runs.EnqueueWithOptions(ctx, EnqueueOptions{
		InstanceID: inst.ID,
		Kind:       kind,
		DryRun:     policy.DryRun,
		Seed:       0,
	})
	if err != nil {
		return false, err
	}
	if !created {
		p.m.PlannerDeferred(metrics.DeferralAlreadyRunning)
		return false, nil
	}
	return true, p.rec.Record(ctx, domain.Event{
		RunID: &run.ID, InstanceID: inst.ID, Level: "debug", Type: "run_queued",
		Title: fmt.Sprintf("%s snatch queued", kind),
	})
}

// decision evaluates the instance's own and global schedules. A schedule it cannot read
// fails the decision instead of being skipped (gap audit A1-06).
func (p *Planner) decision(ctx context.Context, instanceID uuid.UUID) (Decision, error) {
	rows, err := p.st.Q().ListEnabledSchedulesFor(ctx, &instanceID)
	if err != nil {
		return Decision{}, fmt.Errorf("schedules: %w", store.MapError(err))
	}
	windows := make([]ScheduleWindow, 0, len(rows))
	for _, row := range rows {
		w, err := windowFromRow(row)
		if err != nil {
			// Fail closed: a dropped pause window would let searches run while the
			// operator asked for none.
			return Decision{}, fmt.Errorf("schedule %s: %w", row.ID, err)
		}
		windows = append(windows, w)
	}
	return Evaluate(windows, p.clk.Now()), nil
}

// EffectiveCap applies the schedule's cap override to a policy cap. When the schedules
// cannot be read it grants nothing: the caller must not fall back to the policy cap.
func (p *Planner) EffectiveCap(ctx context.Context, instanceID uuid.UUID, policyCap int) (int, error) {
	d, err := p.decision(ctx, instanceID)
	if err != nil {
		return 0, err
	}
	if d.CapOverride > 0 && d.CapOverride < policyCap {
		return d.CapOverride, nil
	}
	return policyCap, nil
}

func windowFromRow(row sqlcgen.Schedule) (ScheduleWindow, error) {
	loc, err := time.LoadLocation(row.Tz)
	if err != nil {
		return ScheduleWindow{}, fmt.Errorf("timezone %q: %w", row.Tz, err)
	}
	capValue := 0
	if row.CapValue != nil {
		capValue = int(*row.CapValue)
	}
	return ScheduleWindow{
		DaysMask: int(row.DaysMask),
		StartMin: int(row.StartTime.Microseconds / int64(time.Minute/time.Microsecond)),
		EndMin:   int(row.EndTime.Microseconds / int64(time.Minute/time.Microsecond)),
		Location: loc,
		Action:   ScheduleAction(row.Action),
		CapValue: capValue,
	}, nil
}
