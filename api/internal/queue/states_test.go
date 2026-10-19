// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package queue_test

import (
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/queue"
)

type stateTestCase struct {
	name      string
	settings  queue.Settings
	item      queue.Item
	prev      map[string]queue.Strike
	sweepTime time.Time
	want      wantDecision
}

type wantDecision struct {
	rule      queue.Rule
	strike    bool
	remove    bool
	blocklist bool
	count     int
}

func runSingleCase(t *testing.T, tc stateTestCase) {
	t.Helper()
	now := tc.sweepTime
	if now.IsZero() {
		now = fixedNow
	}
	decisions := queue.Sweep(tc.settings, []queue.Item{tc.item}, tc.prev, now)
	if len(decisions) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(decisions))
	}
	assertDecision(t, decisions[0], tc.want)
}

func assertDecision(t *testing.T, got queue.Decision, want wantDecision) {
	t.Helper()
	if got.Rule != want.rule {
		t.Errorf("Rule = %q, want %q", got.Rule, want.rule)
	}
	if got.Strike != want.strike {
		t.Errorf("Strike = %v, want %v", got.Strike, want.strike)
	}
	if got.Remove != want.remove {
		t.Errorf("Remove = %v, want %v", got.Remove, want.remove)
	}
	if got.Blocklist != want.blocklist {
		t.Errorf("Blocklist = %v, want %v", got.Blocklist, want.blocklist)
	}
	if got.Next.Count != want.count {
		t.Errorf("Next.Count = %d, want %d", got.Next.Count, want.count)
	}
}

func cleanProgressItem(r queue.Rule, added time.Time) queue.Item {
	item := itemFor(r, added)
	item.Status = "downloading"
	item.TrackedDownloadStatus = "ok"
	item.TrackedDownloadState = "downloading"
	item.SizeLeft = 5_000_000
	item.CustomFormatScore = 100
	item.Monitored = true
	return item
}

func TestStateStalled(t *testing.T) {
	t.Parallel()
	cases := append(stalledPositiveCases(), stalledNegativeCases()...)
	cases = append(cases, stalledBoundaryCases()...)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runSingleCase(t, tc)
		})
	}
}

func stalledPositiveCases() []stateTestCase {
	act6 := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleStalled: 6})
	roseItem := itemFor(queue.RuleStalled, fixedNow.Add(-1*time.Hour))
	roseItem.SizeLeft = 10_000_500
	unmonItem := itemFor(queue.RuleStalled, fixedNow.Add(-1*time.Hour))
	unmonItem.Monitored = false
	return []stateTestCase{
		{
			name:     "positive/zero_progress_never_slow",
			settings: act6,
			item:     itemFor(queue.RuleStalled, fixedNow.Add(-1*time.Hour)),
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleStalled, fixedNow.Add(-10*time.Minute), 0)},
			want:     wantDecision{rule: queue.RuleStalled, strike: true, remove: false, blocklist: true, count: 1},
		},
		{
			name:     "positive/sizeleft_rose",
			settings: act6,
			item:     roseItem,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleStalled, fixedNow.Add(-10*time.Minute), 1)},
			want:     wantDecision{rule: queue.RuleStalled, strike: true, remove: false, blocklist: true, count: 2},
		},
		{
			name:     "positive/rule_order_over_unmonitored",
			settings: act6,
			item:     unmonItem,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleStalled, fixedNow.Add(-10*time.Minute), 0)},
			want:     wantDecision{rule: queue.RuleStalled, strike: true, remove: false, blocklist: true, count: 1},
		},
	}
}

func stalledNegativeCases() []stateTestCase {
	act6 := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleStalled: 6})
	zeroItem := itemFor(queue.RuleStalled, fixedNow.Add(-1*time.Hour))
	zeroItem.SizeLeft = 0
	droppedItem := itemFor(queue.RuleStalled, fixedNow.Add(-1*time.Hour))
	droppedItem.SizeLeft = 5_000_000
	return []stateTestCase{
		{
			name:     "negative/first_sighting_no_prev",
			settings: act6,
			item:     itemFor(queue.RuleStalled, fixedNow.Add(-1*time.Hour)),
			prev:     nil,
			want:     wantDecision{rule: "", strike: false, remove: false, blocklist: false, count: 0},
		},
		{
			name:     "negative/sizeleft_zero",
			settings: act6,
			item:     zeroItem,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleStalled, fixedNow.Add(-10*time.Minute), 1)},
			want:     wantDecision{rule: "", strike: false, remove: false, blocklist: false, count: 0},
		},
		{
			name:     "negative/sizeleft_dropped_normal",
			settings: act6,
			item:     droppedItem,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleStalled, fixedNow.Add(-10*time.Minute), 1)},
			want:     wantDecision{rule: "", strike: false, remove: false, blocklist: false, count: 0},
		},
	}
}

func stalledBoundaryCases() []stateTestCase {
	act6 := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleStalled: 6})
	cleanItem := cleanProgressItem(queue.RuleStalled, fixedNow.Add(-1*time.Hour))
	resumeItem := itemFor(queue.RuleStalled, fixedNow.Add(-1*time.Hour))
	resumeItem.SizeLeft = 5_000_000
	return []stateTestCase{
		{
			name:     "boundary/threshold_minus_one_no_remove",
			settings: act6,
			item:     itemFor(queue.RuleStalled, fixedNow.Add(-1*time.Hour)),
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleStalled, fixedNow.Add(-10*time.Minute), 4)},
			want:     wantDecision{rule: queue.RuleStalled, strike: true, remove: false, blocklist: true, count: 5},
		},
		{
			name:     "boundary/threshold_reached_removes",
			settings: act6,
			item:     itemFor(queue.RuleStalled, fixedNow.Add(-1*time.Hour)),
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleStalled, fixedNow.Add(-10*time.Minute), 5)},
			want:     wantDecision{rule: queue.RuleStalled, strike: true, remove: true, blocklist: true, count: 6},
		},
		{
			name:     "boundary/clean_sweep_resets",
			settings: act6,
			item:     cleanItem,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleStalled, fixedNow.Add(-10*time.Minute), 5)},
			want:     wantDecision{rule: "", strike: false, remove: false, blocklist: false, count: 0},
		},
		{
			name:     "boundary/post_clean_resumes_at_one",
			settings: act6,
			item:     resumeItem,
			prev:     map[string]queue.Strike{"dl-42": {Rule: "", Count: 0, LastSizeLeft: 5_000_000, LastSeen: fixedNow.Add(-10 * time.Minute)}},
			want:     wantDecision{rule: queue.RuleStalled, strike: true, remove: false, blocklist: true, count: 1},
		},
	}
}

func TestStateSlow(t *testing.T) {
	t.Parallel()
	cases := append(slowPositiveCases(), slowNegativeCases()...)
	cases = append(cases, slowBoundaryCases()...)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runSingleCase(t, tc)
		})
	}
}

func slowPositiveCases() []stateTestCase {
	act6 := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleSlow: 6})
	belowItem := itemFor(queue.RuleSlow, fixedNow.Add(-1*time.Hour))
	belowItem.SizeLeft = 9_990_010
	unmonItem := itemFor(queue.RuleSlow, fixedNow.Add(-1*time.Hour))
	unmonItem.SizeLeft = 9_990_010
	unmonItem.Monitored = false
	return []stateTestCase{
		{
			name:     "positive/rate_one_below_floor",
			settings: act6,
			item:     belowItem,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleSlow, fixedNow.Add(-10*time.Second), 0)},
			want:     wantDecision{rule: queue.RuleSlow, strike: true, remove: false, blocklist: true, count: 1},
		},
		{
			name:     "positive/rule_order_over_unmonitored",
			settings: act6,
			item:     unmonItem,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleSlow, fixedNow.Add(-10*time.Second), 0)},
			want:     wantDecision{rule: queue.RuleSlow, strike: true, remove: false, blocklist: true, count: 1},
		},
	}
}

func slowNegativeCases() []stateTestCase {
	act6 := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleSlow: 6})
	exactItem := itemFor(queue.RuleSlow, fixedNow.Add(-1*time.Hour))
	exactItem.SizeLeft = 9_990_000
	disabledSettings := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleSlow: 6})
	disabledSettings.MinBytesPerSecond = 0
	disabledItem := itemFor(queue.RuleSlow, fixedNow.Add(-1*time.Hour))
	disabledItem.SizeLeft = 9_999_999
	fastItem := itemFor(queue.RuleSlow, fixedNow.Add(-1*time.Hour))
	fastItem.SizeLeft = 9_980_000
	return []stateTestCase{
		{
			name:     "negative/rate_exactly_equals_floor",
			settings: act6,
			item:     exactItem,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleSlow, fixedNow.Add(-10*time.Second), 0)},
			want:     wantDecision{rule: "", strike: false, remove: false, blocklist: false, count: 0},
		},
		{
			name:     "negative/min_bytes_zero_disabled",
			settings: disabledSettings,
			item:     disabledItem,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleSlow, fixedNow.Add(-10*time.Second), 0)},
			want:     wantDecision{rule: "", strike: false, remove: false, blocklist: false, count: 0},
		},
		{
			name:     "negative/rate_above_floor",
			settings: act6,
			item:     fastItem,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleSlow, fixedNow.Add(-10*time.Second), 0)},
			want:     wantDecision{rule: "", strike: false, remove: false, blocklist: false, count: 0},
		},
	}
}

func slowBoundaryCases() []stateTestCase {
	act6 := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleSlow: 6})
	slowItem := itemFor(queue.RuleSlow, fixedNow.Add(-1*time.Hour))
	slowItem.SizeLeft = 9_990_010
	cleanItem := cleanProgressItem(queue.RuleSlow, fixedNow.Add(-1*time.Hour))
	resumeItem := itemFor(queue.RuleSlow, fixedNow.Add(-1*time.Hour))
	resumeItem.SizeLeft = 4_995_000
	return []stateTestCase{
		{
			name:     "boundary/threshold_minus_one_no_remove",
			settings: act6,
			item:     slowItem,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleSlow, fixedNow.Add(-10*time.Second), 4)},
			want:     wantDecision{rule: queue.RuleSlow, strike: true, remove: false, blocklist: true, count: 5},
		},
		{
			name:     "boundary/threshold_reached_removes",
			settings: act6,
			item:     slowItem,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleSlow, fixedNow.Add(-10*time.Second), 5)},
			want:     wantDecision{rule: queue.RuleSlow, strike: true, remove: true, blocklist: true, count: 6},
		},
		{
			name:     "boundary/clean_sweep_resets",
			settings: act6,
			item:     cleanItem,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleSlow, fixedNow.Add(-10*time.Second), 5)},
			want:     wantDecision{rule: "", strike: false, remove: false, blocklist: false, count: 0},
		},
		{
			name:     "boundary/post_clean_resumes_at_one",
			settings: act6,
			item:     resumeItem,
			prev:     map[string]queue.Strike{"dl-42": {Rule: "", Count: 0, LastSizeLeft: 5_000_000, LastSeen: fixedNow.Add(-10 * time.Second)}},
			want:     wantDecision{rule: queue.RuleSlow, strike: true, remove: false, blocklist: true, count: 1},
		},
	}
}

func TestStateFailed(t *testing.T) {
	t.Parallel()
	cases := append(failedPositiveCases(), failedNegativeCases()...)
	cases = append(cases, failedBoundaryCases()...)
	cases = append(cases, failedLidarrReadarrCases()...)
	cases = append(cases, failedErrorStatusCases()...)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runSingleCase(t, tc)
		})
	}
}

func failedPositiveCases() []stateTestCase {
	act2 := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleFailed: 2})
	pendingItem := itemFor(queue.RuleFailed, fixedNow.Add(-1*time.Hour))
	pendingItem.Status = "downloading"
	pendingItem.TrackedDownloadState = "failedPending"
	statusAloneItem := itemFor(queue.RuleFailed, fixedNow.Add(-1*time.Hour))
	statusAloneItem.Status = "failed"
	statusAloneItem.TrackedDownloadState = "downloading"
	casedItem := itemFor(queue.RuleFailed, fixedNow.Add(-1*time.Hour))
	casedItem.Status = "FAILED"
	casedItem.TrackedDownloadState = "FailedPending"
	unmonItem := itemFor(queue.RuleFailed, fixedNow.Add(-1*time.Hour))
	unmonItem.Monitored = false
	return []stateTestCase{
		{
			name:     "positive/state_failed",
			settings: act2,
			item:     itemFor(queue.RuleFailed, fixedNow.Add(-1*time.Hour)),
			prev:     nil,
			want:     wantDecision{rule: queue.RuleFailed, strike: true, remove: false, blocklist: true, count: 1},
		},
		{
			name:     "positive/state_failed_pending",
			settings: act2,
			item:     pendingItem,
			prev:     nil,
			want:     wantDecision{rule: queue.RuleFailed, strike: true, remove: false, blocklist: true, count: 1},
		},
		{
			name:     "positive/status_failed_alone",
			settings: act2,
			item:     statusAloneItem,
			prev:     nil,
			want:     wantDecision{rule: queue.RuleFailed, strike: true, remove: false, blocklist: true, count: 1},
		},
		{
			name:     "positive/case_insensitive_state_and_status",
			settings: act2,
			item:     casedItem,
			prev:     nil,
			want:     wantDecision{rule: queue.RuleFailed, strike: true, remove: false, blocklist: true, count: 1},
		},
		{
			name:     "positive/rule_order_over_unmonitored",
			settings: act2,
			item:     unmonItem,
			prev:     nil,
			want:     wantDecision{rule: queue.RuleFailed, strike: true, remove: false, blocklist: true, count: 1},
		},
	}
}

func failedNegativeCases() []stateTestCase {
	act2 := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleFailed: 2})
	queuedItem := itemFor(queue.RuleFailed, fixedNow.Add(-1*time.Hour))
	queuedItem.Status = "queued"
	graceItem := itemFor(queue.RuleFailed, fixedNow.Add(-5*time.Minute))
	normalItem := cleanProgressItem(queue.RuleFailed, fixedNow.Add(-1*time.Hour))
	return []stateTestCase{
		{
			name:     "negative/status_queued_never_touched",
			settings: act2,
			item:     queuedItem,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleFailed, fixedNow.Add(-10*time.Minute), 1)},
			want:     wantDecision{rule: "", strike: false, remove: false, blocklist: false, count: 1},
		},
		{
			name:     "negative/within_grace_never_touched",
			settings: act2,
			item:     graceItem,
			prev:     nil,
			want:     wantDecision{rule: "", strike: false, remove: false, blocklist: false, count: 0},
		},
		{
			name:     "negative/normal_downloading_state",
			settings: act2,
			item:     normalItem,
			prev:     nil,
			want:     wantDecision{rule: "", strike: false, remove: false, blocklist: false, count: 0},
		},
	}
}

func failedBoundaryCases() []stateTestCase {
	act2 := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleFailed: 2})
	cleanItem := cleanProgressItem(queue.RuleFailed, fixedNow.Add(-1*time.Hour))
	return []stateTestCase{
		{
			name:     "boundary/threshold_minus_one_no_remove",
			settings: act2,
			item:     itemFor(queue.RuleFailed, fixedNow.Add(-1*time.Hour)),
			prev:     nil,
			want:     wantDecision{rule: queue.RuleFailed, strike: true, remove: false, blocklist: true, count: 1},
		},
		{
			name:     "boundary/threshold_reached_removes",
			settings: act2,
			item:     itemFor(queue.RuleFailed, fixedNow.Add(-1*time.Hour)),
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleFailed, fixedNow.Add(-10*time.Minute), 1)},
			want:     wantDecision{rule: queue.RuleFailed, strike: true, remove: true, blocklist: true, count: 2},
		},
		{
			name:     "boundary/clean_sweep_resets",
			settings: act2,
			item:     cleanItem,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleFailed, fixedNow.Add(-10*time.Minute), 1)},
			want:     wantDecision{rule: "", strike: false, remove: false, blocklist: false, count: 0},
		},
		{
			name:     "boundary/post_clean_resumes_at_one",
			settings: act2,
			item:     itemFor(queue.RuleFailed, fixedNow.Add(-1*time.Hour)),
			prev:     map[string]queue.Strike{"dl-42": {Rule: "", Count: 0, LastSeen: fixedNow.Add(-10 * time.Minute)}},
			want:     wantDecision{rule: queue.RuleFailed, strike: true, remove: false, blocklist: true, count: 1},
		},
	}
}

// failedErrorStatusCases pins how trackedDownloadStatus=error is read.
// Sonarr and Radarr set it only in TrackedDownload.Fail(), together with
// state failedPending; Lidarr and Readarr never set it. The failed rule
// therefore keys on the state, and error alone is no failure.
func failedErrorStatusCases() []stateTestCase {
	act2 := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleFailed: 2})
	withError := func(state string) queue.Item {
		it := itemFor(queue.RuleFailed, fixedNow.Add(-1*time.Hour))
		it.Status, it.TrackedDownloadStatus, it.TrackedDownloadState = "completed", "error", state
		return it
	}
	return []stateTestCase{
		{
			name: "positive/error_with_failed_pending", settings: act2, item: withError("failedPending"),
			want: wantDecision{rule: queue.RuleFailed, strike: true, remove: false, blocklist: true, count: 1},
		},
		{
			name: "negative/error_status_alone_is_not_failed", settings: act2, item: withError("downloading"),
			want: wantDecision{rule: "", strike: false, remove: false, blocklist: false, count: 0},
		},
		{
			name: "boundary/error_with_failed_pending_threshold_reached_removes", settings: act2, item: withError("failedPending"),
			prev: map[string]queue.Strike{"dl-42": prevFor(queue.RuleFailed, fixedNow.Add(-10*time.Minute), 1)},
			want: wantDecision{rule: queue.RuleFailed, strike: true, remove: true, blocklist: true, count: 2},
		},
	}
}

// failedLidarrReadarrCases covers the state names Lidarr and the Readarr
// family use for the same failures (upstream TrackedDownloadState enums).
func failedLidarrReadarrCases() []stateTestCase {
	act2 := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleFailed: 2})
	// Every live Lidarr failure carries trackedDownloadStatus=warning.
	withState := func(status, state string) queue.Item {
		it := itemFor(queue.RuleFailed, fixedNow.Add(-1*time.Hour))
		it.Status, it.TrackedDownloadStatus, it.TrackedDownloadState = status, "warning", state
		return it
	}
	struck := wantDecision{rule: queue.RuleFailed, strike: true, remove: false, blocklist: true, count: 1}
	prevOne := map[string]queue.Strike{"dl-42": prevFor(queue.RuleFailed, fixedNow.Add(-10*time.Minute), 1)}
	return []stateTestCase{
		{name: "positive/lidarr_download_failed", settings: act2, item: withState("downloading", "downloadFailed"), want: struck},
		{name: "positive/lidarr_download_failed_pending", settings: act2, item: withState("downloading", "downloadFailedPending"), want: struck},
		{
			name: "negative/lidarr_import_failed_is_not_failed", settings: act2,
			item: withState("completed", "importFailed"),
			want: wantDecision{rule: queue.RuleImportBlocked, strike: true, remove: false, blocklist: true, count: 1},
		},
		{
			name: "negative/readarr_import_failed_cased_is_not_failed", settings: act2,
			item: withState("completed", "ImportFailed"),
			want: wantDecision{rule: queue.RuleImportBlocked, strike: true, remove: false, blocklist: true, count: 1},
		},
		{
			name: "negative/lidarr_import_blocked_is_not_failed", settings: act2,
			item: withState("completed", "importBlocked"),
			want: wantDecision{rule: queue.RuleImportBlocked, strike: true, remove: false, blocklist: true, count: 1},
		},
		{
			name: "negative/import_failed_paused_never_touched", settings: act2,
			item: withState("paused", "importFailed"),
			want: wantDecision{rule: "", strike: false, remove: false, blocklist: false, count: 0},
		},
		{
			name: "boundary/download_failed_threshold_reached_removes", settings: act2,
			item: withState("downloading", "downloadFailed"), prev: prevOne,
			want: wantDecision{rule: queue.RuleFailed, strike: true, remove: true, blocklist: true, count: 2},
		},
	}
}

func TestStateImportBlocked(t *testing.T) {
	t.Parallel()
	cases := append(importBlockedPositiveCases(), importBlockedNegativeCases()...)
	cases = append(cases, importBlockedBoundaryCases()...)
	cases = append(cases, importBlockedIDMatchCases()...)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runSingleCase(t, tc)
		})
	}
}

// idMatchNote and idMatchNoteRadarr are the Sonarr and Radarr wordings of
// the note a manual import clears, which the id_matched rule force-imports
// (ADR-0013). Both are the texts make arr-forceimport-live reads from the
// real releases.
const (
	idMatchNote       = "Found matching series via grab history, but release was matched to series by ID. Automatic import is not possible. See the FAQ for details."
	idMatchNoteRadarr = "Found matching movie via grab history, but release was matched to movie by ID. Manual Import required."
	// titleMismatchNote is Sonarr's note for a download without grab history:
	// it shares "automatic import is not possible" but is no ID match.
	titleMismatchNote = "Series title mismatch; automatic import is not possible. Check the download troubleshooting entry on the wiki for common causes."
)

// importBlockedIDMatchCases pins the ID-match allow-list: import_blocked
// never strikes an item whose only problem is the ID-match note; that item
// belongs to id_matched (idmatched_test.go).
func importBlockedIDMatchCases() []stateTestCase {
	act6 := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleImportBlocked: 6})
	withLines := func(state string, lines ...string) queue.Item {
		it := itemFor(queue.RuleImportBlocked, fixedNow.Add(-1*time.Hour))
		it.TrackedDownloadState, it.TrackedDownloadStatus = state, "warning"
		it.Title, it.StatusLines = "Show.S01E01.1080p.WEB-GRP", lines
		return it
	}
	struck := wantDecision{rule: queue.RuleImportBlocked, strike: true, remove: false, blocklist: true, count: 1}
	skipped := wantDecision{rule: "", strike: false, remove: false, blocklist: false, count: 0}
	okStatus := withLines("importBlocked", idMatchNote)
	okStatus.TrackedDownloadStatus = "ok"
	return []stateTestCase{
		{name: "negative/id_match_only_needle", settings: act6, item: withLines("importBlocked", idMatchNote), want: skipped},
		{name: "negative/id_match_needle_and_title", settings: act6, item: withLines("importBlocked", "Show.S01E01.1080p.WEB-GRP", idMatchNote), want: skipped},
		{name: "negative/id_match_import_pending", settings: act6, item: withLines("importPending", idMatchNote), want: skipped},
		{name: "negative/id_match_radarr_note", settings: act6, item: withLines("importBlocked", "Show.S01E01.1080p.WEB-GRP", idMatchNoteRadarr), want: skipped},
		{name: "positive/id_match_title_mismatch_is_no_id_match", settings: act6, item: withLines("importBlocked", titleMismatchNote), want: wantDecision{rule: queue.RuleImportFixable, strike: true, remove: false, blocklist: false, count: 1}},
		{name: "positive/id_match_needle_and_other_problem", settings: act6, item: withLines("importBlocked", idMatchNote, "Not an upgrade for existing episode file(s)"), want: wantDecision{rule: queue.RuleImportNotWanted, strike: true, remove: false, blocklist: true, count: 1}},
		{name: "positive/id_match_no_status_lines", settings: act6, item: withLines("importBlocked"), want: struck},
		{name: "positive/id_match_title_only", settings: act6, item: withLines("importBlocked", "Show.S01E01.1080p.WEB-GRP"), want: struck},
		{name: "positive/id_match_needs_warning_status", settings: act6, item: okStatus, want: wantDecision{rule: queue.RuleImportFixable, strike: true, remove: false, blocklist: false, count: 1}},
		{name: "boundary/id_match_needle_case_and_padding", settings: act6, item: withLines("ImportBlocked", "  RELEASE WAS MATCHED TO SERIES BY ID.", " Show.S01E01.1080p.WEB-GRP "), want: skipped},
	}
}

func importBlockedPositiveCases() []stateTestCase {
	act6 := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleImportBlocked: 6})
	warnItem := itemFor(queue.RuleImportBlocked, fixedNow.Add(-1*time.Hour))
	warnItem.TrackedDownloadState = "importPending"
	warnItem.TrackedDownloadStatus = "warning"
	casedItem := itemFor(queue.RuleImportBlocked, fixedNow.Add(-1*time.Hour))
	casedItem.TrackedDownloadState = "ImportBlocked"
	casedItem.TrackedDownloadStatus = "Ok"
	unmonItem := itemFor(queue.RuleImportBlocked, fixedNow.Add(-1*time.Hour))
	unmonItem.Monitored = false
	return []stateTestCase{
		{
			name:     "positive/state_import_blocked",
			settings: act6,
			item:     itemFor(queue.RuleImportBlocked, fixedNow.Add(-1*time.Hour)),
			prev:     nil,
			want:     wantDecision{rule: queue.RuleImportBlocked, strike: true, remove: false, blocklist: true, count: 1},
		},
		{
			name:     "positive/import_pending_with_warning",
			settings: act6,
			item:     warnItem,
			prev:     nil,
			want:     wantDecision{rule: queue.RuleImportBlocked, strike: true, remove: false, blocklist: true, count: 1},
		},
		{
			name:     "positive/case_insensitive_state",
			settings: act6,
			item:     casedItem,
			prev:     nil,
			want:     wantDecision{rule: queue.RuleImportBlocked, strike: true, remove: false, blocklist: true, count: 1},
		},
		{
			name:     "positive/rule_order_over_unmonitored",
			settings: act6,
			item:     unmonItem,
			prev:     nil,
			want:     wantDecision{rule: queue.RuleImportBlocked, strike: true, remove: false, blocklist: true, count: 1},
		},
	}
}

func importBlockedNegativeCases() []stateTestCase {
	act6 := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleImportBlocked: 6})
	okPending := itemFor(queue.RuleImportBlocked, fixedNow.Add(-1*time.Hour))
	okPending.TrackedDownloadState = "importPending"
	okPending.TrackedDownloadStatus = "ok"
	importing := itemFor(queue.RuleImportBlocked, fixedNow.Add(-1*time.Hour))
	importing.TrackedDownloadState = "importing"
	imported := itemFor(queue.RuleImportBlocked, fixedNow.Add(-1*time.Hour))
	imported.TrackedDownloadState = "imported"
	return []stateTestCase{
		{
			name:     "negative/import_pending_with_ok_status",
			settings: act6,
			item:     okPending,
			prev:     nil,
			want:     wantDecision{rule: "", strike: false, remove: false, blocklist: false, count: 0},
		},
		{
			name:     "negative/importing_state_never_touched",
			settings: act6,
			item:     importing,
			prev:     nil,
			want:     wantDecision{rule: "", strike: false, remove: false, blocklist: false, count: 0},
		},
		{
			name:     "negative/imported_state_never_touched",
			settings: act6,
			item:     imported,
			prev:     nil,
			want:     wantDecision{rule: "", strike: false, remove: false, blocklist: false, count: 0},
		},
	}
}

func importBlockedBoundaryCases() []stateTestCase {
	act6 := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleImportBlocked: 6})
	cleanItem := cleanProgressItem(queue.RuleImportBlocked, fixedNow.Add(-1*time.Hour))
	return []stateTestCase{
		{
			name:     "boundary/threshold_minus_one_no_remove",
			settings: act6,
			item:     itemFor(queue.RuleImportBlocked, fixedNow.Add(-1*time.Hour)),
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleImportBlocked, fixedNow.Add(-10*time.Minute), 4)},
			want:     wantDecision{rule: queue.RuleImportBlocked, strike: true, remove: false, blocklist: true, count: 5},
		},
		{
			name:     "boundary/threshold_reached_never_removes",
			settings: act6,
			item:     itemFor(queue.RuleImportBlocked, fixedNow.Add(-1*time.Hour)),
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleImportBlocked, fixedNow.Add(-10*time.Minute), 5)},
			want:     wantDecision{rule: queue.RuleImportBlocked, strike: true, remove: false, blocklist: true, count: 6},
		},
		{
			name:     "boundary/clean_sweep_resets",
			settings: act6,
			item:     cleanItem,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleImportBlocked, fixedNow.Add(-10*time.Minute), 5)},
			want:     wantDecision{rule: "", strike: false, remove: false, blocklist: false, count: 0},
		},
		{
			name:     "boundary/post_clean_resumes_at_one",
			settings: act6,
			item:     itemFor(queue.RuleImportBlocked, fixedNow.Add(-1*time.Hour)),
			prev:     map[string]queue.Strike{"dl-42": {Rule: "", Count: 0, LastSeen: fixedNow.Add(-10 * time.Minute)}},
			want:     wantDecision{rule: queue.RuleImportBlocked, strike: true, remove: false, blocklist: true, count: 1},
		},
	}
}

func TestStateUnwantedScore(t *testing.T) {
	t.Parallel()
	cases := append(unwantedScorePositiveCases(), unwantedScoreNegativeCases()...)
	cases = append(cases, unwantedScoreBoundaryCases()...)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runSingleCase(t, tc)
		})
	}
}

func unwantedScorePositiveCases() []stateTestCase {
	act1 := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleUnwantedScore: 1})
	belowItem := cleanProgressItem(queue.RuleUnwantedScore, fixedNow.Add(-1*time.Hour))
	belowItem.CustomFormatScore = 49
	unmonItem := cleanProgressItem(queue.RuleUnwantedScore, fixedNow.Add(-1*time.Hour))
	unmonItem.CustomFormatScore = 49
	unmonItem.Monitored = false
	return []stateTestCase{
		{
			name:     "positive/score_one_below_min",
			settings: act1,
			item:     belowItem,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleUnwantedScore, fixedNow.Add(-10*time.Minute), 0)},
			want:     wantDecision{rule: queue.RuleUnwantedScore, strike: true, remove: true, blocklist: true, count: 1},
		},
		{
			name:     "positive/rule_order_over_unmonitored",
			settings: act1,
			item:     unmonItem,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleUnwantedScore, fixedNow.Add(-10*time.Minute), 0)},
			want:     wantDecision{rule: queue.RuleUnwantedScore, strike: true, remove: true, blocklist: true, count: 1},
		},
	}
}

func unwantedScoreNegativeCases() []stateTestCase {
	act1 := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleUnwantedScore: 1})
	equalItem := cleanProgressItem(queue.RuleUnwantedScore, fixedNow.Add(-1*time.Hour))
	equalItem.CustomFormatScore = 50
	aboveItem := cleanProgressItem(queue.RuleUnwantedScore, fixedNow.Add(-1*time.Hour))
	aboveItem.CustomFormatScore = 100
	disabledSettings := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleUnwantedScore: 1})
	disabledSettings.MinCustomFormatScore = nil
	negativeScoreItem := cleanProgressItem(queue.RuleUnwantedScore, fixedNow.Add(-1*time.Hour))
	negativeScoreItem.CustomFormatScore = -100
	return []stateTestCase{
		{
			name:     "negative/score_equals_min",
			settings: act1,
			item:     equalItem,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleUnwantedScore, fixedNow.Add(-10*time.Minute), 0)},
			want:     wantDecision{rule: "", strike: false, remove: false, blocklist: false, count: 0},
		},
		{
			name:     "negative/score_above_min",
			settings: act1,
			item:     aboveItem,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleUnwantedScore, fixedNow.Add(-10*time.Minute), 0)},
			want:     wantDecision{rule: "", strike: false, remove: false, blocklist: false, count: 0},
		},
		{
			name:     "negative/nil_min_score_disabled",
			settings: disabledSettings,
			item:     negativeScoreItem,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleUnwantedScore, fixedNow.Add(-10*time.Minute), 0)},
			want:     wantDecision{rule: "", strike: false, remove: false, blocklist: false, count: 0},
		},
	}
}

func unwantedScoreBoundaryCases() []stateTestCase {
	act2 := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleUnwantedScore: 2})
	belowItem := cleanProgressItem(queue.RuleUnwantedScore, fixedNow.Add(-1*time.Hour))
	belowItem.CustomFormatScore = 49
	cleanItem := cleanProgressItem(queue.RuleUnwantedScore, fixedNow.Add(-1*time.Hour))
	cleanItem.CustomFormatScore = 100
	resumeItem := cleanProgressItem(queue.RuleUnwantedScore, fixedNow.Add(-1*time.Hour))
	resumeItem.SizeLeft = 4_000_000
	resumeItem.CustomFormatScore = 49
	return []stateTestCase{
		{
			name:     "boundary/threshold_minus_one_no_remove",
			settings: act2,
			item:     belowItem,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleUnwantedScore, fixedNow.Add(-10*time.Minute), 0)},
			want:     wantDecision{rule: queue.RuleUnwantedScore, strike: true, remove: false, blocklist: true, count: 1},
		},
		{
			name:     "boundary/threshold_reached_removes",
			settings: act2,
			item:     belowItem,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleUnwantedScore, fixedNow.Add(-10*time.Minute), 1)},
			want:     wantDecision{rule: queue.RuleUnwantedScore, strike: true, remove: true, blocklist: true, count: 2},
		},
		{
			name:     "boundary/clean_sweep_resets",
			settings: act2,
			item:     cleanItem,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleUnwantedScore, fixedNow.Add(-10*time.Minute), 1)},
			want:     wantDecision{rule: "", strike: false, remove: false, blocklist: false, count: 0},
		},
		{
			name:     "boundary/post_clean_resumes_at_one",
			settings: act2,
			item:     resumeItem,
			prev:     map[string]queue.Strike{"dl-42": {Rule: "", Count: 0, LastSizeLeft: 5_000_000, LastSeen: fixedNow.Add(-10 * time.Minute)}},
			want:     wantDecision{rule: queue.RuleUnwantedScore, strike: true, remove: false, blocklist: true, count: 1},
		},
	}
}

func TestStateUnwantedUnmonitored(t *testing.T) {
	t.Parallel()
	cases := append(unwantedUnmonitoredPositiveCases(), unwantedUnmonitoredNegativeCases()...)
	cases = append(cases, unwantedUnmonitoredBoundaryCases()...)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runSingleCase(t, tc)
		})
	}
}

func unwantedUnmonitoredPositiveCases() []stateTestCase {
	act1 := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleUnwantedUnmonitored: 1})
	unmonItem := cleanProgressItem(queue.RuleUnwantedUnmonitored, fixedNow.Add(-1*time.Hour))
	unmonItem.Monitored = false
	return []stateTestCase{
		{
			name:     "positive/monitored_false_threshold_one_no_blocklist",
			settings: act1,
			item:     unmonItem,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleUnwantedUnmonitored, fixedNow.Add(-10*time.Minute), 0)},
			want:     wantDecision{rule: queue.RuleUnwantedUnmonitored, strike: true, remove: true, blocklist: false, count: 1},
		},
	}
}

func unwantedUnmonitoredNegativeCases() []stateTestCase {
	act1 := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleUnwantedUnmonitored: 1})
	cleanItem := cleanProgressItem(queue.RuleUnwantedUnmonitored, fixedNow.Add(-1*time.Hour))
	return []stateTestCase{
		{
			name:     "negative/monitored_true",
			settings: act1,
			item:     cleanItem,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleUnwantedUnmonitored, fixedNow.Add(-10*time.Minute), 0)},
			want:     wantDecision{rule: "", strike: false, remove: false, blocklist: false, count: 0},
		},
	}
}

func unwantedUnmonitoredBoundaryCases() []stateTestCase {
	act2 := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleUnwantedUnmonitored: 2})
	unmonItem := cleanProgressItem(queue.RuleUnwantedUnmonitored, fixedNow.Add(-1*time.Hour))
	unmonItem.Monitored = false
	cleanItem := cleanProgressItem(queue.RuleUnwantedUnmonitored, fixedNow.Add(-1*time.Hour))
	resumeItem := cleanProgressItem(queue.RuleUnwantedUnmonitored, fixedNow.Add(-1*time.Hour))
	resumeItem.SizeLeft = 4_000_000
	resumeItem.Monitored = false
	return []stateTestCase{
		{
			name:     "boundary/threshold_minus_one_no_remove",
			settings: act2,
			item:     unmonItem,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleUnwantedUnmonitored, fixedNow.Add(-10*time.Minute), 0)},
			want:     wantDecision{rule: queue.RuleUnwantedUnmonitored, strike: true, remove: false, blocklist: false, count: 1},
		},
		{
			name:     "boundary/threshold_reached_removes",
			settings: act2,
			item:     unmonItem,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleUnwantedUnmonitored, fixedNow.Add(-10*time.Minute), 1)},
			want:     wantDecision{rule: queue.RuleUnwantedUnmonitored, strike: true, remove: true, blocklist: false, count: 2},
		},
		{
			name:     "boundary/clean_sweep_resets",
			settings: act2,
			item:     cleanItem,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleUnwantedUnmonitored, fixedNow.Add(-10*time.Minute), 1)},
			want:     wantDecision{rule: "", strike: false, remove: false, blocklist: false, count: 0},
		},
		{
			name:     "boundary/post_clean_resumes_at_one",
			settings: act2,
			item:     resumeItem,
			prev:     map[string]queue.Strike{"dl-42": {Rule: "", Count: 0, LastSizeLeft: 5_000_000, LastSeen: fixedNow.Add(-10 * time.Minute)}},
			want:     wantDecision{rule: queue.RuleUnwantedUnmonitored, strike: true, remove: false, blocklist: false, count: 1},
		},
	}
}

func TestStateNotAnUpgrade(t *testing.T) {
	t.Parallel()
	cases := append(notAnUpgradePositiveCases(), notAnUpgradeNegativeCases()...)
	cases = append(cases, notAnUpgradeBoundaryCases()...)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runSingleCase(t, tc)
		})
	}
}

// notAnUpgradeItem is a healthy download scoring 100 against a file on disk
// scoring existing; nil means no file.
func notAnUpgradeItem(existing *int32) queue.Item {
	item := cleanProgressItem(queue.RuleNotAnUpgrade, fixedNow.Add(-1*time.Hour))
	item.ExistingScore = existing
	return item
}

func notAnUpgradePositiveCases() []stateTestCase {
	act1 := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleNotAnUpgrade: 1})
	unmonitored := notAnUpgradeItem(new(int32(150)))
	unmonitored.Monitored = false
	return []stateTestCase{
		{
			name:     "positive/file_scores_higher_no_blocklist",
			settings: act1,
			item:     notAnUpgradeItem(new(int32(150))),
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleNotAnUpgrade, fixedNow.Add(-10*time.Minute), 0)},
			want:     wantDecision{rule: queue.RuleNotAnUpgrade, strike: true, remove: true, blocklist: false, count: 1},
		},
		{
			name:     "positive/wins_over_unmonitored",
			settings: act1,
			item:     unmonitored,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleNotAnUpgrade, fixedNow.Add(-10*time.Minute), 0)},
			want:     wantDecision{rule: queue.RuleNotAnUpgrade, strike: true, remove: true, blocklist: false, count: 1},
		},
	}
}

func notAnUpgradeNegativeCases() []stateTestCase {
	act1 := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleNotAnUpgrade: 1})
	prev := map[string]queue.Strike{"dl-42": prevFor(queue.RuleNotAnUpgrade, fixedNow.Add(-10*time.Minute), 0)}
	clean := wantDecision{rule: "", strike: false, remove: false, blocklist: false, count: 0}
	return []stateTestCase{
		{name: "negative/no_file_on_disk", settings: act1, item: notAnUpgradeItem(nil), prev: prev, want: clean},
		{name: "negative/real_upgrade", settings: act1, item: notAnUpgradeItem(new(int32(99))), prev: prev, want: clean},
		{name: "negative/negative_file_score_upgrade", settings: act1, item: notAnUpgradeItem(new(int32(-50))), prev: prev, want: clean},
	}
}

func notAnUpgradeBoundaryCases() []stateTestCase {
	act1 := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleNotAnUpgrade: 1})
	act2 := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleNotAnUpgrade: 2})
	equal := notAnUpgradeItem(new(int32(100)))
	return []stateTestCase{
		{
			name:     "boundary/equal_score_is_no_upgrade",
			settings: act1,
			item:     equal,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleNotAnUpgrade, fixedNow.Add(-10*time.Minute), 0)},
			want:     wantDecision{rule: queue.RuleNotAnUpgrade, strike: true, remove: true, blocklist: false, count: 1},
		},
		{
			name:     "boundary/one_point_above_is_upgrade",
			settings: act1,
			item:     notAnUpgradeItem(new(int32(99))),
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleNotAnUpgrade, fixedNow.Add(-10*time.Minute), 0)},
			want:     wantDecision{rule: "", strike: false, remove: false, blocklist: false, count: 0},
		},
		{
			name:     "boundary/threshold_minus_one_no_remove",
			settings: act2,
			item:     equal,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleNotAnUpgrade, fixedNow.Add(-10*time.Minute), 0)},
			want:     wantDecision{rule: queue.RuleNotAnUpgrade, strike: true, remove: false, blocklist: false, count: 1},
		},
		{
			name:     "boundary/threshold_reached_removes",
			settings: act2,
			item:     equal,
			prev:     map[string]queue.Strike{"dl-42": prevFor(queue.RuleNotAnUpgrade, fixedNow.Add(-10*time.Minute), 1)},
			want:     wantDecision{rule: queue.RuleNotAnUpgrade, strike: true, remove: true, blocklist: false, count: 2},
		},
	}
}
