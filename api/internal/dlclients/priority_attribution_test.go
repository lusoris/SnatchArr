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
	c1, c2 := uuid.New(), uuid.New()
	instances := []domain.Instance{{ID: first, Kind: domain.KindRadarr}, {ID: second, Kind: domain.KindSonarr, DownloadPriority: new(90)}}
	rows := []domain.DownloadClient{{ID: c1, InstanceID: &first, Category: "movies"}, {ID: c2, InstanceID: &second, Category: "tv"}}
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
		{"unknown instance", "movies", []domain.DownloadClient{{ID: uuid.New(), InstanceID: new(uuid.New()), Category: "movies"}}, 0, 1},
		{"global row", "movies", []domain.DownloadClient{{ID: uuid.New(), Category: "movies"}}, 0, 1},
		{"ambiguous", "movies", append(append([]domain.DownloadClient{}, rows...), rows[0]), 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			item := PriorityItem{Category: tc.category, ID: "id", State: domain.DownloadItemQueued, LastWrittenPriority: new(1), PausedBySnatchArr: true}
			got, unknown, _ := attributePriorityItems([]PriorityItem{item}, tc.rows, instances, domain.PriorityPreset{domain.KindRadarr: 7}, nil)
			if unknown != tc.unknown || len(got) != 1-tc.unknown {
				t.Fatalf("ranked = %+v, unattributed = %d", got, unknown)
			}
			if len(got) > 0 && (got[0].Rank != tc.rank || got[0].LastWrittenPriority != nil || got[0].PausedBySnatchArr) {
				t.Fatalf("ranked = %+v", got)
			}
		})
	}
}

func TestPriorityAttributionWithOwnership(t *testing.T) {
	t.Parallel()
	instID := uuid.New()
	clientID := uuid.New()
	instances := []domain.Instance{{ID: instID, Kind: domain.KindRadarr}}
	rows := []domain.DownloadClient{{ID: clientID, InstanceID: &instID, Category: "movies"}}
	prio := 50
	ownership := []PriorityOwnership{
		{ClientID: clientID, ItemID: "owned-item", LastWrittenPriority: &prio, PausedBySnatchArr: true},
		{ClientID: uuid.New(), ItemID: "other-client", LastWrittenPriority: &prio, PausedBySnatchArr: true},
	}
	items := []PriorityItem{
		{Category: "movies", ID: "owned-item", State: domain.DownloadItemQueued},
		{Category: "movies", ID: "unowned-item", State: domain.DownloadItemQueued},
		{Category: "movies", ID: "other-client", State: domain.DownloadItemQueued},
	}
	got, unknown, itemClients := attributePriorityItems(items, rows, instances, domain.PriorityPreset{domain.KindRadarr: 1}, ownership)
	if unknown != 0 || len(got) != 3 {
		t.Fatalf("got = %+v, unknown = %d", got, unknown)
	}
	if itemClients["owned-item"] != clientID {
		t.Fatalf("itemClients = %+v", itemClients)
	}
	if got[0].LastWrittenPriority == nil || *got[0].LastWrittenPriority != 50 || !got[0].PausedBySnatchArr {
		t.Fatalf("owned item ownership not fed: %+v", got[0])
	}
	if got[1].LastWrittenPriority != nil || got[1].PausedBySnatchArr {
		t.Fatalf("unowned item should be nil/false: %+v", got[1])
	}
	if got[2].LastWrittenPriority != nil || got[2].PausedBySnatchArr {
		t.Fatalf("other client item should not match: %+v", got[2])
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
