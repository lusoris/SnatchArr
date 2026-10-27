// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"fmt"

	"github.com/lusoris/SnatchArr/api/internal/adult"
	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func toOASAdultExternalRef(r adult.ExternalRef) oas.AdultExternalRef {
	out := oas.AdultExternalRef{
		ID:         oas.NewOptUUID(r.ID),
		SourceKind: r.SourceKind,
		ForeignID:  r.ForeignID,
		IsFavorite: r.IsFavorite,
	}
	if r.SourceInstanceID != nil {
		out.SourceInstanceID = oas.NewOptUUID(*r.SourceInstanceID)
	}
	if r.URL != "" {
		out.URL = oas.NewOptString(r.URL)
	}
	return out
}

func toOASAdultPerformer(p adult.Performer) oas.AdultPerformer {
	out := oas.AdultPerformer{
		ID:            p.ID,
		Name:          p.CanonicalName,
		CanonicalName: oas.NewOptString(p.CanonicalName),
		Urls:          p.URLs,
		IsFavorite:    p.IsFavorite,
		RefCount:      oas.NewOptInt(p.RefCount),
		CreatedAt:     oas.NewOptDateTime(p.CreatedAt),
		UpdatedAt:     oas.NewOptDateTime(p.UpdatedAt),
	}
	if p.Disambiguation != "" {
		out.Disambiguation = oas.NewOptString(p.Disambiguation)
	}
	if len(p.ExternalRefs) > 0 {
		refs := make([]oas.AdultExternalRef, 0, len(p.ExternalRefs))
		for _, r := range p.ExternalRefs {
			refs = append(refs, toOASAdultExternalRef(r))
		}
		out.ExternalRefs = refs
	}
	return out
}

func toOASAdultStudio(s adult.Studio) oas.AdultStudio {
	out := oas.AdultStudio{
		ID:            s.ID,
		Name:          s.CanonicalName,
		CanonicalName: oas.NewOptString(s.CanonicalName),
		Urls:          s.URLs,
		IsFavorite:    s.IsFavorite,
		RefCount:      oas.NewOptInt(s.RefCount),
		CreatedAt:     oas.NewOptDateTime(s.CreatedAt),
		UpdatedAt:     oas.NewOptDateTime(s.UpdatedAt),
	}
	if len(s.ExternalRefs) > 0 {
		refs := make([]oas.AdultExternalRef, 0, len(s.ExternalRefs))
		for _, r := range s.ExternalRefs {
			refs = append(refs, toOASAdultExternalRef(r))
		}
		out.ExternalRefs = refs
	}
	return out
}

// ListAdultPerformers implements listAdultPerformers operation.
func (h *Handlers) ListAdultPerformers(ctx context.Context, params oas.ListAdultPerformersParams) (*oas.AdultPerformerPage, error) {
	if h.adult == nil {
		return nil, fmt.Errorf("%w: module adult is disabled", domain.ErrNotFound)
	}
	var fav *bool
	if params.Favorite.IsSet() {
		fav = &params.Favorite.Value
	}
	limit := 50
	if params.Limit.IsSet() {
		limit = params.Limit.Value
	}
	var cursor string
	if params.Cursor.IsSet() {
		cursor = params.Cursor.Value
	}

	page, err := h.adult.ListPerformers(ctx, adult.CatalogFilter{
		Favorite: fav,
		Limit:    limit,
		Cursor:   cursor,
	})
	if err != nil {
		return nil, mapAdultError(err)
	}

	items := make([]oas.AdultPerformer, 0, len(page.Items))
	for _, p := range page.Items {
		items = append(items, toOASAdultPerformer(p))
	}

	res := &oas.AdultPerformerPage{
		Items: items,
	}
	if page.NextCursor != "" {
		res.NextCursor = oas.NewOptString(page.NextCursor)
		res.Cursor = oas.NewOptString(page.NextCursor)
	}
	return res, nil
}

// GetAdultPerformer implements getAdultPerformer operation.
func (h *Handlers) GetAdultPerformer(ctx context.Context, params oas.GetAdultPerformerParams) (*oas.AdultPerformer, error) {
	if h.adult == nil {
		return nil, fmt.Errorf("%w: module adult is disabled", domain.ErrNotFound)
	}
	p, err := h.adult.GetPerformer(ctx, params.PerformerId)
	if err != nil {
		return nil, mapAdultError(err)
	}
	res := toOASAdultPerformer(p)
	return &res, nil
}

// ListAdultStudios implements listAdultStudios operation.
func (h *Handlers) ListAdultStudios(ctx context.Context, params oas.ListAdultStudiosParams) (*oas.AdultStudioPage, error) {
	if h.adult == nil {
		return nil, fmt.Errorf("%w: module adult is disabled", domain.ErrNotFound)
	}
	var fav *bool
	if params.Favorite.IsSet() {
		fav = &params.Favorite.Value
	}
	limit := 50
	if params.Limit.IsSet() {
		limit = params.Limit.Value
	}
	var cursor string
	if params.Cursor.IsSet() {
		cursor = params.Cursor.Value
	}

	page, err := h.adult.ListStudios(ctx, adult.CatalogFilter{
		Favorite: fav,
		Limit:    limit,
		Cursor:   cursor,
	})
	if err != nil {
		return nil, mapAdultError(err)
	}

	items := make([]oas.AdultStudio, 0, len(page.Items))
	for _, s := range page.Items {
		items = append(items, toOASAdultStudio(s))
	}

	res := &oas.AdultStudioPage{
		Items: items,
	}
	if page.NextCursor != "" {
		res.NextCursor = oas.NewOptString(page.NextCursor)
		res.Cursor = oas.NewOptString(page.NextCursor)
	}
	return res, nil
}

// GetAdultStudio implements getAdultStudio operation.
func (h *Handlers) GetAdultStudio(ctx context.Context, params oas.GetAdultStudioParams) (*oas.AdultStudio, error) {
	if h.adult == nil {
		return nil, fmt.Errorf("%w: module adult is disabled", domain.ErrNotFound)
	}
	s, err := h.adult.GetStudio(ctx, params.StudioId)
	if err != nil {
		return nil, mapAdultError(err)
	}
	res := toOASAdultStudio(s)
	return &res, nil
}
