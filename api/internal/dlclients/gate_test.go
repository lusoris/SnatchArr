// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/dlclients"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func client(name string, maxActive int, budget int64) domain.DownloadClient {
	return domain.DownloadClient{ID: uuid.New(), Name: name, Kind: domain.ClientQBittorrent, Enabled: true, MaxActive: maxActive, BandwidthBudgetBPS: budget}
}

func snapFor(c domain.DownloadClient, s dlclients.Snapshot) map[uuid.UUID]dlclients.Snapshot {
	s.ClientID = c.ID
	return map[uuid.UUID]dlclients.Snapshot{c.ID: s}
}

func TestEvaluate(t *testing.T) {
	t.Parallel()
	healthy := dlclients.Snapshot{Reachable: true}
	cases := []struct {
		name       string
		client     domain.DownloadClient
		snap       dlclients.Snapshot
		observed   bool
		allowed    bool
		reasonPart string
		factor     float64
	}{
		{"no clients", domain.DownloadClient{}, healthy, false, true, "", 1},
		{"not observed yet never blocks", client("q", 1, 0), dlclients.Snapshot{}, false, true, "", 1},
		{"healthy", client("q", 0, 0), healthy, true, true, "", 1},
		{"unreachable", client("q", 0, 0), dlclients.Snapshot{Error: "dial tcp: refused"}, true, false, "unreachable", 0},
		{"paused", client("q", 0, 0), dlclients.Snapshot{Reachable: true, Paused: true}, true, false, "paused", 0},
		{"at max active", client("q", 3, 0), dlclients.Snapshot{Reachable: true, Active: 3}, true, false, "3 active downloads (limit 3)", 0},
		{"below max active", client("q", 3, 0), dlclients.Snapshot{Reachable: true, Active: 2}, true, true, "", 1},
		{"half bandwidth", client("q", 0, 1000), dlclients.Snapshot{Reachable: true, DownloadRate: 500}, true, true, "", 0.5},
		{"bandwidth saturated", client("q", 0, 1000), dlclients.Snapshot{Reachable: true, DownloadRate: 1200}, true, false, "saturating", 0},
		{"disabled client ignored", func() domain.DownloadClient { c := client("q", 0, 0); c.Enabled = false; return c }(), dlclients.Snapshot{}, true, true, "", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var clients []domain.DownloadClient
			snaps := map[uuid.UUID]dlclients.Snapshot{}
			if tc.client.Name != "" {
				clients = []domain.DownloadClient{tc.client}
			}
			if tc.observed {
				snaps = snapFor(tc.client, tc.snap)
			}
			v := dlclients.Evaluate(clients, snaps)
			if v.Allowed != tc.allowed || !strings.Contains(v.Reason, tc.reasonPart) || v.PaceFactor != tc.factor {
				t.Fatalf("got %+v", v)
			}
		})
	}
}

func TestEvaluateTakesTheTightestBudget(t *testing.T) {
	t.Parallel()
	a, b := client("a", 0, 1000), client("b", 0, 1000)
	snaps := map[uuid.UUID]dlclients.Snapshot{
		a.ID: {ClientID: a.ID, Reachable: true, DownloadRate: 250},
		b.ID: {ClientID: b.ID, Reachable: true, DownloadRate: 900},
	}
	v := dlclients.Evaluate([]domain.DownloadClient{a, b}, snaps)
	if !v.Allowed || v.PaceFactor < 0.09 || v.PaceFactor > 0.11 {
		t.Fatalf("got %+v", v)
	}
}

func TestScale(t *testing.T) {
	t.Parallel()
	cases := []struct {
		perCycle int
		factor   float64
		want     int
	}{
		{0, 1, 0}, {5, 1, 5}, {5, 1.5, 5}, {5, 0, 0}, {5, 0.5, 3}, {5, 0.01, 1}, {-1, 1, 0}, {10, 0.25, 3},
	}
	for _, tc := range cases {
		if got := dlclients.Scale(tc.perCycle, tc.factor); got != tc.want {
			t.Errorf("Scale(%d, %v) = %d, want %d", tc.perCycle, tc.factor, got, tc.want)
		}
	}
}
