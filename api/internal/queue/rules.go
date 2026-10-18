// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package queue implements the pure rule engine for SnatchArr queue management.
package queue

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

// Rule identifies a queue condition that can incur strikes.
type Rule string

// Queue rules in priority order.
const (
	RuleStalled             Rule = "stalled"
	RuleSlow                Rule = "slow"
	RuleFailed              Rule = "failed"
	RuleImportBlocked       Rule = "import_blocked"
	RuleUnwantedScore       Rule = "unwanted_score"
	RuleUnwantedUnmonitored Rule = "unwanted_unmonitored"
)

// RuleOrder defines the priority order for evaluating rules.
var RuleOrder = []Rule{
	RuleStalled,
	RuleSlow,
	RuleFailed,
	RuleImportBlocked,
	RuleUnwantedScore,
	RuleUnwantedUnmonitored,
}

// Mode defines how a rule reacts when its strike threshold is met.
type Mode string

// Evaluation modes.
const (
	ModeObserve Mode = "observe"
	ModeAct     Mode = "act"
)

// RuleSettings configures execution mode and strike threshold for one rule.
type RuleSettings struct {
	Mode      Mode
	Threshold int
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
	Monitored             bool
	// Title is the release title the *arr shows for the download.
	Title string
	// StatusLines flattens statusMessages: each message's title, then its messages.
	StatusLines []string
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
	// Deferred reports a removal held back by MaxRemovalsPerSweep; the next sweep retries.
	Deferred bool
}

// ErrInvalid indicates that queue settings violate bounds or constraints.
var ErrInvalid = errors.New("invalid queue settings")

// ErrQueueUnsupported indicates an *arr kind does not support queue operations.
var ErrQueueUnsupported = errors.New("queue management is not supported for this *arr kind")

// DefaultSettings returns safe queue defaults: disabled, observe mode for every
// rule, standard thresholds (6, 6, 2, 6, 1, 1), 15m grace, and 10 removals per sweep.
func DefaultSettings() Settings {
	rules := make(map[Rule]RuleSettings, len(RuleOrder))
	for _, r := range RuleOrder {
		rules[r] = defaultRuleSettings(r)
	}
	return Settings{
		Enabled:              false,
		Rules:                rules,
		Grace:                15 * time.Minute,
		MinBytesPerSecond:    0,
		MinCustomFormatScore: nil,
		MaxRemovalsPerSweep:  10,
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
	if s.MinBytesPerSecond < 0 {
		return fmt.Errorf("%w: min bytes per second must be non-negative, got %d", ErrInvalid, s.MinBytesPerSecond)
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
