// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package seerrclient reads what SnatchArr needs from a Seerr server: status, the
// approved-but-unavailable requests, the Sonarr/Radarr servers Seerr sends them to, and
// titles. It is a thin adapter over goenvoy's typed seerr client: one Seerr URL and key per
// call, a bound on paging, and the SnatchArr Server shape.
package seerrclient

import (
	"context"
	"fmt"
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

// Request is one Seerr media request. Read ServerAssigned to tell server 0 from a request
// without a server.
type Request = seerr.MediaRequest

// Media is the media record a request points at.
type Media = seerr.MediaInfo

// MediaOf returns the request's media record, or the zero record when Seerr sent none.
func MediaOf(r Request) Media {
	if r.Media == nil {
		return Media{}
	}
	return *r.Media
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

// HTTP implements Client over goenvoy's seerr client.
type HTTP struct {
	factory arrclient.BaseFactory
}

// New builds the client.
func New(timeout time.Duration, userAgent string) *HTTP {
	return &HTTP{factory: arrclient.BaseFactory{Timeout: timeout, UserAgent: userAgent}}
}

func (h *HTTP) client(baseURL, apiKey string) (*seerr.Client, error) {
	c, err := seerr.New(baseURL, apiKey, h.factory.Options()...)
	if err != nil {
		return nil, fmt.Errorf("seerr: client: %w", err)
	}
	return c, nil
}

// Status implements Client.
func (h *HTTP) Status(ctx context.Context, baseURL, apiKey string) (Status, error) {
	c, err := h.client(baseURL, apiKey)
	if err != nil {
		return Status{}, err
	}
	out, err := c.GetStatus(ctx)
	if err != nil {
		return Status{}, fmt.Errorf("%w: seerr status: %w", domain.ErrUnreachable, arrclient.HintAPIError(err, "Seerr"))
	}
	return Status{Version: out.Version}, nil
}

// ProcessingRequests implements Client: approved requests whose media is not available
// yet (`filter=processing`), every page up to the bound. Seerr orders by request id when no
// sort is given, so skip-based paging stays stable.
func (h *HTTP) ProcessingRequests(ctx context.Context, baseURL, apiKey string) ([]Request, error) {
	c, err := h.client(baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	var all []Request
	for page := range maxPages {
		results, info, err := c.GetRequests(ctx, pageSize, page*pageSize, "processing")
		if err != nil {
			return nil, fmt.Errorf("%w: seerr requests: %w", domain.ErrUnreachable, arrclient.HintAPIError(err, "Seerr"))
		}
		all = append(all, results...)
		if len(results) < pageSize || page+1 >= info.Pages {
			break
		}
	}
	return all, nil
}

// Servers implements Client: Seerr's Sonarr and Radarr settings, API keys included.
func (h *HTTP) Servers(ctx context.Context, baseURL, apiKey string) ([]Server, error) {
	c, err := h.client(baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	var out []Server
	for _, kind := range []domain.AppKind{domain.KindSonarr, domain.KindRadarr} {
		get := c.GetSonarrSettings
		if kind == domain.KindRadarr {
			get = c.GetRadarrSettings
		}
		list, err := get(ctx)
		if err != nil {
			return nil, fmt.Errorf("%w: seerr %s settings: %w", domain.ErrUnreachable, kind, arrclient.HintAPIError(err, "Seerr"))
		}
		for _, s := range list {
			out = append(out, Server{
				Kind: kind, ID: s.ID, Name: s.Name, BaseURL: s.URL(),
				APIKey: s.APIKey, Is4K: s.Is4k, IsDefault: s.IsDefault,
			})
		}
	}
	return out, nil
}

// Title implements Client: the TMDB title Seerr knows for a movie or show.
func (h *HTTP) Title(ctx context.Context, baseURL, apiKey string, mediaType domain.SeerrMediaType, tmdbID int) (string, error) {
	c, err := h.client(baseURL, apiKey)
	if err != nil {
		return "", err
	}
	if mediaType == domain.SeerrTV {
		show, tvErr := c.GetTV(ctx, tmdbID)
		if tvErr != nil {
			return "", fmt.Errorf("%w: seerr title: %w", domain.ErrUnreachable, arrclient.HintAPIError(tvErr, "Seerr"))
		}
		return show.Name, nil
	}
	movie, err := c.GetMovie(ctx, tmdbID)
	if err != nil {
		return "", fmt.Errorf("%w: seerr title: %w", domain.ErrUnreachable, arrclient.HintAPIError(err, "Seerr"))
	}
	return movie.Title, nil
}

// MaxTitleLookups bounds title fetches per sync so a big backlog never stalls it.
const MaxTitleLookups = maxTitleHits
