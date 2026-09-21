// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/seerr"
)

// ListSeerrLinks returns every Seerr link without secrets.
func (h *Handlers) ListSeerrLinks(ctx context.Context) ([]oas.SeerrLink, error) {
	list, err := h.seerr.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]oas.SeerrLink, 0, len(list))
	for _, l := range list {
		out = append(out, seerrLinkToOAS(l))
	}
	return out, nil
}

// GetSeerrLink returns one link.
func (h *Handlers) GetSeerrLink(ctx context.Context, params oas.GetSeerrLinkParams) (*oas.SeerrLink, error) {
	l, err := h.seerr.Get(ctx, params.LinkId)
	if err != nil {
		return nil, err
	}
	out := seerrLinkToOAS(l)
	return &out, nil
}

// CreateSeerrLink adds a link.
func (h *Handlers) CreateSeerrLink(ctx context.Context, req *oas.SeerrLinkInput) (*oas.SeerrLink, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	l, err := h.seerr.Create(ctx, seerrInputFromOAS(req))
	if err != nil {
		return nil, err
	}
	out := seerrLinkToOAS(l)
	return &out, nil
}

// UpdateSeerrLink edits a link.
func (h *Handlers) UpdateSeerrLink(ctx context.Context, req *oas.SeerrLinkInput, params oas.UpdateSeerrLinkParams) (*oas.SeerrLink, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	l, err := h.seerr.Update(ctx, params.LinkId, seerrInputFromOAS(req))
	if err != nil {
		return nil, err
	}
	out := seerrLinkToOAS(l)
	return &out, nil
}

// DeleteSeerrLink removes a link.
func (h *Handlers) DeleteSeerrLink(ctx context.Context, params oas.DeleteSeerrLinkParams) error {
	if err := requireAdmin(ctx); err != nil {
		return err
	}
	return h.seerr.Delete(ctx, params.LinkId)
}

// TestSeerrLink probes a stored link.
func (h *Handlers) TestSeerrLink(ctx context.Context, params oas.TestSeerrLinkParams) (*oas.ProbeResult, error) {
	st, err := h.seerr.Test(ctx, params.LinkId)
	if err != nil {
		return nil, err
	}
	return &oas.ProbeResult{AppName: "Seerr", Version: st.Version}, nil
}

// TestSeerrLinkInput probes unsaved credentials.
func (h *Handlers) TestSeerrLinkInput(ctx context.Context, req *oas.SeerrLinkInput) (*oas.ProbeResult, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	st, err := h.seerr.TestInput(ctx, seerrInputFromOAS(req))
	if err != nil {
		return nil, err
	}
	return &oas.ProbeResult{AppName: "Seerr", Version: st.Version}, nil
}

// SyncSeerrLink refreshes the request cache now.
func (h *Handlers) SyncSeerrLink(ctx context.Context, params oas.SyncSeerrLinkParams) (*oas.SeerrSyncResult, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	res, err := h.seerr.Sync(ctx, params.LinkId)
	if err != nil {
		return nil, err
	}
	return &oas.SeerrSyncResult{Seen: res.Seen, Resolved: res.Resolved, Unresolved: res.Unresolved, Removed: res.Removed}, nil
}

// ImportSeerrInstances creates instances from Seerr's server settings.
func (h *Handlers) ImportSeerrInstances(ctx context.Context, params oas.ImportSeerrInstancesParams) (*oas.SeerrImportResult, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	res, err := h.seerr.ImportInstances(ctx, params.LinkId)
	if err != nil {
		return nil, err
	}
	return &oas.SeerrImportResult{Created: emptyIfNil(res.Created), Skipped: emptyIfNil(res.Skipped)}, nil
}

// SnatchSeerrRequest queues a quickie focused on one request.
func (h *Handlers) SnatchSeerrRequest(ctx context.Context, params oas.SnatchSeerrRequestParams) (*oas.Run, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	run, err := h.seerr.SnatchRequest(ctx, params.LinkId, params.RequestId)
	if err != nil {
		return nil, err
	}
	out := runToOAS(run)
	return &out, nil
}

// ListSeerrRequests returns the cached requests with their snatch status.
func (h *Handlers) ListSeerrRequests(ctx context.Context) ([]oas.SeerrRequest, error) {
	list, err := h.seerr.Requests(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]oas.SeerrRequest, 0, len(list))
	for _, r := range list {
		out = append(out, seerrRequestToOAS(r))
	}
	return out, nil
}

func seerrLinkToOAS(l domain.SeerrLink) oas.SeerrLink {
	out := oas.SeerrLink{
		ID: l.ID, Name: l.Name, BaseURL: l.BaseURL, Enabled: l.Enabled,
		LastSeenVersion: optString(l.LastSeenVersion), LastCheckAt: optTime(l.LastCheckAt), LastError: optString(l.LastError),
		LastSyncAt: optTime(l.LastSyncAt), CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt,
	}
	if l.SonarrInstanceID != nil {
		out.SonarrInstanceID = oas.NewOptUUID(*l.SonarrInstanceID)
	}
	if l.RadarrInstanceID != nil {
		out.RadarrInstanceID = oas.NewOptUUID(*l.RadarrInstanceID)
	}
	return out
}

func seerrInputFromOAS(in *oas.SeerrLinkInput) seerr.Input {
	out := seerr.Input{Name: in.Name, BaseURL: in.BaseURL, APIKey: in.APIKey.Or(""), Enabled: in.Enabled.Or(true)}
	if v, ok := in.SonarrInstanceID.Get(); ok {
		out.SonarrInstanceID = &v
	}
	if v, ok := in.RadarrInstanceID.Get(); ok {
		out.RadarrInstanceID = &v
	}
	return out
}

func seerrRequestToOAS(r seerr.RequestView) oas.SeerrRequest {
	seasons := make([]int, 0, len(r.Seasons))
	seasons = append(seasons, r.Seasons...)
	out := oas.SeerrRequest{
		LinkID: r.LinkID, LinkName: r.LinkName, RequestID: r.RequestID, MediaType: oas.SeerrRequestMediaType(r.MediaType),
		TmdbID: r.TmdbID, TvdbID: r.TvdbID, Title: r.Title, RequestStatus: r.RequestStatus, MediaStatus: r.MediaStatus,
		Is4k: r.Is4K, RequestedBy: r.RequestedBy, Seasons: seasons, InstanceName: optString(r.InstanceName),
		Resolved: r.Resolved(), UnresolvedReason: optString(seerr.UnresolvedReason(r.SeerrRequest)),
		RequestedAt: optTime(r.RequestedAt), LastSnatchedAt: optTime(r.LastSnatchedAt), LastSeenAt: r.LastSeenAt,
	}
	if r.InstanceID != nil {
		out.InstanceID = oas.NewOptUUID(*r.InstanceID)
	}
	if r.EntityID != nil {
		out.EntityID = oas.NewOptInt64(*r.EntityID)
	}
	return out
}
