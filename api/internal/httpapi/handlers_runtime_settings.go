// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/runtimesettings"
)

// GetRuntimeSettings returns all runtime settings (admin only).
func (h *Handlers) GetRuntimeSettings(ctx context.Context) (oas.GetRuntimeSettingsRes, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	if h.runtimeSettings == nil {
		return nil, errors.New("runtime settings service unavailable")
	}
	list, err := h.runtimeSettings.List(ctx)
	if err != nil {
		return nil, err
	}
	res := runtimeSettingsToOAS(list)
	return &res, nil
}

// UpdateRuntimeSettings validates and applies a batch of runtime setting overrides (admin only).
func (h *Handlers) UpdateRuntimeSettings(ctx context.Context, req *oas.RuntimeSettingsUpdateRequest) (oas.UpdateRuntimeSettingsRes, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	if h.runtimeSettings == nil {
		return nil, errors.New("runtime settings service unavailable")
	}
	var values map[string]string
	if req != nil {
		values = req.Values
	}
	var userID *uuid.UUID
	if u, ok := UserFrom(ctx); ok {
		userID = &u.ID
	}
	res, err := h.runtimeSettings.Put(ctx, userID, values)
	if err != nil {
		return nil, err
	}
	list, err := h.runtimeSettings.List(ctx)
	if err != nil {
		return nil, err
	}
	return &oas.RuntimeSettingsUpdateResult{
		PendingRestart: res.PendingRestart,
		Settings:       runtimeSettingsToOAS(list),
	}, nil
}

func runtimeSettingToOAS(s runtimesettings.SettingItem) oas.RuntimeSetting {
	item := oas.RuntimeSetting{
		Key:     s.Key,
		Env:     s.Env,
		Class:   oas.RuntimeSettingClass(s.Class),
		Type:    s.Type,
		Default: s.Default,
		Source:  oas.RuntimeSettingSource(s.Source),
		Locked:  s.Locked,
		Apply:   oas.RuntimeSettingApply(s.Apply),
	}
	if s.Value != nil {
		item.Value = oas.NewOptString(*s.Value)
	}
	if s.IsSet != nil {
		item.IsSet = oas.NewOptBool(*s.IsSet)
	}
	return item
}

func runtimeSettingsToOAS(list []runtimesettings.SettingItem) oas.GetRuntimeSettingsOKApplicationJSON {
	out := make(oas.GetRuntimeSettingsOKApplicationJSON, 0, len(list))
	for _, item := range list {
		out = append(out, runtimeSettingToOAS(item))
	}
	return out
}
