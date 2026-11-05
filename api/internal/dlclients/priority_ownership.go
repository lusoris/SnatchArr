// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

const ownershipTimeout = 5 * time.Second

func ownershipContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), ownershipTimeout)
}

func rowIDs(rows []domain.DownloadClient) []uuid.UUID {
	ids := make([]uuid.UUID, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	return ids
}

func (r *Reconciler) prepareWrite(ctx context.Context, clientID uuid.UUID, itemID, action string, priority *int) bool {
	if err := r.source.PrepareWrite(ctx, clientID, itemID, action, priority); err != nil {
		r.priorityWriteFailed(ctx, clientID, "prepare_ownership")
		return false
	}
	return true
}

// prepareItems persists ownership intents before a batch of writes and cancels prepared intents if preparation fails.
func (r *Reconciler) prepareItems(ctx context.Context, ids []string, clients map[string]uuid.UUID, action string) bool {
	for i, id := range ids {
		if !r.prepareWrite(ctx, clients[id], id, action, nil) {
			r.cancelUnwritten(ctx, ids[:i], errors.New("intent preparation failed"), clients)
			return false
		}
	}
	return true
}

// recoverPriorityOwnership reconciles durable ownership with observed items before planning further writes.
// Only the durable intent, written while an item was eligible, permits claiming a paused item.
// An observed paused item without an intent or confirmed owner is never claimed.
// An observed running item releases confirmed pause ownership after a manual resume.
func (r *Reconciler) recoverPriorityOwnership(ctx context.Context, items []PriorityItem, ownership []PriorityOwnership) bool {
	if len(ownership) > maxPriorityItemsPerGroup*maxPriorityRowsPerTick {
		r.logger.WarnContext(ctx, "download priority ownership truncated")
		return false
	}
	observed := make(map[string]PriorityItem, len(items))
	for _, item := range items {
		observed[item.ID] = item
	}
	recordCtx, cancel := ownershipContext(ctx)
	defer cancel()
	for _, own := range ownership {
		item, exists := observed[own.ItemID]
		if !exists {
			continue
		}
		if err := r.recoverOwnershipItem(recordCtx, own, item); err != nil {
			r.priorityWriteFailed(ctx, own.ClientID, "repair_ownership")
			return false
		}
	}
	return true
}

// recoverOwnershipItem confirms or cancels a pending intent, or releases pause ownership after a manual resume.
// A paused item can be claimed only through a durable pause intent.
func (r *Reconciler) recoverOwnershipItem(ctx context.Context, own PriorityOwnership, item PriorityItem) error {
	if own.PendingAction == "" {
		return r.releaseResumedPauseOwnership(ctx, own, item)
	}
	switch {
	case own.PendingAction == "priority" && own.PendingPriority != nil && item.Priority == *own.PendingPriority:
		return r.source.UpsertPriority(ctx, own.ClientID, own.ItemID, *own.PendingPriority)
	case own.PendingAction == "pause" && item.State == domain.DownloadItemPaused:
		return r.source.SetPaused(ctx, own.ClientID, own.ItemID)
	case own.PendingAction == "resume" && item.State != domain.DownloadItemPaused:
		return r.source.ClearPaused(ctx, own.ClientID, own.ItemID)
	default:
		return r.source.CancelWrite(ctx, own.ClientID, own.ItemID)
	}
}

// releaseResumedPauseOwnership releases confirmed pause ownership when an item runs with no pending action.
// The user has taken the item back, so a later user pause must remain unowned.
func (r *Reconciler) releaseResumedPauseOwnership(ctx context.Context, own PriorityOwnership, item PriorityItem) error {
	if own.PausedBySnatchArr && item.State != domain.DownloadItemPaused {
		return r.source.ClearPaused(ctx, own.ClientID, own.ItemID)
	}
	return nil
}

func (r *Reconciler) cancelUnwritten(ctx context.Context, ids []string, writeErr error, clients map[string]uuid.UUID) {
	var done []string
	if partial, ok := errors.AsType[*PartialWriteError](writeErr); ok {
		done = partial.Done
	}
	recordCtx, cancel := ownershipContext(ctx)
	defer cancel()
	for _, id := range ids {
		if slices.Contains(done, id) {
			continue
		}
		if err := r.source.CancelWrite(recordCtx, clients[id], id); err != nil {
			r.priorityWriteFailed(ctx, clients[id], "cancel_ownership")
		}
	}
}
