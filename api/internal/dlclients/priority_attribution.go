// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients

import (
	"fmt"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func attributePriorityItems(items []PriorityItem, rows []domain.DownloadClient, instances []domain.Instance, preset domain.PriorityPreset) ([]domain.DownloadQueueItem, int) {
	ranks := categoryRanks(rows, instances, preset)
	ranked := make([]domain.DownloadQueueItem, 0, len(items))
	unattributed := 0
	for _, item := range items {
		rank, known := ranks[item.Category]
		if !known || rank < domain.MinPriorityRank || rank > domain.MaxPriorityRank {
			unattributed++
			continue
		}
		queueItem := item.DownloadQueueItem
		queueItem.Rank = rank
		queueItem.LastWrittenPriority = nil
		queueItem.PausedBySnatchArr = false
		ranked = append(ranked, queueItem)
	}
	return ranked, unattributed
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
