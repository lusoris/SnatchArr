// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func namedPriorityPresetToOAS(in domain.NamedPriorityPreset) *oas.PriorityPreset {
	out := &oas.PriorityPreset{
		Name: in.Name, Builtin: in.Builtin,
		Search: priorityPresetToOAS(in.Search), Download: priorityPresetToOAS(in.Download),
	}
	if !in.Builtin {
		out.ID = oas.NewOptUUID(in.ID)
	}
	return out
}

func priorityPresetInputFromOAS(in *oas.PriorityPresetInput) domain.PriorityPresetInput {
	return domain.PriorityPresetInput{
		Name: in.Name, Search: priorityPresetFromOAS(in.Search), Download: priorityPresetFromOAS(in.Download),
	}
}

// GetPriorityPresets returns shipped and saved search and download priority presets.
func (h *Handlers) GetPriorityPresets(ctx context.Context) ([]oas.PriorityPreset, error) {
	presets, err := h.settings.ListPriorityPresets(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]oas.PriorityPreset, 0, len(presets))
	for _, preset := range presets {
		out = append(out, *namedPriorityPresetToOAS(preset))
	}
	return out, nil
}

// CreatePriorityPreset saves a snapshot of both orders for administrators.
func (h *Handlers) CreatePriorityPreset(ctx context.Context, req *oas.PriorityPresetInput) (*oas.PriorityPreset, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	preset, err := h.settings.CreatePriorityPreset(ctx, priorityPresetInputFromOAS(req))
	if err != nil {
		return nil, err
	}
	return namedPriorityPresetToOAS(preset), nil
}

// UpdatePriorityPreset updates a saved snapshot without applying it.
func (h *Handlers) UpdatePriorityPreset(ctx context.Context, req *oas.PriorityPresetInput, params oas.UpdatePriorityPresetParams) (*oas.PriorityPreset, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	preset, err := h.settings.UpdatePriorityPreset(ctx, params.PresetId, priorityPresetInputFromOAS(req))
	if err != nil {
		return nil, err
	}
	return namedPriorityPresetToOAS(preset), nil
}

// DeletePriorityPreset removes a saved snapshot for administrators.
func (h *Handlers) DeletePriorityPreset(ctx context.Context, params oas.DeletePriorityPresetParams) error {
	if err := requireAdmin(ctx); err != nil {
		return err
	}
	return h.settings.DeletePriorityPreset(ctx, params.PresetId)
}

// ApplyPriorityPreset copies both orders to settings using the settings service.
func (h *Handlers) ApplyPriorityPreset(ctx context.Context, params oas.ApplyPriorityPresetParams) (*oas.Settings, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	in, err := h.settings.ApplyPriorityPreset(ctx, params.PresetId)
	if err != nil {
		return nil, err
	}
	return settingsToOAS(in), nil
}
