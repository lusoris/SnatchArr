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
	ok := domain.Settings{HistoryRetentionDays: 90, UserAgent: "SnatchArr/1.0", GlobalHourlyCap: 0}
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid settings rejected: %v", err)
	}
	bad := []domain.Settings{
		{HistoryRetentionDays: 0, UserAgent: "x"},
		{HistoryRetentionDays: 4000, UserAgent: "x"},
		{HistoryRetentionDays: 1, UserAgent: " "},
		{HistoryRetentionDays: 1, UserAgent: "a\nb"},
		{HistoryRetentionDays: 1, UserAgent: "x", GlobalHourlyCap: -1},
		{HistoryRetentionDays: 1, UserAgent: "x", GlobalHourlyCap: domain.GlobalHourlyCapLimit + 1},
	}
	for i, s := range bad {
		if err := s.Validate(); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("case %d: expected ErrInvalid, got %v", i, err)
		}
	}
}
