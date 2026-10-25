// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package watchaware

import (
	"testing"
)

func TestSeasonWatched(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		watched     int
		total       int
		lastWatched bool
		want        bool
	}{
		{
			name:        "boundary 499 of 1000 episodes (49.9%) holds",
			watched:     499,
			total:       1000,
			lastWatched: false,
			want:        false,
		},
		{
			name:        "boundary 500 of 1000 episodes (50.0%) promotes",
			watched:     500,
			total:       1000,
			lastWatched: false,
			want:        true,
		},
		{
			name:        "boundary 1 of 2 episodes (50.0%) promotes",
			watched:     1,
			total:       2,
			lastWatched: false,
			want:        true,
		},
		{
			name:        "0 of 2 episodes (0.0%) holds",
			watched:     0,
			total:       2,
			lastWatched: false,
			want:        false,
		},
		{
			name:        "0 total episodes holds",
			watched:     0,
			total:       0,
			lastWatched: false,
			want:        false,
		},
		{
			name:        "last episode watched alone promotes even if share is below 50%",
			watched:     1,
			total:       10,
			lastWatched: true,
			want:        true,
		},
		{
			name:        "all episodes watched promotes",
			watched:     10,
			total:       10,
			lastWatched: true,
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := seasonWatched(tt.watched, tt.total, tt.lastWatched)
			if got != tt.want {
				t.Errorf("seasonWatched(%d, %d, %v) = %v, want %v",
					tt.watched, tt.total, tt.lastWatched, got, tt.want)
			}
		})
	}
}

func TestEvaluatePromotion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		seriesID int
		held     []int32
		watches  []SeasonWatch
		cfg      GateConfig
		want     []GateDecision
	}{
		{
			name:     "positive season 1 fully watched promotes season 2",
			seriesID: 10,
			held:     []int32{2, 3},
			watches: []SeasonWatch{
				{UserKey: "s1/u1", Season: 1, Episode: 1, Watched: true},
				{UserKey: "s1/u1", Season: 1, Episode: 2, Watched: true},
			},
			cfg: GateConfig{
				Mode:         GateModeObserve,
				LookAhead:    1,
				PromoteWhose: "any",
			},
			want: []GateDecision{
				{
					SeriesID: 10,
					Season:   2,
					Action:   GateActionPromote,
					WouldAct: false,
					Reason:   "season 1 watched (any), look-ahead 1",
				},
			},
		},
		{
			name:     "boundary 49.9% holds",
			seriesID: 10,
			held:     []int32{2, 3},
			watches: func() []SeasonWatch {
				ws := make([]SeasonWatch, 1000)
				for i := range ws {
					ws[i] = SeasonWatch{
						UserKey: "s1/u1",
						Season:  1,
						Episode: i + 1,
						Watched: i < 499, // 499 of 1000; episode 1000 unwatched
					}
				}
				return ws
			}(),
			cfg: GateConfig{
				Mode:         GateModeObserve,
				LookAhead:    1,
				PromoteWhose: "any",
			},
			want: nil,
		},
		{
			name:     "boundary 50.0% promotes",
			seriesID: 10,
			held:     []int32{2, 3},
			watches: []SeasonWatch{
				{UserKey: "s1/u1", Season: 1, Episode: 1, Watched: true},
				{UserKey: "s1/u1", Season: 1, Episode: 2, Watched: false},
			},
			cfg: GateConfig{
				Mode:         GateModeObserve,
				LookAhead:    1,
				PromoteWhose: "any",
			},
			want: []GateDecision{
				{
					SeriesID: 10,
					Season:   2,
					Action:   GateActionPromote,
					WouldAct: false,
					Reason:   "season 1 watched (any), look-ahead 1",
				},
			},
		},
		{
			name:     "last episode watched alone promotes",
			seriesID: 10,
			held:     []int32{2, 3},
			watches: []SeasonWatch{
				{UserKey: "s1/u1", Season: 1, Episode: 1, Watched: false},
				{UserKey: "s1/u1", Season: 1, Episode: 2, Watched: false},
				{UserKey: "s1/u1", Season: 1, Episode: 3, Watched: true},
			},
			cfg: GateConfig{
				Mode:         GateModeObserve,
				LookAhead:    1,
				PromoteWhose: "any",
			},
			want: []GateDecision{
				{
					SeriesID: 10,
					Season:   2,
					Action:   GateActionPromote,
					WouldAct: false,
					Reason:   "season 1 watched (any), look-ahead 1",
				},
			},
		},
		{
			name:     "look-ahead 0 promotes none",
			seriesID: 10,
			held:     []int32{2, 3},
			watches: []SeasonWatch{
				{UserKey: "s1/u1", Season: 1, Episode: 1, Watched: true},
			},
			cfg: GateConfig{
				Mode:         GateModeObserve,
				LookAhead:    0,
				PromoteWhose: "any",
			},
			want: nil,
		},
		{
			name:     "look-ahead 2 with held [2,3,4] and F=1 promotes 2 and 3",
			seriesID: 10,
			held:     []int32{2, 3, 4},
			watches: []SeasonWatch{
				{UserKey: "s1/u1", Season: 1, Episode: 1, Watched: true},
			},
			cfg: GateConfig{
				Mode:         GateModeObserve,
				LookAhead:    2,
				PromoteWhose: "any",
			},
			want: []GateDecision{
				{
					SeriesID: 10,
					Season:   2,
					Action:   GateActionPromote,
					WouldAct: false,
					Reason:   "season 1 watched (any), look-ahead 2",
				},
				{
					SeriesID: 10,
					Season:   3,
					Action:   GateActionPromote,
					WouldAct: false,
					Reason:   "season 1 watched (any), look-ahead 2",
				},
			},
		},
		{
			name:     "whose all with one of two users unwatched promotes none",
			seriesID: 10,
			held:     []int32{2, 3},
			watches: []SeasonWatch{
				{UserKey: "s1/u1", Season: 1, Episode: 1, Watched: true},
				{UserKey: "s1/u2", Season: 1, Episode: 1, Watched: false},
			},
			cfg: GateConfig{
				Mode:         GateModeObserve,
				LookAhead:    1,
				PromoteWhose: "all",
			},
			want: nil,
		},
		{
			name:     "whose any with one of two users unwatched promotes",
			seriesID: 10,
			held:     []int32{2, 3},
			watches: []SeasonWatch{
				{UserKey: "s1/u1", Season: 1, Episode: 1, Watched: true},
				{UserKey: "s1/u2", Season: 1, Episode: 1, Watched: false},
			},
			cfg: GateConfig{
				Mode:         GateModeObserve,
				LookAhead:    1,
				PromoteWhose: "any",
			},
			want: []GateDecision{
				{
					SeriesID: 10,
					Season:   2,
					Action:   GateActionPromote,
					WouldAct: false,
					Reason:   "season 1 watched (any), look-ahead 1",
				},
			},
		},
		{
			name:     "specials season 0 ignored",
			seriesID: 10,
			held:     []int32{1, 2},
			watches: []SeasonWatch{
				{UserKey: "s1/u1", Season: 0, Episode: 1, Watched: true},
			},
			cfg: GateConfig{
				Mode:         GateModeObserve,
				LookAhead:    1,
				PromoteWhose: "any",
			},
			want: nil,
		},
		{
			name:     "empty watches promotes none",
			seriesID: 10,
			held:     []int32{2, 3},
			watches:  nil,
			cfg: GateConfig{
				Mode:         GateModeObserve,
				LookAhead:    1,
				PromoteWhose: "any",
			},
			want: nil,
		},
		{
			name:     "held already promoted promotes none",
			seriesID: 10,
			held:     []int32{3, 4},
			watches: []SeasonWatch{
				{UserKey: "s1/u1", Season: 1, Episode: 1, Watched: true},
			},
			cfg: GateConfig{
				Mode:         GateModeObserve,
				LookAhead:    1, // target is 1 + 1 = 2; held has [3, 4]
				PromoteWhose: "any",
			},
			want: nil,
		},
		{
			name:     "act mode sets WouldAct true",
			seriesID: 10,
			held:     []int32{2},
			watches: []SeasonWatch{
				{UserKey: "s1/u1", Season: 1, Episode: 1, Watched: true},
			},
			cfg: GateConfig{
				Mode:         GateModeAct,
				LookAhead:    1,
				PromoteWhose: "any",
			},
			want: []GateDecision{
				{
					SeriesID: 10,
					Season:   2,
					Action:   GateActionPromote,
					WouldAct: true,
					Reason:   "season 1 watched (any), look-ahead 1",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := EvaluatePromotion(tt.seriesID, tt.held, tt.watches, tt.cfg)
			if len(got) != len(tt.want) {
				t.Fatalf("len(got) = %d, want %d: %+v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}
