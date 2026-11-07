// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestGetSnatchPolicyPresetsHTTP(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	_ = completeInitialSetup(t, f)

	resp, raw := doPriorityRequest(t, f, http.MethodGet, "/snatch-policy-presets", "", "")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /snatch-policy-presets = %d; want 200: %s", resp.StatusCode, raw)
	}

	var presets []oas.SnatchPolicyPreset
	if err := json.Unmarshal(raw, &presets); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Four shipped presets, in order.
	if len(presets) != 4 {
		t.Fatalf("len(presets) = %d; want 4", len(presets))
	}
	wantKeys := []string{"gentle", "balanced", "catch_up", "upgrades"}
	for i, want := range wantKeys {
		if presets[i].Key != want {
			t.Errorf("presets[%d].Key = %q; want %q", i, presets[i].Key, want)
		}
	}

	// Balanced matches server defaults.
	balanced := presets[1]
	dp := domain.DefaultPolicy(uuid.Nil)
	if balanced.MissingPerCycle != dp.MissingPerCycle {
		t.Errorf("balanced.MissingPerCycle = %d; want %d", balanced.MissingPerCycle, dp.MissingPerCycle)
	}
	if balanced.UpgradePerCycle != dp.UpgradePerCycle {
		t.Errorf("balanced.UpgradePerCycle = %d; want %d", balanced.UpgradePerCycle, dp.UpgradePerCycle)
	}
	if balanced.HourlyCap != dp.HourlyCap {
		t.Errorf("balanced.HourlyCap = %d; want %d", balanced.HourlyCap, dp.HourlyCap)
	}
	if balanced.CycleIntervalS != int(dp.CycleInterval.Seconds()) {
		t.Errorf("balanced.CycleIntervalS = %d; want %d", balanced.CycleIntervalS, int(dp.CycleInterval.Seconds()))
	}
	if string(balanced.SonarrMissingMode) != dp.SonarrMissingMode {
		t.Errorf("balanced.SonarrMissingMode = %q; want %q", balanced.SonarrMissingMode, dp.SonarrMissingMode)
	}
	if string(balanced.SonarrUpgradeMode) != dp.SonarrUpgradeMode {
		t.Errorf("balanced.SonarrUpgradeMode = %q; want %q", balanced.SonarrUpgradeMode, dp.SonarrUpgradeMode)
	}

	// Every preset carries Sonarr modes per ADR-0020 matrix.
	for i, p := range presets {
		if string(p.SonarrMissingMode) != "dynamic" {
			t.Errorf("presets[%d].SonarrMissingMode = %q; want 'dynamic'", i, p.SonarrMissingMode)
		}
		if string(p.SonarrUpgradeMode) != "episodes" {
			t.Errorf("presets[%d].SonarrUpgradeMode = %q; want 'episodes'", i, p.SonarrUpgradeMode)
		}
	}

	// Every preset sets selection per ADR-0020 matrix.
	catchUp := presets[2]
	if !catchUp.Selection.Set {
		t.Error("catch_up.Selection not set; want sequential")
	} else if catchUp.Selection.Value != oas.SnatchPolicyPresetSelectionSequential {
		t.Errorf("catch_up.Selection = %q; want sequential", catchUp.Selection.Value)
	}
	if !presets[0].Selection.Set {
		t.Error("gentle.Selection not set; want random")
	} else if presets[0].Selection.Value != oas.SnatchPolicyPresetSelectionRandom {
		t.Errorf("gentle.Selection = %q; want random", presets[0].Selection.Value)
	}

	// Negative: gentle has upgrade_per_cycle=0.
	gentle := presets[0]
	if gentle.UpgradePerCycle != 0 {
		t.Errorf("gentle.UpgradePerCycle = %d; want 0", gentle.UpgradePerCycle)
	}
}
