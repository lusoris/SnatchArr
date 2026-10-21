// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package queue_test

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/queue"
)

var fixedNow = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

func testSettings(mode queue.Mode, thresholds map[queue.Rule]int) queue.Settings {
	s := queue.DefaultSettings()
	s.Enabled = true
	s.Grace = 15 * time.Minute
	s.MinBytesPerSecond = 1000
	scoreLimit := int32(50)
	s.MinCustomFormatScore = &scoreLimit
	for r, th := range thresholds {
		s.Rules[r] = queue.RuleSettings{Mode: mode, Threshold: th}
	}
	return s
}

func itemFor(rule queue.Rule, added time.Time) queue.Item {
	base := queue.Item{
		ID:                    42,
		DownloadID:            "dl-42",
		Added:                 added,
		Status:                "downloading",
		TrackedDownloadStatus: "ok",
		TrackedDownloadState:  "downloading",
		SizeLeft:              10_000_000,
		CustomFormatScore:     100,
		Monitored:             true,
	}
	switch rule {
	case queue.RuleStalled:
		return base
	case queue.RuleMetadataStuck:
		base.Protocol = "torrent"
		base.Status = "queued"
		base.TrackedDownloadState = "queued"
		base.StatusLines = []string{"qBittorrent is downloading metadata"}
		base.Size = 0
		base.SizeLeft = 0
		return base
	case queue.RuleSlow:
		base.SizeLeft = 9_999_990
		return base
	case queue.RuleFailed:
		base.Status = "failed"
		base.TrackedDownloadState = "failed"
		return base
	case queue.RuleImportBlocked:
		base.TrackedDownloadState = "importBlocked"
		return base
	case queue.RuleImportFixable:
		base.TrackedDownloadState = "importBlocked"
		base.StatusLines = []string{"Single episode file contains all episodes in seasons. Review file name or manually import"}
		return base
	case queue.RuleImportBroken:
		base.TrackedDownloadState = "importBlocked"
		base.StatusLines = []string{"Couldn't find similar album for Artist - Album"}
		return base
	case queue.RuleImportNotWanted:
		base.TrackedDownloadState = "importBlocked"
		base.StatusLines = []string{"Has fewer tracks than existing release"}
		return base
	case queue.RuleIDMatched:
		base.TrackedDownloadStatus = "warning"
		base.TrackedDownloadState = "importBlocked"
		base.StatusLines = []string{idMatchNote}
		return base
	case queue.RuleUnwantedScore:
		base.CustomFormatScore = 10
		return base
	case queue.RuleNotAnUpgrade:
		base.ExistingScore = new(int32(100))
		return base
	case queue.RuleUnwantedUnmonitored:
		base.Monitored = false
		return base
	case queue.RuleDuplicate:
		// A duplicate needs a second item; see duplicate_test.go.
		return base
	default:
		return base
	}
}

func prevFor(rule queue.Rule, lastSeen time.Time, count int) queue.Strike {
	return queue.Strike{
		Rule:         rule,
		Count:        count,
		LastSizeLeft: 10_000_000,
		LastSeen:     lastSeen,
	}
}

func TestDefaultSettingsFollowSpec(t *testing.T) {
	t.Parallel()
	want := map[queue.Rule]int{
		queue.RuleStalled:             6,
		queue.RuleMetadataStuck:       180,
		queue.RuleSlow:                6,
		queue.RuleFailed:              2,
		queue.RuleIDMatched:           1,
		queue.RuleImportFixable:       1,
		queue.RuleImportBroken:        2,
		queue.RuleImportNotWanted:     1,
		queue.RuleImportBlocked:       6,
		queue.RuleUnwantedScore:       1,
		queue.RuleNotAnUpgrade:        1,
		queue.RuleDuplicate:           1,
		queue.RuleUnwantedUnmonitored: 1,
	}
	s := queue.DefaultSettings()
	if s.Enabled || s.Grace != 15*time.Minute || s.MaxRemovalsPerSweep != 10 || s.MaxImportsPerSweep != 25 || s.ForceImport ||
		s.MinBytesPerSecond != 0 || s.MinCustomFormatScore != nil {
		t.Fatalf("defaults = %+v, want off, 15m grace, 10 removals, 25 imports, slow and score rules off", s)
	}
	if len(s.Rules) != len(want) {
		t.Fatalf("default rules = %d, want %d", len(s.Rules), len(want))
	}
	for r, threshold := range want {
		got := s.Rules[r]
		if got.Mode != queue.ModeObserve || got.Threshold != threshold {
			t.Errorf("default %s = %+v, want observe with threshold %d", r, got, threshold)
		}
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("defaults fail validation: %v", err)
	}
}

func TestRuleOrderDefinitions(t *testing.T) {
	t.Parallel()
	expected := []queue.Rule{
		queue.RuleStalled,
		queue.RuleMetadataStuck,
		queue.RuleSlow,
		queue.RuleFailed,
		queue.RuleIDMatched,
		queue.RuleImportFixable,
		queue.RuleImportBroken,
		queue.RuleImportNotWanted,
		queue.RuleImportBlocked,
		queue.RuleUnwantedScore,
		queue.RuleNotAnUpgrade,
		queue.RuleDuplicate,
		queue.RuleUnwantedUnmonitored,
	}
	if len(queue.RuleOrder) != len(expected) {
		t.Fatalf("RuleOrder length = %d, want %d", len(queue.RuleOrder), len(expected))
	}
	for i, r := range expected {
		if queue.RuleOrder[i] != r {
			t.Errorf("RuleOrder[%d] = %q, want %q", i, queue.RuleOrder[i], r)
		}
	}
}

func TestEveryRulePositive(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		rule          queue.Rule
		threshold     int
		wantBlocklist bool
	}{
		{"stalled", queue.RuleStalled, 6, true},
		{"slow", queue.RuleSlow, 6, true},
		{"failed", queue.RuleFailed, 2, true},
		{"import_broken", queue.RuleImportBroken, 2, true},
		{"import_not_wanted", queue.RuleImportNotWanted, 1, true},
		{"import_blocked", queue.RuleImportBlocked, 6, true},
		{"unwanted_score", queue.RuleUnwantedScore, 1, true},
		{"not_an_upgrade", queue.RuleNotAnUpgrade, 1, false},
		{"unwanted_unmonitored", queue.RuleUnwantedUnmonitored, 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := testSettings(queue.ModeAct, map[queue.Rule]int{tt.rule: tt.threshold})
			item := itemFor(tt.rule, fixedNow.Add(-1*time.Hour))
			prev := map[string]queue.Strike{}
			if tt.rule == queue.RuleStalled || tt.rule == queue.RuleSlow {
				prev[item.DownloadID] = prevFor(tt.rule, fixedNow.Add(-10*time.Minute), 0)
			}
			for sweep := 1; sweep <= tt.threshold; sweep++ {
				sweepNow := fixedNow.Add(time.Duration(sweep) * 10 * time.Minute)
				current := item
				if tt.rule == queue.RuleSlow {
					current.SizeLeft = 10_000_000 - int64(sweep)*10
				}
				decisions := queue.Sweep(s, []queue.Item{current}, prev, sweepNow)
				if len(decisions) != 1 {
					t.Fatalf("sweep %d returned %d decisions, want 1", sweep, len(decisions))
				}
				d := decisions[0]
				if d.Rule != tt.rule {
					t.Fatalf("sweep %d rule = %q, want %q", sweep, d.Rule, tt.rule)
				}
				if !d.Strike {
					t.Fatalf("sweep %d expected strike to be true", sweep)
				}
				if d.Blocklist != tt.wantBlocklist {
					t.Fatalf("sweep %d blocklist = %v, want %v", sweep, d.Blocklist, tt.wantBlocklist)
				}
				if d.Next.Count != sweep {
					t.Fatalf("sweep %d Next.Count = %d, want %d", sweep, d.Next.Count, sweep)
				}
				switch {
				case tt.rule == queue.RuleImportBlocked:
					if d.Remove || d.WouldRemove || !d.Unknown {
						t.Fatalf("sweep %d: import_blocked must never remove and must be unknown: %+v", sweep, d)
					}
				case sweep < tt.threshold:
					if d.Remove {
						t.Fatalf("sweep %d: premature remove before threshold", sweep)
					}
				default:
					if !d.Remove {
						t.Fatalf("sweep %d: expected remove at threshold", sweep)
					}
				}
				prev[item.DownloadID] = d.Next
			}
		})
	}
}

func TestEveryRuleNegative(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		item       queue.Item
		prev       queue.Strike
		settingsFn func() queue.Settings
	}{
		{
			name: "downloading with normal progress",
			item: queue.Item{
				ID:                   1,
				DownloadID:           "dl-1",
				Added:                fixedNow.Add(-1 * time.Hour),
				TrackedDownloadState: "downloading",
				SizeLeft:             5000,
				Monitored:            true,
				CustomFormatScore:    100,
			},
			prev: queue.Strike{
				Rule:         "",
				Count:        0,
				LastSizeLeft: 10000,
				LastSeen:     fixedNow.Add(-1 * time.Second),
			},
			settingsFn: func() queue.Settings {
				s := testSettings(queue.ModeAct, nil)
				s.MinBytesPerSecond = 100
				return s
			},
		},
		{
			name: "slow rule off when min bytes per second is zero",
			item: queue.Item{
				ID:                   2,
				DownloadID:           "dl-2",
				Added:                fixedNow.Add(-1 * time.Hour),
				TrackedDownloadState: "downloading",
				SizeLeft:             9999,
				Monitored:            true,
				CustomFormatScore:    100,
			},
			prev: queue.Strike{
				Rule:         "",
				Count:        0,
				LastSizeLeft: 10000,
				LastSeen:     fixedNow.Add(-10 * time.Minute),
			},
			settingsFn: func() queue.Settings {
				s := testSettings(queue.ModeAct, nil)
				s.MinBytesPerSecond = 0
				return s
			},
		},
		{
			name: "unwanted score off when min score is nil",
			item: queue.Item{
				ID:                   3,
				DownloadID:           "dl-3",
				Added:                fixedNow.Add(-1 * time.Hour),
				TrackedDownloadState: "downloading",
				SizeLeft:             5000,
				Monitored:            true,
				CustomFormatScore:    -50,
			},
			prev: queue.Strike{
				Rule:         "",
				Count:        0,
				LastSizeLeft: 10000,
				LastSeen:     fixedNow.Add(-1 * time.Second),
			},
			settingsFn: func() queue.Settings {
				s := testSettings(queue.ModeAct, nil)
				s.MinCustomFormatScore = nil
				s.MinBytesPerSecond = 100
				return s
			},
		},
		{
			name: "never touched status queued",
			item: queue.Item{
				ID:         4,
				DownloadID: "dl-4",
				Added:      fixedNow.Add(-1 * time.Hour),
				Status:     "queued",
			},
			prev: queue.Strike{
				Rule:         queue.RuleFailed,
				Count:        1,
				LastSizeLeft: 100,
				LastSeen:     fixedNow.Add(-10 * time.Minute),
			},
			settingsFn: func() queue.Settings { return testSettings(queue.ModeAct, nil) },
		},
		{
			name: "never touched status paused",
			item: queue.Item{
				ID:         5,
				DownloadID: "dl-5",
				Added:      fixedNow.Add(-1 * time.Hour),
				Status:     "paused",
			},
			prev:       queue.Strike{},
			settingsFn: func() queue.Settings { return testSettings(queue.ModeAct, nil) },
		},
		{
			name: "never touched status delay",
			item: queue.Item{
				ID:         6,
				DownloadID: "dl-6",
				Added:      fixedNow.Add(-1 * time.Hour),
				Status:     "delay",
			},
			prev:       queue.Strike{},
			settingsFn: func() queue.Settings { return testSettings(queue.ModeAct, nil) },
		},
		{
			name: "never touched status downloadClientUnavailable",
			item: queue.Item{
				ID:         7,
				DownloadID: "dl-7",
				Added:      fixedNow.Add(-1 * time.Hour),
				Status:     "downloadClientUnavailable",
			},
			prev:       queue.Strike{},
			settingsFn: func() queue.Settings { return testSettings(queue.ModeAct, nil) },
		},
		{
			name: "never touched state importing",
			item: queue.Item{
				ID:                   8,
				DownloadID:           "dl-8",
				Added:                fixedNow.Add(-1 * time.Hour),
				TrackedDownloadState: "importing",
			},
			prev:       queue.Strike{},
			settingsFn: func() queue.Settings { return testSettings(queue.ModeAct, nil) },
		},
		{
			name: "never touched state imported",
			item: queue.Item{
				ID:                   9,
				DownloadID:           "dl-9",
				Added:                fixedNow.Add(-1 * time.Hour),
				TrackedDownloadState: "imported",
			},
			prev:       queue.Strike{},
			settingsFn: func() queue.Settings { return testSettings(queue.ModeAct, nil) },
		},
		{
			name: "never touched state ignored",
			item: queue.Item{
				ID:                   10,
				DownloadID:           "dl-10",
				Added:                fixedNow.Add(-1 * time.Hour),
				TrackedDownloadState: "ignored",
			},
			prev:       queue.Strike{},
			settingsFn: func() queue.Settings { return testSettings(queue.ModeAct, nil) },
		},
		{
			name: "never touched within grace period",
			item: queue.Item{
				ID:                   11,
				DownloadID:           "dl-11",
				Added:                fixedNow.Add(-5 * time.Minute),
				Status:               "failed",
				TrackedDownloadState: "failed",
			},
			prev:       queue.Strike{},
			settingsFn: func() queue.Settings { return testSettings(queue.ModeAct, nil) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := tt.settingsFn()
			prev := map[string]queue.Strike{
				tt.item.DownloadID: tt.prev,
			}
			decisions := queue.Sweep(s, []queue.Item{tt.item}, prev, fixedNow)
			if len(decisions) != 1 {
				t.Fatalf("expected 1 decision, got %d", len(decisions))
			}
			d := decisions[0]
			if d.Strike {
				t.Fatalf("expected no strike, got rule %q count %d", d.Rule, d.Next.Count)
			}
			if d.Remove {
				t.Fatal("expected Remove to be false")
			}
		})
	}
}

func TestEveryRuleBoundary(t *testing.T) {
	t.Parallel()
	rules := []struct {
		rule      queue.Rule
		threshold int
	}{
		{queue.RuleStalled, 3},
		{queue.RuleSlow, 3},
		{queue.RuleFailed, 2},
		{queue.RuleImportBlocked, 3},
		{queue.RuleUnwantedScore, 2},
		{queue.RuleUnwantedUnmonitored, 2},
	}
	for _, tc := range rules {
		t.Run(string(tc.rule), func(t *testing.T) {
			t.Parallel()
			s := testSettings(queue.ModeAct, map[queue.Rule]int{tc.rule: tc.threshold})
			item := itemFor(tc.rule, fixedNow.Add(-1*time.Hour))
			prev := map[string]queue.Strike{}
			if tc.rule == queue.RuleStalled || tc.rule == queue.RuleSlow {
				prev[item.DownloadID] = prevFor(tc.rule, fixedNow.Add(-10*time.Minute), 0)
			}
			for i := 1; i < tc.threshold; i++ {
				now := fixedNow.Add(time.Duration(i) * 10 * time.Minute)
				current := item
				if tc.rule == queue.RuleSlow {
					current.SizeLeft = 10_000_000 - int64(i)*10
				}
				d := queue.Sweep(s, []queue.Item{current}, prev, now)[0]
				if d.Remove {
					t.Fatalf("boundary check: strike %d/%d removed prematurely", i, tc.threshold)
				}
				prev[item.DownloadID] = d.Next
			}
			cleanItem := queue.Item{
				ID:                    item.ID,
				DownloadID:            item.DownloadID,
				Added:                 fixedNow.Add(-1 * time.Hour),
				Status:                "downloading",
				TrackedDownloadStatus: "ok",
				TrackedDownloadState:  "downloading",
				SizeLeft:              4_000_000,
				CustomFormatScore:     100,
				Monitored:             true,
			}
			cleanNow := fixedNow.Add(time.Duration(tc.threshold) * 10 * time.Minute)
			cleanDecisions := queue.Sweep(s, []queue.Item{cleanItem}, prev, cleanNow)
			cleanD := cleanDecisions[0]
			if cleanD.Rule != "" || cleanD.Strike || cleanD.Next.Count != 0 {
				t.Fatalf("clean sweep failed to reset: rule=%q, strike=%v, count=%d", cleanD.Rule, cleanD.Strike, cleanD.Next.Count)
			}
			prev[item.DownloadID] = cleanD.Next
			retryNow := cleanNow.Add(10 * time.Minute)
			retryItem := item
			switch tc.rule {
			case queue.RuleSlow:
				retryItem.SizeLeft = cleanItem.SizeLeft - 10
			case queue.RuleStalled:
				retryItem.SizeLeft = cleanItem.SizeLeft
			case queue.RuleFailed, queue.RuleIDMatched, queue.RuleImportFixable, queue.RuleImportBroken, queue.RuleImportNotWanted, queue.RuleImportBlocked, queue.RuleUnwantedScore, queue.RuleNotAnUpgrade, queue.RuleDuplicate, queue.RuleUnwantedUnmonitored, queue.RuleMetadataStuck:
				// item already has rule-triggering fields set.
			}
			retryD := queue.Sweep(s, []queue.Item{retryItem}, prev, retryNow)[0]
			if retryD.Next.Count != 1 {
				t.Fatalf("post-clean sweep strike count = %d, want 1", retryD.Next.Count)
			}
			if retryD.Remove {
				t.Fatal("post-clean sweep first strike removed prematurely")
			}
		})
	}
}

func TestObserveModeWouldRemove(t *testing.T) {
	t.Parallel()
	s := testSettings(queue.ModeObserve, map[queue.Rule]int{queue.RuleFailed: 2})
	item := itemFor(queue.RuleFailed, fixedNow.Add(-1*time.Hour))
	prev := map[string]queue.Strike{
		item.DownloadID: prevFor(queue.RuleFailed, fixedNow.Add(-10*time.Minute), 1),
	}
	decisions := queue.Sweep(s, []queue.Item{item}, prev, fixedNow)
	if len(decisions) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(decisions))
	}
	d := decisions[0]
	if d.Remove {
		t.Fatal("observe mode must never set Remove to true")
	}
	if !d.WouldRemove {
		t.Fatal("observe mode at threshold must set WouldRemove to true")
	}
	if d.Deferred {
		t.Fatal("observe mode must not set Deferred")
	}
}

func TestRemovalBoundExhaustion(t *testing.T) {
	t.Parallel()
	s := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleFailed: 1})
	s.MaxRemovalsPerSweep = 2
	items := []queue.Item{
		{ID: 1, DownloadID: "dl-1", Added: fixedNow.Add(-1 * time.Hour), TrackedDownloadState: "failed", Monitored: true},
		{ID: 2, DownloadID: "dl-2", Added: fixedNow.Add(-1 * time.Hour), TrackedDownloadState: "failed", Monitored: true},
		{ID: 3, DownloadID: "dl-3", Added: fixedNow.Add(-1 * time.Hour), TrackedDownloadState: "failed", Monitored: true},
	}
	decisions := queue.Sweep(s, items, nil, fixedNow)
	if len(decisions) != 3 {
		t.Fatalf("expected 3 decisions, got %d", len(decisions))
	}
	if !decisions[0].Remove || decisions[0].Deferred {
		t.Errorf("item 0: expected Remove=true, Deferred=false, got remove=%v, deferred=%v", decisions[0].Remove, decisions[0].Deferred)
	}
	if !decisions[1].Remove || decisions[1].Deferred {
		t.Errorf("item 1: expected Remove=true, Deferred=false, got remove=%v, deferred=%v", decisions[1].Remove, decisions[1].Deferred)
	}
	if decisions[2].Remove || !decisions[2].Deferred {
		t.Errorf("item 2: expected Remove=false, Deferred=true, got remove=%v, deferred=%v", decisions[2].Remove, decisions[2].Deferred)
	}
}

func TestGraceProtectsNewItem(t *testing.T) {
	t.Parallel()
	s := testSettings(queue.ModeAct, nil)
	s.Grace = 15 * time.Minute
	item := queue.Item{
		ID:                   1,
		DownloadID:           "dl-new",
		Added:                fixedNow.Add(-5 * time.Minute),
		TrackedDownloadState: "failed",
		Status:               "failed",
		Monitored:            false,
		CustomFormatScore:    -100,
	}
	existingStrike := queue.Strike{Rule: queue.RuleFailed, Count: 2, LastSeen: fixedNow.Add(-10 * time.Minute)}
	prev := map[string]queue.Strike{item.DownloadID: existingStrike}
	decisions := queue.Sweep(s, []queue.Item{item}, prev, fixedNow)
	if len(decisions) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(decisions))
	}
	d := decisions[0]
	if d.Rule != "" || d.Strike || d.Remove || d.WouldRemove || d.Deferred {
		t.Fatalf("grace protection failed: rule=%q, strike=%v, remove=%v", d.Rule, d.Strike, d.Remove)
	}
	if d.Next != existingStrike {
		t.Fatalf("Next strike modified on never-touched item: got %+v, want %+v", d.Next, existingStrike)
	}
}

func TestRetryAfterDeferredDoesNotDoubleCount(t *testing.T) {
	t.Parallel()
	s := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleFailed: 2})
	s.MaxRemovalsPerSweep = 0
	item := queue.Item{
		ID:                   1,
		DownloadID:           "dl-retry",
		Added:                fixedNow.Add(-1 * time.Hour),
		TrackedDownloadState: "failed",
		Monitored:            true,
	}
	prev := map[string]queue.Strike{
		item.DownloadID: prevFor(queue.RuleFailed, fixedNow.Add(-10*time.Minute), 2),
	}
	sweep1 := queue.Sweep(s, []queue.Item{item}, prev, fixedNow)
	if len(sweep1) != 1 {
		t.Fatalf("sweep 1 expected 1 decision, got %d", len(sweep1))
	}
	d1 := sweep1[0]
	if !d1.Deferred || d1.Remove {
		t.Fatalf("sweep 1 expected Deferred=true, Remove=false; got def=%v, rem=%v", d1.Deferred, d1.Remove)
	}
	if d1.Next.Count != 2 {
		t.Fatalf("sweep 1 Next.Count = %d, want capped threshold 2", d1.Next.Count)
	}
	s.MaxRemovalsPerSweep = 10
	prev[item.DownloadID] = d1.Next
	sweep2 := queue.Sweep(s, []queue.Item{item}, prev, fixedNow.Add(10*time.Minute))
	d2 := sweep2[0]
	if !d2.Remove || d2.Deferred {
		t.Fatalf("sweep 2 expected Remove=true, Deferred=false; got rem=%v, def=%v", d2.Remove, d2.Deferred)
	}
	if d2.Next.Count != 2 {
		t.Fatalf("sweep 2 Next.Count = %d, want capped threshold 2", d2.Next.Count)
	}
}

func TestFirstSightingGivesNoStalledStrike(t *testing.T) {
	t.Parallel()
	s := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleStalled: 3})
	item := queue.Item{
		ID:                   99,
		DownloadID:           "dl-first",
		Added:                fixedNow.Add(-1 * time.Hour),
		TrackedDownloadState: "downloading",
		SizeLeft:             10_000,
		Monitored:            true,
		CustomFormatScore:    100,
	}
	sweep1 := queue.Sweep(s, []queue.Item{item}, nil, fixedNow)
	if len(sweep1) != 1 {
		t.Fatalf("sweep 1 expected 1 decision, got %d", len(sweep1))
	}
	d1 := sweep1[0]
	if d1.Strike || d1.Rule != "" {
		t.Fatalf("first sighting gave strike: rule=%q, count=%d", d1.Rule, d1.Next.Count)
	}
	if d1.Next.LastSizeLeft != 10_000 || d1.Next.LastSeen.IsZero() {
		t.Fatalf("baseline not stored on first sighting: %+v", d1.Next)
	}
	prev := map[string]queue.Strike{item.DownloadID: d1.Next}
	sweep2 := queue.Sweep(s, []queue.Item{item}, prev, fixedNow.Add(10*time.Minute))
	d2 := sweep2[0]
	if !d2.Strike || d2.Rule != queue.RuleStalled {
		t.Fatalf("second sweep expected stalled strike, got strike=%v, rule=%q", d2.Strike, d2.Rule)
	}
	if d2.Next.Count != 1 {
		t.Fatalf("second sweep strike count = %d, want 1", d2.Next.Count)
	}
}

func TestDisabledSettingsReturnsNil(t *testing.T) {
	t.Parallel()
	s := queue.DefaultSettings()
	s.Enabled = false
	item := queue.Item{ID: 1, DownloadID: "dl-1", TrackedDownloadState: "failed"}
	decisions := queue.Sweep(s, []queue.Item{item}, nil, fixedNow)
	if decisions != nil {
		t.Fatalf("Sweep on disabled settings returned %v, want nil", decisions)
	}
}

func TestFallbackKeyToID(t *testing.T) {
	t.Parallel()
	s := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleFailed: 2})
	item := queue.Item{
		ID:                   123,
		DownloadID:           "",
		Added:                fixedNow.Add(-1 * time.Hour),
		TrackedDownloadState: "failed",
		Monitored:            true,
	}
	idKey := strconv.Itoa(123)
	prev := map[string]queue.Strike{
		idKey: prevFor(queue.RuleFailed, fixedNow.Add(-10*time.Minute), 1),
	}
	decisions := queue.Sweep(s, []queue.Item{item}, prev, fixedNow)
	if len(decisions) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(decisions))
	}
	if decisions[0].Next.Count != 2 {
		t.Fatalf("fallback key count = %d, want 2", decisions[0].Next.Count)
	}
}

func TestValidateSettingsBoundaries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(*queue.Settings)
		wantErr bool
	}{
		{"valid default settings", func(_ *queue.Settings) {}, false},
		{"valid grace zero", func(s *queue.Settings) { s.Grace = 0 }, false},
		{"valid grace 24h", func(s *queue.Settings) { s.Grace = 24 * time.Hour }, false},
		{"invalid grace negative", func(s *queue.Settings) { s.Grace = -time.Second }, true},
		{"invalid grace above 24h", func(s *queue.Settings) { s.Grace = 24*time.Hour + time.Second }, true},
		{"valid max removals 1", func(s *queue.Settings) { s.MaxRemovalsPerSweep = 1 }, false},
		{"valid max removals 100", func(s *queue.Settings) { s.MaxRemovalsPerSweep = 100 }, false},
		{"invalid max removals 0", func(s *queue.Settings) { s.MaxRemovalsPerSweep = 0 }, true},
		{"invalid max removals 101", func(s *queue.Settings) { s.MaxRemovalsPerSweep = 101 }, true},
		{"valid min bps zero", func(s *queue.Settings) { s.MinBytesPerSecond = 0 }, false},
		{"valid min bps positive", func(s *queue.Settings) { s.MinBytesPerSecond = 1000 }, false},
		{"invalid min bps negative", func(s *queue.Settings) { s.MinBytesPerSecond = -1 }, true},
		{"valid threshold 1", func(s *queue.Settings) {
			s.Rules[queue.RuleStalled] = queue.RuleSettings{Mode: queue.ModeObserve, Threshold: 1}
		}, false},
		{"valid threshold 100", func(s *queue.Settings) {
			s.Rules[queue.RuleStalled] = queue.RuleSettings{Mode: queue.ModeObserve, Threshold: 100}
		}, false},
		{"invalid threshold 0", func(s *queue.Settings) {
			s.Rules[queue.RuleStalled] = queue.RuleSettings{Mode: queue.ModeObserve, Threshold: 0}
		}, true},
		{"invalid threshold 101", func(s *queue.Settings) {
			s.Rules[queue.RuleStalled] = queue.RuleSettings{Mode: queue.ModeObserve, Threshold: 101}
		}, true},
		{"invalid unknown rule", func(s *queue.Settings) {
			s.Rules[queue.Rule("unknown_rule")] = queue.RuleSettings{Mode: queue.ModeObserve, Threshold: 5}
		}, true},
		{"invalid mode", func(s *queue.Settings) {
			s.Rules[queue.RuleStalled] = queue.RuleSettings{Mode: queue.Mode("destroy"), Threshold: 5}
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := queue.DefaultSettings()
			tt.mutate(&s)
			err := s.Validate()
			if tt.wantErr && err == nil {
				t.Fatalf("%s: expected error, got nil", tt.name)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("%s: expected nil error, got %v", tt.name, err)
			}
			if tt.wantErr && !errors.Is(err, queue.ErrInvalid) {
				t.Fatalf("%s: error %v does not wrap queue.ErrInvalid", tt.name, err)
			}
		})
	}
}

func TestRuleBlocklistSetting(t *testing.T) {
	t.Parallel()
	on, off := true, false
	tests := []struct {
		name      string
		rule      queue.Rule
		blocklist *bool
		want      bool
	}{
		{"positive/unmonitored switched on", queue.RuleUnwantedUnmonitored, &on, true},
		{"negative/failed switched off", queue.RuleFailed, &off, false},
		{"negative/stalled switched off", queue.RuleStalled, &off, false},
		{"boundary/unset keeps failed default", queue.RuleFailed, nil, true},
		{"boundary/unset keeps unmonitored default", queue.RuleUnwantedUnmonitored, nil, false},
		{"boundary/explicit value equal to default", queue.RuleFailed, &on, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := testSettings(queue.ModeAct, nil)
			s.Rules[tt.rule] = queue.RuleSettings{Mode: queue.ModeAct, Threshold: 1, Blocklist: tt.blocklist}
			prev := map[string]queue.Strike{}
			item := itemFor(tt.rule, fixedNow.Add(-1*time.Hour))
			if tt.rule == queue.RuleStalled {
				prev[item.DownloadID] = prevFor(tt.rule, fixedNow.Add(-10*time.Minute), 0)
			}
			decisions := queue.Sweep(s, []queue.Item{item}, prev, fixedNow)
			if len(decisions) != 1 || decisions[0].Rule != tt.rule || !decisions[0].Remove {
				t.Fatalf("Sweep() = %+v, want one %s removal", decisions, tt.rule)
			}
			if decisions[0].Blocklist != tt.want {
				t.Errorf("Blocklist = %v, want %v", decisions[0].Blocklist, tt.want)
			}
		})
	}
}

func TestRuleBlocklistDefaultsMatchTable(t *testing.T) {
	t.Parallel()
	want := map[queue.Rule]bool{
		queue.RuleStalled:             true,
		queue.RuleMetadataStuck:       true,
		queue.RuleSlow:                true,
		queue.RuleFailed:              true,
		queue.RuleImportBlocked:       true,
		queue.RuleUnwantedScore:       true,
		queue.RuleNotAnUpgrade:        false,
		queue.RuleDuplicate:           false,
		queue.RuleUnwantedUnmonitored: false,
	}
	s := queue.DefaultSettings()
	for r, blocklist := range want {
		got := s.Rules[r].Blocklist
		if got == nil || *got != blocklist {
			t.Errorf("default %s blocklist = %v, want %v", r, got, blocklist)
		}
		if rs := queue.ResolveRule(queue.Settings{}, r); rs.Blocklist == nil || *rs.Blocklist != blocklist {
			t.Errorf("ResolveRule(empty, %s) blocklist = %v, want %v", r, rs.Blocklist, blocklist)
		}
	}
}
