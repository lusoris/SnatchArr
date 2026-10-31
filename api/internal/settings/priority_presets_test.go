// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package settings

import (
	"errors"
	"fmt"
	"maps"
	"testing"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestSettingsWithPriorityPresetCopies(t *testing.T) {
	t.Parallel()
	for _, rank := range []int{1, 10, 100} {
		t.Run(fmt.Sprintf("rank-%d", rank), func(t *testing.T) {
			t.Parallel()
			in := domain.Settings{
				HistoryRetentionDays: 30, UserAgent: "test-agent", GlobalHourlyCap: 150, LLMEnabled: true,
				SearchPriorityPreset: domain.MediaFirstPreset(), DownloadPriorityPreset: domain.MediaFirstPreset(),
			}
			preset := shippedPriorityPreset()
			preset.Search[domain.KindLidarr] = rank
			preset.Download[domain.KindSonarr] = 101 - rank
			got := settingsWithPriorityPreset(in, preset)
			if !maps.Equal(got.SearchPriorityPreset, preset.Search) || !maps.Equal(got.DownloadPriorityPreset, preset.Download) {
				t.Fatalf("applied maps = %v/%v; want %v/%v", got.SearchPriorityPreset, got.DownloadPriorityPreset, preset.Search, preset.Download)
			}
			if got.HistoryRetentionDays != 30 || got.UserAgent != "test-agent" || got.GlobalHourlyCap != 150 || !got.LLMEnabled {
				t.Fatalf("unrelated settings changed: %+v", got)
			}
			preset.Search[domain.KindLidarr] = 99
			preset.Download[domain.KindSonarr] = 99
			if got.SearchPriorityPreset[domain.KindLidarr] != rank || got.DownloadPriorityPreset[domain.KindSonarr] != 101-rank {
				t.Fatal("preset edits changed applied settings")
			}
			if in.SearchPriorityPreset[domain.KindLidarr] != 20 || in.DownloadPriorityPreset[domain.KindSonarr] != 10 {
				t.Fatal("applying mutated the input settings maps")
			}
		})
	}
}

func TestPriorityPresetReference(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	cases := []struct {
		name string
		want error
	}{
		{id.String(), nil},
		{"media_first", domain.ErrForbidden},
		{"MEDIA_FIRST", domain.ErrForbidden},
		{"", domain.ErrInvalid},
		{"night", domain.ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := priorityPresetID(tc.name)
			if !errors.Is(err, tc.want) || (tc.want == nil && got != id) {
				t.Fatalf("priorityPresetID(%q) = %v, %v; want %v", tc.name, got, err, tc.want)
			}
		})
	}
}
