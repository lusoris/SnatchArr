// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/policyoas"
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
	return policyoas.QueuePolicyToOAS(s)
}

func queueRuleToOAS(rs queue.RuleSettings) oas.QueueRuleSettings {
	return policyoas.QueueRuleToOAS(rs)
}

func queuePolicyFromOAS(req *oas.QueuePolicy) queue.Settings {
	return policyoas.QueuePolicyFromOAS(req)
}

func queueRuleFromOAS(s oas.QueueRuleSettings) queue.RuleSettings {
	return policyoas.QueueRuleFromOAS(s)
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
