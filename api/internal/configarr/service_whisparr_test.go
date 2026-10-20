// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package configarr_test

import (
	"strings"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/configarr"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/instances"
)

type whisparrTestCase struct {
	name         string
	probeRes     arrclient.Result
	probeErr     error
	initialKind  domain.AppKind
	wantKind     domain.AppKind
	wantImported int
	wantUpdated  int
	wantWarnSub  string
	wantNoWarn   bool
}

func whisparrTestCases() []whisparrTestCase {
	return []whisparrTestCase{
		{
			name:         "positive_probe_3_whisparr_v3",
			probeRes:     arrclient.Result{AppName: "Whisparr", Version: "3.0.1.123"},
			probeErr:     nil,
			initialKind:  "",
			wantKind:     domain.KindWhisparrV3,
			wantImported: 1,
			wantUpdated:  0,
			wantNoWarn:   true,
		},
		{
			name:         "positive_probe_2_whisparr_v2",
			probeRes:     arrclient.Result{AppName: "Whisparr", Version: "2.0.0.1234"},
			probeErr:     domain.ErrInvalid,
			initialKind:  "",
			wantKind:     domain.KindWhisparrV2,
			wantImported: 1,
			wantUpdated:  0,
			wantNoWarn:   true,
		},
		{
			name:         "negative_probe_unreachable_nothing_stored",
			probeRes:     arrclient.Result{},
			probeErr:     domain.ErrUnreachable,
			initialKind:  "",
			wantKind:     "",
			wantImported: 0,
			wantUpdated:  0,
			wantWarnSub:  "not imported",
		},
		{
			name:         "negative_probe_reports_4_0_0",
			probeRes:     arrclient.Result{AppName: "Whisparr", Version: "4.0.0"},
			probeErr:     domain.ErrInvalid,
			initialKind:  "",
			wantKind:     "",
			wantImported: 0,
			wantUpdated:  0,
			wantWarnSub:  "not imported",
		},
		{
			name:         "boundary_probe_unreachable_stored_whisparr_v3",
			probeRes:     arrclient.Result{},
			probeErr:     domain.ErrUnreachable,
			initialKind:  domain.KindWhisparrV3,
			wantKind:     domain.KindWhisparrV3,
			wantImported: 0,
			wantUpdated:  1,
			wantWarnSub:  "could not probe Whisparr",
		},
		{
			name:         "boundary_probe_unreachable_stored_whisparr_v2",
			probeRes:     arrclient.Result{},
			probeErr:     domain.ErrUnreachable,
			initialKind:  domain.KindWhisparrV2,
			wantKind:     domain.KindWhisparrV2,
			wantImported: 0,
			wantUpdated:  1,
			wantWarnSub:  "could not probe Whisparr",
		},
	}
}

func TestWhisparr_Detection(t *testing.T) {
	t.Parallel()
	for _, tc := range whisparrTestCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runWhisparrTest(t, tc)
		})
	}
}

func runWhisparrTest(t *testing.T, tc whisparrTestCase) {
	t.Helper()
	f := newFixture(t)
	ctx := t.Context()
	seedWhisparrInitialKind(t, f, tc.initialKind)
	f.probe.res = tc.probeRes
	f.probe.err = tc.probeErr
	dir := t.TempDir()
	configPath := writeConfig(t, dir, `
whisparr:
  main:
    base_url: http://whisparr.media.svc:6969
    api_key: whisparr-api-key
`)
	res, err := f.svc.Import(ctx, configPath, "")
	if err != nil {
		t.Fatalf("Import() err = %v", err)
	}
	assertWhisparrCounts(t, res, tc)
	assertWhisparrWarnings(t, res.Warnings, tc)
	assertWhisparrStoredKind(t, f, tc.wantKind)
}

func seedWhisparrInitialKind(t *testing.T, f fixture, initialKind domain.AppKind) {
	t.Helper()
	if initialKind == "" {
		return
	}
	_, err := f.instSvc.UpsertConfigarr(t.Context(), "whisparr/main", instances.Input{
		Kind:    initialKind,
		Name:    "main",
		BaseURL: "http://whisparr.media.svc:6969",
		APIKey:  "whisparr-api-key",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("setup initial kind: %v", err)
	}
}

func assertWhisparrCounts(t *testing.T, res configarr.Result, tc whisparrTestCase) {
	t.Helper()
	if res.Imported != tc.wantImported {
		t.Errorf("Imported = %d, want %d", res.Imported, tc.wantImported)
	}
	if res.Updated != tc.wantUpdated {
		t.Errorf("Updated = %d, want %d", res.Updated, tc.wantUpdated)
	}
}

func assertWhisparrWarnings(t *testing.T, warnings []string, tc whisparrTestCase) {
	t.Helper()
	if tc.wantNoWarn && len(warnings) > 0 {
		t.Errorf("Warnings = %v, want none", warnings)
	}
	if tc.wantWarnSub != "" {
		for _, w := range warnings {
			if strings.Contains(w, tc.wantWarnSub) {
				return
			}
		}
		t.Errorf("warnings %v do not contain %q", warnings, tc.wantWarnSub)
	}
}

func assertWhisparrStoredKind(t *testing.T, f fixture, wantKind domain.AppKind) {
	t.Helper()
	ctx := t.Context()
	storedKind, ok := f.instSvc.KindByConfigarrKey(ctx, "whisparr/main")
	if wantKind == "" {
		if ok {
			t.Errorf("found stored kind %v, want not imported", storedKind)
		}
		return
	}
	if !ok || storedKind != wantKind {
		t.Errorf("storedKind = %v (ok=%v), want %v", storedKind, ok, wantKind)
	}
}
