// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package settings_test

import (
	"errors"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/settings"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

// defaults are the column defaults of the settings table.
var defaults = domain.Settings{
	HistoryRetentionDays:   90,
	UserAgent:              "SnatchArr/1.0 (https://github.com/lusoris/SnatchArr)",
	GlobalHourlyCap:        0,
	SearchPriorityPreset:   domain.MediaFirstPreset(),
	DownloadPriorityPreset: domain.MediaFirstPreset(),
}

func newService(t *testing.T) *settings.Service {
	t.Helper()
	return settings.New(storetest.New(t), clock.NewFake())
}

func editable(s domain.Settings) domain.Settings {
	return domain.Settings{
		HistoryRetentionDays:   s.HistoryRetentionDays,
		UserAgent:              s.UserAgent,
		GlobalHourlyCap:        s.GlobalHourlyCap,
		SearchPriorityPreset:   s.SearchPriorityPreset,
		DownloadPriorityPreset: s.DownloadPriorityPreset,
		LLMEnabled:             s.LLMEnabled,
		LLMWriteTools:          s.LLMWriteTools,
	}
}

func settingsEqual(a, b domain.Settings) bool {
	return a.HistoryRetentionDays == b.HistoryRetentionDays &&
		a.UserAgent == b.UserAgent &&
		a.GlobalHourlyCap == b.GlobalHourlyCap &&
		a.LLMEnabled == b.LLMEnabled &&
		maps.Equal(a.SearchPriorityPreset, b.SearchPriorityPreset) &&
		maps.Equal(a.DownloadPriorityPreset, b.DownloadPriorityPreset) &&
		slices.Equal(a.LLMWriteTools, b.LLMWriteTools)
}

func TestGetCreatesTheDefaults(t *testing.T) {
	t.Parallel()
	svc := newService(t)
	for range 2 {
		got, err := svc.Get(t.Context())
		if err != nil || !settingsEqual(editable(got), defaults) {
			t.Fatalf("Get() = %+v, %v; want %+v", got, err, defaults)
		}
	}
}

func TestUpdate(t *testing.T) {
	t.Parallel()
	preset := domain.MediaFirstPreset()
	cases := []struct {
		name string
		in   domain.Settings
		want domain.Settings
		err  error
	}{
		{
			"positive user agent is trimmed",
			domain.Settings{HistoryRetentionDays: 30, UserAgent: "  agent/2  ", GlobalHourlyCap: 100, SearchPriorityPreset: preset, DownloadPriorityPreset: preset},
			domain.Settings{HistoryRetentionDays: 30, UserAgent: "agent/2", GlobalHourlyCap: 100, SearchPriorityPreset: preset, DownloadPriorityPreset: preset},
			nil,
		},
		{
			"boundary lowest values",
			domain.Settings{HistoryRetentionDays: 1, UserAgent: "a", GlobalHourlyCap: 0, SearchPriorityPreset: preset, DownloadPriorityPreset: preset},
			domain.Settings{HistoryRetentionDays: 1, UserAgent: "a", GlobalHourlyCap: 0, SearchPriorityPreset: preset, DownloadPriorityPreset: preset},
			nil,
		},
		{
			"boundary highest values",
			domain.Settings{HistoryRetentionDays: 3650, UserAgent: strings.Repeat("a", 200), GlobalHourlyCap: domain.GlobalHourlyCapLimit, SearchPriorityPreset: preset, DownloadPriorityPreset: preset},
			domain.Settings{HistoryRetentionDays: 3650, UserAgent: strings.Repeat("a", 200), GlobalHourlyCap: domain.GlobalHourlyCapLimit, SearchPriorityPreset: preset, DownloadPriorityPreset: preset},
			nil,
		},
		{
			"positive llm_enabled true and write tools round-trip",
			domain.Settings{
				HistoryRetentionDays: 30, UserAgent: "agent/llm", GlobalHourlyCap: 50, LLMEnabled: true,
				LLMWriteTools:        []string{domain.LLMWriteToolTriggerRun, domain.LLMWriteToolDeleteInstance},
				SearchPriorityPreset: preset, DownloadPriorityPreset: preset,
			},
			domain.Settings{
				HistoryRetentionDays: 30, UserAgent: "agent/llm", GlobalHourlyCap: 50, LLMEnabled: true,
				LLMWriteTools:        []string{domain.LLMWriteToolTriggerRun, domain.LLMWriteToolDeleteInstance},
				SearchPriorityPreset: preset, DownloadPriorityPreset: preset,
			},
			nil,
		},
		{"negative retention of zero days", domain.Settings{HistoryRetentionDays: 0, UserAgent: "a", SearchPriorityPreset: preset, DownloadPriorityPreset: preset}, defaults, domain.ErrInvalid},
		{"negative user agent of spaces only", domain.Settings{HistoryRetentionDays: 30, UserAgent: "   ", SearchPriorityPreset: preset, DownloadPriorityPreset: preset}, defaults, domain.ErrInvalid},
		{"negative user agent over two lines", domain.Settings{HistoryRetentionDays: 30, UserAgent: "a\nb", SearchPriorityPreset: preset, DownloadPriorityPreset: preset}, defaults, domain.ErrInvalid},
		{"negative global cap above the limit", domain.Settings{HistoryRetentionDays: 30, UserAgent: "a", GlobalHourlyCap: domain.GlobalHourlyCapLimit + 1, SearchPriorityPreset: preset, DownloadPriorityPreset: preset}, defaults, domain.ErrInvalid},
		{"negative missing priority preset", domain.Settings{HistoryRetentionDays: 30, UserAgent: "a"}, defaults, domain.ErrInvalid},
		{"negative unknown llm write tool", domain.Settings{HistoryRetentionDays: 30, UserAgent: "a", LLMWriteTools: []string{"unknown_tool"}, SearchPriorityPreset: preset, DownloadPriorityPreset: preset}, defaults, domain.ErrBadRequest},
		{"negative duplicate llm write tool", domain.Settings{HistoryRetentionDays: 30, UserAgent: "a", LLMWriteTools: []string{domain.LLMWriteToolTriggerRun, domain.LLMWriteToolTriggerRun}, SearchPriorityPreset: preset, DownloadPriorityPreset: preset}, defaults, domain.ErrBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc := newService(t)
			updated, err := svc.Update(t.Context(), tc.in)
			if !errors.Is(err, tc.err) || (tc.err == nil) != (err == nil) {
				t.Fatalf("Update() = %v, want %v", err, tc.err)
			}
			if tc.err == nil && !settingsEqual(editable(updated), tc.want) {
				t.Errorf("Update() = %+v, want %+v", updated, tc.want)
			}
			if got, err := svc.Get(t.Context()); err != nil || !settingsEqual(editable(got), tc.want) {
				t.Errorf("Get() after Update() = %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
}

func TestLLMMirrorWithModules(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	svc := settings.New(st, clock.NewFake())
	ctx := t.Context()

	preset := domain.MediaFirstPreset()
	in := domain.Settings{
		HistoryRetentionDays:   30,
		UserAgent:              "agent/mirror",
		GlobalHourlyCap:        50,
		LLMEnabled:             true,
		SearchPriorityPreset:   preset,
		DownloadPriorityPreset: preset,
	}

	// 1. Update LLMEnabled=true -> modules llm enabled
	updated, err := svc.Update(ctx, in)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if !updated.LLMEnabled {
		t.Fatalf("Update().LLMEnabled = false, want true")
	}

	mod, err := st.Q().GetModule(ctx, domain.ModuleLLM)
	if err != nil {
		t.Fatalf("GetModule(%s) error = %v", domain.ModuleLLM, err)
	}
	if !mod.Enabled {
		t.Fatalf("modules row %q Enabled = false after Update(LLMEnabled=true), want true", domain.ModuleLLM)
	}

	// 2. modules llm set false directly -> settings Get reports false
	_, err = st.Pool().Exec(ctx, "UPDATE modules SET enabled = false WHERE id = $1", domain.ModuleLLM)
	if err != nil {
		t.Fatalf("direct UPDATE modules failed: %v", err)
	}

	got, err := svc.Get(ctx)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.LLMEnabled {
		t.Errorf("Get().LLMEnabled = true after modules row set to false, want false")
	}
}
