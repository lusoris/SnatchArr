// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package cleanuparrclient reads status and strikes from a Cleanuparr server
// (`X-Api-Key` auth, `/api/status`, `/api/strikes`). Nothing is ever written.
package cleanuparrclient

import (
	"context"
	"fmt"
	"time"

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

// Bounds (HISS-02).
const (
	MaxRecent      = 50
	summaryPage    = 500
	maxSummaryPage = 4
)

// HTTP implements Client over goenvoy's base client (same X-Api-Key convention).
type HTTP struct {
	arrclient.BaseFactory
}

// New builds the client.
func New(timeout time.Duration, userAgent string) *HTTP {
	return &HTTP{Timeout: timeout, UserAgent: userAgent}
}

//nolint:tagliatelle // Cleanuparr speaks camelCase
type statusJSON struct {
	Application struct {
		Version   string `json:"version"`
		StartTime string `json:"startTime"`
	} `json:"application"`
	MediaManagers map[string]struct {
		InstanceCount int `json:"instanceCount"`
	} `json:"mediaManagers"`
}

// Status implements Client.
func (h *HTTP) Status(ctx context.Context, baseURL, apiKey string) (Status, error) {
	c, err := h.Base(baseURL, apiKey)
	if err != nil {
		return Status{}, fmt.Errorf("cleanuparr: client: %w", err)
	}
	var out statusJSON
	if err := c.Get(ctx, "/api/status", &out); err != nil {
		return Status{}, fmt.Errorf("%w: cleanuparr status: %w", domain.ErrUnreachable, err)
	}
	st := Status{Version: out.Application.Version, MediaManagers: make(map[string]int, len(out.MediaManagers))}
	if t, err := time.Parse(time.RFC3339Nano, out.Application.StartTime); err == nil {
		t = t.UTC()
		st.StartedAt = &t
	}
	for name, m := range out.MediaManagers {
		st.MediaManagers[name] = m.InstanceCount
	}
	return st, nil
}

//nolint:tagliatelle // Cleanuparr speaks camelCase
type recentJSON struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	CreatedAt  string `json:"createdAt"`
	DownloadID string `json:"downloadId"`
	Title      string `json:"title"`
	IsDryRun   bool   `json:"isDryRun"`
}

// RecentStrikes implements Client: the newest strikes, at most MaxRecent.
func (h *HTTP) RecentStrikes(ctx context.Context, baseURL, apiKey string, count int) ([]Strike, error) {
	c, err := h.Base(baseURL, apiKey)
	if err != nil {
		return nil, fmt.Errorf("cleanuparr: client: %w", err)
	}
	var list []recentJSON
	if err := c.Get(ctx, fmt.Sprintf("/api/strikes/recent?count=%d", min(max(count, 1), MaxRecent)), &list); err != nil {
		return nil, fmt.Errorf("%w: cleanuparr strikes: %w", domain.ErrUnreachable, err)
	}
	out := make([]Strike, 0, len(list))
	for _, s := range list {
		created, _ := time.Parse(time.RFC3339Nano, s.CreatedAt)
		out = append(out, Strike{ID: s.ID, Type: s.Type, CreatedAt: created.UTC(), DownloadID: s.DownloadID, Title: s.Title, DryRun: s.IsDryRun})
	}
	return out, nil
}

//nolint:tagliatelle // Cleanuparr speaks camelCase
type strikesPage struct {
	Items []struct {
		IsMarkedForRemoval bool `json:"isMarkedForRemoval"`
		IsRemoved          bool `json:"isRemoved"`
		IsReturning        bool `json:"isReturning"`
	} `json:"items"`
	TotalCount int `json:"totalCount"`
	TotalPages int `json:"totalPages"`
}

// Summary implements Client: counts over the struck downloads (bounded pages).
func (h *HTTP) Summary(ctx context.Context, baseURL, apiKey string) (StrikeSummary, error) {
	c, err := h.Base(baseURL, apiKey)
	if err != nil {
		return StrikeSummary{}, fmt.Errorf("cleanuparr: client: %w", err)
	}
	var sum StrikeSummary
	for page := 1; page <= maxSummaryPage; page++ {
		var out strikesPage
		if err := c.Get(ctx, fmt.Sprintf("/api/strikes?page=%d&pageSize=%d", page, summaryPage), &out); err != nil {
			return StrikeSummary{}, fmt.Errorf("%w: cleanuparr strikes: %w", domain.ErrUnreachable, err)
		}
		sum.StruckDownloads = out.TotalCount
		tally(&sum, out)
		if page >= out.TotalPages {
			break
		}
	}
	return sum, nil
}

func tally(sum *StrikeSummary, page strikesPage) {
	for _, item := range page.Items {
		if item.IsMarkedForRemoval {
			sum.MarkedForRemoval++
		}
		if item.IsRemoved {
			sum.RemovedDownloads++
		}
		if item.IsReturning {
			sum.ReturningDownload++
		}
	}
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
