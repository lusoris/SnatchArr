// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package queue

import (
	"strconv"
	"strings"
	"time"
)

// Sweep evaluates one batch of queue items against settings and previous strikes.
// Deterministic order: items are processed in input order.
// Returns nil when settings are disabled.
func Sweep(s Settings, items []Item, prev map[string]Strike, now time.Time) []Decision {
	if !s.Enabled {
		return nil
	}
	decisions := make([]Decision, 0, len(items))
	removalsCount := 0
	for _, item := range items {
		key := itemKey(item)
		var prevStrike Strike
		if prev != nil {
			prevStrike = prev[key]
		}
		dec := evaluateItem(s, item, prevStrike, now, &removalsCount)
		decisions = append(decisions, dec)
	}
	return decisions
}

func itemKey(item Item) string {
	if item.DownloadID != "" {
		return item.DownloadID
	}
	return strconv.FormatInt(int64(item.ID), 10)
}

func evaluateItem(s Settings, item Item, prev Strike, now time.Time, removalsCount *int) Decision {
	if isNeverTouched(item, s.Grace, now) {
		return Decision{
			ItemID:     item.ID,
			DownloadID: item.DownloadID,
			Rule:       "",
			Next:       prev,
		}
	}
	rule := matchRule(s, item, prev, now)
	if rule == "" {
		return Decision{
			ItemID:     item.ID,
			DownloadID: item.DownloadID,
			Rule:       "",
			Next: Strike{
				Rule:         "",
				Count:        0,
				LastSizeLeft: item.SizeLeft,
				LastSeen:     now,
			},
		}
	}
	return applyRuleMatch(s, item, prev, rule, now, removalsCount)
}

func applyRuleMatch(s Settings, item Item, prev Strike, r Rule, now time.Time, removalsCount *int) Decision {
	rs := getRuleSettings(s, r)
	count := 1
	if prev.Rule == r && prev.Count > 0 {
		count = prev.Count + 1
	}
	if count > rs.Threshold {
		count = rs.Threshold
	}
	next := Strike{
		Rule:         r,
		Count:        count,
		LastSizeLeft: item.SizeLeft,
		LastSeen:     now,
	}
	var remove, wouldRemove, deferred bool
	if count >= rs.Threshold {
		remove, wouldRemove, deferred = resolveThresholdAction(rs.Mode, s.MaxRemovalsPerSweep, removalsCount)
	}
	return Decision{
		ItemID:      item.ID,
		DownloadID:  item.DownloadID,
		Rule:        r,
		Next:        next,
		Strike:      true,
		Threshold:   rs.Threshold,
		Remove:      remove,
		WouldRemove: wouldRemove,
		Blocklist:   isBlocklistRule(r),
		Deferred:    deferred,
	}
}

func resolveThresholdAction(mode Mode, maxRemovals int, removalsCount *int) (bool, bool, bool) {
	if mode == ModeObserve {
		return false, true, false
	}
	if *removalsCount < maxRemovals {
		*removalsCount++
		return true, false, false
	}
	return false, false, true
}

func defaultRuleSettings(r Rule) RuleSettings {
	switch r {
	case RuleStalled, RuleSlow, RuleImportBlocked:
		return RuleSettings{Mode: ModeObserve, Threshold: 6}
	case RuleFailed:
		return RuleSettings{Mode: ModeObserve, Threshold: 2}
	case RuleUnwantedScore, RuleUnwantedUnmonitored:
		return RuleSettings{Mode: ModeObserve, Threshold: 1}
	default:
		return RuleSettings{Mode: ModeObserve, Threshold: 1}
	}
}

func getRuleSettings(s Settings, r Rule) RuleSettings {
	def := defaultRuleSettings(r)
	if s.Rules == nil {
		return def
	}
	rs, ok := s.Rules[r]
	if !ok {
		return def
	}
	if rs.Mode == "" {
		rs.Mode = def.Mode
	}
	if rs.Threshold <= 0 {
		rs.Threshold = def.Threshold
	}
	return rs
}

func isBlocklistRule(r Rule) bool {
	return r != RuleUnwantedUnmonitored
}

func isNeverTouched(item Item, grace time.Duration, now time.Time) bool {
	if isNeverTouchedStatus(item.Status) {
		return true
	}
	if isNeverTouchedState(item.TrackedDownloadState) {
		return true
	}
	if !item.Added.IsZero() && now.Sub(item.Added) < grace {
		return true
	}
	return false
}

func isNeverTouchedStatus(status string) bool {
	switch strings.ToLower(status) {
	case "queued", "paused", "delay", "downloadclientunavailable":
		return true
	default:
		return false
	}
}

func isNeverTouchedState(state string) bool {
	switch strings.ToLower(state) {
	case "importing", "imported", "ignored":
		return true
	default:
		return false
	}
}

func matchRule(s Settings, item Item, prev Strike, now time.Time) Rule {
	for _, rule := range RuleOrder {
		if ruleMatches(rule, s, item, prev, now) {
			return rule
		}
	}
	return ""
}

func ruleMatches(rule Rule, s Settings, item Item, prev Strike, now time.Time) bool {
	switch rule {
	case RuleStalled:
		return checkStalled(item, prev)
	case RuleSlow:
		return checkSlow(item, prev, s.MinBytesPerSecond, now)
	case RuleFailed:
		return checkFailed(item)
	case RuleImportBlocked:
		return checkImportBlocked(item)
	case RuleUnwantedScore:
		return checkUnwantedScore(item, s.MinCustomFormatScore)
	case RuleUnwantedUnmonitored:
		return checkUnwantedUnmonitored(item)
	default:
		return false
	}
}

func checkStalled(item Item, prev Strike) bool {
	if !strings.EqualFold(item.TrackedDownloadState, "downloading") {
		return false
	}
	if item.SizeLeft <= 0 || prev.LastSeen.IsZero() {
		return false
	}
	return item.SizeLeft >= prev.LastSizeLeft
}

func checkSlow(item Item, prev Strike, minBps int64, now time.Time) bool {
	if minBps <= 0 {
		return false
	}
	if !strings.EqualFold(item.TrackedDownloadState, "downloading") {
		return false
	}
	if prev.LastSeen.IsZero() || item.SizeLeft >= prev.LastSizeLeft {
		return false
	}
	elapsed := now.Sub(prev.LastSeen)
	if elapsed <= 0 {
		return false
	}
	dropped := prev.LastSizeLeft - item.SizeLeft
	rate := float64(dropped) / elapsed.Seconds()
	return rate < float64(minBps)
}

func checkFailed(item Item) bool {
	if strings.EqualFold(item.Status, "failed") {
		return true
	}
	switch strings.ToLower(item.TrackedDownloadState) {
	// Sonarr and Radarr name the states failed and failedPending; Lidarr
	// and the Readarr family name them downloadFailed and
	// downloadFailedPending, and importFailed marks a terminal import
	// failure that the app never retries.
	case "failed", "failedpending", "downloadfailed", "downloadfailedpending", "importfailed":
		return true
	default:
		return false
	}
}

func checkImportBlocked(item Item) bool {
	if isUnblockerCase(item) {
		return false
	}
	if strings.EqualFold(item.TrackedDownloadState, "importBlocked") {
		return true
	}
	return strings.EqualFold(item.TrackedDownloadState, "importPending") &&
		strings.EqualFold(item.TrackedDownloadStatus, "warning")
}

// idMatchNeedle marks a release the *arr matched by grab history but whose
// title it cannot parse. A manual import fixes it, which is what the
// cluster's import unblocker does, so the sweep leaves such items alone.
const idMatchNeedle = "automatic import is not possible"

// isUnblockerCase reports whether the ID-match note is the item's only
// problem: status warning, state importBlocked or importPending, and every
// status line is either the note or the item's own release title. It
// mirrors the allow-list of the import unblocker.
func isUnblockerCase(item Item) bool {
	if !strings.EqualFold(item.TrackedDownloadStatus, "warning") {
		return false
	}
	state := strings.ToLower(item.TrackedDownloadState)
	if state != "importblocked" && state != "importpending" {
		return false
	}
	title := strings.TrimSpace(item.Title)
	sawNeedle := false
	for _, line := range item.StatusLines {
		switch {
		case strings.Contains(strings.ToLower(line), idMatchNeedle):
			sawNeedle = true
		case strings.TrimSpace(line) == title:
		default:
			return false
		}
	}
	return sawNeedle
}

func checkUnwantedScore(item Item, minScore *int32) bool {
	if minScore == nil {
		return false
	}
	return item.CustomFormatScore < *minScore
}

func checkUnwantedUnmonitored(item Item) bool {
	return !item.Monitored
}
