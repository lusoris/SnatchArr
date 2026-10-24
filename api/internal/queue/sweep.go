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
	var spent budget
	losers := duplicateLosers(items)
	for _, item := range items {
		item.duplicate = losers[itemKey(item)]
		key := itemKey(item)
		var prevStrike Strike
		if prev != nil {
			prevStrike = prev[key]
		}
		dec := evaluateItem(s, item, prevStrike, now, &spent)
		decisions = append(decisions, dec)
	}
	return decisions
}

// download is one queue item within a media group. A season pack lists one
// item per episode under a single download id; items of one download never
// strike each other, so the pack counts once.
type download struct {
	key         string
	score       int32
	qualityRank int
	added       time.Time
	id          int32
}

// duplicateLosers groups items by MediaID and returns the keys of every
// download that is not the best of its group: highest score wins, then higher
// quality rank, then older grab (earlier Added), then lowest queue id breaks
// a tie. A download that loses in any group loses.
func duplicateLosers(items []Item) map[string]bool {
	groups := make(map[int32][]download, len(items))
	for _, item := range items {
		if item.MediaID == 0 {
			continue
		}
		groups[item.MediaID] = append(groups[item.MediaID], download{
			key:         itemKey(item),
			score:       item.CustomFormatScore,
			qualityRank: item.QualityRank,
			added:       item.Added,
			id:          item.ID,
		})
	}
	losers := make(map[string]bool)
	for _, group := range groups {
		best := group[0]
		for _, d := range group[1:] {
			if beats(d, best) {
				best = d
			}
		}
		for _, d := range group {
			if d.key != best.key {
				losers[d.key] = true
			}
		}
	}
	return losers
}

func beats(a, b download) bool {
	if a.score != b.score {
		return a.score > b.score
	}
	if a.qualityRank != b.qualityRank {
		return a.qualityRank > b.qualityRank
	}
	if !a.added.Equal(b.added) {
		if a.added.IsZero() {
			return false
		}
		if b.added.IsZero() {
			return true
		}
		return a.added.Before(b.added)
	}
	return a.id < b.id
}

// budget counts the removals and imports one sweep has spent. importKeys holds
// the downloads that already got an id_matched action, so a season pack, one
// queue item per episode under one download id, is imported and reported once.
type budget struct {
	removals   int
	imports    int
	importKeys map[string]bool
}

func itemKey(item Item) string {
	if item.DownloadID != "" {
		return item.DownloadID
	}
	return strconv.FormatInt(int64(item.ID), 10)
}

func evaluateItem(s Settings, item Item, prev Strike, now time.Time, spent *budget) Decision {
	if isNeverTouched(item, s.Grace, now) {
		return Decision{
			ItemID:     item.ID,
			DownloadID: item.DownloadID,
			Rule:       "",
			Next:       prev,
		}
	}
	rule, class, pattern, pct := matchRule(s, item, prev, now)
	if rule == "" {
		return Decision{
			ItemID:     item.ID,
			DownloadID: item.DownloadID,
			Rule:       "",
			Next: Strike{
				Rule:          "",
				Count:         0,
				LastSizeLeft:  item.SizeLeft,
				LastSeen:      now,
				ImportedAt:    prev.ImportedAt,
				ImportRefused: prev.ImportRefused,
			},
		}
	}
	d := applyRuleMatch(s, item, prev, rule, now, spent)
	d.Class = class
	d.Pattern = pattern
	d.MatchPercent = pct
	return d
}

func nextStrikeCount(prev Strike, r Rule, threshold int) int {
	if prev.Rule == r && prev.Count > 0 {
		if prev.Count+1 > threshold {
			return threshold
		}
		return prev.Count + 1
	}
	return 1
}

func applyRuleMatch(s Settings, item Item, prev Strike, r Rule, now time.Time, spent *budget) Decision {
	rs := ResolveRule(s, r)
	count := nextStrikeCount(prev, r, rs.Threshold)
	next := Strike{
		Rule:          r,
		Count:         count,
		LastSizeLeft:  item.SizeLeft,
		LastSeen:      now,
		ImportedAt:    prev.ImportedAt,
		ImportRefused: prev.ImportRefused,
	}
	thresholdMet := isThresholdMet(r, item, rs, next, now)
	var d Decision
	if r == RuleIDMatched || r == RuleImportFixable {
		d = applyImportMatch(s, item, prev, rs, next, now, spent)
	} else {
		d = applyRemovalMatch(s, item, rs, next, thresholdMet, spent)
	}
	if r == RuleFailed || r == RuleImportBroken {
		d.Research = true
	}
	d.NewStrike = prev.Rule != r || prev.Count == 0
	d.Reached = isThresholdReached(r, item, prev, rs, thresholdMet)
	d.Title = item.Title
	return d
}

func itemAge(item Item, now time.Time) time.Duration {
	if item.Added.IsZero() {
		return 0
	}
	return now.Sub(item.Added)
}

func isThresholdMet(r Rule, item Item, rs RuleSettings, next Strike, now time.Time) bool {
	if r == RuleMetadataStuck {
		if item.Added.IsZero() {
			return false
		}
		return itemAge(item, now) >= time.Duration(rs.Threshold)*time.Minute
	}
	return next.Count >= rs.Threshold
}

func isThresholdReached(r Rule, item Item, prev Strike, rs RuleSettings, thresholdMet bool) bool {
	if !thresholdMet {
		return false
	}
	if r == RuleMetadataStuck {
		if prev.Rule != r || prev.LastSeen.IsZero() {
			return true
		}
		prevAge := itemAge(item, prev.LastSeen)
		return prevAge < time.Duration(rs.Threshold)*time.Minute
	}
	return prev.Rule != r || prev.Count < rs.Threshold
}

func applyRemovalMatch(s Settings, item Item, rs RuleSettings, next Strike, thresholdMet bool, spent *budget) Decision {
	var remove, wouldRemove, deferred bool
	if next.Rule == RuleImportBlocked {
		return Decision{
			ItemID:      item.ID,
			DownloadID:  item.DownloadID,
			Rule:        next.Rule,
			Next:        next,
			Strike:      true,
			Threshold:   rs.Threshold,
			Remove:      false,
			WouldRemove: false,
			Blocklist:   *rs.Blocklist,
			Deferred:    false,
			Unknown:     true,
		}
	}
	if thresholdMet {
		remove, wouldRemove, deferred = resolveThresholdAction(rs.Mode, s.MaxRemovalsPerSweep, &spent.removals)
	}
	return Decision{
		ItemID:      item.ID,
		DownloadID:  item.DownloadID,
		Rule:        next.Rule,
		Next:        next,
		Strike:      true,
		Threshold:   rs.Threshold,
		Remove:      remove,
		WouldRemove: wouldRemove,
		Blocklist:   *rs.Blocklist,
		Deferred:    deferred,
	}
}

// applyImportMatch decides the id_matched action. An item imported less than
// strikeTTL ago, or refused less than RefusedRetry ago, is not tried again;
// the first sweep after an import reports the item as a failed import, because
// a cleared item leaves the queue.
func applyImportMatch(s Settings, item Item, prev Strike, rs RuleSettings, next Strike, now time.Time, spent *budget) Decision {
	d := Decision{
		ItemID:     item.ID,
		DownloadID: item.DownloadID,
		Rule:       next.Rule,
		Next:       next,
		Strike:     true,
		Threshold:  rs.Threshold,
	}
	key := itemKey(item)
	if spent.importKeys[key] {
		return d
	}
	cooldown := strikeTTL
	if prev.ImportRefused {
		cooldown = RefusedRetry
	}
	if !prev.ImportedAt.IsZero() && now.Sub(prev.ImportedAt) < cooldown {
		d.ImportFailed = !prev.ImportRefused && prev.LastSeen.Equal(prev.ImportedAt)
		markImportKey(spent, key, d.ImportFailed)
		return d
	}
	if next.Count < rs.Threshold {
		return d
	}
	d.WouldImport, d.Import, d.Deferred = resolveImportAction(rs.Mode, s.MaxImportsPerSweep, spent)
	markImportKey(spent, key, true)
	return d
}

func markImportKey(spent *budget, key string, acted bool) {
	if !acted {
		return
	}
	if spent.importKeys == nil {
		spent.importKeys = make(map[string]bool)
	}
	spent.importKeys[key] = true
}

// resolveImportAction returns wouldImport, doImport and deferred for an id_matched
// item at its threshold.
func resolveImportAction(mode Mode, maxImports int, spent *budget) (bool, bool, bool) {
	if mode == ModeObserve {
		return true, false, false
	}
	if spent.imports < maxImports {
		spent.imports++
		return false, true, false
	}
	return false, false, true
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
	blocklist := isBlocklistRule(r)
	switch r {
	case RuleMetadataStuck:
		// Default 180 min (measured: every successful torrent import over 30 days finished <= 140.2 min after grab; see comment).
		return RuleSettings{Mode: ModeObserve, Threshold: 180, Blocklist: &blocklist}
	case RuleStalled, RuleSlow, RuleImportBlocked:
		return RuleSettings{Mode: ModeObserve, Threshold: 6, Blocklist: &blocklist}
	case RuleFailed, RuleImportBroken:
		return RuleSettings{Mode: ModeObserve, Threshold: 2, Blocklist: &blocklist}
	case RuleIDMatched, RuleImportFixable, RuleImportNotWanted, RuleUnwantedScore, RuleNotAnUpgrade, RuleDuplicate, RuleUnwantedUnmonitored:
		return RuleSettings{Mode: ModeObserve, Threshold: 1, Blocklist: &blocklist}
	default:
		return RuleSettings{Mode: ModeObserve, Threshold: 1, Blocklist: &blocklist}
	}
}

// ResolveRule returns the effective settings for r: stored values, with the rule's
// default for every unset field.
func ResolveRule(s Settings, r Rule) RuleSettings {
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
	if rs.Blocklist == nil {
		rs.Blocklist = def.Blocklist
	}
	return rs
}

// isBlocklistRule reports the blocklist default: a release that is no
// upgrade, a duplicate or belongs to an unmonitored item is not defective,
// so those rules remove it without a blocklist entry; id_matched and
// import_fixable import and never remove.
func isBlocklistRule(r Rule) bool {
	return r != RuleNotAnUpgrade && r != RuleDuplicate && r != RuleUnwantedUnmonitored && r != RuleIDMatched && r != RuleImportFixable
}

func isNeverTouched(item Item, grace time.Duration, now time.Time) bool {
	if MatchMetadataStuck(item) {
		return false
	}
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

type itemClassifier struct {
	item        Item
	s           Settings
	now         time.Time
	classified  bool
	itemClass   Class
	itemPattern string
	itemPct     *float64
}

func (c *itemClassifier) ensure() {
	if !c.classified {
		c.itemClass, c.itemPattern, c.itemPct = ClassifyImportItem(c.item, c.s, c.now)
		c.classified = true
	}
}

func (c *itemClassifier) getClass() Class {
	c.ensure()
	return c.itemClass
}

func matchRule(s Settings, item Item, prev Strike, now time.Time) (Rule, Class, string, *float64) {
	c := &itemClassifier{item: item, s: s, now: now}
	for _, rule := range RuleOrder {
		if checkRuleMatch(rule, s, item, prev, c) {
			if c.classified {
				return rule, c.itemClass, c.itemPattern, c.itemPct
			}
			return rule, "", "", nil
		}
	}
	return "", "", "", nil
}

func checkRuleMatch(rule Rule, s Settings, item Item, prev Strike, c *itemClassifier) bool {
	switch rule {
	case RuleStalled, RuleMetadataStuck, RuleSlow, RuleFailed:
		return checkDownloadHealthRule(rule, s, item, prev, c.now)
	case RuleIDMatched, RuleImportFixable, RuleImportBroken, RuleImportNotWanted,
		RuleImportBlocked, RuleUnwantedScore, RuleNotAnUpgrade, RuleDuplicate,
		RuleUnwantedUnmonitored:
		return checkPolicyRule(rule, s, item, c)
	default:
		return false
	}
}

func checkDownloadHealthRule(rule Rule, s Settings, item Item, prev Strike, now time.Time) bool {
	//exhaustive:ignore
	switch rule {
	case RuleStalled:
		return checkStalled(item, prev)
	case RuleMetadataStuck:
		return MatchMetadataStuck(item)
	case RuleSlow:
		return checkSlow(item, prev, s.MinBytesPerSecond, now)
	case RuleFailed:
		return checkFailed(item)
	default:
		return false
	}
}

func checkPolicyRule(rule Rule, s Settings, item Item, c *itemClassifier) bool {
	//exhaustive:ignore
	switch rule {
	case RuleIDMatched:
		return checkIDMatchedWithClass(s, item, c)
	case RuleImportFixable, RuleImportBroken, RuleImportNotWanted, RuleImportBlocked:
		return checkImportRule(rule, item, c)
	case RuleUnwantedScore:
		return checkUnwantedScore(item, s.MinCustomFormatScore)
	case RuleNotAnUpgrade:
		return checkNotAnUpgrade(item)
	case RuleDuplicate:
		return item.duplicate
	case RuleUnwantedUnmonitored:
		return checkUnwantedUnmonitored(item)
	default:
		return false
	}
}

func checkIDMatchedWithClass(s Settings, item Item, c *itemClassifier) bool {
	matched := checkIDMatched(s, item)
	if matched && isImportProblem(item) {
		c.ensure()
	}
	return matched
}

func checkImportRule(rule Rule, item Item, c *itemClassifier) bool {
	if !isImportProblem(item) {
		return false
	}
	switch rule {
	case RuleImportFixable:
		if isIDMatchBlock(item) {
			return false
		}
		return c.getClass() == ClassFixable
	case RuleImportBroken:
		return c.getClass() == ClassBroken
	case RuleImportNotWanted:
		return c.getClass() == ClassNotWanted
	case RuleImportBlocked:
		if isIDMatchBlock(item) {
			return false
		}
		return c.getClass() == ClassUnknown
	case RuleStalled, RuleMetadataStuck, RuleSlow, RuleFailed, RuleIDMatched, RuleUnwantedScore, RuleNotAnUpgrade, RuleDuplicate, RuleUnwantedUnmonitored:
		return false
	default:
		return false
	}
}

// checkStalled strikes torrents only: usenet clients queue jobs serially and report
// waiting jobs as downloading, and their own failure handling covers dead jobs.
func checkStalled(item Item, prev Strike) bool {
	if !isTorrentProtocol(item.Protocol) {
		return false
	}
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
	// downloadFailedPending.
	case "failed", "failedpending", "downloadfailed", "downloadfailedpending":
		return true
	default:
		return false
	}
}

func isImportProblem(item Item) bool {
	state := strings.ToLower(item.TrackedDownloadState)
	if state == "importblocked" || state == "importfailed" {
		return true
	}
	return state == "importpending" && strings.EqualFold(item.TrackedDownloadStatus, "warning")
}

// idMatchNeedles mark a release the *arr matched by grab history but whose
// title it cannot parse, in the wording of Sonarr and Radarr
// (CompletedDownloadService, checked against both releases by
// make arr-forceimport-live). A manual import fixes it (rule id_matched,
// ADR-0013), so import_blocked never removes such an item.
var idMatchNeedles = []string{
	"release was matched to series by id",
	"release was matched to movie by id",
}

func isIDMatchNote(line string) bool {
	line = strings.ToLower(line)
	for _, needle := range idMatchNeedles {
		if strings.Contains(line, needle) {
			return true
		}
	}
	return false
}

// isIDMatchBlock reports whether the ID-match note is the item's only
// problem: status warning, state importBlocked or importPending, and every
// status line is either the note or the item's own release title. It keeps
// the allow-list of the cluster's import unblocker, which id_matched
// replaces (ADR-0013).
// checkIDMatched is rule id_matched: force import on, a download to import, and the
// ID-match note as the only problem.
func checkIDMatched(s Settings, item Item) bool {
	return s.ForceImport && item.DownloadID != "" && isIDMatchBlock(item)
}

func isIDMatchBlock(item Item) bool {
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
		case isIDMatchNote(line):
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

// checkNotAnUpgrade matches a download whose score does not beat the file
// on disk; with no file, or an unknown file score, it never matches.
func checkNotAnUpgrade(item Item) bool {
	if item.ExistingScore == nil {
		return false
	}
	return item.CustomFormatScore <= *item.ExistingScore
}

func checkUnwantedUnmonitored(item Item) bool {
	return !item.Monitored
}

// MatchMetadataStuck reports whether item is a torrent stuck fetching metadata.
// Signals (verified in Sonarr source QBittorrent.cs):
// DHT on -> status "queued" with message "qBittorrent is downloading metadata"
// DHT off -> status "warning" with message "qBittorrent cannot resolve magnet link with DHT disabled"
// Message text may be localized: also match protocol torrent + size 0 (unknown total) + status queued/warning.
// The size-0 fallback also matches a torrent queued by the client's active-torrent limit before
// metadata; age threshold (default 180 min) and observe default bound the effect.
func MatchMetadataStuck(item Item) bool {
	if !isTorrentProtocol(item.Protocol) {
		return false
	}
	isQueued, isWarning := isMetadataStuckStatus(item)
	if hasMetadataMessage(item, isQueued, isWarning) {
		return true
	}
	return isLocalizedMetadataStuck(item, isQueued, isWarning)
}

// isTorrentProtocol reports whether protocol is torrent; empty/unknown protocol is treated as torrent.
func isTorrentProtocol(protocol string) bool {
	return protocol == "" || strings.EqualFold(protocol, "torrent")
}

func isMetadataStuckStatus(item Item) (isQueued bool, isWarning bool) {
	status := strings.ToLower(item.Status)
	isQueued = status == "queued"
	isWarning = status == "warning" || strings.EqualFold(item.TrackedDownloadStatus, "warning")
	return isQueued, isWarning
}

func hasMetadataMessage(item Item, isQueued, isWarning bool) bool {
	if isQueued && hasStatusLine(item.StatusLines, "downloading metadata") {
		return true
	}
	return isWarning && hasStatusLine(item.StatusLines, "cannot resolve magnet link")
}

func isLocalizedMetadataStuck(item Item, isQueued, isWarning bool) bool {
	if !strings.EqualFold(item.Protocol, "torrent") || item.Size != 0 {
		return false
	}
	return isQueued || isWarning
}

func hasStatusLine(lines []string, substr string) bool {
	sub := strings.ToLower(substr)
	for _, line := range lines {
		if strings.Contains(strings.ToLower(line), sub) {
			return true
		}
	}
	return false
}
