// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package queue_test

import (
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/queue"
)

func TestReasonRulesObserveVsActAndFlags(t *testing.T) {
	t.Parallel()

	now := fixedNow

	t.Run("import_fixable/observe_vs_act_and_bound", func(t *testing.T) {
		t.Parallel()

		item := queue.Item{
			ID:                    101,
			DownloadID:            "dl-fixable",
			Added:                 now.Add(-1 * time.Hour),
			TrackedDownloadState:  "importBlocked",
			TrackedDownloadStatus: "warning",
			StatusLines: []string{
				"Single episode file contains all episodes in seasons. Review file name or manually import",
			},
		}

		// Observe mode: threshold 1 -> WouldImport
		sObs := queue.DefaultSettings()
		sObs.Enabled = true
		sObs.Grace = 0
		sObs.Rules[queue.RuleImportFixable] = queue.RuleSettings{Mode: queue.ModeObserve, Threshold: 1}
		dObs := sweepOne(t, sObs, item, nil, now)
		if dObs.Rule != queue.RuleImportFixable {
			t.Fatalf("rule = %q, want import_fixable", dObs.Rule)
		}
		if dObs.Import || !dObs.WouldImport {
			t.Fatalf("observe: import = %v, wouldImport = %v, want false, true", dObs.Import, dObs.WouldImport)
		}
		if dObs.Research || dObs.Blocklist || dObs.Remove || dObs.WouldRemove {
			t.Fatalf("unexpected flags on import_fixable observe: %+v", dObs)
		}

		// Act mode: threshold 1 -> Import
		sAct := sObs
		sAct.Rules[queue.RuleImportFixable] = queue.RuleSettings{Mode: queue.ModeAct, Threshold: 1}
		dAct := sweepOne(t, sAct, item, nil, now)
		if !dAct.Import || dAct.WouldImport {
			t.Fatalf("act: import = %v, wouldImport = %v, want true, false", dAct.Import, dAct.WouldImport)
		}

		// MaxImportsPerSweep bound
		sBound := sAct
		sBound.MaxImportsPerSweep = 1
		items := []queue.Item{
			item,
			{
				ID:                    102,
				DownloadID:            "dl-fixable-2",
				Added:                 now.Add(-1 * time.Hour),
				TrackedDownloadState:  "importBlocked",
				TrackedDownloadStatus: "warning",
				StatusLines: []string{
					"Single episode file contains all episodes in seasons. Review file name or manually import",
				},
			},
		}
		ds := queue.Sweep(sBound, items, nil, now)
		if len(ds) != 2 {
			t.Fatalf("sweep returned %d decisions, want 2", len(ds))
		}
		if !ds[0].Import || ds[0].Deferred {
			t.Errorf("item 0: import = %v, deferred = %v, want true, false", ds[0].Import, ds[0].Deferred)
		}
		if ds[1].Import || !ds[1].Deferred {
			t.Errorf("item 1: import = %v, deferred = %v, want false, true", ds[1].Import, ds[1].Deferred)
		}
	})

	t.Run("import_not_wanted/observe_vs_act_and_flags", func(t *testing.T) {
		t.Parallel()

		item := queue.Item{
			ID:                   201,
			DownloadID:           "dl-notwanted",
			Added:                now.Add(-1 * time.Hour),
			TrackedDownloadState: "importBlocked",
			StatusLines: []string{
				"Has fewer tracks than existing release",
			},
		}

		// Observe mode: threshold 1 -> WouldRemove, Blocklist=true, Research=false
		sObs := queue.DefaultSettings()
		sObs.Enabled = true
		sObs.Grace = 0
		sObs.Rules[queue.RuleImportNotWanted] = queue.RuleSettings{Mode: queue.ModeObserve, Threshold: 1}
		dObs := sweepOne(t, sObs, item, nil, now)
		if dObs.Rule != queue.RuleImportNotWanted {
			t.Fatalf("rule = %q, want import_not_wanted", dObs.Rule)
		}
		if dObs.Remove || !dObs.WouldRemove {
			t.Fatalf("observe: remove = %v, wouldRemove = %v, want false, true", dObs.Remove, dObs.WouldRemove)
		}
		if !dObs.Blocklist || dObs.Research {
			t.Fatalf("flags: blocklist = %v, research = %v, want true, false", dObs.Blocklist, dObs.Research)
		}

		// Act mode: threshold 1 -> Remove, Blocklist=true, Research=false
		sAct := sObs
		sAct.Rules[queue.RuleImportNotWanted] = queue.RuleSettings{Mode: queue.ModeAct, Threshold: 1}
		dAct := sweepOne(t, sAct, item, nil, now)
		if !dAct.Remove || dAct.WouldRemove {
			t.Fatalf("act: remove = %v, wouldRemove = %v, want true, false", dAct.Remove, dAct.WouldRemove)
		}
		if !dAct.Blocklist || dAct.Research {
			t.Fatalf("flags: blocklist = %v, research = %v, want true, false", dAct.Blocklist, dAct.Research)
		}
	})

	t.Run("import_broken/observe_vs_act_threshold_2_and_flags", func(t *testing.T) {
		t.Parallel()

		item := queue.Item{
			ID:                   301,
			DownloadID:           "dl-broken",
			Added:                now.Add(-1 * time.Hour),
			TrackedDownloadState: "importBlocked",
			StatusLines: []string{
				"Couldn't find similar album for Artist - Title",
			},
		}

		sAct := queue.DefaultSettings()
		sAct.Enabled = true
		sAct.Grace = 0
		sAct.Rules[queue.RuleImportBroken] = queue.RuleSettings{Mode: queue.ModeAct, Threshold: 2}

		// Sweep 1: count 1, threshold not reached
		d1 := sweepOne(t, sAct, item, nil, now)
		if d1.Rule != queue.RuleImportBroken || d1.Next.Count != 1 || d1.Remove || d1.WouldRemove {
			t.Fatalf("sweep 1: rule %q count %d remove %v, want import_broken, 1, false", d1.Rule, d1.Next.Count, d1.Remove)
		}
		if !d1.Research || !d1.Blocklist {
			t.Fatalf("flags sweep 1: research = %v, blocklist = %v, want true, true", d1.Research, d1.Blocklist)
		}

		// Sweep 2: count 2, threshold reached -> Remove=true, Research=true, Blocklist=true
		prev := map[string]queue.Strike{"dl-broken": d1.Next}
		d2 := sweepOne(t, sAct, item, prev, now.Add(10*time.Minute))
		if d2.Rule != queue.RuleImportBroken || d2.Next.Count != 2 || !d2.Remove || d2.WouldRemove {
			t.Fatalf("sweep 2: rule %q count %d remove %v, want import_broken, 2, true", d2.Rule, d2.Next.Count, d2.Remove)
		}
		if !d2.Research || !d2.Blocklist {
			t.Fatalf("flags sweep 2: research = %v, blocklist = %v, want true, true", d2.Research, d2.Blocklist)
		}

		// Observe mode sweep 2 -> WouldRemove=true, Remove=false
		sObs := sAct
		sObs.Rules[queue.RuleImportBroken] = queue.RuleSettings{Mode: queue.ModeObserve, Threshold: 2}
		dObs2 := sweepOne(t, sObs, item, prev, now.Add(10*time.Minute))
		if dObs2.Remove || !dObs2.WouldRemove || !dObs2.Research || !dObs2.Blocklist {
			t.Fatalf("observe sweep 2: remove = %v, would = %v, research = %v, blocklist = %v", dObs2.Remove, dObs2.WouldRemove, dObs2.Research, dObs2.Blocklist)
		}
	})

	t.Run("import_blocked/act_mode_never_removes", func(t *testing.T) {
		t.Parallel()

		item := queue.Item{
			ID:                   401,
			DownloadID:           "dl-unknown",
			Added:                now.Add(-1 * time.Hour),
			TrackedDownloadState: "importBlocked",
			StatusLines: []string{
				"Some completely unmapped operational condition",
			},
		}

		sAct := queue.DefaultSettings()
		sAct.Enabled = true
		sAct.Grace = 0
		sAct.Rules[queue.RuleImportBlocked] = queue.RuleSettings{Mode: queue.ModeAct, Threshold: 6}

		prev := map[string]queue.Strike{
			"dl-unknown": {Rule: queue.RuleImportBlocked, Count: 5, LastSeen: now.Add(-10 * time.Minute)},
		}

		d := sweepOne(t, sAct, item, prev, now)
		if d.Rule != queue.RuleImportBlocked {
			t.Fatalf("rule = %q, want import_blocked", d.Rule)
		}
		if d.Next.Count != 6 {
			t.Fatalf("count = %d, want 6", d.Next.Count)
		}
		if d.Remove || d.WouldRemove {
			t.Fatalf("import_blocked in act mode must NEVER remove: remove = %v, wouldRemove = %v", d.Remove, d.WouldRemove)
		}
		if !d.Unknown {
			t.Fatalf("import_blocked must report Unknown = true")
		}
	})

	t.Run("failed/no_longer_matches_importFailed", func(t *testing.T) {
		t.Parallel()

		item := queue.Item{
			ID:                   501,
			DownloadID:           "dl-importfailed",
			Added:                now.Add(-1 * time.Hour),
			Status:               "completed",
			TrackedDownloadState: "importFailed",
			StatusLines:          nil,
		}

		sAct := queue.DefaultSettings()
		sAct.Enabled = true
		sAct.Grace = 0
		sAct.Rules[queue.RuleFailed] = queue.RuleSettings{Mode: queue.ModeAct, Threshold: 1}
		sAct.Rules[queue.RuleImportBlocked] = queue.RuleSettings{Mode: queue.ModeObserve, Threshold: 6}

		d := sweepOne(t, sAct, item, nil, now)
		if d.Rule == queue.RuleFailed {
			t.Fatalf("importFailed must not match RuleFailed, got %q", d.Rule)
		}
		if d.Rule != queue.RuleImportBlocked {
			t.Fatalf("importFailed with no lines should match import_blocked (unknown), got %q", d.Rule)
		}
	})
}

func TestLidarrCensusShapes(t *testing.T) {
	t.Parallel()

	now := fixedNow
	oldAdded := now.Add(-1 * time.Hour)

	tests := []struct {
		name         string
		statusLines  []string
		wantRule     queue.Rule
		wantClass    queue.Class
		wantUnknown  bool
		wantPattern  string
		wantMatchPct float64
	}{
		{
			name:         "album_77_percent_import_fixable",
			statusLines:  []string{"Album match is not close enough: 77.0% vs 80%"},
			wantRule:     queue.RuleImportFixable,
			wantClass:    queue.ClassFixable,
			wantUnknown:  false,
			wantMatchPct: 77.0,
		},
		{
			name:         "album_10_2_percent_import_blocked_unknown_rematch_pending",
			statusLines:  []string{"Album match is not close enough: 10.2% vs 80%"},
			wantRule:     queue.RuleImportBlocked,
			wantClass:    queue.ClassUnknown,
			wantUnknown:  true,
			wantPattern:  "rematch_pending",
			wantMatchPct: 10.2,
		},
		{
			name:         "album_55_percent_import_blocked_unknown_rematch_pending",
			statusLines:  []string{"Album match is not close enough: 55% vs 80%"},
			wantRule:     queue.RuleImportBlocked,
			wantClass:    queue.ClassUnknown,
			wantUnknown:  true,
			wantPattern:  "rematch_pending",
			wantMatchPct: 55.0,
		},
		{
			name:        "not_enough_free_space_unknown",
			statusLines: []string{"Not enough free space"},
			wantRule:    queue.RuleImportBlocked,
			wantClass:   queue.ClassUnknown,
			wantUnknown: true,
		},
		{
			name:        "has_fewer_tracks_import_not_wanted",
			statusLines: []string{"Has fewer tracks than existing release"},
			wantRule:    queue.RuleImportNotWanted,
			wantClass:   queue.ClassNotWanted,
			wantUnknown: false,
		},
		{
			name:        "couldnt_find_similar_album_import_broken",
			statusLines: []string{"Couldn't find similar album for Artist - Title"},
			wantRule:    queue.RuleImportBroken,
			wantClass:   queue.ClassBroken,
			wantUnknown: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := queue.DefaultSettings()
			s.Enabled = true
			s.Grace = 0

			item := queue.Item{
				ID:                   601,
				Title:                "Artist - Album (2024)",
				DownloadID:           "dl-lidarr-shape",
				Added:                oldAdded,
				TrackedDownloadState: "importBlocked",
				StatusLines:          append([]string{"Artist - Album (2024)"}, tt.statusLines...),
			}

			d := sweepOne(t, s, item, nil, now)
			if d.Rule != tt.wantRule {
				t.Errorf("Rule = %q, want %q", d.Rule, tt.wantRule)
			}
			if d.Class != tt.wantClass {
				t.Errorf("Class = %q, want %q", d.Class, tt.wantClass)
			}
			if d.Unknown != tt.wantUnknown {
				t.Errorf("Unknown = %v, want %v", d.Unknown, tt.wantUnknown)
			}
			if tt.wantPattern != "" && d.Pattern != tt.wantPattern {
				t.Errorf("Pattern = %q, want %q", d.Pattern, tt.wantPattern)
			}
			if tt.wantMatchPct > 0 {
				if d.MatchPercent == nil || *d.MatchPercent != tt.wantMatchPct {
					t.Errorf("MatchPercent = %v, want %v", d.MatchPercent, tt.wantMatchPct)
				}
			}
		})
	}
}
