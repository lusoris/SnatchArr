// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/foreigntitles"
)

// foreignTitleSource resolves the search titles of one *arr item.
type foreignTitleSource interface {
	Resolve(ctx context.Context, instanceID uuid.UUID, itemID int, ruleCode string, season *int) (foreigntitles.Result, error)
}

// foreignTitleResolver keeps a nil service a nil interface.
func foreignTitleResolver(s *foreigntitles.Service) foreignTitleSource {
	if s == nil {
		return nil
	}
	return s
}

// GetForeignTitles lists the foreign-language search titles of one Sonarr or Radarr item.
func (h *Handlers) GetForeignTitles(ctx context.Context, params oas.GetForeignTitlesParams) (*oas.ForeignTitles, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	if h.foreignTitles == nil {
		return nil, fmt.Errorf("%w: foreign titles are not configured", domain.ErrInvalid)
	}
	var season *int
	if params.Season.Set {
		season = &params.Season.Value
	}
	res, err := h.foreignTitles.Resolve(ctx, params.InstanceId, params.ItemId, params.Language, season)
	if err != nil {
		return nil, err
	}
	return foreignTitlesToOAS(res), nil
}

func foreignTitlesToOAS(r foreigntitles.Result) *oas.ForeignTitles {
	out := &oas.ForeignTitles{
		Language:    r.Language,
		Titles:      make([]oas.ForeignTitle, 0, len(r.Titles)),
		Tvdb:        oas.ForeignTitlesTvdb(r.TVDB),
		Attribution: foreigntitles.Attribution,
	}
	if r.Season != nil {
		out.Season = oas.NewOptInt(*r.Season)
	}
	for _, t := range r.Titles {
		out.Titles = append(out.Titles, oas.ForeignTitle{Title: t.Title, Source: oas.ForeignTitleSource(t.Source)})
	}
	return out
}
