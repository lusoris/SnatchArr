// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package arrclient is the Go side's thin use of goenvoy: connectivity probes and queue
// sizes. Hunting itself lives in the Rust worker.
package arrclient

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/golusoris/goenvoy/arr/lidarr"
	"github.com/golusoris/goenvoy/arr/radarr"
	"github.com/golusoris/goenvoy/arr/readarr"
	"github.com/golusoris/goenvoy/arr/sonarr"
	"github.com/golusoris/goenvoy/arr/v2"
	"github.com/golusoris/goenvoy/arr/whisparr"
	"go.uber.org/fx"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// Result is what a successful probe learns about an instance.
type Result struct {
	AppName string
	Version string
}

// Prober checks that an *arr instance answers with the expected API.
type Prober interface {
	Probe(ctx context.Context, kind domain.AppKind, baseURL, apiKey string) (Result, error)
}

// Discoverer lists the download clients an *arr instance has configured.
type Discoverer interface {
	DownloadClients(ctx context.Context, kind domain.AppKind, baseURL, apiKey string) ([]arr.ProviderResource, error)
}

// Options tune the probe.
type Options struct {
	Timeout   time.Duration
	UserAgent string
}

// DefaultOptions returns a 15 s timeout and the product User-Agent.
func DefaultOptions() Options {
	return Options{Timeout: 15 * time.Second, UserAgent: "SnatchArr/1.0 (https://github.com/lusoris/SnatchArr)"}
}

// GoenvoyProber probes through goenvoy's typed clients.
type GoenvoyProber struct {
	opts Options
}

// NewGoenvoyProber builds a prober.
func NewGoenvoyProber(opts Options) *GoenvoyProber {
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultOptions().Timeout
	}
	if opts.UserAgent == "" {
		opts.UserAgent = DefaultOptions().UserAgent
	}
	return &GoenvoyProber{opts: opts}
}

// Probe calls GET /api/v{1,3}/system/status and validates the API generation for
// Whisparr, whose v2 and v3 share a URL shape but not a data model.
func (p *GoenvoyProber) Probe(ctx context.Context, kind domain.AppKind, baseURL, apiKey string) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, p.opts.Timeout)
	defer cancel()
	st, err := p.status(ctx, kind, baseURL, apiKey)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", domain.ErrUnreachable, err)
	}
	res := Result{AppName: st.AppName, Version: st.Version}
	if err := checkWhisparrGeneration(kind, st.Version); err != nil {
		return res, err
	}
	return res, nil
}

func (p *GoenvoyProber) clientOptions() []arr.Option {
	return []arr.Option{arr.WithTimeout(p.opts.Timeout), arr.WithUserAgent(p.opts.UserAgent)}
}

// arrClient is the slice of every goenvoy *arr client the control plane uses.
type arrClient interface {
	GetSystemStatus(ctx context.Context) (*arr.StatusResponse, error)
	GetDownloadClients(ctx context.Context) ([]arr.ProviderResource, error)
}

// newClient builds the goenvoy client for a kind. Kept non-generic on purpose: govulncheck
// v1.4.0's call-graph analysis panics on generic helpers ("ForEachElement called on type
// containing *types.TypeParam").
func (p *GoenvoyProber) newClient(kind domain.AppKind, baseURL, apiKey string) (arrClient, error) {
	o := p.clientOptions()
	switch kind {
	case domain.KindSonarr:
		return wrapNew(sonarr.New(baseURL, apiKey, o...))
	case domain.KindRadarr:
		return wrapNew(radarr.New(baseURL, apiKey, o...))
	case domain.KindLidarr:
		return wrapNew(lidarr.New(baseURL, apiKey, o...))
	case domain.KindReadarr:
		return wrapNew(readarr.New(baseURL, apiKey, o...))
	case domain.KindWhisparrV2:
		return wrapNew(whisparr.New(baseURL, apiKey, o...))
	case domain.KindWhisparrV3:
		return wrapNew(whisparr.NewV3(baseURL, apiKey, o...))
	default:
		return nil, fmt.Errorf("%w: unsupported kind %q", domain.ErrInvalid, kind)
	}
}

// wrapNew folds a goenvoy constructor result into the interface and wraps its error.
func wrapNew(c arrClient, err error) (arrClient, error) {
	if err != nil {
		return nil, fmt.Errorf("arrclient: new client: %w", err)
	}
	return c, nil
}

func (p *GoenvoyProber) status(ctx context.Context, kind domain.AppKind, baseURL, apiKey string) (*arr.StatusResponse, error) {
	c, err := p.newClient(kind, baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	return c.GetSystemStatus(ctx)
}

// DownloadClients implements Discoverer: GET /api/v{1,3}/downloadclient.
func (p *GoenvoyProber) DownloadClients(ctx context.Context, kind domain.AppKind, baseURL, apiKey string) ([]arr.ProviderResource, error) {
	ctx, cancel := context.WithTimeout(ctx, p.opts.Timeout)
	defer cancel()
	c, err := p.newClient(kind, baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	list, err := c.GetDownloadClients(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrUnreachable, err)
	}
	return list, nil
}

func checkWhisparrGeneration(kind domain.AppKind, version string) error {
	major, _, _ := strings.Cut(version, ".")
	switch kind {
	case domain.KindWhisparrV2:
		if major != "2" {
			return fmt.Errorf("%w: expected Whisparr v2, instance reports %s", domain.ErrInvalid, version)
		}
	case domain.KindWhisparrV3:
		if major != "3" {
			return fmt.Errorf("%w: expected Whisparr v3 (Eros), instance reports %s", domain.ErrInvalid, version)
		}
	case domain.KindSonarr, domain.KindRadarr, domain.KindLidarr, domain.KindReadarr:
		// Only Whisparr has two incompatible API generations behind one URL shape.
	}
	return nil
}

// Module provides the goenvoy-backed Prober and Discoverer.
var Module = fx.Module("snatcharr.arrclient",
	fx.Provide(
		func() *GoenvoyProber { return NewGoenvoyProber(DefaultOptions()) },
		func(p *GoenvoyProber) Prober { return p },
		func(p *GoenvoyProber) Discoverer { return p },
	),
)
