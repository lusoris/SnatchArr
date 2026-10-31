// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"net/url"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/watchaware"
)

// ListWatchawareSources returns all configured watch-aware sources without secrets.
func (h *Handlers) ListWatchawareSources(ctx context.Context) ([]oas.WatchawareSource, error) {
	list, err := h.watchaware.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]oas.WatchawareSource, 0, len(list))
	for _, s := range list {
		out = append(out, watchawareSourceToOAS(s))
	}
	return out, nil
}

// CreateWatchawareSource creates a new watch-aware source (admin only).
func (h *Handlers) CreateWatchawareSource(ctx context.Context, req *oas.WatchawareSourceInput) (*oas.WatchawareSource, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	in := watchawareInputFromOAS(req)
	src, err := h.watchaware.Create(ctx, in)
	if err != nil {
		return nil, err
	}
	out := watchawareSourceToOAS(src)
	return &out, nil
}

// UpdateWatchawareSource updates an existing watch-aware source (admin only).
func (h *Handlers) UpdateWatchawareSource(ctx context.Context, req *oas.WatchawareSourceInput, params oas.UpdateWatchawareSourceParams) (*oas.WatchawareSource, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	stored, err := h.watchaware.Get(ctx, params.SourceId)
	if err != nil {
		return nil, err
	}
	in := watchawareInputFromOAS(req)
	in.Enabled = req.Enabled.Or(stored.Enabled)
	src, err := h.watchaware.Update(ctx, params.SourceId, in)
	if err != nil {
		return nil, err
	}
	out := watchawareSourceToOAS(src)
	return &out, nil
}

// DeleteWatchawareSource deletes a watch-aware source and its synced watches (admin only).
func (h *Handlers) DeleteWatchawareSource(ctx context.Context, params oas.DeleteWatchawareSourceParams) error {
	if err := requireAdmin(ctx); err != nil {
		return err
	}
	return h.watchaware.Delete(ctx, params.SourceId)
}

// GetWatchawareSyncStatus returns current sync status across sources.
func (h *Handlers) GetWatchawareSyncStatus(ctx context.Context) (*oas.WatchawareSyncStatus, error) {
	status, err := h.watchaware.GetSyncStatus(ctx)
	if err != nil {
		return nil, err
	}
	var lastSync oas.OptDateTime
	if status.LastSyncAt != nil {
		lastSync = oas.NewOptDateTime(*status.LastSyncAt)
	}
	var lastErr oas.OptString
	if status.LastError != nil {
		lastErr = oas.NewOptString(*status.LastError)
	}
	return &oas.WatchawareSyncStatus{
		LastSyncAt:     lastSync,
		TotalSources:   status.TotalSources,
		EnabledSources: status.EnabledSources,
		TotalWatches:   status.TotalWatches,
		LastError:      lastErr,
	}, nil
}

func watchawareSourceToOAS(s watchaware.Source) oas.WatchawareSource {
	var lastSync oas.OptDateTime
	if s.LastSyncAt != nil {
		lastSync = oas.NewOptDateTime(*s.LastSyncAt)
	}
	var lastErr oas.OptString
	if s.LastError != nil {
		lastErr = oas.NewOptString(*s.LastError)
	}
	u, err := url.Parse(s.URL)
	if err != nil {
		u = &url.URL{}
	}
	return oas.WatchawareSource{
		ID:         s.ID,
		Kind:       oas.WatchawareSourceKind(s.Kind),
		Name:       s.Name,
		URL:        *u,
		HasAPIKey:  s.HasAPIKey,
		Enabled:    s.Enabled,
		LastSyncAt: lastSync,
		LastError:  lastErr,
		CreatedAt:  s.CreatedAt,
		UpdatedAt:  s.UpdatedAt,
	}
}

func watchawareInputFromOAS(req *oas.WatchawareSourceInput) watchaware.SourceInput {
	var apiKey string
	if req.APIKey.IsSet() {
		apiKey = req.APIKey.Value
	}
	return watchaware.SourceInput{
		Kind:    watchaware.Kind(req.Kind),
		Name:    req.Name,
		URL:     req.URL.String(),
		APIKey:  apiKey,
		Enabled: req.Enabled.Or(true),
	}
}
