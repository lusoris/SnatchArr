// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/prowlarr"
)

// ListProwlarrLinks returns every link without secrets.
func (h *Handlers) ListProwlarrLinks(ctx context.Context) ([]oas.ProwlarrLink, error) {
	list, err := h.prowlarr.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]oas.ProwlarrLink, 0, len(list))
	for _, l := range list {
		out = append(out, prowlarrLinkToOAS(l))
	}
	return out, nil
}

// GetProwlarrLink returns one link.
func (h *Handlers) GetProwlarrLink(ctx context.Context, params oas.GetProwlarrLinkParams) (*oas.ProwlarrLink, error) {
	l, err := h.prowlarr.Get(ctx, params.LinkId)
	if err != nil {
		return nil, err
	}
	out := prowlarrLinkToOAS(l)
	return &out, nil
}

// CreateProwlarrLink adds a link.
func (h *Handlers) CreateProwlarrLink(ctx context.Context, req *oas.ProwlarrLinkInput) (*oas.ProwlarrLink, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	l, err := h.prowlarr.Create(ctx, prowlarrInputFromOAS(req))
	if err != nil {
		return nil, err
	}
	out := prowlarrLinkToOAS(l)
	return &out, nil
}

// UpdateProwlarrLink edits a link.
func (h *Handlers) UpdateProwlarrLink(ctx context.Context, req *oas.ProwlarrLinkInput, params oas.UpdateProwlarrLinkParams) (*oas.ProwlarrLink, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	l, err := h.prowlarr.Update(ctx, params.LinkId, prowlarrInputFromOAS(req))
	if err != nil {
		return nil, err
	}
	out := prowlarrLinkToOAS(l)
	return &out, nil
}

// DeleteProwlarrLink removes a link.
func (h *Handlers) DeleteProwlarrLink(ctx context.Context, params oas.DeleteProwlarrLinkParams) error {
	if err := requireAdmin(ctx); err != nil {
		return err
	}
	return h.prowlarr.Delete(ctx, params.LinkId)
}

// TestProwlarrLink probes a stored link.
func (h *Handlers) TestProwlarrLink(ctx context.Context, params oas.TestProwlarrLinkParams) (*oas.ProbeResult, error) {
	st, err := h.prowlarr.Test(ctx, params.LinkId)
	if err != nil {
		return nil, err
	}
	return &oas.ProbeResult{AppName: "Prowlarr", Version: st.Version}, nil
}

// TestProwlarrLinkInput probes unsaved credentials.
func (h *Handlers) TestProwlarrLinkInput(ctx context.Context, req *oas.ProwlarrLinkInput) (*oas.ProbeResult, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	st, err := h.prowlarr.TestInput(ctx, prowlarrInputFromOAS(req))
	if err != nil {
		return nil, err
	}
	return &oas.ProbeResult{AppName: "Prowlarr", Version: st.Version}, nil
}

// GetProwlarrStatus returns the live view of every enabled link.
func (h *Handlers) GetProwlarrStatus(ctx context.Context) ([]oas.ProwlarrStatus, error) {
	views, err := h.prowlarr.Status(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]oas.ProwlarrStatus, 0, len(views))
	for _, v := range views {
		out = append(out, prowlarrStatusToOAS(v))
	}
	return out, nil
}

func prowlarrLinkToOAS(l domain.ProwlarrLink) oas.ProwlarrLink {
	return oas.ProwlarrLink{
		ID:              l.ID,
		Name:            l.Name,
		BaseURL:         l.BaseURL,
		Enabled:         l.Enabled,
		LastSeenVersion: optString(l.LastSeenVersion),
		LastCheckAt:     optTime(l.LastCheckAt),
		LastError:       optString(l.LastError),
		CreatedAt:       l.CreatedAt,
		UpdatedAt:       l.UpdatedAt,
	}
}

func prowlarrInputFromOAS(in *oas.ProwlarrLinkInput) prowlarr.Input {
	return prowlarr.Input{Name: in.Name, BaseURL: in.BaseURL, APIKey: in.APIKey.Or(""), Enabled: in.Enabled.Or(true)}
}

func prowlarrStatusToOAS(v prowlarr.LinkStatus) oas.ProwlarrStatus {
	idxOut := make([]oas.ProwlarrIndexerStatus, 0, len(v.Indexers))
	for _, idx := range v.Indexers {
		idxOut = append(idxOut, oas.ProwlarrIndexerStatus{
			ID:           idx.ID,
			Name:         idx.Name,
			QueryLimit:   optInt(idx.QueryLimit),
			GrabLimit:    optInt(idx.GrabLimit),
			LimitsUnit:   idx.LimitsUnit,
			UsedQueries:  idx.UsedQueries,
			UsedGrabs:    idx.UsedGrabs,
			DisabledTill: optTime(idx.DisabledTill),
			Exhausted:    idx.Exhausted,
		})
	}
	return oas.ProwlarrStatus{
		LinkID:    v.Link.ID,
		Name:      v.Link.Name,
		Reachable: v.Reachable,
		Error:     optString(v.Error),
		Version:   optString(v.Status.Version),
		CheckedAt: v.CheckedAt,
		Indexers:  idxOut,
	}
}
