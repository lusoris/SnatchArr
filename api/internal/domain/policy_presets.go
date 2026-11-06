// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain

import "time"

// PolicyPresetKey identifies a shipped snatch policy preset.
type PolicyPresetKey string

// Shipped snatch policy preset keys.
const (
	PresetGentle   PolicyPresetKey = "gentle"
	PresetBalanced PolicyPresetKey = "balanced"
	PresetCatchUp  PolicyPresetKey = "catch_up"
	PresetUpgrades PolicyPresetKey = "upgrades"
)

// PolicyPresetValues holds the partial policy fields a preset applies.
type PolicyPresetValues struct {
	MissingPerCycle int
	UpgradePerCycle int
	CycleInterval   time.Duration
	HourlyCap       int
	Selection       Selection
	HasSelection    bool
}

// PolicyPreset is a shipped preset with its key and values.
type PolicyPreset struct {
	Key    PolicyPresetKey
	Values PolicyPresetValues
}

// SnatchPolicyPresets returns the shipped presets in display order.
// Values and reasons: docs/adr/0020-snatch-policy-defaults-and-presets.md.
func SnatchPolicyPresets() []PolicyPreset {
	return []PolicyPreset{
		{Key: PresetGentle, Values: PolicyPresetValues{
			MissingPerCycle: 1, UpgradePerCycle: 0,
			CycleInterval: 900 * time.Second, HourlyCap: 4,
		}},
		{Key: PresetBalanced, Values: PolicyPresetValues{
			MissingPerCycle: 2, UpgradePerCycle: 1,
			CycleInterval: 900 * time.Second, HourlyCap: 15,
		}},
		{Key: PresetCatchUp, Values: PolicyPresetValues{
			MissingPerCycle: 10, UpgradePerCycle: 2,
			CycleInterval: 300 * time.Second, HourlyCap: 60,
			Selection: SelectionSequential, HasSelection: true,
		}},
		{Key: PresetUpgrades, Values: PolicyPresetValues{
			MissingPerCycle: 1, UpgradePerCycle: 5,
			CycleInterval: 600 * time.Second, HourlyCap: 25,
		}},
	}
}
