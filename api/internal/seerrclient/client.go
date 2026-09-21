// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package seerrclient reads what SnatchArr needs from a Seerr server: status, the
// approved-but-unavailable requests, the Sonarr/Radarr servers Seerr sends them to, and
// titles. It uses goenvoy's shared base client with SnatchArr's own wire types because
// the typed goenvoy seerr package drops the request/media type fields.
package seerrclient

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/golusoris/goenvoy/arr/v2"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// Bounds (HISS-02).
const (
	pageSize     = 100
	maxPages     = 20
	maxTitleHits = 25
)

// Status is what a successful probe learns.
type Status struct {
	Version string
}

// Request is one Seerr media request with the fields SnatchArr maps.
//
//nolint:tagliatelle // Seerr speaks camelCase
type Request struct {
	ID          int    `json:"id"`
	Type        string `json:"type"`
	Status      int    `json:"status"`
	Is4K        bool   `json:"is4k"`
	ServerID    *int   `json:"serverId"`
	CreatedAt   string `json:"createdAt"`
	RequestedBy struct {
		DisplayName string `json:"displayName"`
		Username    string `json:"username"`
		Email       string `json:"email"`
	} `json:"requestedBy"`
	Seasons []struct {
		SeasonNumber int `json:"seasonNumber"`
	} `json:"seasons"`
	Media struct {
		ID        int    `json:"id"`
		MediaType string `json:"mediaType"`
		TmdbID    int    `json:"tmdbId"`
		TvdbID    *int   `json:"tvdbId"`
		Status    int    `json:"status"`
		Status4K  int    `json:"status4k"`
	} `json:"media"`
}

// Requester renders the requesting user.
func (r Request) Requester() string {
	for _, s := range []string{r.RequestedBy.DisplayName, r.RequestedBy.Username, r.RequestedBy.Email} {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// Server is one Sonarr or Radarr server configured in Seerr.
type Server struct {
	Kind      domain.AppKind
	ID        int
	Name      string
	BaseURL   string
	APIKey    string
	Is4K      bool
	IsDefault bool
}

// Client is the subset of the Seerr API SnatchArr reads.
type Client interface {
	Status(ctx context.Context, baseURL, apiKey string) (Status, error)
	ProcessingRequests(ctx context.Context, baseURL, apiKey string) ([]Request, error)
	Servers(ctx context.Context, baseURL, apiKey string) ([]Server, error)
	Title(ctx context.Context, baseURL, apiKey string, mediaType domain.SeerrMediaType, tmdbID int) (string, error)
}

// HTTP implements Client over goenvoy's base client.
type HTTP struct {
	timeout   time.Duration
	userAgent string
}

// New builds the client.
func New(timeout time.Duration, userAgent string) *HTTP {
	return &HTTP{timeout: timeout, userAgent: userAgent}
}

func (h *HTTP) base(baseURL, apiKey string) (*arr.BaseClient, error) {
	c, err := arr.NewBaseClient(baseURL, apiKey, arr.WithTimeout(h.timeout), arr.WithUserAgent(h.userAgent))
	if err != nil {
		return nil, fmt.Errorf("seerr: client: %w", err)
	}
	return c, nil
}

// Status implements Client.
func (h *HTTP) Status(ctx context.Context, baseURL, apiKey string) (Status, error) {
	c, err := h.base(baseURL, apiKey)
	if err != nil {
		return Status{}, err
	}
	var out struct {
		Version string `json:"version"`
	}
	if err := c.Get(ctx, "/api/v1/status", &out); err != nil {
		return Status{}, fmt.Errorf("%w: seerr status: %w", domain.ErrUnreachable, err)
	}
	return Status{Version: out.Version}, nil
}

//nolint:tagliatelle // Seerr speaks camelCase
type pagedRequests struct {
	PageInfo struct {
		Pages   int `json:"pages"`
		Results int `json:"results"`
	} `json:"pageInfo"`
	Results []Request `json:"results"`
}

// ProcessingRequests implements Client: approved requests whose media is not available
// yet (`filter=processing`), every page up to the bound.
func (h *HTTP) ProcessingRequests(ctx context.Context, baseURL, apiKey string) ([]Request, error) {
	c, err := h.base(baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	var all []Request
	for page := range maxPages {
		var out pagedRequests
		path := fmt.Sprintf("/api/v1/request?filter=processing&take=%d&skip=%d&sort=added", pageSize, page*pageSize)
		if err := c.Get(ctx, path, &out); err != nil {
			return nil, fmt.Errorf("%w: seerr requests: %w", domain.ErrUnreachable, err)
		}
		all = append(all, out.Results...)
		if len(out.Results) < pageSize || page+1 >= out.PageInfo.Pages {
			break
		}
	}
	return all, nil
}

//nolint:tagliatelle // Seerr speaks camelCase
type serverJSON struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Hostname  string `json:"hostname"`
	Port      int    `json:"port"`
	APIKey    string `json:"apiKey"`
	UseSSL    bool   `json:"useSsl"`
	BaseURL   string `json:"baseUrl"`
	Is4K      bool   `json:"is4k"`
	IsDefault bool   `json:"isDefault"`
}

// Servers implements Client: Seerr's Sonarr and Radarr settings, API keys included.
func (h *HTTP) Servers(ctx context.Context, baseURL, apiKey string) ([]Server, error) {
	c, err := h.base(baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	var out []Server
	for _, kind := range []domain.AppKind{domain.KindSonarr, domain.KindRadarr} {
		var list []serverJSON
		if err := c.Get(ctx, "/api/v1/settings/"+string(kind), &list); err != nil {
			return nil, fmt.Errorf("%w: seerr %s settings: %w", domain.ErrUnreachable, kind, err)
		}
		for _, s := range list {
			out = append(out, Server{
				Kind: kind, ID: s.ID, Name: s.Name, BaseURL: ServerURL(s.Hostname, s.Port, s.UseSSL, s.BaseURL),
				APIKey: s.APIKey, Is4K: s.Is4K, IsDefault: s.IsDefault,
			})
		}
	}
	return out, nil
}

// ServerURL rebuilds the *arr URL from Seerr's hostname/port/useSsl/baseUrl fields.
func ServerURL(hostname string, port int, useSSL bool, base string) string {
	scheme := "http"
	if useSSL {
		scheme = "https"
	}
	host := hostname
	if port > 0 {
		host = net.JoinHostPort(hostname, strconv.Itoa(port))
	}
	u := url.URL{Scheme: scheme, Host: host}
	if p := strings.Trim(base, "/"); p != "" {
		u.Path = "/" + p
	}
	return u.String()
}

// Title implements Client: the TMDB title Seerr knows for a movie or show.
func (h *HTTP) Title(ctx context.Context, baseURL, apiKey string, mediaType domain.SeerrMediaType, tmdbID int) (string, error) {
	c, err := h.base(baseURL, apiKey)
	if err != nil {
		return "", err
	}
	var out struct {
		Title string `json:"title"`
		Name  string `json:"name"`
	}
	if err := c.Get(ctx, fmt.Sprintf("/api/v1/%s/%d", mediaType, tmdbID), &out); err != nil {
		return "", fmt.Errorf("%w: seerr title: %w", domain.ErrUnreachable, err)
	}
	if out.Title != "" {
		return out.Title, nil
	}
	return out.Name, nil
}

// MaxTitleLookups bounds title fetches per sync so a big backlog never stalls it.
const MaxTitleLookups = maxTitleHits
