// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package policyoas converts snatch and queue policies between domain models and OpenAPI types.
package policyoas

import (
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/queue"
)

// PolicyToOAS converts a domain.Policy into an OpenAPI SnatchPolicy.
func PolicyToOAS(p domain.Policy) *oas.SnatchPolicy {
	return &oas.SnatchPolicy{
		MissingPerCycle:        p.MissingPerCycle,
		UpgradePerCycle:        p.UpgradePerCycle,
		CycleIntervalS:         int(p.CycleInterval / time.Second),
		HourlyCap:              p.HourlyCap,
		Selection:              oas.SnatchPolicySelection(p.Selection),
		MonitoredOnly:          p.MonitoredOnly,
		SkipFutureReleases:     p.SkipFutureReleases,
		RadarrReleaseType:      oas.SnatchPolicyRadarrReleaseType(p.RadarrReleaseType),
		SonarrMissingMode:      oas.SnatchPolicySonarrMissingMode(p.SonarrMissingMode),
		SonarrUpgradeMode:      oas.SnatchPolicySonarrUpgradeMode(p.SonarrUpgradeMode),
		LidarrMissingMode:      oas.SnatchPolicyLidarrMissingMode(p.LidarrMissingMode),
		ProcessedTTLH:          int(p.ProcessedTTL / time.Hour),
		MaxQueueSize:           p.MaxQueueSize,
		AwaitCommand:           p.AwaitCommand,
		PageSize:               p.PageSize,
		RecentGrabWindowH:      int(p.RecentGrabWindow / time.Hour),
		AfterglowMaxH:          int(p.AfterglowMax / time.Hour),
		IncludeTags:            domain.FilterIDs(p.IncludeTags),
		ExcludeTags:            domain.FilterIDs(p.ExcludeTags),
		IncludeQualityProfiles: domain.FilterIDs(p.IncludeQualityProfiles),
		ExcludeQualityProfiles: domain.FilterIDs(p.ExcludeQualityProfiles),
		ReleaseDelayH:          oas.NewOptInt(int(p.ReleaseDelay / time.Hour)),
		DryRun:                 oas.NewOptBool(p.DryRun),
		SkipUnavailableMovies:  oas.NewOptBool(p.SkipUnavailableMovies),
		UpdatedAt:              oas.NewOptDateTime(p.UpdatedAt),
	}
}

// PolicyFromOAS converts an OpenAPI SnatchPolicy into a domain.Policy.
func PolicyFromOAS(instanceID uuid.UUID, in *oas.SnatchPolicy) domain.Policy {
	return domain.Policy{
		InstanceID:             instanceID,
		MissingPerCycle:        in.MissingPerCycle,
		UpgradePerCycle:        in.UpgradePerCycle,
		CycleInterval:          time.Duration(in.CycleIntervalS) * time.Second,
		HourlyCap:              in.HourlyCap,
		Selection:              domain.Selection(in.Selection),
		MonitoredOnly:          in.MonitoredOnly,
		SkipFutureReleases:     in.SkipFutureReleases,
		RadarrReleaseType:      string(in.RadarrReleaseType),
		SonarrMissingMode:      string(in.SonarrMissingMode),
		SonarrUpgradeMode:      string(in.SonarrUpgradeMode),
		LidarrMissingMode:      string(in.LidarrMissingMode),
		ProcessedTTL:           time.Duration(in.ProcessedTTLH) * time.Hour,
		MaxQueueSize:           in.MaxQueueSize,
		AwaitCommand:           in.AwaitCommand,
		PageSize:               in.PageSize,
		RecentGrabWindow:       time.Duration(in.RecentGrabWindowH) * time.Hour,
		AfterglowMax:           time.Duration(in.AfterglowMaxH) * time.Hour,
		IncludeTags:            domain.FilterIDs(in.IncludeTags),
		ExcludeTags:            domain.FilterIDs(in.ExcludeTags),
		IncludeQualityProfiles: domain.FilterIDs(in.IncludeQualityProfiles),
		ExcludeQualityProfiles: domain.FilterIDs(in.ExcludeQualityProfiles),
		ReleaseDelay:           time.Duration(in.ReleaseDelayH.Or(0)) * time.Hour,
		DryRun:                 in.DryRun.Or(false),
		SkipUnavailableMovies:  in.SkipUnavailableMovies.Or(true),
	}
}

// QueuePolicyToOAS converts queue.Settings into an OpenAPI QueuePolicy.
func QueuePolicyToOAS(s queue.Settings) *oas.QueuePolicy {
	var minCFS oas.OptInt32
	if s.MinCustomFormatScore != nil {
		minCFS.SetTo(*s.MinCustomFormatScore)
	}
	rules := oas.QueueRules{
		Stalled:             QueueRuleToOAS(queue.ResolveRule(s, queue.RuleStalled)),
		Slow:                QueueRuleToOAS(queue.ResolveRule(s, queue.RuleSlow)),
		Failed:              QueueRuleToOAS(queue.ResolveRule(s, queue.RuleFailed)),
		ImportBlocked:       QueueRuleToOAS(queue.ResolveRule(s, queue.RuleImportBlocked)),
		UnwantedScore:       QueueRuleToOAS(queue.ResolveRule(s, queue.RuleUnwantedScore)),
		UnwantedUnmonitored: QueueRuleToOAS(queue.ResolveRule(s, queue.RuleUnwantedUnmonitored)),
	}
	rules.NotAnUpgrade.SetTo(QueueRuleToOAS(queue.ResolveRule(s, queue.RuleNotAnUpgrade)))
	rules.Duplicate.SetTo(QueueRuleToOAS(queue.ResolveRule(s, queue.RuleDuplicate)))
	rules.IDMatched.SetTo(QueueRuleToOAS(queue.ResolveRule(s, queue.RuleIDMatched)))
	rules.ImportFixable.SetTo(QueueRuleToOAS(queue.ResolveRule(s, queue.RuleImportFixable)))
	rules.ImportBroken.SetTo(QueueRuleToOAS(queue.ResolveRule(s, queue.RuleImportBroken)))
	rules.ImportNotWanted.SetTo(QueueRuleToOAS(queue.ResolveRule(s, queue.RuleImportNotWanted)))
	return &oas.QueuePolicy{
		Enabled:                 s.Enabled,
		Rules:                   rules,
		GraceSeconds:            int(s.Grace / time.Second), // #nosec G115 -- validated 0..86400
		MinBytesPerSecond:       s.MinBytesPerSecond,
		MinCustomFormatScore:    minCFS,
		MaxRemovalsPerSweep:     s.MaxRemovalsPerSweep,
		MaxImportsPerSweep:      oas.NewOptInt(s.MaxImportsPerSweep),
		MatchFixableMinPercent:  oas.NewOptFloat64(s.MatchFixableMinPercent),
		MatchBrokenBelowPercent: oas.NewOptFloat64(s.MatchBrokenBelowPercent),
		TrackFixableMinPercent:  oas.NewOptFloat64(s.TrackFixableMinPercent),
		TrackBrokenBelowPercent: oas.NewOptFloat64(s.TrackBrokenBelowPercent),
	}
}

// QueueRuleToOAS converts queue.RuleSettings into OpenAPI QueueRuleSettings.
func QueueRuleToOAS(rs queue.RuleSettings) oas.QueueRuleSettings {
	out := oas.QueueRuleSettings{
		Mode:      oas.QueueRuleMode(rs.Mode),
		Threshold: rs.Threshold,
	}
	if rs.Blocklist != nil {
		out.Blocklist.SetTo(*rs.Blocklist)
	}
	return out
}

// QueuePolicyFromOAS converts an OpenAPI QueuePolicy into queue.Settings.
func QueuePolicyFromOAS(req *oas.QueuePolicy) queue.Settings {
	var minCFS *int32
	if val, ok := req.MinCustomFormatScore.Get(); ok {
		v := val
		minCFS = &v
	}
	rules := map[queue.Rule]queue.RuleSettings{
		queue.RuleStalled:             QueueRuleFromOAS(req.Rules.Stalled),
		queue.RuleSlow:                QueueRuleFromOAS(req.Rules.Slow),
		queue.RuleFailed:              QueueRuleFromOAS(req.Rules.Failed),
		queue.RuleImportBlocked:       QueueRuleFromOAS(req.Rules.ImportBlocked),
		queue.RuleUnwantedScore:       QueueRuleFromOAS(req.Rules.UnwantedScore),
		queue.RuleUnwantedUnmonitored: QueueRuleFromOAS(req.Rules.UnwantedUnmonitored),
	}
	optional := map[queue.Rule]oas.OptQueueRuleSettings{
		queue.RuleNotAnUpgrade:    req.Rules.NotAnUpgrade,
		queue.RuleDuplicate:       req.Rules.Duplicate,
		queue.RuleIDMatched:       req.Rules.IDMatched,
		queue.RuleImportFixable:   req.Rules.ImportFixable,
		queue.RuleImportBroken:    req.Rules.ImportBroken,
		queue.RuleImportNotWanted: req.Rules.ImportNotWanted,
	}
	for rule, opt := range optional {
		if v, ok := opt.Get(); ok {
			rules[rule] = QueueRuleFromOAS(v)
		}
	}
	return queue.Settings{
		Enabled:                 req.Enabled,
		Rules:                   rules,
		Grace:                   time.Duration(req.GraceSeconds) * time.Second,
		MinBytesPerSecond:       req.MinBytesPerSecond,
		MinCustomFormatScore:    minCFS,
		MaxRemovalsPerSweep:     req.MaxRemovalsPerSweep,
		MaxImportsPerSweep:      req.MaxImportsPerSweep.Or(queue.DefaultSettings().MaxImportsPerSweep),
		MatchFixableMinPercent:  req.MatchFixableMinPercent.Or(queue.DefaultMatchFixableMinPercent),
		MatchBrokenBelowPercent: req.MatchBrokenBelowPercent.Or(queue.DefaultMatchBrokenBelowPercent),
		TrackFixableMinPercent:  req.TrackFixableMinPercent.Or(queue.DefaultTrackFixableMinPercent),
		TrackBrokenBelowPercent: req.TrackBrokenBelowPercent.Or(queue.DefaultTrackBrokenBelowPercent),
	}
}

// QueueRuleFromOAS converts OpenAPI QueueRuleSettings into queue.RuleSettings.
func QueueRuleFromOAS(s oas.QueueRuleSettings) queue.RuleSettings {
	rs := queue.RuleSettings{
		Mode:      queue.Mode(s.Mode),
		Threshold: s.Threshold,
	}
	if v, ok := s.Blocklist.Get(); ok {
		rs.Blocklist = &v
	}
	return rs
}
