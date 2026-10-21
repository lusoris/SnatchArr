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
