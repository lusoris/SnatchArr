// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"encoding/json"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
)

// ListModules returns the basic state of all optional modules.
func (h *Handlers) ListModules(ctx context.Context) ([]oas.ModuleState, error) {
	list, err := h.modules.List(ctx)
	if err != nil {
		return nil, err
	}
	return moduleStatesToOAS(list), nil
}

// GetModule returns the detailed configuration and state of the specified module (admin only).
func (h *Handlers) GetModule(ctx context.Context, params oas.GetModuleParams) (*oas.Module, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	m, err := h.modules.Get(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	return moduleToOAS(m)
}

// UpdateModule updates the configuration and state of the specified module (admin only).
func (h *Handlers) UpdateModule(ctx context.Context, req *oas.ModuleUpdate, params oas.UpdateModuleParams) (*oas.Module, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	rawConfig, hasConfig, err := moduleUpdateConfigFromOAS(req.Config)
	if err != nil {
		return nil, err
	}
	var configPtr *json.RawMessage
	if hasConfig {
		raw := json.RawMessage(rawConfig)
		configPtr = &raw
	}
	m, err := h.modules.Update(ctx, params.ID, req.Enabled, configPtr)
	if err != nil {
		return nil, err
	}
	return moduleToOAS(m)
}
