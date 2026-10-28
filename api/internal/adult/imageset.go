// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// ImageSetDetail bundles an image set with its ordered items and .stashignore status.
type ImageSetDetail struct {
	Set               sqlcgen.AdultImageSet
	Items             []sqlcgen.AdultImageItem
	IgnoreFilePresent bool
}

// ScanSummary reports image set and item counts and ignore-file status after a scan sweep.
type ScanSummary struct {
	SetsFound         int
	ItemsFound        int
	IgnoreFilePresent bool
}

// ListImageSets retrieves a page of discovered image sets ordered by created_at DESC.
func (s *Service) ListImageSets(ctx context.Context, limit, offset int) ([]sqlcgen.AdultImageSet, error) {
	clampedLimit := int32(min(max(limit, 1), 200)) // #nosec G115 -- clamped to [1, 200]
	clampedOffset := int32(max(offset, 0))         // #nosec G115 -- non-negative offset
	rows, err := s.st.Q().ListAdultImageSetsPaged(ctx, sqlcgen.ListAdultImageSetsPagedParams{
		Limit:  clampedLimit,
		Offset: clampedOffset,
	})
	if err != nil {
		return nil, fmt.Errorf("adult: list image sets: %w", store.MapError(err))
	}
	return rows, nil
}

// GetImageSetDetail loads an image set, its ordered items, and current .stashignore status.
func (s *Service) GetImageSetDetail(ctx context.Context, id uuid.UUID) (ImageSetDetail, error) {
	set, err := s.st.Q().GetAdultImageSet(ctx, id)
	if err != nil {
		return ImageSetDetail{}, fmt.Errorf("adult: get image set: %w", store.MapError(err))
	}
	items, err := s.st.Q().ListAdultImageItemsForSet(ctx, id)
	if err != nil {
		return ImageSetDetail{}, fmt.Errorf("adult: list items for set: %w", store.MapError(err))
	}
	return ImageSetDetail{
		Set:               set,
		Items:             items,
		IgnoreFilePresent: s.IsStashIgnorePresent(ctx),
	}, nil
}

// ScanNow triggers an immediate scan sweep and returns summary statistics.
func (s *Service) ScanNow(ctx context.Context) (ScanSummary, error) {
	if err := s.ScanImageSets(ctx); err != nil {
		return ScanSummary{}, err
	}
	setCount, err := s.st.Q().CountAdultImageSets(ctx)
	if err != nil {
		return ScanSummary{}, fmt.Errorf("adult: count image sets: %w", store.MapError(err))
	}
	itemCount, err := s.st.Q().CountAdultImageItems(ctx)
	if err != nil {
		return ScanSummary{}, fmt.Errorf("adult: count image items: %w", store.MapError(err))
	}
	return ScanSummary{
		SetsFound:         int(setCount),
		ItemsFound:        int(itemCount),
		IgnoreFilePresent: s.IsStashIgnorePresent(ctx),
	}, nil
}

// IsStashIgnorePresent checks whether .stashignore exists at the image_set_dir root.
func (s *Service) IsStashIgnorePresent(ctx context.Context) bool {
	m, err := s.mod.Get(ctx, domain.ModuleAdult)
	if err != nil {
		return false
	}
	cfg, err := parseAdultConfig(m.Config)
	if err != nil || cfg.ImageSetDir == nil || *cfg.ImageSetDir == "" {
		return false
	}
	target := filepath.Join(filepath.Clean(*cfg.ImageSetDir), StashIgnoreFilename)
	_, statErr := os.Stat(target)
	return statErr == nil
}
