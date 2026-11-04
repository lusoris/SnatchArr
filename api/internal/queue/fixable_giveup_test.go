// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package queue_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/queue"
)

func TestImportFixableGiveUp(t *testing.T) {
	t.Parallel()

	now := fixedNow
	refusedPrev := queue.Strike{
		Rule:          queue.RuleImportFixable,
		Count:         1,
		ImportedAt:    now.Add(-10 * time.Minute),
		ImportRefused: true,
		LastSeen:      now.Add(-15 * time.Minute),
	}
	retryPrev := queue.Strike{
		Rule:          queue.RuleImportFixable,
		Count:         1,
		ImportedAt:    now.Add(-20 * time.Minute),
		ImportRefused: true,
		LastSeen:      now.Add(-20 * time.Minute),
	}
	erroredPrev := queue.Strike{
		Rule:     queue.RuleImportFixable,
		Count:    1,
		LastSeen: now.Add(-15 * time.Minute),
	}
	blocklistFalse := false

	tests := []struct {
		name          string
		added         time.Time
		prev          *queue.Strike
		mode          queue.Mode
		blocklist     *bool
		wantRule      queue.Rule
		wantRemove    bool
		wantWouldRem  bool
		wantBlocklist bool
		wantResearch  bool
		wantImport    bool
	}{
		{
			name:          "positive/25h_give_up",
			added:         now.Add(-25 * time.Hour),
			prev:          &refusedPrev,
			mode:          queue.ModeAct,
			wantRule:      queue.RuleImportFixable,
			wantRemove:    true,
			wantWouldRem:  false,
			wantBlocklist: true,
			wantResearch:  true,
			wantImport:    false,
		},
		{
			name:          "negative/23h_cooldown_no_removal",
			added:         now.Add(-23 * time.Hour),
			prev:          &refusedPrev,
			mode:          queue.ModeAct,
			wantRule:      queue.RuleImportFixable,
			wantRemove:    false,
			wantWouldRem:  false,
			wantBlocklist: false,
			wantResearch:  false,
			wantImport:    false,
		},
		{
			name:          "negative/23h_retry_import",
			added:         now.Add(-23 * time.Hour),
			prev:          &retryPrev,
			mode:          queue.ModeAct,
			wantRule:      queue.RuleImportFixable,
			wantRemove:    false,
			wantWouldRem:  false,
			wantBlocklist: false,
			wantResearch:  false,
			wantImport:    true,
		},
		{
			name:          "errored_counts/stamp_absent_removes_at_25h",
			added:         now.Add(-25 * time.Hour),
			prev:          &erroredPrev,
			mode:          queue.ModeAct,
			wantRule:      queue.RuleImportFixable,
			wantRemove:    true,
			wantWouldRem:  false,
			wantBlocklist: true,
			wantResearch:  true,
			wantImport:    false,
		},
		{
			name:          "boundary/exactly_24h_removes",
			added:         now.Add(-24 * time.Hour),
			prev:          &refusedPrev,
			mode:          queue.ModeAct,
			wantRule:      queue.RuleImportFixable,
			wantRemove:    true,
			wantWouldRem:  false,
			wantBlocklist: true,
			wantResearch:  true,
			wantImport:    false,
		},
		{
			name:          "boundary/24h_plus_1s_stays_on_import_cooldown",
			added:         now.Add(-24*time.Hour + 1*time.Second),
			prev:          &refusedPrev,
			mode:          queue.ModeAct,
			wantRule:      queue.RuleImportFixable,
			wantRemove:    false,
			wantWouldRem:  false,
			wantBlocklist: false,
			wantResearch:  false,
			wantImport:    false,
		},
		{
			name:          "observe_mode_at_25h_would_remove",
			added:         now.Add(-25 * time.Hour),
			prev:          &refusedPrev,
			mode:          queue.ModeObserve,
			wantRule:      queue.RuleImportFixable,
			wantRemove:    false,
			wantWouldRem:  true,
			wantBlocklist: true,
			wantResearch:  true,
			wantImport:    false,
		},
		{
			name:          "added_zero_no_give_up",
			added:         time.Time{},
			prev:          &refusedPrev,
			mode:          queue.ModeAct,
			wantRule:      queue.RuleImportFixable,
			wantRemove:    false,
			wantWouldRem:  false,
			wantBlocklist: false,
			wantResearch:  false,
			wantImport:    false,
		},
		{
			name:          "blocklist_switched_off_at_25h",
			added:         now.Add(-25 * time.Hour),
			prev:          &refusedPrev,
			mode:          queue.ModeAct,
			blocklist:     &blocklistFalse,
			wantRule:      queue.RuleImportFixable,
			wantRemove:    true,
			wantWouldRem:  false,
			wantBlocklist: false,
			wantResearch:  true,
			wantImport:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := queue.DefaultSettings()
			s.Enabled = true
			s.Grace = 0
			s.Rules[queue.RuleImportFixable] = queue.RuleSettings{
				Mode:      tt.mode,
				Threshold: 1,
				Blocklist: tt.blocklist,
			}

			item := queue.Item{
				ID:                    101,
				DownloadID:            "dl-fixable-test",
				Title:                 "Artist - Album",
				Added:                 tt.added,
				TrackedDownloadState:  "importBlocked",
				TrackedDownloadStatus: "warning",
				StatusLines: []string{
					"Album match is not close enough: 77.0% vs 80%",
				},
			}

			var prev map[string]queue.Strike
			if tt.prev != nil {
				prev = map[string]queue.Strike{item.DownloadID: *tt.prev}
			}

			d := sweepOne(t, s, item, prev, now)
			if d.Rule != tt.wantRule {
				t.Fatalf("rule = %q, want %q", d.Rule, tt.wantRule)
			}
			if d.Remove != tt.wantRemove {
				t.Errorf("remove = %v, want %v", d.Remove, tt.wantRemove)
			}
			if d.WouldRemove != tt.wantWouldRem {
				t.Errorf("wouldRemove = %v, want %v", d.WouldRemove, tt.wantWouldRem)
			}
			if d.Blocklist != tt.wantBlocklist {
				t.Errorf("blocklist = %v, want %v", d.Blocklist, tt.wantBlocklist)
			}
			if d.Research != tt.wantResearch {
				t.Errorf("research = %v, want %v", d.Research, tt.wantResearch)
			}
			if d.Import != tt.wantImport {
				t.Errorf("import = %v, want %v", d.Import, tt.wantImport)
			}
		})
	}
}

func TestImportFixableGiveUpMaxRemovalsBound(t *testing.T) {
	t.Parallel()

	now := fixedNow
	s := queue.DefaultSettings()
	s.Enabled = true
	s.Grace = 0
	s.MaxRemovalsPerSweep = 1
	s.Rules[queue.RuleImportFixable] = queue.RuleSettings{
		Mode:      queue.ModeAct,
		Threshold: 1,
	}

	item1 := queue.Item{
		ID:                    101,
		DownloadID:            "dl-bound-1",
		Title:                 "Artist - Album 1",
		Added:                 now.Add(-25 * time.Hour),
		TrackedDownloadState:  "importBlocked",
		TrackedDownloadStatus: "warning",
		StatusLines: []string{
			"Album match is not close enough: 77.0% vs 80%",
		},
	}
	item2 := queue.Item{
		ID:                    102,
		DownloadID:            "dl-bound-2",
		Title:                 "Artist - Album 2",
		Added:                 now.Add(-25 * time.Hour),
		TrackedDownloadState:  "importBlocked",
		TrackedDownloadStatus: "warning",
		StatusLines: []string{
			"Album match is not close enough: 77.0% vs 80%",
		},
	}

	prev := map[string]queue.Strike{
		item1.DownloadID: {Rule: queue.RuleImportFixable, Count: 1, LastSeen: now.Add(-10 * time.Minute)},
		item2.DownloadID: {Rule: queue.RuleImportFixable, Count: 1, LastSeen: now.Add(-10 * time.Minute)},
	}

	decisions := queue.Sweep(s, []queue.Item{item1, item2}, prev, now)
	if len(decisions) != 2 {
		t.Fatalf("returned %d decisions, want 2", len(decisions))
	}
	if !decisions[0].Remove || decisions[0].Deferred {
		t.Errorf("item1: remove %v, deferred %v, want true, false", decisions[0].Remove, decisions[0].Deferred)
	}
	if decisions[1].Remove || !decisions[1].Deferred {
		t.Errorf("item2: remove %v, deferred %v, want false, true", decisions[1].Remove, decisions[1].Deferred)
	}
}

func TestServiceImportFixableGiveUpRemoves(t *testing.T) {
	t.Parallel()

	instID := uuid.New()
	inst := queue.Instance{
		ID:      instID,
		Name:    "lidarr-main",
		Kind:    domain.KindLidarr,
		BaseURL: "http://lidarr.local",
		APIKey:  "secret-key",
	}

	st := newFakeStore()
	st.settings[instID] = queue.Settings{
		Enabled: true,
		Rules: map[queue.Rule]queue.RuleSettings{
			queue.RuleImportFixable: {Mode: queue.ModeAct, Threshold: 1},
		},
		Grace:               0,
		MaxRemovalsPerSweep: 10,
	}

	clk := clock.NewFake()
	now := clk.Now()
	st.strikes[instID] = map[string]queue.Strike{
		"dl-fixable-25h": {
			Rule:          queue.RuleImportFixable,
			Count:         1,
			ImportedAt:    now.Add(-10 * time.Minute),
			ImportRefused: true,
			LastSeen:      now.Add(-15 * time.Minute),
		},
	}

	arr := newFakeArrClient()
	arrKey := string(domain.KindLidarr) + ":http://lidarr.local"
	arr.items[arrKey] = []queue.Item{
		{
			ID:                    101,
			DownloadID:            "dl-fixable-25h",
			Title:                 "Artist - Album",
			Added:                 now.Add(-25 * time.Hour),
			TrackedDownloadState:  "importBlocked",
			TrackedDownloadStatus: "warning",
			StatusLines: []string{
				"Album match is not close enough: 77.0% vs 80%",
			},
		},
	}

	is := &fakeInstanceSource{instances: []queue.Instance{inst}}
	ev := &fakeEventPublisher{}
	m := &fakeMetricsRecorder{}

	svc := newTestService(st, is, arr, clk, ev, m)
	if err := svc.SweepAll(t.Context()); err != nil {
		t.Fatalf("SweepAll() error: %v", err)
	}

	if len(arr.removals) != 1 {
		t.Fatalf("got %d removals, want 1", len(arr.removals))
	}
	if !arr.removals[0].blocklist {
		t.Errorf("removal blocklist = false, want true")
	}
	if !arr.removals[0].research {
		t.Errorf("removal research = false, want true")
	}
	if len(arr.imports) != 0 {
		t.Errorf("got %d imports, want 0", len(arr.imports))
	}
}
