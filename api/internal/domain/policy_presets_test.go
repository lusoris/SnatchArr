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
}

func TestSnatchPolicyPresetsValuesInRange(t *testing.T) {
	t.Parallel()
	for _, p := range SnatchPolicyPresets() {
		v := p.Values
		if v.MissingPerCycle < 0 || v.MissingPerCycle > 100 {
			t.Errorf("%s: MissingPerCycle %d out of [0, 100]", p.Key, v.MissingPerCycle)
		}
		if v.UpgradePerCycle < 0 || v.UpgradePerCycle > 100 {
			t.Errorf("%s: UpgradePerCycle %d out of [0, 100]", p.Key, v.UpgradePerCycle)
		}
		if v.CycleInterval < 60*time.Second || v.CycleInterval > 604800*time.Second {
			t.Errorf("%s: CycleInterval %v out of [60s, 604800s]", p.Key, v.CycleInterval)
		}
		if v.HourlyCap < 1 || v.HourlyCap > 500 {
			t.Errorf("%s: HourlyCap %d out of [1, 500]", p.Key, v.HourlyCap)
		}
	}
}

func TestSnatchPolicyPresetsCatchUpHasSelection(t *testing.T) {
	t.Parallel()
	presets := SnatchPolicyPresets()
	catchUp := presets[2]
	if !catchUp.Values.HasSelection {
		t.Fatal("catch_up: HasSelection false; want true")
	}
	if catchUp.Values.Selection != SelectionSequential {
		t.Errorf("catch_up: Selection = %q; want sequential", catchUp.Values.Selection)
	}
	for _, p := range []PolicyPreset{presets[0], presets[1], presets[3]} {
		if p.Values.HasSelection {
			t.Errorf("%s: HasSelection true; want false", p.Key)
		}
	}
}
