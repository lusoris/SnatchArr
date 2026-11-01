// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients

import (
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestPriorityAttribution(t *testing.T) {
	t.Parallel()
	first, second := uuid.New(), uuid.New()
	instances := []domain.Instance{{ID: first, Kind: domain.KindRadarr}, {ID: second, Kind: domain.KindSonarr, DownloadPriority: new(90)}}
	rows := []domain.DownloadClient{{InstanceID: &first, Category: "movies"}, {InstanceID: &second, Category: "tv"}}
	for _, tc := range []struct {
		name          string
		category      string
		rows          []domain.DownloadClient
		rank, unknown int
	}{
		{"matching category uses active preset", "movies", rows, 7, 0},
		{"other instance uses override", "tv", rows, 90, 0},
		{"unknown category", "other", rows, 0, 1},
		{"case sensitive", "Movies", rows, 0, 1},
		{"missing category", "", rows, 0, 1},
		{"unknown instance", "movies", []domain.DownloadClient{{InstanceID: new(uuid.New()), Category: "movies"}}, 0, 1},
		{"global row", "movies", []domain.DownloadClient{{Category: "movies"}}, 0, 1},
		{"ambiguous", "movies", append(append([]domain.DownloadClient{}, rows...), rows[0]), 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			item := PriorityItem{Category: tc.category, ID: "id", State: domain.DownloadItemQueued, LastWrittenPriority: new(1), PausedBySnatchArr: true}
			got, unknown := attributePriorityItems([]PriorityItem{item}, tc.rows, instances, domain.PriorityPreset{domain.KindRadarr: 7})
			if unknown != tc.unknown || len(got) != 1-tc.unknown {
				t.Fatalf("ranked = %+v, unattributed = %d", got, unknown)
			}
			if len(got) > 0 && (got[0].Rank != tc.rank || got[0].LastWrittenPriority != nil || got[0].PausedBySnatchArr) {
				t.Fatalf("ranked = %+v", got)
			}
		})
	}
}

func TestPriorityPlanQueuedOnly(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		kind       domain.DownloadClientKind
		disabled   bool
		priorities []int
		wantOrder  []string
		wantWrites int
	}{
		{"qbit queued ranks", domain.ClientQBittorrent, false, []int{2, 1}, []string{"best", "worst"}, 0},
		{"qbit queue disabled", domain.ClientQBittorrent, true, []int{-1, 0}, nil, 0},
		{"qbit disabled despite stale positions", domain.ClientQBittorrent, true, []int{2, 1}, nil, 0},
		{"qbit no queue positions", domain.ClientQBittorrent, false, []int{0, -1}, nil, 0},
		{"nzbget levels", domain.ClientNZBGet, false, []int{0, 0}, nil, 2},
		{"sab levels", domain.ClientSABnzbd, false, []int{0, 0}, nil, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			items := []domain.DownloadQueueItem{
				{ID: "best", Rank: 1, State: domain.DownloadItemQueued, Priority: tc.priorities[0]},
				{ID: "worst", Rank: 100, State: domain.DownloadItemQueued, Priority: tc.priorities[1]},
				{ID: "active", Rank: 100, State: domain.DownloadItemActive},
				{ID: "user-paused", Rank: 100, State: domain.DownloadItemPaused},
				{ID: "finished", Rank: 100, State: domain.DownloadItemFinished},
			}
			got, err := planPriorityObservation(tc.kind, tc.disabled, items)
			if err != nil || !reflect.DeepEqual(got.Order, tc.wantOrder) || len(got.Priority) != tc.wantWrites || !reflect.DeepEqual(got.Pause, []string{"active"}) || len(got.Resume) != 0 {
				t.Fatalf("plan = %+v, err = %v", got, err)
			}
		})
	}
}

func TestPriorityPlanOneRank(t *testing.T) {
	t.Parallel()
	for _, kind := range []domain.DownloadClientKind{domain.ClientQBittorrent, domain.ClientNZBGet, domain.ClientSABnzbd} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			items := []domain.DownloadQueueItem{{ID: "a", Rank: 1, Priority: 2, State: domain.DownloadItemQueued}, {ID: "b", Rank: 1, Priority: 1, State: domain.DownloadItemQueued}}
			got, err := planPriorityObservation(kind, false, items)
			if err != nil || len(got.Order)+len(got.Priority)+len(got.Pause)+len(got.Resume) != 0 {
				t.Fatalf("plan = %+v, err = %v", got, err)
			}
		})
	}
}
