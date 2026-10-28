// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/adult"
	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

func toOASAdultImageSet(s sqlcgen.AdultImageSet) oas.AdultImageSet {
	out := oas.AdultImageSet{
		ID:             s.ID,
		Title:          s.Title,
		DirectoryPath:  s.DirectoryPath,
		ItemCount:      int(s.ItemCount),
		TotalSizeBytes: s.TotalSizeBytes,
		CreatedAt:      s.CreatedAt,
		UpdatedAt:      s.UpdatedAt,
	}
	if s.WhisparrSceneForeignID != nil {
		out.WhisparrSceneForeignID = oas.NewOptString(*s.WhisparrSceneForeignID)
	}
	if s.PerformerID != nil {
		out.PerformerID = oas.NewOptUUID(*s.PerformerID)
	}
	if s.CoverImagePath != nil {
		out.CoverImagePath = oas.NewOptString(*s.CoverImagePath)
	}
	return out
}

func toOASAdultImageItem(item sqlcgen.AdultImageItem) oas.AdultImageItem {
	out := oas.AdultImageItem{
		ID:             item.ID,
		ImageSetID:     item.ImageSetID,
		FilePath:       item.FilePath,
		FileName:       item.FileName,
		FileSizeBytes:  item.FileSizeBytes,
		MimeType:       item.MimeType,
		SHA256Checksum: item.Sha256Checksum,
		SortOrder:      int(item.SortOrder),
		CreatedAt:      item.CreatedAt,
	}
	if item.Width != nil {
		out.Width = oas.NewOptInt(int(*item.Width))
	}
	if item.Height != nil {
		out.Height = oas.NewOptInt(int(*item.Height))
	}
	return out
}

func toOASAdultImageSetDetail(detail adult.ImageSetDetail) oas.AdultImageSetDetail {
	base := toOASAdultImageSet(detail.Set)
	items := make([]oas.AdultImageItem, 0, len(detail.Items))
	for _, it := range detail.Items {
		items = append(items, toOASAdultImageItem(it))
	}
	return oas.AdultImageSetDetail{
		ID:                     base.ID,
		Title:                  base.Title,
		DirectoryPath:          base.DirectoryPath,
		WhisparrSceneForeignID: base.WhisparrSceneForeignID,
		PerformerID:            base.PerformerID,
		ItemCount:              base.ItemCount,
		TotalSizeBytes:         base.TotalSizeBytes,
		CoverImagePath:         base.CoverImagePath,
		CreatedAt:              base.CreatedAt,
		UpdatedAt:              base.UpdatedAt,
		Items:                  items,
		IgnoreFilePresent:      detail.IgnoreFilePresent,
	}
}

// ListAdultImageSets implements listAdultImageSets operation.
func (h *Handlers) ListAdultImageSets(ctx context.Context, params oas.ListAdultImageSetsParams) (*oas.AdultImageSetPage, error) {
	if h.adult == nil {
		return nil, fmt.Errorf("%w: module adult is disabled", domain.ErrNotFound)
	}
	limit := 50
	if params.Limit.IsSet() {
		limit = params.Limit.Value
	}
	offset := 0
	if params.Offset.IsSet() {
		offset = params.Offset.Value
	}
	sets, err := h.adult.ListImageSets(ctx, limit, offset)
	if err != nil {
		return nil, mapAdultError(err)
	}
	total, err := h.adult.CountImageSets(ctx)
	if err != nil {
		return nil, mapAdultError(err)
	}
	items := make([]oas.AdultImageSet, 0, len(sets))
	for _, s := range sets {
		items = append(items, toOASAdultImageSet(s))
	}
	return &oas.AdultImageSetPage{
		Items: items,
		Total: total,
	}, nil
}

// GetAdultImageSet implements getAdultImageSet operation.
func (h *Handlers) GetAdultImageSet(ctx context.Context, params oas.GetAdultImageSetParams) (*oas.AdultImageSetDetail, error) {
	if h.adult == nil {
		return nil, fmt.Errorf("%w: module adult is disabled", domain.ErrNotFound)
	}
	detail, err := h.adult.GetImageSetDetail(ctx, params.SetId)
	if err != nil {
		return nil, mapAdultError(err)
	}
	res := toOASAdultImageSetDetail(detail)
	return &res, nil
}

// TriggerAdultScan implements triggerAdultScan operation.
func (h *Handlers) TriggerAdultScan(ctx context.Context) (*oas.AdultScanSummary, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	if h.adult == nil {
		return nil, fmt.Errorf("%w: module adult is disabled", domain.ErrNotFound)
	}
	summary, err := h.adult.ScanNow(ctx)
	if err != nil {
		return nil, mapAdultError(err)
	}
	return &oas.AdultScanSummary{
		SetsFound:         summary.SetsFound,
		ItemsFound:        summary.ItemsFound,
		IgnoreFilePresent: summary.IgnoreFilePresent,
	}, nil
}

// ServeAdultImageItemFile returns an http.HandlerFunc that streams image bytes.
func (h *Handlers) ServeAdultImageItemFile() http.HandlerFunc {
	onErr := ErrorHandler(h.logger)
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if h.adult == nil {
			onErr(ctx, w, r, fmt.Errorf("%w: module %s is disabled", domain.ErrNotFound, domain.ModuleAdult))
			return
		}
		if h.modules != nil {
			enabled, err := h.modules.IsEnabled(ctx, domain.ModuleAdult)
			if err != nil {
				onErr(ctx, w, r, fmt.Errorf("adult: check module enabled: %w", err))
				return
			}
			if !enabled {
				onErr(ctx, w, r, fmt.Errorf("%w: module %s is disabled", domain.ErrNotFound, domain.ModuleAdult))
				return
			}
		}
		rawID := chi.URLParam(r, "itemId")
		itemID, err := uuid.Parse(rawID)
		if err != nil {
			onErr(ctx, w, r, fmt.Errorf("%w: invalid item id", domain.ErrNotFound))
			return
		}
		if err := h.adult.ServeImage(w, r, itemID); err != nil {
			onErr(ctx, w, r, mapAdultError(err))
			return
		}
	}
}
