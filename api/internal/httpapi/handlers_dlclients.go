// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/dlclients"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// ListDownloadClients returns every download client without secrets.
func (h *Handlers) ListDownloadClients(ctx context.Context) ([]oas.DownloadClient, error) {
	list, err := h.clients.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]oas.DownloadClient, 0, len(list))
	for _, c := range list {
		out = append(out, downloadClientToOAS(c))
	}
	return out, nil
}

// GetDownloadClient returns one download client.
func (h *Handlers) GetDownloadClient(ctx context.Context, params oas.GetDownloadClientParams) (*oas.DownloadClient, error) {
	c, err := h.clients.Get(ctx, params.ClientId)
	if err != nil {
		return nil, err
	}
	out := downloadClientToOAS(c)
	return &out, nil
}

// CreateDownloadClient adds a manual download client.
func (h *Handlers) CreateDownloadClient(ctx context.Context, req *oas.DownloadClientInput) (*oas.DownloadClient, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	c, err := h.clients.Create(ctx, downloadClientInputFromOAS(req))
	if err != nil {
		return nil, err
	}
	out := downloadClientToOAS(c)
	return &out, nil
}

// UpdateDownloadClient edits a download client (an omitted secret keeps the stored one).
func (h *Handlers) UpdateDownloadClient(ctx context.Context, req *oas.DownloadClientInput, params oas.UpdateDownloadClientParams) (*oas.DownloadClient, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	c, err := h.clients.Update(ctx, params.ClientId, downloadClientInputFromOAS(req))
	if err != nil {
		return nil, err
	}
	out := downloadClientToOAS(c)
	return &out, nil
}

// DeleteDownloadClient removes a download client.
func (h *Handlers) DeleteDownloadClient(ctx context.Context, params oas.DeleteDownloadClientParams) error {
	if err := requireAdmin(ctx); err != nil {
		return err
	}
	return h.clients.Delete(ctx, params.ClientId)
}

// TestDownloadClient observes a stored client now.
func (h *Handlers) TestDownloadClient(ctx context.Context, params oas.TestDownloadClientParams) (*oas.DownloadClientStatus, error) {
	snap, err := h.clients.Test(ctx, params.ClientId)
	if err != nil {
		return nil, err
	}
	out := snapshotToOAS(snap)
	return &out, nil
}

// TestDownloadClientInput observes credentials that are not stored yet.
func (h *Handlers) TestDownloadClientInput(ctx context.Context, req *oas.DownloadClientInput) (*oas.DownloadClientStatus, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	snap, err := h.clients.TestInput(ctx, downloadClientInputFromOAS(req))
	if err != nil {
		return nil, err
	}
	out := snapshotToOAS(snap)
	return &out, nil
}

// GetDownloadClientStatus returns the latest observation of every enabled client.
func (h *Handlers) GetDownloadClientStatus(ctx context.Context) ([]oas.DownloadClientStatus, error) {
	snaps, err := h.clients.Status(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]oas.DownloadClientStatus, 0, len(snaps))
	for _, s := range snaps {
		out = append(out, snapshotToOAS(s))
	}
	return out, nil
}

// DiscoverDownloadClients imports an instance's configured download clients.
func (h *Handlers) DiscoverDownloadClients(ctx context.Context, params oas.DiscoverDownloadClientsParams) (*oas.DiscoveryResult, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	res, err := h.clients.Discover(ctx, params.InstanceId)
	if err != nil {
		return nil, err
	}
	return &oas.DiscoveryResult{
		Imported: res.Imported, Updated: res.Updated, Removed: res.Removed,
		Unsupported: emptyIfNil(res.Unsupported), Warnings: emptyIfNil(res.Warnings),
	}, nil
}

func emptyIfNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

func downloadClientToOAS(c domain.DownloadClient) oas.DownloadClient {
	out := oas.DownloadClient{
		ID: c.ID, Kind: oas.DownloadClientKind(c.Kind), Protocol: oas.DownloadClientProtocol(c.Kind.Protocol()),
		Name: c.Name, BaseURL: c.BaseURL, Username: c.Username, Enabled: c.Enabled,
		Source: oas.DownloadClientSource(c.Source), MaxActive: c.MaxActive, BandwidthBudgetBps: c.BandwidthBudgetBPS,
		LastCheckAt: optTime(c.LastCheckAt), LastError: optString(c.LastError), CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
	if c.InstanceID != nil {
		out.InstanceID = oas.NewOptUUID(*c.InstanceID)
	}
	return out
}

func downloadClientInputFromOAS(in *oas.DownloadClientInput) dlclients.Input {
	out := dlclients.Input{
		Kind: domain.DownloadClientKind(in.Kind), Name: in.Name, BaseURL: in.BaseURL,
		Username: in.Username.Or(""), Secret: in.Secret.Or(""), Enabled: in.Enabled.Or(true),
		MaxActive: in.MaxActive.Or(0), BandwidthBudgetBPS: in.BandwidthBudgetBps.Or(0),
	}
	if v, ok := in.InstanceID.Get(); ok {
		out.InstanceID = &v
	}
	return out
}

func snapshotToOAS(s dlclients.Snapshot) oas.DownloadClientStatus {
	out := oas.DownloadClientStatus{
		Name: s.Name, Kind: oas.DownloadClientKind(s.Kind), Reachable: s.Reachable, Paused: s.Paused,
		Active: s.Active, Queued: s.Queued, DownloadRateBps: s.DownloadRate, UploadRateBps: s.UploadRate,
		Version: optString(s.Version), Error: optString(s.Error), CheckedAt: s.CheckedAt,
	}
	if s.ClientID != [16]byte{} {
		out.ClientID = oas.NewOptUUID(s.ClientID)
	}
	return out
}
