// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package hunt

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// TickInterval is how often the planner evaluates every instance.
const TickInterval = 60 * time.Second

// maxTicks bounds the planner loop (HISS-02): 2^30 minutes is about 2000 years.
const maxTicks = 1 << 30

// MaxInstancesPerTick bounds one tick (HISS-02).
const MaxInstancesPerTick = 500

// Gate lets other subsystems veto a hunt for an instance (download-client backpressure,
// Cleanuparr signals). A nil reason means "allowed".
type Gate interface {
	Allow(ctx context.Context, inst domain.Instance, kind domain.HuntKind) (allowed bool, reason string, err error)
}

// AllowAll is the gate used until download-client backpressure lands.
type AllowAll struct{}

// Allow always allows.
func (AllowAll) Allow(context.Context, domain.Instance, domain.HuntKind) (bool, string, error) {
	return true, "", nil
}

// Planner decides when runs are due and enqueues them.
type Planner struct {
	st     *store.Store
	clk    clock.Clock
	runs   *Runs
	gates  []Gate
	rec    *Recorder
	logger *slog.Logger
}

// NewPlanner wires the planner.
func NewPlanner(st *store.Store, clk clock.Clock, runs *Runs, rec *Recorder, logger *slog.Logger, gates ...Gate) *Planner {
	return &Planner{st: st, clk: clk, runs: runs, rec: rec, logger: logger, gates: gates}
}

// Tick evaluates every enabled instance once. It never returns early on a single
// instance's failure; errors are logged and the instance is skipped.
func (p *Planner) Tick(ctx context.Context) (int, error) {
	rows, err := p.st.Q().ListEnabledInstances(ctx)
	if err != nil {
		return 0, fmt.Errorf("hunt: planner list instances: %w", store.MapError(err))
	}
	if len(rows) > MaxInstancesPerTick {
		rows = rows[:MaxInstancesPerTick]
	}
	queued := 0
	for _, row := range rows {
		inst := store.InstanceFromRow(row)
		n, err := p.planInstance(ctx, inst)
		if err != nil {
			p.logger.WarnContext(ctx, "hunt: planner skipped instance", slog.String("instance", inst.Name), slog.String("error", err.Error()))
			continue
		}
		queued += n
	}
	return queued, nil
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

func dueKinds(policy domain.Policy) []domain.HuntKind {
	kinds := make([]domain.HuntKind, 0, 2)
	if policy.MissingPerCycle > 0 {
		kinds = append(kinds, domain.HuntMissing)
	}
	if policy.UpgradePerCycle > 0 {
		kinds = append(kinds, domain.HuntUpgrade)
	}
	return kinds
}

func (p *Planner) maybeEnqueue(ctx context.Context, inst domain.Instance, policy domain.Policy, kind domain.HuntKind) (bool, error) {
	last, err := p.runs.LastFinished(ctx, inst.ID, kind)
	if err != nil {
		return false, err
	}
	if !last.IsZero() && p.clk.Now().Before(last.Add(policy.CycleInterval)) {
		return false, nil
	}
	for _, g := range p.gates {
		allowed, reason, gateErr := g.Allow(ctx, inst, kind)
		if gateErr != nil {
			return false, fmt.Errorf("gate: %w", gateErr)
		}
		if !allowed {
			p.logger.InfoContext(ctx, "hunt: gated", slog.String("instance", inst.Name), slog.String("kind", string(kind)), slog.String("reason", reason))
			return false, nil
		}
	}
	run, created, err := p.runs.Enqueue(ctx, inst.ID, kind)
	if err != nil || !created {
		return false, err
	}
	return true, p.rec.Record(ctx, domain.Event{
		RunID: &run.ID, InstanceID: inst.ID, Level: "debug", Type: "run_queued",
		Title: fmt.Sprintf("%s hunt queued", kind),
	})
}

// decision evaluates the instance's own and global schedules.
func (p *Planner) decision(ctx context.Context, instanceID uuid.UUID) (Decision, error) {
	rows, err := p.st.Q().ListEnabledSchedulesFor(ctx, &instanceID)
	if err != nil {
		return Decision{}, fmt.Errorf("schedules: %w", store.MapError(err))
	}
	windows := make([]ScheduleWindow, 0, len(rows))
	for _, row := range rows {
		w, err := windowFromRow(row)
		if err != nil {
			p.logger.WarnContext(ctx, "hunt: bad schedule", slog.String("id", row.ID.String()), slog.String("error", err.Error()))
			continue
		}
		windows = append(windows, w)
	}
	return Evaluate(windows, p.clk.Now()), nil
}

// EffectiveCap applies the schedule's cap override to a policy cap.
func (p *Planner) EffectiveCap(ctx context.Context, instanceID uuid.UUID, policyCap int) (int, error) {
	d, err := p.decision(ctx, instanceID)
	if err != nil {
		return policyCap, err
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
