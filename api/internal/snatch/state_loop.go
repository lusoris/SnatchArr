// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package snatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.uber.org/fx"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/leader/gate"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/metrics"
	"github.com/lusoris/SnatchArr/api/internal/settings"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

const (
	// StateInterval is how often the leader loop refreshes domain gauges.
	StateInterval = 30 * time.Second
	// stateQueryTimeout bounds each database query the state loop executes (HISS-02).
	stateQueryTimeout = 5 * time.Second
)

// StateTickerParams groups the dependencies for the state metrics loop.
type StateTickerParams struct {
	fx.In
	Store    *store.Store
	Clk      clock.Clock
	Budget   *Budget
	Planner  *Planner
	Settings *settings.Service
	Logger   *slog.Logger
	Metrics  *metrics.Metrics
}

// StateTicker runs the state refresh loop once per StateInterval while this replica leads.
type StateTicker struct{ *gate.Periodic }

// StateLoop queries Postgres and sets domain state gauges.
type StateLoop struct {
	st       *store.Store
	clk      clock.Clock
	budget   *Budget
	planner  *Planner
	settings *settings.Service
	logger   *slog.Logger
	m        *metrics.Metrics
}

// NewStateTicker builds the state metrics leader loop.
func NewStateTicker(p StateTickerParams) *StateTicker {
	loop := &StateLoop{
		st:       p.Store,
		clk:      p.Clk,
		budget:   p.Budget,
		planner:  p.Planner,
		settings: p.Settings,
		logger:   p.Logger,
		m:        p.Metrics,
	}
	return &StateTicker{gate.NewPeriodic("metrics: state", StateInterval, p.Clk, p.Metrics, func(ctx context.Context) error {
		if err := loop.Tick(ctx); err != nil {
			p.Logger.ErrorContext(ctx, "metrics: state tick", slog.String("error", err.Error()))
			return err
		}
		return nil
	})}
}

// Tick runs one state refresh pass.
func (l *StateLoop) Tick(ctx context.Context) error {
	if err := l.refreshRuns(ctx); err != nil {
		return fmt.Errorf("state metrics: runs: %w", err)
	}
	if err := l.refreshStamina(ctx); err != nil {
		return fmt.Errorf("state metrics: stamina: %w", err)
	}
	if err := l.refreshInstanceRuns(ctx); err != nil {
		return fmt.Errorf("state metrics: instance runs: %w", err)
	}
	if err := l.refreshSeerr(ctx); err != nil {
		return fmt.Errorf("state metrics: seerr: %w", err)
	}
	return nil
}

func (l *StateLoop) refreshRuns(ctx context.Context) error {
	qCtx, cancel := context.WithTimeout(ctx, stateQueryTimeout)
	defer cancel()
	rows, err := l.st.Q().CountRunsByStatus(qCtx)
	if err != nil {
		return store.MapError(err)
	}
	counts := map[string]int64{
		string(domain.RunQueued):    0,
		string(domain.RunLeased):    0,
		string(domain.RunDone):      0,
		string(domain.RunFailed):    0,
		string(domain.RunCancelled): 0,
	}
	for _, row := range rows {
		counts[row.Status] = row.Count
	}
	for status, count := range counts {
		l.m.SetRuns(status, float64(count))
	}
	return nil
}

type staminaValues struct {
	instance  string
	used      float64
	remaining float64
}

func (l *StateLoop) refreshStamina(ctx context.Context) error {
	qCtx, cancel := context.WithTimeout(ctx, stateQueryTimeout)
	defer cancel()
	rows, err := l.st.Q().ListEnabledInstances(qCtx)
	if err != nil {
		return store.MapError(err)
	}
	var set []staminaValues
	for _, row := range rows {
		inst := store.InstanceFromRow(row)
		val, err := l.computeInstanceStamina(ctx, inst)
		if err != nil {
			l.logger.WarnContext(ctx, "snatch: stamina metric refresh failed", slog.String("instance", inst.Name), slog.String("error", err.Error()))
			continue
		}
		set = append(set, val)
	}
	if gVal, ok, err := l.computeGlobalStamina(ctx); err != nil {
		l.logger.WarnContext(ctx, "snatch: global stamina refresh failed", slog.String("error", err.Error()))
	} else if ok {
		set = append(set, gVal)
	}

	l.m.ResetStamina()
	for _, s := range set {
		l.m.SetStaminaUsed(s.instance, s.used)
		l.m.SetStaminaRemaining(s.instance, s.remaining)
	}
	return nil
}

func (l *StateLoop) computeInstanceStamina(ctx context.Context, inst domain.Instance) (staminaValues, error) {
	qCtx, cancel := context.WithTimeout(ctx, stateQueryTimeout)
	defer cancel()
	policyRow, err := l.st.Q().GetPolicy(qCtx, inst.ID)
	var hourlyCap int
	if err != nil {
		mapped := store.MapError(err)
		if errors.Is(mapped, domain.ErrNotFound) {
			hourlyCap = domain.DefaultPolicy(inst.ID).HourlyCap
		} else {
			return staminaValues{}, mapped
		}
	} else {
		hourlyCap = int(policyRow.HourlyCap)
	}
	capacity, err := l.planner.EffectiveCap(qCtx, inst.ID, hourlyCap)
	if err != nil {
		return staminaValues{}, err
	}
	used, _, err := l.budget.Used(qCtx, inst.ID)
	if err != nil {
		return staminaValues{}, err
	}
	remaining := max(capacity-used, 0)
	return staminaValues{
		instance:  inst.Name,
		used:      float64(used),
		remaining: float64(remaining),
	}, nil
}

func (l *StateLoop) computeGlobalStamina(ctx context.Context) (staminaValues, bool, error) {
	qCtx, cancel := context.WithTimeout(ctx, stateQueryTimeout)
	defer cancel()
	cfg, err := l.settings.Get(qCtx)
	if err != nil {
		return staminaValues{}, false, err
	}
	if cfg.GlobalHourlyCap <= 0 {
		return staminaValues{}, false, nil
	}
	used, _, err := l.budget.GlobalUsed(qCtx)
	if err != nil {
		return staminaValues{}, false, err
	}
	remaining := max(cfg.GlobalHourlyCap-used, 0)
	return staminaValues{
		instance:  "_global",
		used:      float64(used),
		remaining: float64(remaining),
	}, true, nil
}

type runIntervalValue struct {
	instance string
	seconds  float64
}

func (l *StateLoop) refreshInstanceRuns(ctx context.Context) error {
	lastRuns, err := l.queryLastRuns(ctx)
	if err != nil {
		return err
	}
	intervalRows, err := l.queryRunIntervals(ctx)
	if err != nil {
		return err
	}
	intervals := l.computeRunIntervals(ctx, intervalRows)

	l.m.ResetInstanceRuns()
	for _, r := range lastRuns {
		l.m.SetInstanceLastRunFinished(r.InstanceName, r.Status, r.LastFinished)
	}
	for _, iv := range intervals {
		l.m.SetInstanceRunInterval(iv.instance, iv.seconds)
	}
	return nil
}

func (l *StateLoop) queryLastRuns(ctx context.Context) ([]sqlcgen.ListInstanceLastRunFinishedRow, error) {
	qCtx, cancel := context.WithTimeout(ctx, stateQueryTimeout)
	defer cancel()
	rows, err := l.st.Q().ListInstanceLastRunFinished(qCtx)
	if err != nil {
		return nil, store.MapError(err)
	}
	return rows, nil
}

func (l *StateLoop) queryRunIntervals(ctx context.Context) ([]sqlcgen.ListInstanceRunIntervalsRow, error) {
	qCtx, cancel := context.WithTimeout(ctx, stateQueryTimeout)
	defer cancel()
	rows, err := l.st.Q().ListInstanceRunIntervals(qCtx)
	if err != nil {
		return nil, store.MapError(err)
	}
	return rows, nil
}

func (l *StateLoop) computeRunIntervals(ctx context.Context, rows []sqlcgen.ListInstanceRunIntervalsRow) []runIntervalValue {
	var intervals []runIntervalValue
	for _, row := range rows {
		open, err := l.isScheduleOpen(ctx, row.InstanceID)
		if err != nil {
			l.logger.WarnContext(ctx, "snatch: run interval schedule check failed",
				slog.String("instance", row.InstanceName), slog.String("error", err.Error()))
			continue
		}
		if !open {
			continue
		}
		intervals = append(intervals, runIntervalValue{
			instance: row.InstanceName,
			seconds:  float64(row.CycleIntervalS),
		})
	}
	return intervals
}

func (l *StateLoop) isScheduleOpen(ctx context.Context, instanceID uuid.UUID) (bool, error) {
	qCtx, cancel := context.WithTimeout(ctx, stateQueryTimeout)
	defer cancel()
	d, err := l.planner.decision(qCtx, instanceID)
	if err != nil {
		return false, err
	}
	return !d.Paused, nil
}

func (l *StateLoop) refreshSeerr(ctx context.Context) error {
	qCtx, cancel := context.WithTimeout(ctx, stateQueryTimeout)
	defer cancel()
	links, err := l.st.Q().ListEnabledSeerrLinks(qCtx)
	if err != nil {
		return store.MapError(err)
	}
	total := int64(0)
	for _, link := range links {
		count, err := l.st.Q().CountSeerrRequests(qCtx, link.ID)
		if err != nil {
			return store.MapError(err)
		}
		total += count
	}
	l.m.SetSeerrRequestsPending(float64(total))
	return nil
}
