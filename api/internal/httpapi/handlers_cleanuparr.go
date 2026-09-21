// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/cleanuparr"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// ListCleanuparrLinks returns every link without secrets.
func (h *Handlers) ListCleanuparrLinks(ctx context.Context) ([]oas.CleanuparrLink, error) {
	list, err := h.cleanuparr.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]oas.CleanuparrLink, 0, len(list))
	for _, l := range list {
		out = append(out, cleanuparrLinkToOAS(l))
	}
	return out, nil
}

// GetCleanuparrLink returns one link.
func (h *Handlers) GetCleanuparrLink(ctx context.Context, params oas.GetCleanuparrLinkParams) (*oas.CleanuparrLink, error) {
	l, err := h.cleanuparr.Get(ctx, params.LinkId)
	if err != nil {
		return nil, err
	}
	out := cleanuparrLinkToOAS(l)
	return &out, nil
}

// CreateCleanuparrLink adds a link.
func (h *Handlers) CreateCleanuparrLink(ctx context.Context, req *oas.CleanuparrLinkInput) (*oas.CleanuparrLink, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	l, err := h.cleanuparr.Create(ctx, cleanuparrInputFromOAS(req))
	if err != nil {
		return nil, err
	}
	out := cleanuparrLinkToOAS(l)
	return &out, nil
}

// UpdateCleanuparrLink edits a link.
func (h *Handlers) UpdateCleanuparrLink(ctx context.Context, req *oas.CleanuparrLinkInput, params oas.UpdateCleanuparrLinkParams) (*oas.CleanuparrLink, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	l, err := h.cleanuparr.Update(ctx, params.LinkId, cleanuparrInputFromOAS(req))
	if err != nil {
		return nil, err
	}
	out := cleanuparrLinkToOAS(l)
	return &out, nil
}

// DeleteCleanuparrLink removes a link.
func (h *Handlers) DeleteCleanuparrLink(ctx context.Context, params oas.DeleteCleanuparrLinkParams) error {
	if err := requireAdmin(ctx); err != nil {
		return err
	}
	return h.cleanuparr.Delete(ctx, params.LinkId)
}

// TestCleanuparrLink probes a stored link.
func (h *Handlers) TestCleanuparrLink(ctx context.Context, params oas.TestCleanuparrLinkParams) (*oas.ProbeResult, error) {
	st, err := h.cleanuparr.Test(ctx, params.LinkId)
	if err != nil {
		return nil, err
	}
	return &oas.ProbeResult{AppName: "Cleanuparr", Version: st.Version}, nil
}

// TestCleanuparrLinkInput probes unsaved credentials.
func (h *Handlers) TestCleanuparrLinkInput(ctx context.Context, req *oas.CleanuparrLinkInput) (*oas.ProbeResult, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	st, err := h.cleanuparr.TestInput(ctx, cleanuparrInputFromOAS(req))
	if err != nil {
		return nil, err
	}
	return &oas.ProbeResult{AppName: "Cleanuparr", Version: st.Version}, nil
}

// GetCleanuparrStatus returns the live view of every enabled link.
func (h *Handlers) GetCleanuparrStatus(ctx context.Context) ([]oas.CleanuparrStatus, error) {
	views, err := h.cleanuparr.Status(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]oas.CleanuparrStatus, 0, len(views))
	for _, v := range views {
		out = append(out, cleanuparrStatusToOAS(v))
	}
	return out, nil
}

func cleanuparrLinkToOAS(l domain.CleanuparrLink) oas.CleanuparrLink {
	return oas.CleanuparrLink{
		ID: l.ID, Name: l.Name, BaseURL: l.BaseURL, Enabled: l.Enabled,
		LastSeenVersion: optString(l.LastSeenVersion), LastCheckAt: optTime(l.LastCheckAt), LastError: optString(l.LastError),
		CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt,
	}
}

func cleanuparrInputFromOAS(in *oas.CleanuparrLinkInput) cleanuparr.Input {
	return cleanuparr.Input{Name: in.Name, BaseURL: in.BaseURL, APIKey: in.APIKey.Or(""), Enabled: in.Enabled.Or(true)}
}

func cleanuparrStatusToOAS(v cleanuparr.LinkStatus) oas.CleanuparrStatus {
	out := oas.CleanuparrStatus{
		LinkID: v.Link.ID, Name: v.Link.Name, Reachable: v.Reachable, Error: optString(v.Error),
		Version: optString(v.Status.Version), StartedAt: optTime(v.Status.StartedAt), CheckedAt: v.CheckedAt,
		StruckDownloads: v.Summary.StruckDownloads, MarkedForRemoval: v.Summary.MarkedForRemoval,
		RemovedDownloads: v.Summary.RemovedDownloads, MediaManagers: oas.CleanuparrStatusMediaManagers{},
		RecentStrikes: make([]oas.CleanuparrStrike, 0, len(v.Recent)),
	}
	for name, n := range v.Status.MediaManagers {
		out.MediaManagers[name] = n
	}
	for _, s := range v.Recent {
		out.RecentStrikes = append(out.RecentStrikes, oas.CleanuparrStrike{
			ID: s.ID, Type: s.Type, CreatedAt: s.CreatedAt, DownloadID: s.DownloadID, Title: s.Title, DryRun: s.DryRun,
		})
	}
	return out
}
