// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package settings_test

import (
	"errors"
	"maps"
	"os"
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
	}
}

func settingsEqual(a, b domain.Settings) bool {
	return a.HistoryRetentionDays == b.HistoryRetentionDays &&
		a.UserAgent == b.UserAgent &&
		a.GlobalHourlyCap == b.GlobalHourlyCap &&
		maps.Equal(a.SearchPriorityPreset, b.SearchPriorityPreset) &&
		maps.Equal(a.DownloadPriorityPreset, b.DownloadPriorityPreset)
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
		{"negative retention of zero days", domain.Settings{HistoryRetentionDays: 0, UserAgent: "a", SearchPriorityPreset: preset, DownloadPriorityPreset: preset}, defaults, domain.ErrInvalid},
		{"negative user agent of spaces only", domain.Settings{HistoryRetentionDays: 30, UserAgent: "   ", SearchPriorityPreset: preset, DownloadPriorityPreset: preset}, defaults, domain.ErrInvalid},
		{"negative user agent over two lines", domain.Settings{HistoryRetentionDays: 30, UserAgent: "a\nb", SearchPriorityPreset: preset, DownloadPriorityPreset: preset}, defaults, domain.ErrInvalid},
		{"negative global cap above the limit", domain.Settings{HistoryRetentionDays: 30, UserAgent: "a", GlobalHourlyCap: domain.GlobalHourlyCapLimit + 1, SearchPriorityPreset: preset, DownloadPriorityPreset: preset}, defaults, domain.ErrInvalid},
		{"negative missing priority preset", domain.Settings{HistoryRetentionDays: 30, UserAgent: "a"}, defaults, domain.ErrInvalid},
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
