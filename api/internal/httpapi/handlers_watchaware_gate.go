// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/watchaware"
)

// ListWatchawareGateSeries returns the season gate series status for an instance.
func (h *Handlers) ListWatchawareGateSeries(ctx context.Context, params oas.ListWatchawareGateSeriesParams) ([]oas.WatchawareGateSeries, error) {
	limit := 100
	if params.Limit.IsSet() {
		limit = params.Limit.Value
	}
	records, err := h.watchaware.ListGateSeries(ctx, params.InstanceID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]oas.WatchawareGateSeries, 0, len(records))
	for _, r := range records {
		out = append(out, gateSeriesToOAS(r))
	}
	return out, nil
}

// ListWatchawareGateDecisions returns recent season gate decisions.
func (h *Handlers) ListWatchawareGateDecisions(ctx context.Context, params oas.ListWatchawareGateDecisionsParams) ([]oas.WatchawareGateDecision, error) {
	limit := 100
	if params.Limit.IsSet() {
		limit = params.Limit.Value
	}
	var instID *uuid.UUID
	if params.InstanceID.IsSet() {
		instID = &params.InstanceID.Value
	}
	records, err := h.watchaware.ListGateDecisions(ctx, instID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]oas.WatchawareGateDecision, 0, len(records))
	for _, r := range records {
		out = append(out, gateDecisionToOAS(r))
	}
	return out, nil
}

func gateSeriesToOAS(r watchaware.GateSeriesRecord) oas.WatchawareGateSeries {
	held := r.HeldSeasons
	if held == nil {
		held = []int{}
	}
	return oas.WatchawareGateSeries{
		InstanceID:      r.InstanceID,
		SeriesID:        r.SeriesID,
		TvdbID:          r.TvdbID,
		Title:           r.Title,
		GateSeason:      r.GateSeason,
		State:           oas.WatchawareGateSeriesState(r.State),
		AddedBeforeGate: r.AddedBeforeGate,
		HeldSeasons:     held,
		UpdatedAt:       r.UpdatedAt,
	}
}

func gateDecisionToOAS(r watchaware.GateDecisionRecord) oas.WatchawareGateDecision {
	return oas.WatchawareGateDecision{
		ID:           r.ID,
		InstanceID:   r.InstanceID,
		SeriesID:     r.SeriesID,
		SeasonNumber: r.SeasonNumber,
		Action:       oas.WatchawareGateDecisionAction(r.Action),
		WouldAct:     r.WouldAct,
		Reason:       r.Reason,
		DecidedAt:    r.DecidedAt,
	}
}
