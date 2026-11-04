// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func attributePriorityItems(
	items []PriorityItem,
	rows []domain.DownloadClient,
	instances []domain.Instance,
	preset domain.PriorityPreset,
	ownership []PriorityOwnership,
) ([]domain.DownloadQueueItem, int, map[string]uuid.UUID) {
	ranks := categoryRanks(rows, instances, preset)
	catClients := categoryClients(rows)
	ownMap := make(map[string]PriorityOwnership, len(ownership))
	for _, o := range ownership {
		key := o.ClientID.String() + "\n" + o.ItemID
		ownMap[key] = o
	}
	ranked := make([]domain.DownloadQueueItem, 0, len(items))
	itemClients := make(map[string]uuid.UUID, len(items))
	unattributed := 0
	for _, item := range items {
		rank, known := ranks[item.Category]
		clientID, hasClient := catClients[item.Category]
		if !known || !hasClient || rank < domain.MinPriorityRank || rank > domain.MaxPriorityRank {
			unattributed++
			continue
		}
		queueItem := item.DownloadQueueItem
		queueItem.Rank = rank
		key := clientID.String() + "\n" + item.ID
		if o, ok := ownMap[key]; ok {
			queueItem.LastWrittenPriority = o.LastWrittenPriority
			queueItem.PausedBySnatchArr = o.PausedBySnatchArr
		} else {
			queueItem.LastWrittenPriority = nil
			queueItem.PausedBySnatchArr = false
		}
		ranked = append(ranked, queueItem)
		itemClients[item.ID] = clientID
	}
	return ranked, unattributed, itemClients
}

func categoryClients(rows []domain.DownloadClient) map[string]uuid.UUID {
	clients := make(map[string]uuid.UUID, len(rows))
	ambiguous := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.Category == "" || ambiguous[row.Category] {
			continue
		}
		if _, exists := clients[row.Category]; exists {
			ambiguous[row.Category] = true
			delete(clients, row.Category)
			continue
		}
		clients[row.Category] = row.ID
	}
	return clients
}

func categoryRanks(rows []domain.DownloadClient, instances []domain.Instance, preset domain.PriorityPreset) map[string]int {
	ranks := make(map[string]int, len(rows))
	for _, row := range rows {
		if row.Category == "" {
			continue
		}
		rank := downloadRank(row, instances, preset)
		if _, exists := ranks[row.Category]; exists {
			// More than one definition is ambiguous; never choose an instance arbitrarily.
			ranks[row.Category] = 0
			continue
		}
		ranks[row.Category] = rank
	}
	return ranks
}

func downloadRank(row domain.DownloadClient, instances []domain.Instance, preset domain.PriorityPreset) int {
	if row.InstanceID == nil {
		return 0
	}
	for _, inst := range instances {
		if inst.ID != *row.InstanceID {
			continue
		}
		if _, err := domain.ParseAppKind(string(inst.Kind)); err != nil {
			return 0
		}
		priority := domain.InstancePriority{Download: inst.DownloadPriority}
		rank, _ := priority.ResolveDownload(inst.Kind, preset)
		return rank
	}
	return 0
}

func planPriorityObservation(kind domain.DownloadClientKind, disabled bool, items []domain.DownloadQueueItem) (domain.DownloadClientPlan, error) {
	switch kind {
	case domain.ClientNZBGet:
		return domain.PlanLeveledClient(items, domain.NZBGetPriorityLevels())
	case domain.ClientSABnzbd:
		return domain.PlanLeveledClient(items, domain.SABnzbdPriorityLevels())
	case domain.ClientQBittorrent:
		return planQbitObservation(disabled, items)
	case domain.ClientTransmission, domain.ClientDeluge, domain.ClientRTorrent:
		// ADR-0046 covers qBittorrent, NZBGet and SABnzbd only.
	}
	return domain.DownloadClientPlan{}, fmt.Errorf("%w: unsupported priority client kind %q", domain.ErrInvalid, kind)
}

func planQbitObservation(disabled bool, items []domain.DownloadQueueItem) (domain.DownloadClientPlan, error) {
	plan, err := domain.PlanQueueOrderClient(items)
	if err != nil {
		return domain.DownloadClientPlan{}, err
	}
	plan.Order = nil
	if disabled {
		return plan, nil
	}
	queued := make([]domain.DownloadQueueItem, 0, len(items))
	ranks := make(map[int]bool)
	for _, item := range items {
		if item.State == domain.DownloadItemQueued && item.Priority > 0 {
			queued = append(queued, item)
			ranks[item.Rank] = true
		}
	}
	if len(ranks) < 2 {
		return plan, nil
	}
	order, err := domain.PlanQueueOrderClient(queued)
	if err != nil {
		return domain.DownloadClientPlan{}, err
	}
	plan.Order = order.Order
	return plan, nil
}
