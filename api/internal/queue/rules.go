// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package queue implements the pure rule engine for SnatchArr queue management.
package queue

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// Rule identifies a queue condition that can incur strikes.
type Rule string

// Queue rules in priority order.
const (
	RuleStalled             Rule = "stalled"
	RuleSlow                Rule = "slow"
	RuleFailed              Rule = "failed"
	RuleIDMatched           Rule = "id_matched"
	RuleImportFixable       Rule = "import_fixable"
	RuleImportBroken        Rule = "import_broken"
	RuleImportNotWanted     Rule = "import_not_wanted"
	RuleImportBlocked       Rule = "import_blocked"
	RuleUnwantedScore       Rule = "unwanted_score"
	RuleNotAnUpgrade        Rule = "not_an_upgrade"
	RuleDuplicate           Rule = "duplicate"
	RuleUnwantedUnmonitored Rule = "unwanted_unmonitored"
)

// RuleOrder defines the priority order for evaluating rules.
var RuleOrder = []Rule{
	RuleStalled,
	RuleSlow,
	RuleFailed,
	RuleIDMatched,
	RuleImportFixable,
	RuleImportBroken,
	RuleImportNotWanted,
	RuleImportBlocked,
	RuleUnwantedScore,
	RuleNotAnUpgrade,
	RuleDuplicate,
	RuleUnwantedUnmonitored,
}

// ImportMode defines candidate qualification criteria for a manual import.
type ImportMode string

// Import candidate qualification modes.
const (
	ImportModeClean   ImportMode = "clean"
	ImportModeFixable ImportMode = "fixable"
)

// Mode defines how a rule reacts when its strike threshold is met.
type Mode string

// Evaluation modes.
const (
	ModeObserve Mode = "observe"
	ModeAct     Mode = "act"
)

// RuleSettings configures execution mode, strike threshold and blocklisting for one rule.
type RuleSettings struct {
	Mode      Mode
	Threshold int
	// Blocklist asks a removal by this rule to blocklist the release; nil keeps the rule's default.
	Blocklist *bool
}

// Settings configures queue sweep evaluation.
type Settings struct {
	// Enabled switches queue management on for the instance; off, Sweep decides nothing.
	Enabled bool
	// Rules holds mode and threshold per rule; a missing rule uses its default.
	Rules map[Rule]RuleSettings
	// Grace protects items added more recently than this.
	Grace time.Duration
	// MinBytesPerSecond is the slow rule's floor; 0 switches the rule off.
	MinBytesPerSecond int64
	// MinCustomFormatScore is the unwanted score floor; nil switches the rule off.
	MinCustomFormatScore *int32
	// MaxRemovalsPerSweep bounds the removals of one sweep; later hits wait for the next.
	MaxRemovalsPerSweep int
	// MaxImportsPerSweep bounds the force imports of one sweep; later hits wait for the next.
	MaxImportsPerSweep int
	// ForceImport is set by the service for kinds whose manual-import candidates carry
	// the grab-history mapping (Sonarr, Radarr); without it id_matched never matches.
	ForceImport bool
	// MatchFixableMinPercent is the album/book match percentage floor for manual import (default 75).
	MatchFixableMinPercent float64
	// MatchBrokenBelowPercent is the album/book match fallback threshold (default 40, used by rematch fallback).
	MatchBrokenBelowPercent float64
	// TrackFixableMinPercent is the track match percentage floor for manual import (default 50).
	TrackFixableMinPercent float64
	// TrackBrokenBelowPercent is the track match fallback threshold (default 40, used by rematch fallback).
	TrackBrokenBelowPercent float64
}

// Item represents a download queue record from an upstream *arr instance.
type Item struct {
	ID                    int32
	DownloadID            string
	Added                 time.Time
	Status                string
	TrackedDownloadStatus string
	TrackedDownloadState  string
	SizeLeft              int64
	CustomFormatScore     int32
	// QualityRank is the relative quality rank from quality profiles; higher = better, 0 = unknown.
	QualityRank int
	// ExistingScore is the custom-format score of the file already on disk;
	// nil when there is no file or its score is unknown.
	ExistingScore *int32
	// MediaID is the episode, movie, album or book the download is for;
	// 0 when unknown. Items of one instance sharing it are duplicates.
	MediaID   int32
	Monitored bool
	// Title is the release title the *arr shows for the download.
	Title string
	// StatusLines flattens statusMessages: each message's title, then its messages.
	StatusLines []string

	// duplicate is set by Sweep when another download for the same media
	// item scores higher.
	duplicate bool
}

// Strike records the persistent state of one queue item across sweeps.
type Strike struct {
	// Rule is the rule the item broke last; empty after a clean sweep.
	Rule Rule
	// Count is the number of consecutive strikes for Rule, capped at its threshold.
	Count int
	// LastSizeLeft is the remaining size at LastSeen, the baseline of stalled and slow.
	LastSizeLeft int64
	// LastSeen is the time of the sweep that stored this state; zero means never seen.
	LastSeen time.Time
	// ImportedAt is the time of the last force import of the item; zero means none.
	ImportedAt time.Time
	// ImportRefused marks an import at ImportedAt that found no clean, mapped candidate.
	ImportRefused bool
}

// Decision represents the evaluation outcome for one queue item in a sweep.
type Decision struct {
	// ItemID is the *arr queue id, the id a removal deletes.
	ItemID int32
	// DownloadID is the download client's id, the key of the stored state.
	DownloadID string
	// Rule is the rule broken in this sweep; empty when none was.
	Rule Rule
	// Next is the state to store for the item. A clean sweep stores Count 0 and keeps the
	// size baseline, so the stalled rule can strike on the following sweep.
	Next Strike
	// Strike reports that this sweep added a strike.
	Strike bool
	// Threshold is the strike count at which Rule removes the item.
	Threshold int
	// Remove asks the caller to delete the item: act mode, threshold reached, within bound.
	Remove bool
	// WouldRemove reports a threshold reached in observe mode; nothing is deleted.
	WouldRemove bool
	// Blocklist asks the removal to blocklist the release.
	Blocklist bool
	// Deferred reports a removal or import held back by its per-sweep bound; the next sweep retries.
	Deferred bool
	// Import asks the caller to force-import the item: id_matched or import_fixable, act mode, within bound.
	Import bool
	// WouldImport reports an import threshold reached in observe mode; nothing is imported.
	WouldImport bool
	// ImportFailed reports that the item is still ID-matched one sweep after its import.
	ImportFailed bool
	// NewStrike reports the first sweep in which Rule strikes the item.
	NewStrike bool
	// Reached reports the first sweep in which the strike count reaches Threshold.
	Reached bool
	// Title is the item's release title, logged for the id_matched parity check.
	Title string
	// Class is the categorized import failure class (fixable, not_wanted, broken, unknown).
	Class Class
	// Pattern is the matching pattern string or subreason; empty for unknown.
	Pattern string
	// Unknown reports that the item carries an unknown or operational failure reason.
	Unknown bool
	// Research reports that the removed release should be re-downloaded/re-searched.
	Research bool
	// MatchPercent is the parsed percentage for distance messages (worst of album/track lines).
	MatchPercent *float64
}

// ErrInvalid indicates that queue settings violate bounds or constraints.
var ErrInvalid = errors.New("invalid queue settings")

// ErrQueueUnsupported indicates an *arr kind does not support queue operations.
var ErrQueueUnsupported = errors.New("queue management is not supported for this *arr kind")

// ErrNoCleanCandidate indicates a force import found no candidate without a
// rejection and with a mapping; nothing was imported.
var ErrNoCleanCandidate = errors.New("no clean, mapped manual-import candidate")

// NoCleanCandidateError indicates a force import found no candidate without a
// rejection and with a mapping, carrying distinct rejection reasons.
type NoCleanCandidateError struct {
	Reasons []string
}

func (e *NoCleanCandidateError) Error() string {
	if len(e.Reasons) == 0 {
		return ErrNoCleanCandidate.Error()
	}
	return fmt.Sprintf("%s: %s", ErrNoCleanCandidate.Error(), strings.Join(e.Reasons, "; "))
}

// Unwrap lets errors.Is match ErrNoCleanCandidate.
func (e *NoCleanCandidateError) Unwrap() error {
	return ErrNoCleanCandidate
}

// SupportsForceImport reports the kinds whose manual-import candidates carry the
// grab-history mapping rule id_matched needs (ADR-0013).
func SupportsForceImport(kind domain.AppKind) bool {
	return kind == domain.KindSonarr || kind == domain.KindRadarr
}

// Default match percentage floors.
const (
	DefaultMatchFixableMinPercent  = 75.0
	DefaultMatchBrokenBelowPercent = 40.0
	DefaultTrackFixableMinPercent  = 50.0
	DefaultTrackBrokenBelowPercent = 40.0
)

// DefaultSettings returns safe queue defaults: disabled, observe mode for every
// rule, standard thresholds, 15m grace, 10 removals and 25 imports per sweep,
// and default distance match floors.
func DefaultSettings() Settings {
	rules := make(map[Rule]RuleSettings, len(RuleOrder))
	for _, r := range RuleOrder {
		rules[r] = defaultRuleSettings(r)
	}
	return Settings{
		Enabled:                 false,
		Rules:                   rules,
		Grace:                   15 * time.Minute,
		MinBytesPerSecond:       0,
		MinCustomFormatScore:    nil,
		MaxRemovalsPerSweep:     10,
		MaxImportsPerSweep:      25,
		MatchFixableMinPercent:  DefaultMatchFixableMinPercent,
		MatchBrokenBelowPercent: DefaultMatchBrokenBelowPercent,
		TrackFixableMinPercent:  DefaultTrackFixableMinPercent,
		TrackBrokenBelowPercent: DefaultTrackBrokenBelowPercent,
	}
}

// Validate checks that Settings values satisfy declared bounds and invariants.
func (s Settings) Validate() error {
	if err := validateBounds(s); err != nil {
		return err
	}
	return validateRules(s.Rules)
}

func validateBounds(s Settings) error {
	if s.Grace < 0 || s.Grace > 24*time.Hour {
		return fmt.Errorf("%w: grace must be between 0 and 24h, got %v", ErrInvalid, s.Grace)
	}
	if s.MaxRemovalsPerSweep < 1 || s.MaxRemovalsPerSweep > 100 {
		return fmt.Errorf("%w: max removals per sweep must be between 1 and 100, got %d", ErrInvalid, s.MaxRemovalsPerSweep)
	}
	if s.MaxImportsPerSweep < 1 || s.MaxImportsPerSweep > 100 {
		return fmt.Errorf("%w: max imports per sweep must be between 1 and 100, got %d", ErrInvalid, s.MaxImportsPerSweep)
	}
	if s.MinBytesPerSecond < 0 {
		return fmt.Errorf("%w: min bytes per second must be non-negative, got %d", ErrInvalid, s.MinBytesPerSecond)
	}
	return validateMatchFloors(s)
}

func validateMatchFloors(s Settings) error {
	if err := validateFloorRanges(s); err != nil {
		return err
	}
	if s.MatchFixableMinPercent <= s.MatchBrokenBelowPercent {
		return fmt.Errorf("%w: match fixable min percent (%v) must be greater than match broken below percent (%v)", ErrInvalid, s.MatchFixableMinPercent, s.MatchBrokenBelowPercent)
	}
	if s.TrackFixableMinPercent <= s.TrackBrokenBelowPercent {
		return fmt.Errorf("%w: track fixable min percent (%v) must be greater than track broken below percent (%v)", ErrInvalid, s.TrackFixableMinPercent, s.TrackBrokenBelowPercent)
	}
	return nil
}

func validateFloorRanges(s Settings) error {
	floors := []struct {
		name string
		val  float64
	}{
		{"match fixable min percent", s.MatchFixableMinPercent},
		{"match broken below percent", s.MatchBrokenBelowPercent},
		{"track fixable min percent", s.TrackFixableMinPercent},
		{"track broken below percent", s.TrackBrokenBelowPercent},
	}
	for _, f := range floors {
		if f.val < 0 || f.val > 100 {
			return fmt.Errorf("%w: %s must be between 0 and 100, got %v", ErrInvalid, f.name, f.val)
		}
	}
	return nil
}

func validateRules(rules map[Rule]RuleSettings) error {
	for r, rs := range rules {
		if err := validateRuleEntry(r, rs); err != nil {
			return err
		}
	}
	return nil
}

func validateRuleEntry(r Rule, rs RuleSettings) error {
	if !slices.Contains(RuleOrder, r) {
		return fmt.Errorf("%w: unknown rule %q", ErrInvalid, r)
	}
	if rs.Threshold < 1 || rs.Threshold > 100 {
		return fmt.Errorf("%w: rule %q threshold must be between 1 and 100, got %d", ErrInvalid, r, rs.Threshold)
	}
	if rs.Mode != "" && rs.Mode != ModeObserve && rs.Mode != ModeAct {
		return fmt.Errorf("%w: rule %q mode must be observe or act, got %q", ErrInvalid, r, rs.Mode)
	}
	return nil
}
