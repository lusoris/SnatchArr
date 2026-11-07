// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSnatchPolicyPresetsOrder(t *testing.T) {
	t.Parallel()
	presets := SnatchPolicyPresets()
	if len(presets) != 4 {
		t.Fatalf("len = %d; want 4", len(presets))
	}
	wantKeys := []PolicyPresetKey{PresetGentle, PresetBalanced, PresetCatchUp, PresetUpgrades}
	for i, want := range wantKeys {
		if presets[i].Key != want {
			t.Errorf("[%d].Key = %q; want %q", i, presets[i].Key, want)
		}
	}
}

func TestSnatchPolicyPresetsBalancedMatchesDefault(t *testing.T) {
	t.Parallel()
	presets := SnatchPolicyPresets()
	balanced := presets[1]
	dp := DefaultPolicy(uuid.Nil)

	if balanced.Values.MissingPerCycle != dp.MissingPerCycle {
		t.Errorf("MissingPerCycle = %d; want %d", balanced.Values.MissingPerCycle, dp.MissingPerCycle)
	}
	if balanced.Values.UpgradePerCycle != dp.UpgradePerCycle {
		t.Errorf("UpgradePerCycle = %d; want %d", balanced.Values.UpgradePerCycle, dp.UpgradePerCycle)
	}
	if balanced.Values.HourlyCap != dp.HourlyCap {
		t.Errorf("HourlyCap = %d; want %d", balanced.Values.HourlyCap, dp.HourlyCap)
	}
	if balanced.Values.CycleInterval != dp.CycleInterval {
		t.Errorf("CycleInterval = %v; want %v", balanced.Values.CycleInterval, dp.CycleInterval)
	}
	if balanced.Values.SonarrMissingMode != dp.SonarrMissingMode {
		t.Errorf("SonarrMissingMode = %q; want %q", balanced.Values.SonarrMissingMode, dp.SonarrMissingMode)
	}
	if balanced.Values.SonarrUpgradeMode != dp.SonarrUpgradeMode {
		t.Errorf("SonarrUpgradeMode = %q; want %q", balanced.Values.SonarrUpgradeMode, dp.SonarrUpgradeMode)
	}
}

func TestSnatchPolicyPresetsExactValues(t *testing.T) {
	t.Parallel()
	presets := SnatchPolicyPresets()
	cases := []struct {
		key               PolicyPresetKey
		missing           int
		upgrade           int
		interval          time.Duration
		cap               int
		selection         Selection
		hasSelection      bool
		sonarrMissingMode string
		sonarrUpgradeMode string
	}{
		{PresetGentle, 1, 0, 900 * time.Second, 4, SelectionRandom, true, "dynamic", "episodes"},
		{PresetBalanced, 2, 1, 900 * time.Second, 15, SelectionRandom, true, "dynamic", "episodes"},
		{PresetCatchUp, 10, 2, 300 * time.Second, 60, SelectionSequential, true, "dynamic", "episodes"},
		{PresetUpgrades, 1, 5, 600 * time.Second, 25, SelectionRandom, true, "dynamic", "episodes"},
	}
	if len(presets) != len(cases) {
		t.Fatalf("len = %d; want %d", len(presets), len(cases))
	}
	for i, tc := range cases {
		v := presets[i].Values
		if presets[i].Key != tc.key {
			t.Errorf("[%d] Key = %q; want %q", i, presets[i].Key, tc.key)
		}
		if v.MissingPerCycle != tc.missing {
			t.Errorf("%s: MissingPerCycle = %d; want %d", tc.key, v.MissingPerCycle, tc.missing)
		}
		if v.UpgradePerCycle != tc.upgrade {
			t.Errorf("%s: UpgradePerCycle = %d; want %d", tc.key, v.UpgradePerCycle, tc.upgrade)
		}
		if v.CycleInterval != tc.interval {
			t.Errorf("%s: CycleInterval = %v; want %v", tc.key, v.CycleInterval, tc.interval)
		}
		if v.HourlyCap != tc.cap {
			t.Errorf("%s: HourlyCap = %d; want %d", tc.key, v.HourlyCap, tc.cap)
		}
		if v.HasSelection != tc.hasSelection {
			t.Errorf("%s: HasSelection = %v; want %v", tc.key, v.HasSelection, tc.hasSelection)
		}
		if tc.hasSelection && v.Selection != tc.selection {
			t.Errorf("%s: Selection = %q; want %q", tc.key, v.Selection, tc.selection)
		}
		if v.SonarrMissingMode != tc.sonarrMissingMode {
			t.Errorf("%s: SonarrMissingMode = %q; want %q", tc.key, v.SonarrMissingMode, tc.sonarrMissingMode)
		}
		if v.SonarrUpgradeMode != tc.sonarrUpgradeMode {
			t.Errorf("%s: SonarrUpgradeMode = %q; want %q", tc.key, v.SonarrUpgradeMode, tc.sonarrUpgradeMode)
		}
	}
}
