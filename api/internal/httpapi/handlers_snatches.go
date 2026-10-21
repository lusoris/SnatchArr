// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

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

// PreviewRun queues a dry-run / preview snatch run immediately.
func (h *Handlers) PreviewRun(ctx context.Context, params oas.PreviewRunParams) (*oas.PreviewResult, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	if _, err := h.instances.Get(ctx, params.InstanceId); err != nil {
		return nil, err
	}
	kind, err := domain.ParseSnatchKind(params.Kind)
	if err != nil {
		return nil, err
	}
	seed, err := randomSeed()
	if err != nil {
		return nil, fmt.Errorf("crypto/rand seed: %w", err)
	}
	run, created, err := h.runs.EnqueueWithOptions(ctx, snatch.EnqueueOptions{
		InstanceID: params.InstanceId,
		Kind:       kind,
		DryRun:     true,
		Seed:       seed,
	})
	if err != nil {
		return nil, err
	}
	if !created {
		return nil, fmt.Errorf("%w: a run for kind %s is already active", domain.ErrConflict, kind)
	}
	h.recordPreview(ctx, run)
	return &oas.PreviewResult{
		RunID: run.ID,
		Seed:  seed,
	}, nil
}

func (h *Handlers) recordPreview(ctx context.Context, run domain.Run) {
	err := h.rec.Record(ctx, domain.Event{
		RunID: &run.ID, InstanceID: run.InstanceID, Level: "info", Type: "run_queued",
		Title: fmt.Sprintf("Preview: %s snatch queued by hand", run.Kind),
	})
	if err != nil {
		h.logger.WarnContext(ctx, "httpapi: record preview", slog.String("error", err.Error()))
	}
}

// seedDraws bounds the redraws of an all-zero seed; 16 zero draws in a row from crypto/rand
// mean the source is broken.
const seedDraws = 16

func randomSeed() (int64, error) {
	for range seedDraws {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			return 0, fmt.Errorf("crypto/rand read: %w", err)
		}

		// 53 bits: the seed stays exact as a JSON number in the browser.
		s := int64(binary.BigEndian.Uint64(b[:]) & (1<<53 - 1))
		if s != 0 {
			return s, nil
		}
	}
	return 0, errors.New("crypto/rand returned only zero seeds")
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

// GetRun loads a single run by ID.
func (h *Handlers) GetRun(ctx context.Context, params oas.GetRunParams) (*oas.Run, error) {
	run, err := h.runs.Get(ctx, params.RunId)
	if err != nil {
		return nil, err
	}
	out := runToOAS(run)
	if !run.DryRun {
		return &out, nil
	}
	targets, err := h.previewedTargets(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	out.PreviewedTargets = targets
	return &out, nil
}

func (h *Handlers) previewedTargets(ctx context.Context, runID uuid.UUID) ([]oas.PreviewTarget, error) {
	evs, err := h.rec.ListForRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	targets := make([]oas.PreviewTarget, 0, len(evs))
	for _, ev := range evs {
		if ev.Type != "search_previewed" {
			continue
		}
		cost, err := strconv.Atoi(strings.TrimSpace(ev.Detail))
		if err != nil {
			return nil, fmt.Errorf("httpapi: preview event %d cost %q: %w", ev.ID, ev.Detail, err)
		}
		targets = append(targets, oas.PreviewTarget{
			EntityType: ev.EntityType,
			ID:         ev.EntityID,
			Title:      ev.Title,
			Cost:       cost,
		})
	}
	return targets, nil
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

// GetHourlyCaps reports stamina per instance, plus the shared global stamina when set.
func (h *Handlers) GetHourlyCaps(ctx context.Context) ([]oas.CapStatus, error) {
	list, err := h.instances.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]oas.CapStatus, 0, len(list)+1)
	if global, ok, err := h.globalCap(ctx); err != nil {
		return nil, err
	} else if ok {
		out = append(out, global)
	}
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
		out = append(out, oas.CapStatus{Scope: oas.CapStatusScopeInstance, InstanceID: oas.NewOptUUID(inst.ID), Used: used, Cap: capacity, ResetsAt: resets})
	}
	return out, nil
}

// globalCap is the shared stamina entry, absent when no global cap is configured.
func (h *Handlers) globalCap(ctx context.Context) (oas.CapStatus, bool, error) {
	cfg, err := h.settings.Get(ctx)
	if err != nil || cfg.GlobalHourlyCap <= 0 {
		return oas.CapStatus{}, false, err
	}
	used, resets, err := h.budget.GlobalUsed(ctx)
	if err != nil {
		return oas.CapStatus{}, false, err
	}
	return oas.CapStatus{Scope: oas.CapStatusScopeGlobal, Used: used, Cap: cfg.GlobalHourlyCap, ResetsAt: resets}, true, nil
}

// GetSettings returns the runtime settings.
func (h *Handlers) GetSettings(ctx context.Context) (*oas.Settings, error) {
	s, err := h.settings.Get(ctx)
	if err != nil {
		return nil, err
	}
	return settingsToOAS(s), nil
}

// UpdateSettings replaces the runtime settings.
func (h *Handlers) UpdateSettings(ctx context.Context, req *oas.Settings) (*oas.Settings, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	in := settingsFromOAS(req)
	if err := in.SearchPriorityPreset.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %s", domain.ErrBadRequest, err.Error())
	}
	if err := in.DownloadPriorityPreset.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %s", domain.ErrBadRequest, err.Error())
	}
	s, err := h.settings.Update(ctx, in)
	if err != nil {
		return nil, err
	}
	return settingsToOAS(s), nil
}

// GetPriorityPresets returns the shipped search and download priority presets.
func (h *Handlers) GetPriorityPresets(_ context.Context) ([]oas.PriorityPreset, error) {
	preset := priorityPresetToOAS(domain.MediaFirstPreset())
	return []oas.PriorityPreset{
		{
			Name:     domain.PresetMediaFirst,
			Search:   preset,
			Download: preset,
		},
	}, nil
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
