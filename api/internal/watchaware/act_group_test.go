// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package watchaware

import (
	"testing"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

func pendingRow(series, season int32, action string) sqlcgen.WatchawareGateDecision {
	return sqlcgen.WatchawareGateDecision{
		ID:           uuid.New(),
		SeriesID:     series,
		SeasonNumber: season,
		Action:       action,
		WouldAct:     true,
	}
}

func TestGroupPendingBySeries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pending []sqlcgen.WatchawareGateDecision
		known   map[int]bool
		want    map[int]map[int]bool
		wantIDs map[int]int
	}{
		{
			name: "positive: unmonitor and promote on different seasons",
			pending: []sqlcgen.WatchawareGateDecision{
				pendingRow(7, 2, GateActionUnmonitor),
				pendingRow(7, 1, GateActionPromote),
			},
			known:   map[int]bool{7: true},
			want:    map[int]map[int]bool{7: {1: true, 2: false}},
			wantIDs: map[int]int{7: 2},
		},
		{
			name: "boundary: promote after unmonitor on one season wins",
			pending: []sqlcgen.WatchawareGateDecision{
				pendingRow(7, 2, GateActionUnmonitor),
				pendingRow(7, 2, GateActionPromote),
			},
			known:   map[int]bool{7: true},
			want:    map[int]map[int]bool{7: {2: true}},
			wantIDs: map[int]int{7: 2},
		},
		{
			name: "boundary: promote before unmonitor on one season wins",
			pending: []sqlcgen.WatchawareGateDecision{
				pendingRow(7, 2, GateActionPromote),
				pendingRow(7, 2, GateActionUnmonitor),
			},
			known:   map[int]bool{7: true},
			want:    map[int]map[int]bool{7: {2: true}},
			wantIDs: map[int]int{7: 2},
		},
		{
			name: "negative: series gone from Sonarr is skipped",
			pending: []sqlcgen.WatchawareGateDecision{
				pendingRow(9, 1, GateActionUnmonitor),
			},
			known:   map[int]bool{7: true},
			want:    map[int]map[int]bool{},
			wantIDs: map[int]int{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := groupPendingBySeries(tc.pending, tc.known)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d batches, want %d", len(got), len(tc.want))
			}
			for _, b := range got {
				wantSeasons, ok := tc.want[b.seriesID]
				if !ok {
					t.Fatalf("unexpected batch for series %d", b.seriesID)
				}
				if len(b.seasons) != len(wantSeasons) {
					t.Fatalf("series %d: got seasons %v, want %v", b.seriesID, b.seasons, wantSeasons)
				}
				for n, mon := range wantSeasons {
					if b.seasons[n] != mon {
						t.Errorf("series %d season %d: monitored %v, want %v", b.seriesID, n, b.seasons[n], mon)
					}
				}
				if len(b.decIDs) != tc.wantIDs[b.seriesID] {
					t.Errorf("series %d: %d decision ids, want %d", b.seriesID, len(b.decIDs), tc.wantIDs[b.seriesID])
				}
			}
		})
	}
}
