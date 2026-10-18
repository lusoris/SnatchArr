// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/queue"
)

// GetQueuePolicy returns an instance's queue policy.
func (h *Handlers) GetQueuePolicy(ctx context.Context, params oas.GetQueuePolicyParams) (*oas.QueuePolicy, error) {
	if _, err := h.instances.Get(ctx, params.InstanceId); err != nil {
		return nil, err
	}
	s, err := h.queue.Settings(ctx, params.InstanceId)
	if err != nil {
		return nil, err
	}
	return queuePolicyToOAS(s), nil
}

// UpdateQueuePolicy replaces an instance's queue policy.
func (h *Handlers) UpdateQueuePolicy(ctx context.Context, req *oas.QueuePolicy, params oas.UpdateQueuePolicyParams) (*oas.QueuePolicy, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	if _, err := h.instances.Get(ctx, params.InstanceId); err != nil {
		return nil, err
	}
	cfg := queuePolicyFromOAS(req)
	if err := h.queue.SaveSettings(ctx, params.InstanceId, cfg); err != nil {
		return nil, err
	}
	return queuePolicyToOAS(cfg), nil
}

// ListQueueStrikes lists active queue strikes for an instance.
func (h *Handlers) ListQueueStrikes(ctx context.Context, params oas.ListQueueStrikesParams) ([]oas.QueueStrike, error) {
	if _, err := h.instances.Get(ctx, params.InstanceId); err != nil {
		return nil, err
	}
	cfg, err := h.queue.Settings(ctx, params.InstanceId)
	if err != nil {
		return nil, err
	}
	items, err := h.queue.ListStrikes(ctx, params.InstanceId)
	if err != nil {
		return nil, err
	}
	out := make([]oas.QueueStrike, 0, len(items))
	for _, item := range items {
		out = append(out, queueStrikeToOAS(item, queue.ResolveRule(cfg, item.Rule).Threshold))
	}
	return out, nil
}

func queuePolicyToOAS(s queue.Settings) *oas.QueuePolicy {
	var minCFS oas.OptInt32
	if s.MinCustomFormatScore != nil {
		minCFS.SetTo(*s.MinCustomFormatScore)
	}
	rules := oas.QueueRules{
		Stalled:             queueRuleToOAS(queue.ResolveRule(s, queue.RuleStalled)),
		Slow:                queueRuleToOAS(queue.ResolveRule(s, queue.RuleSlow)),
		Failed:              queueRuleToOAS(queue.ResolveRule(s, queue.RuleFailed)),
		ImportBlocked:       queueRuleToOAS(queue.ResolveRule(s, queue.RuleImportBlocked)),
		UnwantedScore:       queueRuleToOAS(queue.ResolveRule(s, queue.RuleUnwantedScore)),
		UnwantedUnmonitored: queueRuleToOAS(queue.ResolveRule(s, queue.RuleUnwantedUnmonitored)),
	}
	rules.NotAnUpgrade.SetTo(queueRuleToOAS(queue.ResolveRule(s, queue.RuleNotAnUpgrade)))
	rules.Duplicate.SetTo(queueRuleToOAS(queue.ResolveRule(s, queue.RuleDuplicate)))
	rules.IDMatched.SetTo(queueRuleToOAS(queue.ResolveRule(s, queue.RuleIDMatched)))
	return &oas.QueuePolicy{
		Enabled:              s.Enabled,
		Rules:                rules,
		GraceSeconds:         int(s.Grace / time.Second), // #nosec G115 -- validated 0..86400
		MinBytesPerSecond:    s.MinBytesPerSecond,
		MinCustomFormatScore: minCFS,
		MaxRemovalsPerSweep:  s.MaxRemovalsPerSweep,
		MaxImportsPerSweep:   oas.NewOptInt(s.MaxImportsPerSweep),
	}
}

func queueRuleToOAS(rs queue.RuleSettings) oas.QueueRuleSettings {
	out := oas.QueueRuleSettings{
		Mode:      oas.QueueRuleMode(rs.Mode),
		Threshold: rs.Threshold,
	}
	if rs.Blocklist != nil {
		out.Blocklist.SetTo(*rs.Blocklist)
	}
	return out
}

func queuePolicyFromOAS(req *oas.QueuePolicy) queue.Settings {
	var minCFS *int32
	if val, ok := req.MinCustomFormatScore.Get(); ok {
		v := val
		minCFS = &v
	}
	rules := map[queue.Rule]queue.RuleSettings{
		queue.RuleStalled:             queueRuleFromOAS(req.Rules.Stalled),
		queue.RuleSlow:                queueRuleFromOAS(req.Rules.Slow),
		queue.RuleFailed:              queueRuleFromOAS(req.Rules.Failed),
		queue.RuleImportBlocked:       queueRuleFromOAS(req.Rules.ImportBlocked),
		queue.RuleUnwantedScore:       queueRuleFromOAS(req.Rules.UnwantedScore),
		queue.RuleUnwantedUnmonitored: queueRuleFromOAS(req.Rules.UnwantedUnmonitored),
	}
	if v, ok := req.Rules.NotAnUpgrade.Get(); ok {
		rules[queue.RuleNotAnUpgrade] = queueRuleFromOAS(v)
	}
	if v, ok := req.Rules.Duplicate.Get(); ok {
		rules[queue.RuleDuplicate] = queueRuleFromOAS(v)
	}
	if v, ok := req.Rules.IDMatched.Get(); ok {
		rules[queue.RuleIDMatched] = queueRuleFromOAS(v)
	}
	return queue.Settings{
		Enabled:              req.Enabled,
		Rules:                rules,
		Grace:                time.Duration(req.GraceSeconds) * time.Second,
		MinBytesPerSecond:    req.MinBytesPerSecond,
		MinCustomFormatScore: minCFS,
		MaxRemovalsPerSweep:  req.MaxRemovalsPerSweep,
		MaxImportsPerSweep:   req.MaxImportsPerSweep.Or(queue.DefaultSettings().MaxImportsPerSweep),
	}
}

func queueRuleFromOAS(s oas.QueueRuleSettings) queue.RuleSettings {
	rs := queue.RuleSettings{
		Mode:      queue.Mode(s.Mode),
		Threshold: s.Threshold,
	}
	if v, ok := s.Blocklist.Get(); ok {
		rs.Blocklist = &v
	}
	return rs
}

func queueStrikeToOAS(item queue.StrikeItem, threshold int) oas.QueueStrike {
	out := oas.QueueStrike{
		DownloadID: item.DownloadID,
		QueueID:    item.QueueID,
		Rule:       oas.QueueRule(item.Rule),
		Count:      item.Count,
		Threshold:  threshold,
		LastSeen:   item.LastSeen,
	}
	if !item.ImportedAt.IsZero() {
		out.ImportedAt.SetTo(item.ImportedAt)
		out.ImportRefused.SetTo(item.ImportRefused)
	}
	return out
}
