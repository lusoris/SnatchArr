// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"errors"
	"fmt"

	"github.com/lusoris/SnatchArr/api/internal/adult"
	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func toOASAdultSource(s adult.Source) oas.AdultSource {
	return oas.AdultSource{
		ID:            s.ID,
		Name:          s.Name,
		Endpoint:      s.Endpoint,
		HasAPIKey:     s.HasAPIKey,
		Enabled:       s.Enabled,
		SyncFavorites: s.SyncFavorites,
		Priority:      s.Priority,
		CreatedAt:     s.CreatedAt,
		UpdatedAt:     s.UpdatedAt,
	}
}

func fromOASAdultSourceInput(req *oas.AdultSourceInput) adult.SourceInput {
	enabled := true
	if req.Enabled.IsSet() {
		enabled = req.Enabled.Value
	}
	syncFavs := true
	if req.SyncFavorites.IsSet() {
		syncFavs = req.SyncFavorites.Value
	}
	priority := 0
	if req.Priority.IsSet() {
		priority = req.Priority.Value
	}
	apiKey := ""
	if req.APIKey.IsSet() {
		apiKey = req.APIKey.Value
	}
	return adult.SourceInput{
		Name:          req.Name,
		Endpoint:      req.Endpoint,
		APIKey:        apiKey,
		Enabled:       enabled,
		SyncFavorites: syncFavs,
		Priority:      priority,
	}
}

func mapAdultError(err error) error {
	if errors.Is(err, domain.ErrInvalid) {
		return fmt.Errorf("%w: %s", domain.ErrBadRequest, err.Error())
	}
	return err
}

// ListAdultSources implements listAdultSources operation.
func (h *Handlers) ListAdultSources(ctx context.Context) ([]oas.AdultSource, error) {
	if h.adult == nil {
		return nil, fmt.Errorf("%w: module adult is disabled", domain.ErrNotFound)
	}
	rows, err := h.adult.ListSources(ctx)
	if err != nil {
		return nil, mapAdultError(err)
	}
	out := make([]oas.AdultSource, 0, len(rows))
	for _, r := range rows {
		out = append(out, toOASAdultSource(r))
	}
	return out, nil
}

// GetAdultSource implements getAdultSource operation.
func (h *Handlers) GetAdultSource(ctx context.Context, params oas.GetAdultSourceParams) (*oas.AdultSource, error) {
	if h.adult == nil {
		return nil, fmt.Errorf("%w: module adult is disabled", domain.ErrNotFound)
	}
	src, err := h.adult.GetSource(ctx, params.SourceId)
	if err != nil {
		return nil, mapAdultError(err)
	}
	res := toOASAdultSource(src)
	return &res, nil
}

// CreateAdultSource implements createAdultSource operation.
func (h *Handlers) CreateAdultSource(ctx context.Context, req *oas.AdultSourceInput) (*oas.AdultSource, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	if h.adult == nil {
		return nil, fmt.Errorf("%w: module adult is disabled", domain.ErrNotFound)
	}
	src, err := h.adult.CreateSource(ctx, fromOASAdultSourceInput(req))
	if err != nil {
		return nil, mapAdultError(err)
	}
	res := toOASAdultSource(src)
	return &res, nil
}

// UpdateAdultSource implements updateAdultSource operation.
func (h *Handlers) UpdateAdultSource(ctx context.Context, req *oas.AdultSourceInput, params oas.UpdateAdultSourceParams) (*oas.AdultSource, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	if h.adult == nil {
		return nil, fmt.Errorf("%w: module adult is disabled", domain.ErrNotFound)
	}
	src, err := h.adult.UpdateSource(ctx, params.SourceId, fromOASAdultSourceInput(req))
	if err != nil {
		return nil, mapAdultError(err)
	}
	res := toOASAdultSource(src)
	return &res, nil
}

// DeleteAdultSource implements deleteAdultSource operation.
func (h *Handlers) DeleteAdultSource(ctx context.Context, params oas.DeleteAdultSourceParams) error {
	if err := requireAdmin(ctx); err != nil {
		return err
	}
	if h.adult == nil {
		return fmt.Errorf("%w: module adult is disabled", domain.ErrNotFound)
	}
	if err := h.adult.DeleteSource(ctx, params.SourceId); err != nil {
		return mapAdultError(err)
	}
	return nil
}

// TriggerAdultSync implements triggerAdultSync operation.
func (h *Handlers) TriggerAdultSync(ctx context.Context) (*oas.AdultSyncSummary, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	if h.adult == nil {
		return nil, fmt.Errorf("%w: module adult is disabled", domain.ErrNotFound)
	}
	summary, err := h.adult.SyncNow(ctx)
	if err != nil {
		return nil, mapAdultError(err)
	}
	return &oas.AdultSyncSummary{
		SourcesSynced:    summary.SourcesSynced,
		PerformersSynced: summary.PerformersSynced,
		StudiosSynced:    summary.StudiosSynced,
	}, nil
}

// GetAdultSyncStatus implements getAdultSyncStatus operation.
func (h *Handlers) GetAdultSyncStatus(ctx context.Context) (*oas.AdultSyncStatus, error) {
	if h.adult == nil {
		return nil, fmt.Errorf("%w: module adult is disabled", domain.ErrNotFound)
	}
	st, err := h.adult.GetSyncStatus(ctx)
	if err != nil {
		return nil, mapAdultError(err)
	}
	res := &oas.AdultSyncStatus{
		TotalSources:     st.TotalSources,
		EnabledSources:   st.EnabledSources,
		PerformersSynced: st.PerformersSynced,
		StudiosSynced:    st.StudiosSynced,
	}
	if st.LastSyncAt != nil {
		res.LastSyncAt = oas.NewOptDateTime(*st.LastSyncAt)
	}
	if st.LastError != "" {
		res.LastError = oas.NewOptString(st.LastError)
	}
	return res, nil
}
