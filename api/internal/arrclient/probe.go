// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package arrclient is the Go side's thin use of goenvoy: connectivity probes and queue
// sizes. Snatching itself lives in the Rust worker.
package arrclient

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
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
	"github.com/lusoris/SnatchArr/api/internal/queue"
)

// Result is what a successful probe learns about an instance.
type Result struct {
	AppName string
	Version string
	// Product is set for a kind that several products serve, and ProductNone otherwise.
	Product domain.Product
}

// Prober checks that an *arr instance answers with the expected API.
type Prober interface {
	Probe(ctx context.Context, kind domain.AppKind, baseURL, apiKey string) (Result, error)
}

// Discoverer lists the download clients an *arr instance has configured.
type Discoverer interface {
	DownloadClients(ctx context.Context, kind domain.AppKind, baseURL, apiKey string) ([]arr.ProviderResource, error)
}

// IndexerDiscoverer lists the indexers an *arr instance has configured.
type IndexerDiscoverer interface {
	Indexers(ctx context.Context, kind domain.AppKind, baseURL, apiKey string) ([]arr.ProviderResource, error)
}

// Label is one tag or quality profile of an *arr instance.
type Label struct {
	ID   int32
	Name string
}

// Labels are the tags and quality profiles an *arr instance offers, each sorted by name.
type Labels struct {
	Tags     []Label
	Profiles []Label
}

// Labeler reads the tags and quality profiles of an *arr instance.
type Labeler interface {
	Labels(ctx context.Context, kind domain.AppKind, baseURL, apiKey string) (Labels, error)
}

// Options tune the probe.
type Options = BaseFactory

// DefaultOptions returns a 15 s timeout, 120 s import timeout and the product User-Agent.
func DefaultOptions() Options {
	return Options{
		Timeout:       15 * time.Second,
		ImportTimeout: 120 * time.Second,
		UserAgent:     "SnatchArr/1.0 (https://github.com/lusoris/SnatchArr)",
	}
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
	if opts.ImportTimeout <= 0 {
		opts.ImportTimeout = DefaultOptions().ImportTimeout
	}
	if opts.ImportTimeout < opts.Timeout {
		opts.ImportTimeout = opts.Timeout
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
		return Result{}, fmt.Errorf("%w: %w", domain.ErrUnreachable, HintAPIError(err, string(kind)))
	}
	res := Result{AppName: st.AppName, Version: st.Version, Product: detectProduct(kind, st)}
	if err := checkWhisparrGeneration(kind, st.Version); err != nil {
		return res, err
	}
	return res, nil
}

func (p *GoenvoyProber) clientOptions() []arr.Option {
	return p.opts.Options()
}

// arrClient is the slice of every goenvoy *arr client the control plane uses.
type arrClient interface {
	GetSystemStatus(ctx context.Context) (*arr.StatusResponse, error)
	GetDownloadClients(ctx context.Context) ([]arr.ProviderResource, error)
	GetIndexers(ctx context.Context) ([]arr.ProviderResource, error)
	GetTags(ctx context.Context) ([]arr.Tag, error)
	GetQualityProfiles(ctx context.Context) ([]arr.QualityProfile, error)
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
		return nil, fmt.Errorf("%w: %w", domain.ErrUnreachable, HintAPIError(err, string(kind)))
	}
	return list, nil
}

// Indexers implements IndexerDiscoverer: GET /api/v{1,3}/indexer.
func (p *GoenvoyProber) Indexers(ctx context.Context, kind domain.AppKind, baseURL, apiKey string) ([]arr.ProviderResource, error) {
	ctx, cancel := context.WithTimeout(ctx, p.opts.Timeout)
	defer cancel()
	c, err := p.newClient(kind, baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	list, err := c.GetIndexers(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", domain.ErrUnreachable, HintAPIError(err, string(kind)))
	}
	return list, nil
}

// Labels implements Labeler: GET /api/v{1,3}/tag and /api/v{1,3}/qualityprofile.
func (p *GoenvoyProber) Labels(ctx context.Context, kind domain.AppKind, baseURL, apiKey string) (Labels, error) {
	ctx, cancel := context.WithTimeout(ctx, p.opts.Timeout)
	defer cancel()
	c, err := p.newClient(kind, baseURL, apiKey)
	if err != nil {
		return Labels{}, err
	}
	tags, err := c.GetTags(ctx)
	if err != nil {
		return Labels{}, fmt.Errorf("%w: tags: %w", domain.ErrUnreachable, HintAPIError(err, string(kind)))
	}
	profiles, err := c.GetQualityProfiles(ctx)
	if err != nil {
		return Labels{}, fmt.Errorf("%w: quality profiles: %w", domain.ErrUnreachable, HintAPIError(err, string(kind)))
	}
	out := Labels{Tags: make([]Label, 0, len(tags)), Profiles: make([]Label, 0, len(profiles))}
	for _, t := range tags {
		out.Tags = appendLabel(out.Tags, t.ID, t.Label)
	}
	for _, q := range profiles {
		out.Profiles = appendLabel(out.Profiles, q.ID, q.Name)
	}
	sortLabels(out.Tags)
	sortLabels(out.Profiles)
	return out, nil
}

// appendLabel keeps the ids a policy can store (1..MaxInt32) and drops the rest.
func appendLabel(list []Label, id int, name string) []Label {
	if id < 1 || id > math.MaxInt32 {
		return list
	}
	return append(list, Label{ID: int32(id), Name: name}) // #nosec G115 -- bounded to 1..MaxInt32 above
}

// sortLabels orders labels by name, then id, so the form lists them stably.
func sortLabels(list []Label) {
	slices.SortFunc(list, func(a, b Label) int {
		if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
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

// Module provides the goenvoy-backed Prober, Discoverer, IndexerDiscoverer and Labeler.
var Module = fx.Module("snatcharr.arrclient",
	fx.Provide(
		func() *GoenvoyProber { return NewGoenvoyProber(DefaultOptions()) },
		func(p *GoenvoyProber) Prober { return p },
		func(p *GoenvoyProber) Discoverer { return p },
		func(p *GoenvoyProber) IndexerDiscoverer { return p },
		func(p *GoenvoyProber) Labeler { return p },
		func(p *GoenvoyProber) queue.ArrClient { return p },
	),
)
