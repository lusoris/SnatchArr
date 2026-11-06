// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// GetSnatchPolicyPresets returns the shipped snatch policy presets.
func (h *Handlers) GetSnatchPolicyPresets(_ context.Context) ([]oas.SnatchPolicyPreset, error) {
	presets := domain.SnatchPolicyPresets()
	out := make([]oas.SnatchPolicyPreset, 0, len(presets))
	for _, p := range presets {
		item := oas.SnatchPolicyPreset{
			Key:             string(p.Key),
			MissingPerCycle: p.Values.MissingPerCycle,
			UpgradePerCycle: p.Values.UpgradePerCycle,
			CycleIntervalS:  int(p.Values.CycleInterval.Seconds()),
			HourlyCap:       p.Values.HourlyCap,
		}
		if p.Values.HasSelection {
			item.Selection = oas.NewOptSnatchPolicyPresetSelection(
				oas.SnatchPolicyPresetSelection(p.Values.Selection),
			)
		}
		out = append(out, item)
	}
	return out, nil
}
