// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
)

// TriggerRun queues a snatch run immediately.
func (h *Handlers) TriggerRun(ctx context.Context, req *oas.RunTrigger, params oas.TriggerRunParams) (oas.TriggerRunRes, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	if _, err := h.instances.Get(ctx, params.InstanceId); err != nil {
		return nil, err
	}
	kind, err := domain.ParseSnatchKind(string(req.Kind))
	if err != nil {
		return nil, err
	}
	run, created, err := h.runs.Enqueue(ctx, params.InstanceId, kind)
	if err != nil {
		return nil, err
	}
	if created {
		h.recordQuickie(ctx, run)
		out := oas.TriggerRunCreated(runToOAS(run))
		return &out, nil
	}
	existing, err := h.activeRun(ctx, params.InstanceId, kind)
	if err != nil {
		return nil, err
	}
	out := oas.TriggerRunOK(runToOAS(existing))
	return &out, nil
}

// recordQuickie writes the history entry for a manual run-now; a failure to record never
// fails the trigger itself.
func (h *Handlers) recordQuickie(ctx context.Context, run domain.Run) {
	err := h.rec.Record(ctx, domain.Event{
		RunID: &run.ID, InstanceID: run.InstanceID, Level: "info", Type: "run_queued",
		Title: fmt.Sprintf("Quickie: %s snatch queued by hand", run.Kind),
	})
	if err != nil {
		h.logger.WarnContext(ctx, "httpapi: record quickie", slog.String("error", err.Error()))
	}
}

// activeRun finds the queued or leased run that blocked a new enqueue.
func (h *Handlers) activeRun(ctx context.Context, instanceID uuid.UUID, kind domain.SnatchKind) (domain.Run, error) {
	recent, err := h.runs.List(ctx, &instanceID, 10, 0)
	if err != nil {
		return domain.Run{}, err
	}
	for _, r := range recent {
		if r.Kind == kind && (r.Status == domain.RunQueued || r.Status == domain.RunLeased) {
			return r, nil
		}
	}
	return domain.Run{}, domain.ErrConflict
}

// ListRuns pages recent runs.
func (h *Handlers) ListRuns(ctx context.Context, params oas.ListRunsParams) ([]oas.Run, error) {
	var inst *uuid.UUID
	if v, ok := params.InstanceID.Get(); ok {
		inst = &v
	}
	runs, err := h.runs.List(ctx, inst, params.Limit.Or(50), params.Offset.Or(0))
	if err != nil {
		return nil, err
	}
	out := make([]oas.Run, 0, len(runs))
	for _, r := range runs {
		out = append(out, runToOAS(r))
	}
	return out, nil
}

// CancelRun cancels a queued or leased run.
func (h *Handlers) CancelRun(ctx context.Context, params oas.CancelRunParams) error {
	if err := requireAdmin(ctx); err != nil {
		return err
	}
	return h.runs.Cancel(ctx, params.RunId)
}

// ListEvents pages history.
func (h *Handlers) ListEvents(ctx context.Context, params oas.ListEventsParams) ([]oas.Event, error) {
	p := snatch.ListParams{PageSize: params.PageSize.Or(100), Type: params.Type.Or("")}
	if v, ok := params.InstanceID.Get(); ok {
		p.InstanceID = &v
	}
	if v, ok := params.BeforeID.Get(); ok {
		p.BeforeID = &v
	}
	events, err := h.rec.List(ctx, p)
	if err != nil {
		return nil, err
	}
	out := make([]oas.Event, 0, len(events))
	for _, e := range events {
		out = append(out, eventToOAS(e))
	}
	return out, nil
}

// DeleteInstanceEvents clears one instance's history.
func (h *Handlers) DeleteInstanceEvents(ctx context.Context, params oas.DeleteInstanceEventsParams) error {
	if err := requireAdmin(ctx); err != nil {
		return err
	}
	if _, err := h.instances.Get(ctx, params.InstanceId); err != nil {
		return err
	}
	_, err := h.rec.DeleteForInstance(ctx, params.InstanceId)
	return err
}

// GetHourlyCaps reports budget consumption per instance.
func (h *Handlers) GetHourlyCaps(ctx context.Context) ([]oas.CapStatus, error) {
	list, err := h.instances.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]oas.CapStatus, 0, len(list))
	for _, inst := range list {
		policy, err := h.policies.Get(ctx, inst.ID)
		if err != nil {
			return nil, err
		}
		capacity, err := h.planner.EffectiveCap(ctx, inst.ID, policy.HourlyCap)
		if err != nil {
			return nil, err
		}
		used, resets, err := h.budget.Used(ctx, inst.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, oas.CapStatus{InstanceID: inst.ID, Used: used, Cap: capacity, ResetsAt: resets})
	}
	return out, nil
}

// ResetState forgets processed items.
func (h *Handlers) ResetState(ctx context.Context, req oas.OptStateReset) (*oas.StateResetResult, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	var inst *uuid.UUID
	if body, ok := req.Get(); ok {
		if v, ok := body.InstanceID.Get(); ok {
			inst = &v
		}
	}
	n, err := h.memory.Reset(ctx, inst)
	if err != nil {
		return nil, err
	}
	return &oas.StateResetResult{Forgotten: n}, nil
}
