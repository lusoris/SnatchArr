// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// GetMyPreferences implements getMyPreferences operation.
func (h *Handlers) GetMyPreferences(ctx context.Context) (*oas.UserPreferences, error) {
	u, ok := UserFrom(ctx)
	if !ok {
		return nil, domain.ErrUnauthorized
	}
	p, err := h.settings.GetUserPreferences(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	return userPreferencesToOAS(p), nil
}

// UpdateMyPreferences implements updateMyPreferences operation.
func (h *Handlers) UpdateMyPreferences(ctx context.Context, req *oas.UserPreferences) (*oas.UserPreferences, error) {
	u, ok := UserFrom(ctx)
	if !ok {
		return nil, domain.ErrUnauthorized
	}
	p, err := h.settings.UpdateUserPreferences(ctx, u.ID, userPreferencesFromOAS(req))
	if err != nil {
		return nil, err
	}
	return userPreferencesToOAS(p), nil
}
