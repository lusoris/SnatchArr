// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package seerrclient reads what SnatchArr needs from a Seerr server: status, the
// approved-but-unavailable requests, the Sonarr/Radarr servers Seerr sends them to, and
// titles. It uses goenvoy's typed seerr client.
package seerrclient

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/golusoris/goenvoy/arr/seerr"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
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
type Request struct {
	ID          int
	Type        string
	Status      int
	Is4K        bool
	ServerID    *int
	CreatedAt   string
	RequestedBy struct {
		DisplayName string
		Username    string
		Email       string
	}
	Seasons []struct {
		SeasonNumber int
	}
	Media struct {
		ID        int
		MediaType string
		TmdbID    int
		TvdbID    *int
		Status    int
		Status4K  int
	}
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

// HTTP implements Client over goenvoy's typed seerr client.
type HTTP struct {
	arrclient.BaseFactory
}

// New builds the client.
func New(timeout time.Duration, userAgent string) *HTTP {
	return &HTTP{Timeout: timeout, UserAgent: userAgent}
}

// Status implements Client.
func (h *HTTP) Status(ctx context.Context, baseURL, apiKey string) (Status, error) {
	c, err := seerr.New(baseURL, apiKey, h.Options()...)
	if err != nil {
		return Status{}, fmt.Errorf("seerr: client: %w", err)
	}
	st, err := c.GetStatus(ctx)
	if err != nil {
		return Status{}, fmt.Errorf("%w: seerr status: %w", domain.ErrUnreachable, err)
	}
	return Status{Version: st.Version}, nil
}

func mapRequest(mr seerr.MediaRequest) Request {
	var serverID *int
	if mr.ServerAssigned {
		sid := mr.ServerID
		serverID = &sid
	}
	r := Request{
		ID:        mr.ID,
		Type:      mr.Type,
		Status:    mr.Status,
		Is4K:      mr.Is4k,
		ServerID:  serverID,
		CreatedAt: mr.CreatedAt,
	}
	if mr.RequestedBy != nil {
		r.RequestedBy.DisplayName = mr.RequestedBy.DisplayName
		r.RequestedBy.Username = mr.RequestedBy.Username
		r.RequestedBy.Email = mr.RequestedBy.Email
	}
	if mr.Seasons != nil {
		r.Seasons = make([]struct{ SeasonNumber int }, len(*mr.Seasons))
		for i, s := range *mr.Seasons {
			r.Seasons[i].SeasonNumber = s.SeasonNumber
		}
	}
	if mr.Media != nil {
		r.Media.ID = mr.Media.ID
		r.Media.MediaType = mr.Media.MediaType
		r.Media.TmdbID = mr.Media.TmdbID
		r.Media.TvdbID = mr.Media.TvdbID
		r.Media.Status = mr.Media.Status
		r.Media.Status4K = mr.Media.Status4k
	}
	return r
}

// ProcessingRequests implements Client: approved requests whose media is not available
// yet (`filter=processing`), every page up to the bound.
func (h *HTTP) ProcessingRequests(ctx context.Context, baseURL, apiKey string) ([]Request, error) {
	c, err := seerr.New(baseURL, apiKey, h.Options()...)
	if err != nil {
		return nil, fmt.Errorf("seerr: client: %w", err)
	}
	var all []Request
	for page := range maxPages {
		results, pageInfo, err := c.GetRequests(ctx, pageSize, page*pageSize, "processing")
		if err != nil {
			return nil, fmt.Errorf("%w: seerr requests: %w", domain.ErrUnreachable, err)
		}
		for _, mr := range results {
			all = append(all, mapRequest(mr))
		}
		if len(results) < pageSize || pageInfo == nil || page+1 >= pageInfo.Pages {
			break
		}
	}
	return all, nil
}

func mapServers(kind domain.AppKind, list []seerr.ServiceSettings) []Server {
	out := make([]Server, len(list))
	for i, s := range list {
		out[i] = Server{
			Kind:      kind,
			ID:        s.ID,
			Name:      s.Name,
			BaseURL:   ServerURL(s.Hostname, s.Port, s.UseSSL, s.BaseURL),
			APIKey:    s.APIKey,
			Is4K:      s.Is4k,
			IsDefault: s.IsDefault,
		}
	}
	return out
}

// Servers implements Client: Seerr's Sonarr and Radarr settings, API keys included.
func (h *HTTP) Servers(ctx context.Context, baseURL, apiKey string) ([]Server, error) {
	c, err := seerr.New(baseURL, apiKey, h.Options()...)
	if err != nil {
		return nil, fmt.Errorf("seerr: client: %w", err)
	}
	sonarr, err := c.GetSonarrSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: seerr sonarr settings: %w", domain.ErrUnreachable, err)
	}
	radarr, err := c.GetRadarrSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: seerr radarr settings: %w", domain.ErrUnreachable, err)
	}
	out := append(mapServers(domain.KindSonarr, sonarr), mapServers(domain.KindRadarr, radarr)...)
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
	c, err := seerr.New(baseURL, apiKey, h.Options()...)
	if err != nil {
		return "", fmt.Errorf("seerr: client: %w", err)
	}
	switch mediaType {
	case domain.SeerrMovie:
		m, err := c.GetMovie(ctx, tmdbID)
		if err != nil {
			return "", fmt.Errorf("%w: seerr title: %w", domain.ErrUnreachable, err)
		}
		return m.Title, nil
	case domain.SeerrTV:
		tv, err := c.GetTV(ctx, tmdbID)
		if err != nil {
			return "", fmt.Errorf("%w: seerr title: %w", domain.ErrUnreachable, err)
		}
		return tv.Name, nil
	default:
		return "", fmt.Errorf("seerr: title: unknown media type %q", mediaType)
	}
}

// MaxTitleLookups bounds title fetches per sync so a big backlog never stalls it.
const MaxTitleLookups = maxTitleHits
