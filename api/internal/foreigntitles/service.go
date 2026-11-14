// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package foreigntitles composes the foreign-language search titles of one *arr item:
// the *arr's own cached aliases plus, when the operator opted in, TheTVDB translations
// (ADR-0048). TVDB is enrich-only: its failure never fails the response.
package foreigntitles

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/fx"

	"github.com/golusoris/golusoris/cache/memory"
	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/foreigntitle"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	tvdbprovider "github.com/lusoris/SnatchArr/api/internal/metadata/tvdb"
)

// MaxTitles bounds the titles one resolution returns.
const MaxTitles = 50

// SourceArr tags titles that come from the Sonarr or Radarr cache.
const SourceArr = "arr"

// Attribution is the TheTVDB credit that must accompany TVDB-derived data.
const Attribution = "Data courtesy of TheTVDB.com"

// Status reports what the TVDB provider did for one resolution.
type Status string

// TVDB provider outcomes.
const (
	StatusDisabled     Status = "disabled"
	StatusOK           Status = "ok"
	StatusUnauthorized Status = "unauthorized"
	StatusRateLimited  Status = "rate_limited"
	StatusUnavailable  Status = "unavailable"
)

// Title is one search title and where it came from (SourceArr or tvdbprovider.Source).
type Title struct {
	Title  string
	Source string
}

// Result is the composed title list of one item for one language.
type Result struct {
	Language string
	Season   *int
	Titles   []Title
	TVDB     Status
}

// Provider is the TVDB provider surface the service uses.
type Provider interface {
	Enabled() bool
	TitlesFor(ctx context.Context, ref tvdbprovider.Ref, ruleCode string) ([]foreigntitle.LanguageTitle, error)
}

type instanceReader interface {
	GetWithCredentials(ctx context.Context, instID uuid.UUID) (domain.Instance, instances.Credentials, error)
}

// Service resolves foreign titles for *arr items.
type Service struct {
	inst     instanceReader
	titler   arrclient.ItemTitler
	provider Provider
	rules    map[string]foreigntitle.Rules
	logger   *slog.Logger
}

// New builds the service; provider may be nil or disabled.
func New(inst *instances.Service, titler arrclient.ItemTitler, provider *tvdbprovider.Provider, logger *slog.Logger) (*Service, error) {
	return newService(inst, titler, provider, logger)
}

func newService(inst instanceReader, titler arrclient.ItemTitler, provider Provider, logger *slog.Logger) (*Service, error) {
	rules, err := foreigntitle.Load()
	if err != nil {
		return nil, fmt.Errorf("foreigntitles: load rules: %w", err)
	}
	return &Service{inst: inst, titler: titler, provider: provider, rules: rules, logger: logger}, nil
}

// Resolve returns the search titles of one item. An unknown language or an app other
// than Sonarr and Radarr is domain.ErrInvalid; *arr failures pass through.
func (s *Service) Resolve(ctx context.Context, instanceID uuid.UUID, itemID int, ruleCode string, season *int) (Result, error) {
	rule, ok := s.rules[ruleCode]
	if !ok {
		return Result{}, fmt.Errorf("%w: unsupported language %q", domain.ErrInvalid, ruleCode)
	}
	inst, creds, err := s.inst.GetWithCredentials(ctx, instanceID)
	if err != nil {
		return Result{}, fmt.Errorf("foreigntitles: instance: %w", err)
	}
	if inst.Kind != domain.KindSonarr && inst.Kind != domain.KindRadarr {
		return Result{}, fmt.Errorf("%w: foreign titles need a Sonarr or Radarr instance", domain.ErrInvalid)
	}
	titles, err := s.titler.ItemTitles(ctx, inst.Kind, creds.BaseURL, creds.APIKey, itemID)
	if err != nil {
		return Result{}, fmt.Errorf("foreigntitles: item: %w", err)
	}
	in := titles.AliasInput()
	own := foreigntitle.SearchTitles(in, rule, season, MaxTitles)
	status := StatusDisabled
	if s.provider != nil && s.provider.Enabled() {
		var extra []foreigntitle.LanguageTitle
		extra, status = s.enrich(ctx, titles, ruleCode)
		in.Translations = extra
	}
	merged := foreigntitle.SearchTitles(in, rule, season, MaxTitles)
	return Result{Language: ruleCode, Season: season, Titles: tag(merged, own), TVDB: status}, nil
}

// enrich asks TVDB; every failure becomes a status, never an error.
func (s *Service) enrich(ctx context.Context, t arrclient.ItemTitles, ruleCode string) ([]foreigntitle.LanguageTitle, Status) {
	ref := tvdbprovider.Ref{Kind: t.Kind, TVDBID: t.TVDBID, TMDBID: t.TMDBID, IMDBID: t.IMDBID}
	got, err := s.provider.TitlesFor(ctx, ref, ruleCode)
	switch {
	case err == nil:
		return got, StatusOK
	case errors.Is(err, tvdbprovider.ErrUnauthorized):
		return nil, StatusUnauthorized
	case errors.Is(err, tvdbprovider.ErrRateLimited):
		return nil, StatusRateLimited
	default:
		s.logger.WarnContext(ctx, "foreigntitles: tvdb lookup failed", "error", err)
		return nil, StatusUnavailable
	}
}

// tag marks every merged title with its source: *arr when the *arr list already had it.
func tag(merged, own []string) []Title {
	have := make(map[string]struct{}, len(own))
	for _, o := range own {
		have[fold(o)] = struct{}{}
	}
	out := make([]Title, 0, len(merged))
	for _, m := range merged {
		src := tvdbprovider.Source
		if _, ok := have[fold(m)]; ok {
			src = SourceArr
		}
		out = append(out, Title{Title: m, Source: src})
	}
	return out
}

// fold normalises case and whitespace so a title both sources hold counts as *arr's.
func fold(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

func newProvider(cfg config.Options, l1 *memory.Cache, clk clock.Clock, logger *slog.Logger) (*tvdbprovider.Provider, error) {
	ua := tvdbprovider.WithUserAgent(arrclient.DefaultOptions().UserAgent)
	p, err := tvdbprovider.New(cfg.Metadata.TVDB.APIKey, cfg.Metadata.TVDB.PIN, l1, clk, logger, ua)
	if err != nil {
		return nil, fmt.Errorf("foreigntitles: tvdb provider: %w", err)
	}
	return p, nil
}

// Module provides the TVDB provider and the foreign-title service.
var Module = fx.Module("snatcharr.foreigntitles",
	fx.Provide(newProvider, New),
)
