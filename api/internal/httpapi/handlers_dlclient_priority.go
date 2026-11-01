// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/dlclients"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

type priorityPlanReader interface {
	Latest(uuid.UUID) (dlclients.PriorityPlan, error)
}

// GetDownloadClientPriorityPlan returns cached metadata only; it never contacts a client.
func (h *Handlers) GetDownloadClientPriorityPlan(_ context.Context, params oas.GetDownloadClientPriorityPlanParams) (*oas.DownloadClientPriorityPlan, error) {
	if h.priorityPlans == nil {
		return nil, domain.ErrNotFound
	}
	plan, err := h.priorityPlans.Latest(params.ClientId)
	if err != nil {
		return nil, err
	}
	out := priorityPlanToOAS(plan)
	return &out, nil
}

func priorityPlanToOAS(p dlclients.PriorityPlan) oas.DownloadClientPriorityPlan {
	moves := make([]oas.DownloadClientPriorityMove, 0, len(p.Plan.Priority))
	for _, m := range p.Plan.Priority {
		moves = append(moves, oas.DownloadClientPriorityMove{ID: m.ID, From: m.From, To: m.To})
	}
	return oas.DownloadClientPriorityPlan{
		ClientId: p.ClientID, Mode: oas.DownloadClientPriorityMode(p.Mode), ObservedAt: p.ObservedAt,
		QueueingDisabled: p.QueueingDisabled, Unattributed: p.Unattributed, Priority: moves,
		Order: append([]string{}, p.Plan.Order...), Pause: append([]string{}, p.Plan.Pause...), Resume: append([]string{}, p.Plan.Resume...),
	}
}
