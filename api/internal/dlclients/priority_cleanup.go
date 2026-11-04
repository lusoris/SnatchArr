// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients

import (
	"context"
	"log/slog"
	"maps"
	"slices"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func priorityPlanBounded(plan domain.DownloadClientPlan) bool {
	return len(plan.Priority)+len(plan.Order)+len(plan.Pause)+len(plan.Resume) <= maxPriorityMutationsPerGroup
}

// Re-read the complete physical group, including rows added during observation.
// Failed reads and removed or disabled rows withdraw permission to act.
func (r *Reconciler) currentGroupMode(ctx context.Context, rows []domain.DownloadClient) (string, []string) {
	inputs, err := r.source.Load(ctx)
	if err != nil || len(inputs.Clients) > maxPriorityRowsPerTick {
		return "observe", []string{"configuration unavailable"}
	}
	for _, group := range priorityGroups(inputs.Clients) {
		if group[0].Kind == rows[0].Kind && group[0].BaseURL == rows[0].BaseURL {
			if !samePriorityRows(rows, group) {
				return "observe", []string{"configuration changed"}
			}
			return groupPriorityMode(group)
		}
	}
	return "observe", []string{"configuration removed"}
}

func samePriorityRows(before, after []domain.DownloadClient) bool {
	if len(before) != len(after) {
		return false
	}
	for _, row := range before {
		if !slices.ContainsFunc(after, func(current domain.DownloadClient) bool {
			return current.ID == row.ID && current.Category == row.Category && sameInstance(current.InstanceID, row.InstanceID)
		}) {
			return false
		}
	}
	return true
}

// Cleanup follows persisted ownership, independently of current attribution.
func ownershipResumeCandidates(items []PriorityItem, ownership []PriorityOwnership, attributed map[string]uuid.UUID, planned []string, mode string) []string {
	paused := make(map[string]bool, len(items))
	for _, item := range items {
		paused[item.ID] = item.State == domain.DownloadItemPaused
	}
	resume := slices.Clone(planned)
	for _, own := range ownership {
		if !own.PausedBySnatchArr || !paused[own.ItemID] {
			continue
		}
		if mode != "act" || attributed[own.ItemID] != own.ClientID {
			if !slices.Contains(resume, own.ItemID) {
				resume = append(resume, own.ItemID)
			}
		}
	}
	slices.Sort(resume)
	return resume
}

func ownershipClients(attributed map[string]uuid.UUID, ownership []PriorityOwnership) map[string]uuid.UUID {
	clients := make(map[string]uuid.UUID, len(attributed)+len(ownership))
	maps.Copy(clients, attributed)
	for _, own := range ownership {
		if own.PausedBySnatchArr {
			clients[own.ItemID] = own.ClientID
		}
	}
	return clients
}

func sameInstance(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func (r *Reconciler) applyPriorityPlan(ctx context.Context, rows []domain.DownloadClient, client domain.DownloadClient,
	secret string, obs PriorityObservation, items []domain.DownloadQueueItem, itemClients map[string]uuid.UUID,
	ownership []PriorityOwnership, plan domain.DownloadClientPlan,
) (domain.DownloadClientPlan, string, []string) {
	mode, observeRowNames := r.currentGroupMode(ctx, rows)
	plan.Resume = ownershipResumeCandidates(obs.Items, ownership, itemClients, plan.Resume, mode)
	resumeClients := ownershipClients(itemClients, ownership)
	if r.writer != nil {
		if mode == "act" {
			withdrawn := ownershipResumeCandidates(obs.Items, ownership, itemClients, nil, mode)
			mode, observeRowNames = r.executeActWithCleanup(ctx, rows, client, secret, plan, obs.QueueingDisabled, items, resumeClients, ownership, withdrawn)
		} else {
			cleanup := domain.DownloadClientPlan{Resume: plan.Resume}
			if r.priorityPlanAllowed(ctx, cleanup) {
				r.executeResumeWrites(ctx, client, secret, cleanup, resumeClients, ownership)
			}
		}
	}
	return plan, mode, observeRowNames
}

func (r *Reconciler) priorityPlanAllowed(ctx context.Context, plan domain.DownloadClientPlan) bool {
	if priorityPlanBounded(plan) {
		return true
	}
	r.logger.WarnContext(ctx, "download priority mutations truncated", slog.Int("limit", maxPriorityMutationsPerGroup))
	return false
}

// Withdrawal cleanup runs before scheduling, so a failed priority/order write cannot strand it.
func (r *Reconciler) executeActWithCleanup(ctx context.Context, rows []domain.DownloadClient, client domain.DownloadClient, secret string,
	plan domain.DownloadClientPlan, disabled bool, items []domain.DownloadQueueItem, clients map[string]uuid.UUID,
	ownership []PriorityOwnership, withdrawn []string,
) (string, []string) {
	cleanup := domain.DownloadClientPlan{Resume: withdrawn}
	if !r.priorityPlanAllowed(ctx, cleanup) {
		return "act", nil
	}
	if !r.executeResumeWrites(ctx, client, secret, cleanup, clients, ownership) {
		return "act", nil
	}
	remaining := make([]string, 0, len(plan.Resume))
	for _, id := range plan.Resume {
		if !slices.Contains(withdrawn, id) {
			remaining = append(remaining, id)
		}
	}
	plan.Resume = remaining
	mode, names := "act", []string(nil)
	if len(withdrawn) > 0 {
		mode, names = r.currentGroupMode(ctx, rows)
	}
	if mode == "act" {
		r.executeActPlan(ctx, client, secret, plan, disabled, items, clients, ownership)
	}
	return mode, names
}
