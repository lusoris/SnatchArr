// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/requests"
)

// GetRequestsSettings returns the community request settings.
func (h *Handlers) GetRequestsSettings(ctx context.Context) (*oas.RequestsSettings, error) {
	s, err := h.requests.Get(ctx)
	if err != nil {
		return nil, err
	}
	return requestsSettingsToOAS(s), nil
}

// UpdateRequestsSettings validates and replaces the community request settings (admin only).
func (h *Handlers) UpdateRequestsSettings(ctx context.Context, req *oas.RequestsSettings) (*oas.RequestsSettings, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	in := requestsSettingsFromOAS(req)
	s, err := h.requests.Put(ctx, in)
	if err != nil {
		return nil, err
	}
	return requestsSettingsToOAS(s), nil
}

func requestsSettingsToOAS(s requests.Settings) *oas.RequestsSettings {
	return &oas.RequestsSettings{
		VotingMode:       oas.RequestsVotingMode(s.VotingMode),
		AllowViewerVotes: s.AllowViewerVotes,
		Visibility:       oas.RequestsVisibility(s.Visibility),
		VotingTTLDays:    s.VotingTTLDays,
	}
}

func requestsSettingsFromOAS(s *oas.RequestsSettings) requests.Settings {
	if s == nil {
		return requests.Settings{}
	}
	return requests.Settings{
		VotingMode:       requests.VotingMode(s.VotingMode),
		AllowViewerVotes: s.AllowViewerVotes,
		Visibility:       requests.Visibility(s.Visibility),
		VotingTTLDays:    s.VotingTTLDays,
	}
}
