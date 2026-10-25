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

	t.Run("import_blocked/act_mode_removes_unrecognized", func(t *testing.T) {
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
		if !d.Remove || d.WouldRemove {
			t.Fatalf("import_blocked unrecognized in act mode must remove: remove = %v, wouldRemove = %v", d.Remove, d.WouldRemove)
		}
		if !d.Unknown {
			t.Fatalf("import_blocked must report Unknown = true")
		}
		if d.Pattern != "unrecognized" {
			t.Fatalf("pattern = %q, want unrecognized", d.Pattern)
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

func TestSweepImportBlockedRemovableAndOperational(t *testing.T) {
	t.Parallel()

	now := fixedNow
	oldAdded := now.Add(-1 * time.Hour)

	sAct := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleImportBlocked: 2})
	sAct.Grace = 15 * time.Minute

	t.Run("positive/removable_reaches_threshold_in_act", func(t *testing.T) {
		t.Parallel()

		items := []struct {
			name        string
			downloadID  string
			statusLines []string
			wantPattern string
		}{
			{
				name:        "rematch_pending",
				downloadID:  "dl-pos-rematch",
				statusLines: []string{"Album match is not close enough: 10.2% vs 80%"},
				wantPattern: queue.SubreasonRematchPending,
			},
			{
				name:        "unrecognized",
				downloadID:  "dl-pos-unrecog",
				statusLines: []string{"Completely unknown error from downloader"},
				wantPattern: queue.SubreasonUnrecognized,
			},
		}

		for _, tc := range items {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				item := queue.Item{
					ID:                   801,
					DownloadID:           tc.downloadID,
					Title:                tc.name,
					Added:                oldAdded,
					TrackedDownloadState: "importBlocked",
					StatusLines:          append([]string{tc.name}, tc.statusLines...),
				}

				// Sweep 1: count 1, before threshold
				d1 := sweepOne(t, sAct, item, nil, now)
				if d1.Rule != queue.RuleImportBlocked || d1.Next.Count != 1 || d1.Remove || d1.WouldRemove || !d1.Unknown || d1.Research {
					t.Fatalf("sweep 1: got %+v, want count 1, remove false, wouldRemove false, unknown true, research false", d1)
				}
				if d1.Pattern != tc.wantPattern {
					t.Fatalf("sweep 1 pattern: got %q, want %q", d1.Pattern, tc.wantPattern)
				}

				// Sweep 2: count 2, reaches threshold -> Remove true, WouldRemove false, Unknown true, Research false
				prev := map[string]queue.Strike{item.DownloadID: d1.Next}
				d2 := sweepOne(t, sAct, item, prev, now.Add(10*time.Minute))
				if d2.Rule != queue.RuleImportBlocked || d2.Next.Count != 2 || !d2.Remove || d2.WouldRemove || !d2.Unknown || d2.Research {
					t.Fatalf("sweep 2: got %+v, want count 2, remove true, wouldRemove false, unknown true, research false", d2)
				}
				if d2.Pattern != tc.wantPattern {
					t.Fatalf("sweep 2 pattern: got %q, want %q", d2.Pattern, tc.wantPattern)
				}
			})
		}
	})

	t.Run("negative/operational_rows_never_remove", func(t *testing.T) {
		t.Parallel()

		opCases := []struct {
			name        string
			statusLines []string
			added       time.Time
		}{
			{"sample_undetermined", []string{"Unable to determine if file is a sample"}, oldAdded},
			{"episode_tba_title", []string{"Episode has a TBA title and recently aired"}, oldAdded},
			{"episode_no_absolute_number", []string{"Episode does not have an absolute episode number and recently aired"}, oldAdded},
			{"category_skip", []string{"Download wasn't grabbed by Sonarr and not in a category, Skipping."}, oldAdded},
			{"remote_path_mapping", []string{"[/data/dl] is not a valid local path. You may need a Remote Path Mapping."}, oldAdded},
			{"not_enough_free_space", []string{"Not enough free space"}, oldAdded},
			{"still_unpacking", []string{"File is still being unpacked"}, oldAdded},
			{"locked_file", []string{"Locked file, try again later"}, oldAdded},
			{"mapped_to_series_folder", []string{"Import path is mapped to a series folder"}, oldAdded},
			{"destination_not_in_root_folder", []string{"Destination artist folder /music is not in a Root Folder"}, oldAdded},
			// Inside the grace period the sweep never touches an item (isNeverTouched);
			// TestClassifyImportBlockedRemovableAndOperational covers that line.
			{"no_files_with_unpack_locked", []string{"No files found are eligible for import in /downloads/album", "File is still being unpacked"}, oldAdded},
			{"permission_denied", []string{"Access to the path '/media/music' is denied"}, oldAdded},
		}

		for _, tc := range opCases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()

				item := queue.Item{
					ID:                   850,
					DownloadID:           "dl-op-" + tc.name,
					Title:                tc.name,
					Added:                tc.added,
					TrackedDownloadState: "importBlocked",
					StatusLines:          append([]string{tc.name}, tc.statusLines...),
				}

				// Sweep 1: count 1
				d1 := sweepOne(t, sAct, item, nil, now)
				if d1.Remove || d1.WouldRemove || !d1.Unknown || d1.Pattern != "" {
					t.Fatalf("sweep 1: got remove %v, wouldRemove %v, unknown %v, pattern %q", d1.Remove, d1.WouldRemove, d1.Unknown, d1.Pattern)
				}

				// Sweep 2: count 2 = threshold
				prev := map[string]queue.Strike{item.DownloadID: d1.Next}
				d2 := sweepOne(t, sAct, item, prev, now.Add(10*time.Minute))
				if d2.Remove || d2.WouldRemove || !d2.Unknown || d2.Pattern != "" {
					t.Fatalf("sweep 2: got remove %v, wouldRemove %v, unknown %v, pattern %q", d2.Remove, d2.WouldRemove, d2.Unknown, d2.Pattern)
				}

				// Sweep 3: count 2, past threshold
				prev[item.DownloadID] = d2.Next
				d3 := sweepOne(t, sAct, item, prev, now.Add(20*time.Minute))
				if d3.Remove || d3.WouldRemove || !d3.Unknown || d3.Pattern != "" {
					t.Fatalf("sweep 3: got remove %v, wouldRemove %v, unknown %v, pattern %q", d3.Remove, d3.WouldRemove, d3.Unknown, d3.Pattern)
				}
			})
		}
	})

	t.Run("negative/mode_observe_with_rematch_pending", func(t *testing.T) {
		t.Parallel()

		sObs := testSettings(queue.ModeObserve, map[queue.Rule]int{queue.RuleImportBlocked: 2})
		item := queue.Item{
			ID:                   890,
			DownloadID:           "dl-obs-rematch",
			Added:                oldAdded,
			TrackedDownloadState: "importBlocked",
			StatusLines:          []string{"Album match is not close enough: 10.2% vs 80%"},
		}

		// Sweep 1: count 1
		d1 := sweepOne(t, sObs, item, nil, now)
		if d1.Remove || d1.WouldRemove || !d1.Unknown {
			t.Fatalf("sweep 1: got remove %v, wouldRemove %v, unknown %v", d1.Remove, d1.WouldRemove, d1.Unknown)
		}

		// Sweep 2: count 2 = threshold -> WouldRemove true, Remove false
		prev := map[string]queue.Strike{item.DownloadID: d1.Next}
		d2 := sweepOne(t, sObs, item, prev, now.Add(10*time.Minute))
		if d2.Remove || !d2.WouldRemove || !d2.Unknown {
			t.Fatalf("sweep 2: got remove %v, wouldRemove %v, unknown %v, want remove false, wouldRemove true", d2.Remove, d2.WouldRemove, d2.Unknown)
		}
	})

	t.Run("boundary/threshold_progression", func(t *testing.T) {
		t.Parallel()

		sAct3 := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleImportBlocked: 3})
		item := queue.Item{
			ID:                   895,
			DownloadID:           "dl-bound-thresh",
			Added:                oldAdded,
			TrackedDownloadState: "importBlocked",
			StatusLines:          []string{"Unrecognized failure"},
		}

		// Sweep 1: count 1
		d1 := sweepOne(t, sAct3, item, nil, now)
		if d1.Remove || d1.Next.Count != 1 {
			t.Fatalf("sweep 1: remove %v, count %d", d1.Remove, d1.Next.Count)
		}

		// Sweep 2: threshold - 1 (count 2) -> no remove
		prev := map[string]queue.Strike{item.DownloadID: d1.Next}
		d2 := sweepOne(t, sAct3, item, prev, now.Add(10*time.Minute))
		if d2.Remove || d2.Next.Count != 2 {
			t.Fatalf("sweep 2: remove %v, count %d", d2.Remove, d2.Next.Count)
		}

		// Sweep 3: threshold (count 3) -> remove
		prev[item.DownloadID] = d2.Next
		d3 := sweepOne(t, sAct3, item, prev, now.Add(20*time.Minute))
		if !d3.Remove || d3.Next.Count != 3 {
			t.Fatalf("sweep 3: remove %v, count %d", d3.Remove, d3.Next.Count)
		}
	})

	t.Run("boundary/max_removals_per_sweep", func(t *testing.T) {
		t.Parallel()

		sBound := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleImportBlocked: 2})
		sBound.MaxRemovalsPerSweep = 1

		item1 := queue.Item{
			ID:                   898,
			DownloadID:           "dl-bound-1",
			Added:                oldAdded,
			TrackedDownloadState: "importBlocked",
			StatusLines:          []string{"Album match is not close enough: 10.2% vs 80%"},
		}
		item2 := queue.Item{
			ID:                   899,
			DownloadID:           "dl-bound-2",
			Added:                oldAdded,
			TrackedDownloadState: "importBlocked",
			StatusLines:          []string{"Unrecognized download issue"},
		}

		prev := map[string]queue.Strike{
			item1.DownloadID: {Rule: queue.RuleImportBlocked, Count: 1, LastSeen: now.Add(-10 * time.Minute)},
			item2.DownloadID: {Rule: queue.RuleImportBlocked, Count: 1, LastSeen: now.Add(-10 * time.Minute)},
		}

		decisions := queue.Sweep(sBound, []queue.Item{item1, item2}, prev, now)
		if len(decisions) != 2 {
			t.Fatalf("returned %d decisions, want 2", len(decisions))
		}
		if !decisions[0].Remove || decisions[0].Deferred || !decisions[0].Unknown {
			t.Errorf("item1: remove %v, deferred %v, unknown %v, want true, false, true", decisions[0].Remove, decisions[0].Deferred, decisions[0].Unknown)
		}
		if decisions[1].Remove || !decisions[1].Deferred || !decisions[1].Unknown {
			t.Errorf("item2: remove %v, deferred %v, unknown %v, want false, true, true", decisions[1].Remove, decisions[1].Deferred, decisions[1].Unknown)
		}
	})
}
