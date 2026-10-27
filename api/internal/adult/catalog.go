// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

const (
	// DefaultCatalogLimit is the default page size when listing performers or studios.
	DefaultCatalogLimit = 50
	// MaxCatalogLimit is the maximum allowed page size for catalogue listing operations.
	MaxCatalogLimit = 200
)

// ExternalRef represents a link between a canonical entity and an external catalogue source.
type ExternalRef struct {
	ID               uuid.UUID
	SourceKind       string
	SourceInstanceID *uuid.UUID
	ForeignID        string
	IsFavorite       bool
	URL              string
}

// Performer represents a synchronized adult performer entity.
type Performer struct {
	ID             uuid.UUID
	CanonicalName  string
	Disambiguation string
	URLs           []string
	IsFavorite     bool
	RefCount       int
	ExternalRefs   []ExternalRef
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// PerformerPage holds a slice of performers and the cursor for the subsequent page.
type PerformerPage struct {
	Items      []Performer
	NextCursor string
}

// Studio represents a synchronized studio or network entity.
type Studio struct {
	ID            uuid.UUID
	CanonicalName string
	URLs          []string
	IsFavorite    bool
	RefCount      int
	ExternalRefs  []ExternalRef
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// StudioPage holds a slice of studios and the cursor for the subsequent page.
type StudioPage struct {
	Items      []Studio
	NextCursor string
}

// CatalogFilter defines query parameters for listing catalogue performers or studios.
type CatalogFilter struct {
	Favorite *bool
	Limit    int
	Cursor   string
}

type catalogCursor struct {
	Name string    `json:"n"`
	ID   uuid.UUID `json:"i"`
}

// EncodeCursor builds an opaque URL-safe keyset cursor from a canonical name and UUID.
func EncodeCursor(name string, id uuid.UUID) string {
	c := catalogCursor{Name: name, ID: id}
	b, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// DecodeCursor parses an opaque keyset cursor into its canonical name and UUID components.
func DecodeCursor(raw string) (string, uuid.UUID, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", uuid.Nil, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(trimmed)
	if err != nil {
		data, err = base64.StdEncoding.DecodeString(trimmed)
		if err != nil {
			return "", uuid.Nil, fmt.Errorf("%w: invalid cursor encoding", domain.ErrInvalid)
		}
	}
	var c catalogCursor
	if err := json.Unmarshal(data, &c); err != nil {
		return "", uuid.Nil, fmt.Errorf("%w: invalid cursor payload", domain.ErrInvalid)
	}
	if c.ID == uuid.Nil {
		return "", uuid.Nil, fmt.Errorf("%w: cursor id cannot be nil", domain.ErrInvalid)
	}
	return c.Name, c.ID, nil
}

func resolveCatalogLimit(limit int) (int, error) {
	if limit == 0 {
		return DefaultCatalogLimit, nil
	}
	if limit < 1 || limit > MaxCatalogLimit {
		return 0, fmt.Errorf("%w: limit must be between 1 and %d", domain.ErrInvalid, MaxCatalogLimit)
	}
	return limit, nil
}

func mapExternalRefs(refs []sqlcgen.AdultExternalRef) []ExternalRef {
	extRefs := make([]ExternalRef, 0, len(refs))
	for _, r := range refs {
		var urlStr string
		if r.CanonicalUrl != nil && *r.CanonicalUrl != "" {
			urlStr = *r.CanonicalUrl
		} else if len(r.ExtraUrls) > 0 && r.ExtraUrls[0] != "" {
			urlStr = r.ExtraUrls[0]
		}
		extRefs = append(extRefs, ExternalRef{
			ID:               r.ID,
			SourceKind:       r.SourceKind,
			SourceInstanceID: r.SourceInstanceID,
			ForeignID:        r.ForeignID,
			IsFavorite:       r.IsFavorite,
			URL:              urlStr,
		})
	}
	return extRefs
}

func toPerformer(r sqlcgen.ListAdultPerformersKeysetRow) Performer {
	var dis string
	if r.Disambiguation != nil {
		dis = *r.Disambiguation
	}
	return Performer{
		ID:             r.ID,
		CanonicalName:  r.CanonicalName,
		Disambiguation: dis,
		URLs:           r.Urls,
		IsFavorite:     r.IsFavorite,
		RefCount:       int(r.RefCount),
		CreatedAt:      r.CreatedAt,
		UpdatedAt:      r.UpdatedAt,
	}
}

func toStudio(r sqlcgen.ListAdultStudiosKeysetRow) Studio {
	return Studio{
		ID:            r.ID,
		CanonicalName: r.CanonicalName,
		URLs:          r.Urls,
		IsFavorite:    r.IsFavorite,
		RefCount:      int(r.RefCount),
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
	}
}

// ListPerformers queries synchronized performers ordered by canonical_name ASC, id ASC with keyset cursor pagination.
func (s *Service) ListPerformers(ctx context.Context, filter CatalogFilter) (PerformerPage, error) {
	limit, err := resolveCatalogLimit(filter.Limit)
	if err != nil {
		return PerformerPage{}, err
	}

	var afterName *string
	var afterID *uuid.UUID
	if filter.Cursor != "" {
		name, id, decErr := DecodeCursor(filter.Cursor)
		if decErr != nil {
			return PerformerPage{}, decErr
		}
		afterName = &name
		afterID = &id
	}

	fetchCount := int32(limit) + 1 // #nosec G115 -- limit is bounded 1..200
	params := sqlcgen.ListAdultPerformersKeysetParams{
		Favorite:           filter.Favorite,
		AfterCanonicalName: afterName,
		AfterID:            afterID,
		LimitCount:         fetchCount,
	}

	rows, err := s.st.Q().ListAdultPerformersKeyset(ctx, params)
	if err != nil {
		return PerformerPage{}, fmt.Errorf("adult: list performers: %w", err)
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	items := make([]Performer, 0, len(rows))
	for _, r := range rows {
		items = append(items, toPerformer(r))
	}

	var nextCursor string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		nextCursor = EncodeCursor(last.CanonicalName, last.ID)
	}

	return PerformerPage{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

// GetPerformer retrieves a performer by UUID along with their associated external catalogue references.
func (s *Service) GetPerformer(ctx context.Context, id uuid.UUID) (Performer, error) {
	p, err := s.st.Q().GetAdultPerformer(ctx, id)
	if err != nil {
		return Performer{}, fmt.Errorf("adult: get performer: %w", store.MapError(err))
	}

	refs, err := s.st.Q().ListAdultExternalRefsByEntity(ctx, sqlcgen.ListAdultExternalRefsByEntityParams{
		EntityType: EntityTypePerformer,
		EntityID:   id,
	})
	if err != nil {
		return Performer{}, fmt.Errorf("adult: list performer refs: %w", err)
	}

	extRefs := mapExternalRefs(refs)
	var dis string
	if p.Disambiguation != nil {
		dis = *p.Disambiguation
	}

	return Performer{
		ID:             p.ID,
		CanonicalName:  p.CanonicalName,
		Disambiguation: dis,
		URLs:           p.Urls,
		IsFavorite:     p.IsFavorite,
		RefCount:       len(extRefs),
		ExternalRefs:   extRefs,
		CreatedAt:      p.CreatedAt,
		UpdatedAt:      p.UpdatedAt,
	}, nil
}

// ListStudios queries synchronized studios ordered by canonical_name ASC, id ASC with keyset cursor pagination.
func (s *Service) ListStudios(ctx context.Context, filter CatalogFilter) (StudioPage, error) {
	limit, err := resolveCatalogLimit(filter.Limit)
	if err != nil {
		return StudioPage{}, err
	}

	var afterName *string
	var afterID *uuid.UUID
	if filter.Cursor != "" {
		name, id, decErr := DecodeCursor(filter.Cursor)
		if decErr != nil {
			return StudioPage{}, decErr
		}
		afterName = &name
		afterID = &id
	}

	fetchCount := int32(limit) + 1 // #nosec G115 -- limit is bounded 1..200
	params := sqlcgen.ListAdultStudiosKeysetParams{
		Favorite:           filter.Favorite,
		AfterCanonicalName: afterName,
		AfterID:            afterID,
		LimitCount:         fetchCount,
	}

	rows, err := s.st.Q().ListAdultStudiosKeyset(ctx, params)
	if err != nil {
		return StudioPage{}, fmt.Errorf("adult: list studios: %w", err)
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	items := make([]Studio, 0, len(rows))
	for _, r := range rows {
		items = append(items, toStudio(r))
	}

	var nextCursor string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		nextCursor = EncodeCursor(last.CanonicalName, last.ID)
	}

	return StudioPage{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

// GetStudio retrieves a studio by UUID along with its associated external catalogue references.
func (s *Service) GetStudio(ctx context.Context, id uuid.UUID) (Studio, error) {
	st, err := s.st.Q().GetAdultStudio(ctx, id)
	if err != nil {
		return Studio{}, fmt.Errorf("adult: get studio: %w", store.MapError(err))
	}

	refs, err := s.st.Q().ListAdultExternalRefsByEntity(ctx, sqlcgen.ListAdultExternalRefsByEntityParams{
		EntityType: EntityTypeStudio,
		EntityID:   id,
	})
	if err != nil {
		return Studio{}, fmt.Errorf("adult: list studio refs: %w", err)
	}

	extRefs := mapExternalRefs(refs)

	return Studio{
		ID:            st.ID,
		CanonicalName: st.CanonicalName,
		URLs:          st.Urls,
		IsFavorite:    st.IsFavorite,
		RefCount:      len(extRefs),
		ExternalRefs:  extRefs,
		CreatedAt:     st.CreatedAt,
		UpdatedAt:     st.UpdatedAt,
	}, nil
}
