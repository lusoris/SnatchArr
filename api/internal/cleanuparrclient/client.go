// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package cleanuparrclient reads status, strikes and Seeker configuration from
// a Cleanuparr server (`X-Api-Key` auth). Status and strikes go through
// goenvoy's arr/cleanuparr module; the Seeker configuration endpoints are not
// in goenvoy yet (golusoris/goenvoy#102) and stay here. Nothing is ever written.
package cleanuparrclient

import (
	"context"
	"fmt"
	"time"

	"github.com/golusoris/goenvoy/arr/cleanuparr"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// Status is what /api/status reports.
type Status struct {
	Version       string
	StartedAt     *time.Time
	MediaManagers map[string]int // instance count per *arr type as Cleanuparr names them
}

// Strike is one recent strike with its download.
type Strike struct {
	ID         string
	Type       string // Stalled, DownloadingMetadata, FailedImport, SlowSpeed, SlowTime, DeadTorrent
	CreatedAt  time.Time
	DownloadID string
	Title      string
	DryRun     bool
}

// StrikeSummary counts struck downloads.
type StrikeSummary struct {
	StruckDownloads   int
	MarkedForRemoval  int
	RemovedDownloads  int
	ReturningDownload int
}

// SeekerConfig is the configuration returned by /api/configuration/seeker.
type SeekerConfig struct {
	SearchEnabled          bool
	ProactiveSearchEnabled bool
	Instances              []SeekerInstance
}

// SeekerInstance is one *arr instance configured in Seeker.
type SeekerInstance struct {
	ArrInstanceID      string
	InstanceName       string
	InstanceType       string
	Enabled            bool
	ArrInstanceEnabled bool
}

// ArrInstanceRef is an *arr instance from /api/configuration/{sonarr,radarr}.
// ALLOW-LIST: read ONLY ID, Name, URL, Enabled. Never read, store, log or return apiKey.
type ArrInstanceRef struct {
	ID      string
	Name    string
	URL     string
	Enabled bool
}

// Client is the subset of the Cleanuparr API SnatchArr reads.
type Client interface {
	Status(ctx context.Context, baseURL, apiKey string) (Status, error)
	RecentStrikes(ctx context.Context, baseURL, apiKey string, count int) ([]Strike, error)
	Summary(ctx context.Context, baseURL, apiKey string) (StrikeSummary, error)
	SeekerConfig(ctx context.Context, baseURL, apiKey string) (SeekerConfig, error)
	GeneralDryRun(ctx context.Context, baseURL, apiKey string) (bool, error)
	ArrInstances(ctx context.Context, baseURL, apiKey string, appType string) ([]ArrInstanceRef, error)
}

// MaxRecent bounds RecentStrikes (HISS-02); goenvoy clamps to the same value.
const MaxRecent = cleanuparr.MaxRecentStrikes

// HTTP implements Client over goenvoy's base client (same X-Api-Key convention).
type HTTP struct {
	arrclient.BaseFactory
}

// New builds the client.
func New(timeout time.Duration, userAgent string) *HTTP {
	return &HTTP{Timeout: timeout, UserAgent: userAgent}
}

// goenvoy builds the goenvoy Cleanuparr client with the factory's timeout and user agent.
func (h *HTTP) goenvoy(baseURL, apiKey string) (*cleanuparr.Client, error) {
	c, err := cleanuparr.New(baseURL, apiKey, h.Options()...)
	if err != nil {
		return nil, fmt.Errorf("cleanuparr: client: %w", err)
	}
	return c, nil
}

// Status implements Client.
func (h *HTTP) Status(ctx context.Context, baseURL, apiKey string) (Status, error) {
	c, err := h.goenvoy(baseURL, apiKey)
	if err != nil {
		return Status{}, err
	}
	out, err := c.GetStatus(ctx)
	if err != nil {
		return Status{}, fmt.Errorf("%w: cleanuparr status: %w", domain.ErrUnreachable, err)
	}
	st := Status{Version: out.Application.Version, MediaManagers: make(map[string]int, len(out.MediaManagers))}
	if t, err := out.Application.StartTimeParsed(); err == nil {
		t = t.UTC()
		st.StartedAt = &t
	}
	for name, m := range out.MediaManagers {
		st.MediaManagers[name] = m.InstanceCount
	}
	return st, nil
}

// RecentStrikes implements Client: the newest strikes, at most MaxRecent.
func (h *HTTP) RecentStrikes(ctx context.Context, baseURL, apiKey string, count int) ([]Strike, error) {
	c, err := h.goenvoy(baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	list, err := c.GetRecentStrikes(ctx, count)
	if err != nil {
		return nil, fmt.Errorf("%w: cleanuparr strikes: %w", domain.ErrUnreachable, err)
	}
	out := make([]Strike, 0, len(list))
	for _, s := range list {
		out = append(out, Strike{ID: s.ID, Type: s.Type, CreatedAt: s.CreatedAt.UTC(), DownloadID: s.DownloadID, Title: s.Title, DryRun: s.IsDryRun})
	}
	return out, nil
}

// Summary implements Client: counts over the struck downloads (goenvoy reads at
// most 4 pages of 500).
func (h *HTTP) Summary(ctx context.Context, baseURL, apiKey string) (StrikeSummary, error) {
	c, err := h.goenvoy(baseURL, apiKey)
	if err != nil {
		return StrikeSummary{}, err
	}
	out, err := c.SummarizeStrikes(ctx)
	if err != nil {
		return StrikeSummary{}, fmt.Errorf("%w: cleanuparr strikes: %w", domain.ErrUnreachable, err)
	}
	return StrikeSummary{
		StruckDownloads:   out.StruckDownloads,
		MarkedForRemoval:  out.MarkedForRemoval,
		RemovedDownloads:  out.RemovedDownloads,
		ReturningDownload: out.ReturningDownloads,
	}, nil
}

//nolint:tagliatelle // Cleanuparr speaks camelCase
type seekerConfigJSON struct {
	SearchEnabled          bool `json:"searchEnabled"`
	ProactiveSearchEnabled bool `json:"proactiveSearchEnabled"`
	Instances              []struct {
		ArrInstanceID      string `json:"arrInstanceId"`
		InstanceName       string `json:"instanceName"`
		InstanceType       string `json:"instanceType"`
		Enabled            bool   `json:"enabled"`
		ArrInstanceEnabled bool   `json:"arrInstanceEnabled"`
	} `json:"instances"`
}

// SeekerConfig implements Client: returns Seeker configuration.
func (h *HTTP) SeekerConfig(ctx context.Context, baseURL, apiKey string) (SeekerConfig, error) {
	c, err := h.Base(baseURL, apiKey)
	if err != nil {
		return SeekerConfig{}, fmt.Errorf("cleanuparr: client: %w", err)
	}
	var out seekerConfigJSON
	if err := c.Get(ctx, "/api/configuration/seeker", &out); err != nil {
		return SeekerConfig{}, fmt.Errorf("cleanuparr seeker config: %w", err)
	}
	cfg := SeekerConfig{
		SearchEnabled:          out.SearchEnabled,
		ProactiveSearchEnabled: out.ProactiveSearchEnabled,
		Instances:              make([]SeekerInstance, 0, len(out.Instances)),
	}
	for _, inst := range out.Instances {
		cfg.Instances = append(cfg.Instances, SeekerInstance{
			ArrInstanceID:      inst.ArrInstanceID,
			InstanceName:       inst.InstanceName,
			InstanceType:       inst.InstanceType,
			Enabled:            inst.Enabled,
			ArrInstanceEnabled: inst.ArrInstanceEnabled,
		})
	}
	return cfg, nil
}

//nolint:tagliatelle // Cleanuparr speaks camelCase
type generalConfigJSON struct {
	DryRun bool `json:"dryRun"`
}

// GeneralDryRun implements Client: checks if Cleanuparr is in global dry-run mode.
func (h *HTTP) GeneralDryRun(ctx context.Context, baseURL, apiKey string) (bool, error) {
	c, err := h.Base(baseURL, apiKey)
	if err != nil {
		return false, fmt.Errorf("cleanuparr: client: %w", err)
	}
	var out generalConfigJSON
	if err := c.Get(ctx, "/api/configuration/general", &out); err != nil {
		return false, fmt.Errorf("cleanuparr general config: %w", err)
	}
	return out.DryRun, nil
}

// ALLOW-LIST: read ONLY id, name, url, enabled. Never read, store, log or return apiKey.
type arrConfigJSON struct {
	Instances []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		URL     string `json:"url"`
		Enabled bool   `json:"enabled"`
	} `json:"instances"`
}

// ArrInstances implements Client: reads configured instances for an app type (sonarr or radarr).
func (h *HTTP) ArrInstances(ctx context.Context, baseURL, apiKey string, appType string) ([]ArrInstanceRef, error) {
	c, err := h.Base(baseURL, apiKey)
	if err != nil {
		return nil, fmt.Errorf("cleanuparr: client: %w", err)
	}
	var out arrConfigJSON
	if err := c.Get(ctx, "/api/configuration/"+appType, &out); err != nil {
		return nil, fmt.Errorf("cleanuparr %s config: %w", appType, err)
	}
	refs := make([]ArrInstanceRef, 0, len(out.Instances))
	for _, inst := range out.Instances {
		refs = append(refs, ArrInstanceRef{
			ID:      inst.ID,
			Name:    inst.Name,
			URL:     inst.URL,
			Enabled: inst.Enabled,
		})
	}
	return refs, nil
}
