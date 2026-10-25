// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package watchaware_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/watchaware"
)

func TestEvaluateOnAdd(t *testing.T) {
	t.Parallel()

	defaultCfg := watchaware.DefaultGateConfig()
	defaultCfg.Enabled = true

	exemptTagID := 42

	cases := []struct {
		name          string
		series        watchaware.GateSeries
		cfg           watchaware.GateConfig
		exemptTagID   int
		hasExemptTag  bool
		wantState     string
		wantSeason    int
		wantDecisions []watchaware.GateDecision
	}{
		{
			name: "positive: new unwatched 3-season series, seasons_on_add 1",
			series: watchaware.GateSeries{
				ID:     101,
				TvdbID: 201,
				Title:  "Three Seasons Show",
				TagIDs: []int{10, 20},
				Seasons: []watchaware.GateSeason{
					{Number: 1, Monitored: true, FileCount: 0},
					{Number: 2, Monitored: true, FileCount: 0},
					{Number: 3, Monitored: true, FileCount: 0},
				},
			},
			cfg:          defaultCfg,
			exemptTagID:  exemptTagID,
			hasExemptTag: true,
			wantState:    watchaware.GateStateGated,
			wantSeason:   1,
			wantDecisions: []watchaware.GateDecision{
				{SeriesID: 101, Season: 2, Action: watchaware.GateActionUnmonitor, WouldAct: false, Reason: "season after gate"},
				{SeriesID: 101, Season: 3, Action: watchaware.GateActionUnmonitor, WouldAct: false, Reason: "season after gate"},
			},
		},
		{
			name: "negative: exempt tag present on series",
			series: watchaware.GateSeries{
				ID:     102,
				TvdbID: 202,
				Title:  "Exempt Show",
				TagIDs: []int{10, exemptTagID},
				Seasons: []watchaware.GateSeason{
					{Number: 1, Monitored: true, FileCount: 0},
					{Number: 2, Monitored: true, FileCount: 0},
				},
			},
			cfg:           defaultCfg,
			exemptTagID:   exemptTagID,
			hasExemptTag:  true,
			wantState:     watchaware.GateStateExemptTag,
			wantSeason:    0,
			wantDecisions: nil,
		},
		{
			name: "negative: tag matches ID but hasExemptTag is false",
			series: watchaware.GateSeries{
				ID:     103,
				TvdbID: 203,
				Title:  "Unmatched Tag Show",
				TagIDs: []int{exemptTagID},
				Seasons: []watchaware.GateSeason{
					{Number: 1, Monitored: true, FileCount: 0},
					{Number: 2, Monitored: true, FileCount: 0},
				},
			},
			cfg:          defaultCfg,
			exemptTagID:  exemptTagID,
			hasExemptTag: false,
			wantState:    watchaware.GateStateGated,
			wantSeason:   1,
			wantDecisions: []watchaware.GateDecision{
				{SeriesID: 103, Season: 2, Action: watchaware.GateActionUnmonitor, WouldAct: false, Reason: "season after gate"},
			},
		},
		{
			name: "negative: file in season 2 beyond gate",
			series: watchaware.GateSeries{
				ID:     104,
				TvdbID: 204,
				Title:  "Existing Files Show",
				TagIDs: []int{},
				Seasons: []watchaware.GateSeason{
					{Number: 1, Monitored: true, FileCount: 5},
					{Number: 2, Monitored: true, FileCount: 1},
					{Number: 3, Monitored: true, FileCount: 0},
				},
			},
			cfg:           defaultCfg,
			exemptTagID:   exemptTagID,
			hasExemptTag:  true,
			wantState:     watchaware.GateStateFilesBeyondGate,
			wantSeason:    0,
			wantDecisions: nil,
		},
		{
			name: "negative: already unmonitored season 3 produces no decision for 3",
			series: watchaware.GateSeries{
				ID:     105,
				TvdbID: 205,
				Title:  "Partially Monitored Show",
				TagIDs: []int{},
				Seasons: []watchaware.GateSeason{
					{Number: 1, Monitored: true, FileCount: 0},
					{Number: 2, Monitored: true, FileCount: 0},
					{Number: 3, Monitored: false, FileCount: 0},
				},
			},
			cfg:          defaultCfg,
			exemptTagID:  exemptTagID,
			hasExemptTag: true,
			wantState:    watchaware.GateStateGated,
			wantSeason:   1,
			wantDecisions: []watchaware.GateDecision{
				{SeriesID: 105, Season: 2, Action: watchaware.GateActionUnmonitor, WouldAct: false, Reason: "season after gate"},
			},
		},
		{
			name: "boundary: seasons_on_add 2 keeps season 2 monitored",
			series: watchaware.GateSeries{
				ID:     106,
				TvdbID: 206,
				Title:  "Keep Two Seasons",
				TagIDs: []int{},
				Seasons: []watchaware.GateSeason{
					{Number: 1, Monitored: true, FileCount: 0},
					{Number: 2, Monitored: true, FileCount: 0},
					{Number: 3, Monitored: true, FileCount: 0},
				},
			},
			cfg: func() watchaware.GateConfig {
				c := defaultCfg
				c.SeasonsOnAdd = 2
				return c
			}(),
			exemptTagID:  exemptTagID,
			hasExemptTag: true,
			wantState:    watchaware.GateStateGated,
			wantSeason:   2,
			wantDecisions: []watchaware.GateDecision{
				{SeriesID: 106, Season: 3, Action: watchaware.GateActionUnmonitor, WouldAct: false, Reason: "season after gate"},
			},
		},
		{
			name: "boundary: season 0 never in decisions",
			series: watchaware.GateSeries{
				ID:     107,
				TvdbID: 207,
				Title:  "Specials Show",
				TagIDs: []int{},
				Seasons: []watchaware.GateSeason{
					{Number: 0, Monitored: true, FileCount: 0},
					{Number: 1, Monitored: true, FileCount: 0},
					{Number: 2, Monitored: true, FileCount: 0},
				},
			},
			cfg:          defaultCfg,
			exemptTagID:  exemptTagID,
			hasExemptTag: true,
			wantState:    watchaware.GateStateGated,
			wantSeason:   1,
			wantDecisions: []watchaware.GateDecision{
				{SeriesID: 107, Season: 2, Action: watchaware.GateActionUnmonitor, WouldAct: false, Reason: "season after gate"},
			},
		},
		{
			name: "boundary: single-season series produces no decisions",
			series: watchaware.GateSeries{
				ID:     108,
				TvdbID: 208,
				Title:  "Mini Series",
				TagIDs: []int{},
				Seasons: []watchaware.GateSeason{
					{Number: 1, Monitored: true, FileCount: 0},
				},
			},
			cfg:           defaultCfg,
			exemptTagID:   exemptTagID,
			hasExemptTag:  true,
			wantState:     watchaware.GateStateGated,
			wantSeason:    1,
			wantDecisions: nil,
		},
		{
			name: "boundary: seasons_on_add 20 keeps up to 20",
			series: watchaware.GateSeries{
				ID:     109,
				TvdbID: 209,
				Title:  "Long Show",
				TagIDs: []int{},
				Seasons: []watchaware.GateSeason{
					{Number: 1, Monitored: true, FileCount: 0},
					{Number: 20, Monitored: true, FileCount: 0},
					{Number: 21, Monitored: true, FileCount: 0},
				},
			},
			cfg: func() watchaware.GateConfig {
				c := defaultCfg
				c.SeasonsOnAdd = 20
				return c
			}(),
			exemptTagID:  exemptTagID,
			hasExemptTag: true,
			wantState:    watchaware.GateStateGated,
			wantSeason:   20,
			wantDecisions: []watchaware.GateDecision{
				{SeriesID: 109, Season: 21, Action: watchaware.GateActionUnmonitor, WouldAct: false, Reason: "season after gate"},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			state, gateSeason, decisions := watchaware.EvaluateOnAdd(tc.series, tc.cfg, tc.exemptTagID, tc.hasExemptTag)
			if state != tc.wantState {
				t.Fatalf("EvaluateOnAdd() state = %q, want %q", state, tc.wantState)
			}
			if gateSeason != tc.wantSeason {
				t.Fatalf("EvaluateOnAdd() gateSeason = %d, want %d", gateSeason, tc.wantSeason)
			}
			if len(decisions) != len(tc.wantDecisions) {
				t.Fatalf("EvaluateOnAdd() len(decisions) = %d, want %d: %+v", len(decisions), len(tc.wantDecisions), decisions)
			}
			for i, d := range decisions {
				want := tc.wantDecisions[i]
				if d != want {
					t.Fatalf("EvaluateOnAdd() decision[%d] = %+v, want %+v", i, d, want)
				}
			}
		})
	}
}

func TestGateConfigFrom(t *testing.T) {
	t.Parallel()

	validUUID := uuid.New()

	cases := []struct {
		name         string
		raw          string
		wantEnabled  bool
		wantMode     string
		wantSeasons  int
		wantLook     int
		wantPromote  string
		wantTag      string
		wantInstsLen int
	}{
		{
			name:         "empty json defaults",
			raw:          "",
			wantEnabled:  false,
			wantMode:     "observe",
			wantSeasons:  1,
			wantLook:     1,
			wantPromote:  "any",
			wantTag:      "snatcharr-no-gate",
			wantInstsLen: 0,
		},
		{
			name:         "empty object defaults",
			raw:          "{}",
			wantEnabled:  false,
			wantMode:     "observe",
			wantSeasons:  1,
			wantLook:     1,
			wantPromote:  "any",
			wantTag:      "snatcharr-no-gate",
			wantInstsLen: 0,
		},
		{
			name:         "explicit gate values",
			raw:          `{"gate":{"enabled":true,"mode":"observe","seasons_on_add":3,"look_ahead":2,"promote_whose":"all","exempt_tag":"custom-exempt","instance_ids":["` + validUUID.String() + `"]}}`,
			wantEnabled:  true,
			wantMode:     "observe",
			wantSeasons:  3,
			wantLook:     2,
			wantPromote:  "all",
			wantTag:      "custom-exempt",
			wantInstsLen: 1,
		},
		{
			name:         "clamped seasons_on_add out of bounds falls back to default",
			raw:          `{"gate":{"enabled":true,"seasons_on_add":99}}`,
			wantEnabled:  true,
			wantMode:     "observe",
			wantSeasons:  1,
			wantLook:     1,
			wantPromote:  "any",
			wantTag:      "snatcharr-no-gate",
			wantInstsLen: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := watchaware.GateConfigFrom(json.RawMessage(tc.raw))
			if cfg.Enabled != tc.wantEnabled {
				t.Fatalf("cfg.Enabled = %v, want %v", cfg.Enabled, tc.wantEnabled)
			}
			if cfg.Mode != tc.wantMode {
				t.Fatalf("cfg.Mode = %q, want %q", cfg.Mode, tc.wantMode)
			}
			if cfg.SeasonsOnAdd != tc.wantSeasons {
				t.Fatalf("cfg.SeasonsOnAdd = %d, want %d", cfg.SeasonsOnAdd, tc.wantSeasons)
			}
			if cfg.LookAhead != tc.wantLook {
				t.Fatalf("cfg.LookAhead = %d, want %d", cfg.LookAhead, tc.wantLook)
			}
			if cfg.PromoteWhose != tc.wantPromote {
				t.Fatalf("cfg.PromoteWhose = %q, want %q", cfg.PromoteWhose, tc.wantPromote)
			}
			if cfg.ExemptTag != tc.wantTag {
				t.Fatalf("cfg.ExemptTag = %q, want %q", cfg.ExemptTag, tc.wantTag)
			}
			if len(cfg.InstanceIDs) != tc.wantInstsLen {
				t.Fatalf("len(cfg.InstanceIDs) = %d, want %d", len(cfg.InstanceIDs), tc.wantInstsLen)
			}
			if tc.wantInstsLen > 0 && !cfg.AppliesTo(validUUID) {
				t.Fatalf("cfg.AppliesTo(%s) = false, want true", validUUID)
			}
		})
	}
}
