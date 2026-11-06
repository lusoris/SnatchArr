// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package arrclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/golusoris/goenvoy/arr/radarr"
	"github.com/golusoris/goenvoy/arr/sonarr"
	"github.com/golusoris/goenvoy/arr/v2"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/foreigntitle"
)

const (
	maxItemBody = 4 << 20
	maxAliases  = 200
)

// ItemTitles holds the locally cached titles of one series or movie.
// OriginalLanguage is the upstream name: language IDs differ between apps.
type ItemTitles struct {
	Kind             domain.AppKind
	ItemID           int
	Title            string
	Year             int
	OriginalTitle    string
	OriginalLanguage string
	Aliases          []Alias
}

// Alias holds an alternative title; a nil SeasonNumber applies to the whole item.
// Sonarr uses SeasonNumber, falling back to SceneSeasonNumber only when absent.
// Radarr aliases always apply to the whole movie.
type Alias struct {
	Title        string
	SeasonNumber *int
}

// ItemTitler reads an item's titles from the local Sonarr or Radarr cache.
type ItemTitler interface {
	ItemTitles(ctx context.Context, kind domain.AppKind, baseURL, apiKey string, itemID int) (ItemTitles, error)
}

// ItemTitles fetches bounded metadata through goenvoy's typed item endpoints.
func (p *GoenvoyProber) ItemTitles(ctx context.Context, kind domain.AppKind, baseURL, apiKey string, itemID int) (ItemTitles, error) {
	if itemID < 1 || (kind != domain.KindSonarr && kind != domain.KindRadarr) {
		return ItemTitles{}, fmt.Errorf("%w: item titles require Sonarr or Radarr and a positive item ID", domain.ErrInvalid)
	}
	ctx, cancel := context.WithTimeout(ctx, p.opts.Timeout)
	defer cancel()
	opts, err := p.opts.OptionsCapped(maxItemBody)
	if err != nil {
		return ItemTitles{}, fmt.Errorf("arrclient: %w", err)
	}
	var titles ItemTitles
	if kind == domain.KindSonarr {
		titles, err = sonarrTitles(ctx, baseURL, apiKey, itemID, opts)
	} else {
		titles, err = radarrTitles(ctx, baseURL, apiKey, itemID, opts)
	}
	if err != nil {
		return ItemTitles{}, itemTitlesError(err, kind)
	}
	return titles, nil
}

// itemTitlesError classifies upstream failures while preserving API details and base URL hints.
func itemTitlesError(err error, kind domain.AppKind) error {
	sentinel := domain.ErrUnreachable
	if apiErr, ok := errors.AsType[*arr.APIError](err); ok && apiErr.StatusCode == http.StatusNotFound {
		sentinel = domain.ErrNotFound
	}
	return fmt.Errorf("%w: item titles: %w", sentinel, HintAPIError(err, string(kind)))
}

// sonarrTitles reads cached series metadata, retaining alias season scopes for targeted searches.
func sonarrTitles(ctx context.Context, baseURL, apiKey string, itemID int, opts []arr.Option) (ItemTitles, error) {
	c, err := sonarr.New(baseURL, apiKey, opts...)
	if err != nil {
		return ItemTitles{}, fmt.Errorf("sonarr client: %w", err)
	}
	series, err := c.GetSeries(ctx, itemID)
	if err != nil {
		return ItemTitles{}, fmt.Errorf("sonarr series: %w", err)
	}
	titles := ItemTitles{
		Kind: domain.KindSonarr, ItemID: itemID, Title: series.Title,
		Year: series.Year, OriginalLanguage: series.OriginalLanguage.Name,
	}
	for i := range len(series.AlternateTitles) {
		alias := series.AlternateTitles[i]
		season := alias.SeasonNumber
		if season == nil {
			season = alias.SceneSeasonNumber
		}
		if addItemAlias(&titles, alias.Title, season) {
			break
		}
	}
	return titles, nil
}

// radarrTitles reads cached movie metadata so searches can use original and alternative titles.
func radarrTitles(ctx context.Context, baseURL, apiKey string, itemID int, opts []arr.Option) (ItemTitles, error) {
	c, err := radarr.New(baseURL, apiKey, opts...)
	if err != nil {
		return ItemTitles{}, fmt.Errorf("radarr client: %w", err)
	}
	movie, err := c.GetMovie(ctx, itemID)
	if err != nil {
		return ItemTitles{}, fmt.Errorf("radarr movie: %w", err)
	}
	titles := ItemTitles{
		Kind: domain.KindRadarr, ItemID: itemID, Title: movie.Title, Year: movie.Year,
		OriginalTitle: movie.OriginalTitle, OriginalLanguage: movie.OriginalLanguage.Name,
	}
	for i := range len(movie.AlternateTitles) {
		if addItemAlias(&titles, movie.AlternateTitles[i].Title, nil) {
			break
		}
	}
	return titles, nil
}

// addItemAlias keeps nonblank aliases and signals the bound to stop collecting upstream metadata.
func addItemAlias(t *ItemTitles, title string, season *int) bool {
	if title = strings.TrimSpace(title); title != "" {
		t.Aliases = append(t.Aliases, Alias{Title: title, SeasonNumber: season})
	}
	return len(t.Aliases) >= maxAliases
}

// AliasInput converts cached metadata to the pure foreign-title search input.
func (t ItemTitles) AliasInput() foreigntitle.AliasInput {
	in := foreigntitle.AliasInput{Title: t.Title, OriginalTitle: t.OriginalTitle, OriginalLanguage: t.OriginalLanguage}
	for i := range min(len(t.Aliases), maxAliases) {
		alias := t.Aliases[i]
		in.Aliases = append(in.Aliases, foreigntitle.AliasEntry{Title: alias.Title, SeasonNumber: alias.SeasonNumber})
	}
	return in
}
