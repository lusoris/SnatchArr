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

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/settings"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
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
	BrandMark:              domain.DefaultBrandMark,
	SfwMode:                domain.DefaultSfwMode,
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
		BrandMark:              s.BrandMark,
		SfwMode:                s.SfwMode,
	}
}

func settingsEqual(a, b domain.Settings) bool {
	wantMark := b.BrandMark
	if wantMark == "" {
		wantMark = domain.DefaultBrandMark
	}
	wantMode := b.SfwMode
	if wantMode == "" {
		wantMode = domain.DefaultSfwMode
	}
	return a.HistoryRetentionDays == b.HistoryRetentionDays &&
		a.UserAgent == b.UserAgent &&
		a.GlobalHourlyCap == b.GlobalHourlyCap &&
		a.LLMEnabled == b.LLMEnabled &&
		a.BrandMark == wantMark &&
		a.SfwMode == wantMode &&
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
		{
			"positive brand_mark and sfw_mode round-trip",
			domain.Settings{
				HistoryRetentionDays: 30, UserAgent: "agent/brand", GlobalHourlyCap: 50,
				BrandMark: domain.BrandMarkPenis, SfwMode: domain.SfwModeOn,
				SearchPriorityPreset: preset, DownloadPriorityPreset: preset,
			},
			domain.Settings{
				HistoryRetentionDays: 30, UserAgent: "agent/brand", GlobalHourlyCap: 50,
				BrandMark: domain.BrandMarkPenis, SfwMode: domain.SfwModeOn,
				SearchPriorityPreset: preset, DownloadPriorityPreset: preset,
			},
			nil,
		},
		{"negative invalid brand_mark", domain.Settings{HistoryRetentionDays: 30, UserAgent: "a", BrandMark: "invalid", SearchPriorityPreset: preset, DownloadPriorityPreset: preset}, defaults, domain.ErrInvalid},
		{"negative invalid sfw_mode", domain.Settings{HistoryRetentionDays: 30, UserAgent: "a", SfwMode: "invalid", SearchPriorityPreset: preset, DownloadPriorityPreset: preset}, defaults, domain.ErrInvalid},
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

func TestUserPreferences(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	clk := clock.NewFake()
	svc := settings.New(st, clk)
	ctx := t.Context()

	userID := uuid.New()
	_, err := st.Q().CreateUser(ctx, sqlcgen.CreateUserParams{
		ID:           userID,
		Username:     "prefuser",
		PasswordHash: "hash",
		Role:         "admin",
		CreatedAt:    clk.Now(),
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	empty, err := svc.GetUserPreferences(ctx, userID)
	if err != nil {
		t.Fatalf("GetUserPreferences empty: %v", err)
	}
	if empty.BrandMark != nil || empty.SfwMode != nil {
		t.Fatalf("expected nil preferences, got %+v", empty)
	}

	hand := domain.BrandMarkHand
	off := domain.SfwModeOff
	set, err := svc.UpdateUserPreferences(ctx, userID, domain.UserPreferences{BrandMark: &hand, SfwMode: &off})
	if err != nil {
		t.Fatalf("UpdateUserPreferences set: %v", err)
	}
	if set.BrandMark == nil || *set.BrandMark != domain.BrandMarkHand || set.SfwMode == nil || *set.SfwMode != domain.SfwModeOff {
		t.Fatalf("UpdateUserPreferences set = %+v, want hand/off", set)
	}

	cleared, err := svc.UpdateUserPreferences(ctx, userID, domain.UserPreferences{})
	if err != nil {
		t.Fatalf("UpdateUserPreferences clear: %v", err)
	}
	if cleared.BrandMark != nil || cleared.SfwMode != nil {
		t.Fatalf("UpdateUserPreferences clear = %+v, want both nil", cleared)
	}

	bad := domain.BrandMark("invalid")
	if _, err := svc.UpdateUserPreferences(ctx, userID, domain.UserPreferences{BrandMark: &bad}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("UpdateUserPreferences invalid = %v, want ErrInvalid", err)
	}
}
