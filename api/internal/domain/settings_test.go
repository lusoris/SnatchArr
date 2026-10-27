// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain_test

import (
	"errors"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestSettingsValidate(t *testing.T) {
	t.Parallel()
	preset := domain.MediaFirstPreset()
	ok := domain.Settings{
		HistoryRetentionDays:   90,
		UserAgent:              "SnatchArr/1.0",
		GlobalHourlyCap:        0,
		SearchPriorityPreset:   preset,
		DownloadPriorityPreset: preset,
	}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid settings rejected: %v", err)
	}
	missingKind := domain.MediaFirstPreset()
	delete(missingKind, domain.KindSonarr)
	bad := []domain.Settings{
		{HistoryRetentionDays: 0, UserAgent: "x", SearchPriorityPreset: preset, DownloadPriorityPreset: preset},
		{HistoryRetentionDays: 4000, UserAgent: "x", SearchPriorityPreset: preset, DownloadPriorityPreset: preset},
		{HistoryRetentionDays: 1, UserAgent: " ", SearchPriorityPreset: preset, DownloadPriorityPreset: preset},
		{HistoryRetentionDays: 1, UserAgent: "a\nb", SearchPriorityPreset: preset, DownloadPriorityPreset: preset},
		{HistoryRetentionDays: 1, UserAgent: "x", GlobalHourlyCap: -1, SearchPriorityPreset: preset, DownloadPriorityPreset: preset},
		{HistoryRetentionDays: 1, UserAgent: "x", GlobalHourlyCap: domain.GlobalHourlyCapLimit + 1, SearchPriorityPreset: preset, DownloadPriorityPreset: preset},
		{HistoryRetentionDays: 90, UserAgent: "x", SearchPriorityPreset: missingKind, DownloadPriorityPreset: preset},
		{HistoryRetentionDays: 90, UserAgent: "x", SearchPriorityPreset: preset, DownloadPriorityPreset: missingKind},
	}
	for i, s := range bad {
		if err := s.Validate(); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("case %d: expected ErrInvalid, got %v", i, err)
		}
	}

	withWriteTools := domain.Settings{
		HistoryRetentionDays:   90,
		UserAgent:              "SnatchArr/1.0",
		SearchPriorityPreset:   preset,
		DownloadPriorityPreset: preset,
		LLMWriteTools:          []string{domain.LLMWriteToolTriggerRun, domain.LLMWriteToolDeleteInstance},
	}
	if err := withWriteTools.Validate(); err != nil {
		t.Fatalf("valid llm_write_tools rejected: %v", err)
	}
	allWriteTools := domain.Settings{
		HistoryRetentionDays:   90,
		UserAgent:              "SnatchArr/1.0",
		SearchPriorityPreset:   preset,
		DownloadPriorityPreset: preset,
		LLMWriteTools: []string{
			domain.LLMWriteToolTriggerRun,
			domain.LLMWriteToolUpdatePolicy,
			domain.LLMWriteToolCancelRun,
			domain.LLMWriteToolDeleteInstance,
			domain.LLMWriteToolCreateInstance,
			domain.LLMWriteToolTestInstanceInput,
			domain.LLMWriteToolUpdateInstance,
			domain.LLMWriteToolTestInstance,
			domain.LLMWriteToolUpdateQueuePolicy,
			domain.LLMWriteToolPreviewRun,
			domain.LLMWriteToolDeleteInstanceEvents,
			domain.LLMWriteToolResetState,
			domain.LLMWriteToolCreateSchedule,
			domain.LLMWriteToolUpdateSchedule,
			domain.LLMWriteToolDeleteSchedule,
			domain.LLMWriteToolImportConfigarr,
			domain.LLMWriteToolUpdateSettings,
			domain.LLMWriteToolCreateDownloadClient,
			domain.LLMWriteToolTestDownloadClientInput,
			domain.LLMWriteToolUpdateDownloadClient,
			domain.LLMWriteToolDeleteDownloadClient,
			domain.LLMWriteToolTestDownloadClient,
			domain.LLMWriteToolDiscoverDownloadClients,
			domain.LLMWriteToolCreateSeerrLink,
			domain.LLMWriteToolTestSeerrLinkInput,
			domain.LLMWriteToolSnatchSeerrRequest,
			domain.LLMWriteToolUpdateSeerrLink,
			domain.LLMWriteToolDeleteSeerrLink,
			domain.LLMWriteToolTestSeerrLink,
			domain.LLMWriteToolSyncSeerrLink,
			domain.LLMWriteToolImportSeerrInstances,
			domain.LLMWriteToolCreateProwlarrLink,
			domain.LLMWriteToolTestProwlarrLinkInput,
			domain.LLMWriteToolUpdateProwlarrLink,
			domain.LLMWriteToolDeleteProwlarrLink,
			domain.LLMWriteToolTestProwlarrLink,
			domain.LLMWriteToolCreateNotificationChannel,
			domain.LLMWriteToolUpdateNotificationChannel,
			domain.LLMWriteToolDeleteNotificationChannel,
			domain.LLMWriteToolTestNotificationChannel,
			domain.LLMWriteToolCreateNotificationRule,
			domain.LLMWriteToolUpdateNotificationRule,
			domain.LLMWriteToolDeleteNotificationRule,
		},
	}
	if err := allWriteTools.Validate(); err != nil {
		t.Fatalf("all 43 llm_write_tools rejected: %v", err)
	}
	badWriteTools := []domain.Settings{
		{HistoryRetentionDays: 90, UserAgent: "x", SearchPriorityPreset: preset, DownloadPriorityPreset: preset, LLMWriteTools: []string{"not_a_tool"}},
		{HistoryRetentionDays: 90, UserAgent: "x", SearchPriorityPreset: preset, DownloadPriorityPreset: preset, LLMWriteTools: []string{domain.LLMWriteToolTriggerRun, domain.LLMWriteToolTriggerRun}},
	}
	for i, s := range badWriteTools {
		if err := s.Validate(); !errors.Is(err, domain.ErrBadRequest) {
			t.Errorf("badWriteTools %d: expected ErrBadRequest, got %v", i, err)
		}
	}
}

func TestBrandMarkAndSfwModeValidate(t *testing.T) {
	t.Parallel()
	preset := domain.MediaFirstPreset()
	marks := []domain.BrandMark{domain.BrandMarkVulva, domain.BrandMarkPenis, domain.BrandMarkHand}
	modes := []domain.SfwMode{domain.SfwModeAuto, domain.SfwModeOn, domain.SfwModeOff}
	for _, bm := range marks {
		for _, sm := range modes {
			s := domain.Settings{
				HistoryRetentionDays:   90,
				UserAgent:              "SnatchArr/1.0",
				SearchPriorityPreset:   preset,
				DownloadPriorityPreset: preset,
				BrandMark:              bm,
				SfwMode:                sm,
			}
			if err := s.Validate(); err != nil {
				t.Fatalf("Validate(%q, %q) = %v, want nil", bm, sm, err)
			}
		}
	}
	bad := []domain.Settings{
		{HistoryRetentionDays: 90, UserAgent: "x", SearchPriorityPreset: preset, DownloadPriorityPreset: preset, BrandMark: "bogus"},
		{HistoryRetentionDays: 90, UserAgent: "x", SearchPriorityPreset: preset, DownloadPriorityPreset: preset, SfwMode: "bogus"},
	}
	for i, s := range bad {
		if err := s.Validate(); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("bad[%d]: expected ErrInvalid, got %v", i, err)
		}
	}
}

func TestUserPreferencesValidate(t *testing.T) {
	t.Parallel()
	vulva := domain.BrandMarkVulva
	hand := domain.BrandMarkHand
	on := domain.SfwModeOn
	badMark := domain.BrandMark("invalid")
	badMode := domain.SfwMode("invalid")

	cases := []struct {
		name    string
		in      domain.UserPreferences
		wantErr error
	}{
		{name: "both nil", in: domain.UserPreferences{}, wantErr: nil},
		{name: "brand_mark set", in: domain.UserPreferences{BrandMark: &vulva}, wantErr: nil},
		{name: "sfw_mode set", in: domain.UserPreferences{SfwMode: &on}, wantErr: nil},
		{name: "both set", in: domain.UserPreferences{BrandMark: &hand, SfwMode: &on}, wantErr: nil},
		{name: "invalid brand_mark", in: domain.UserPreferences{BrandMark: &badMark}, wantErr: domain.ErrInvalid},
		{name: "invalid sfw_mode", in: domain.UserPreferences{SfwMode: &badMode}, wantErr: domain.ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.in.Validate()
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Validate() = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
