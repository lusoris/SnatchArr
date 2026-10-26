// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	gerr "github.com/golusoris/golusoris/core/errors"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
)

// ServeImage streams the full-resolution image for the given itemID to w,
// verifying path containment and applying standard caching and security headers.
func (s *Service) ServeImage(w http.ResponseWriter, r *http.Request, itemID uuid.UUID) (err error) {
	item, err := s.st.Q().GetAdultImageItem(r.Context(), itemID)
	if err != nil {
		return fmt.Errorf("adult: get image item: %w", store.MapError(err))
	}
	if checkErr := s.checkImageSetContainment(r.Context(), item.FilePath); checkErr != nil {
		return checkErr
	}

	fi, err := os.Lstat(item.FilePath)
	if err != nil {
		return mapFileErr(err, "lstat")
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular() {
		return fmt.Errorf("adult: file is symlink or not regular: %w", domain.ErrInvalid)
	}

	f, err := os.Open(item.FilePath) // #nosec G304 -- path verified within imageSetDir and Lstat confirms regular file
	if err != nil {
		return mapFileErr(err, "open")
	}
	defer gerr.CloseInto(f, &err, "adult: close image")

	w.Header().Set("Content-Type", item.MimeType)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	http.ServeContent(w, r, item.FileName, fi.ModTime(), f)
	return nil
}

func (s *Service) checkImageSetContainment(ctx context.Context, filePath string) error {
	m, err := s.mod.Get(ctx, domain.ModuleAdult)
	if err != nil {
		return fmt.Errorf("adult: get module config: %w", err)
	}
	cfg, err := parseAdultConfig(m.Config)
	if err != nil {
		return fmt.Errorf("adult: parse module config: %w", err)
	}
	if cfg.ImageSetDir == nil || *cfg.ImageSetDir == "" {
		return fmt.Errorf("adult: image_set_dir not configured: %w", domain.ErrInvalid)
	}
	cleanBase := filepath.Clean(*cfg.ImageSetDir)
	if !isCleanSubdir(cleanBase, filePath) {
		return fmt.Errorf("adult: file path escapes image set dir: %w", domain.ErrInvalid)
	}
	return nil
}

func mapFileErr(err error, op string) error {
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("adult: image file missing: %w", domain.ErrNotFound)
	}
	return fmt.Errorf("adult: %s image: %w", op, err)
}

// ServeThumbnail streams the WebP thumbnail for the given itemID to w,
// verifying path containment and applying standard caching and security headers.
func (s *Service) ServeThumbnail(w http.ResponseWriter, r *http.Request, itemID uuid.UUID) (err error) {
	thumbPath, err := s.Thumbnail(r.Context(), itemID)
	if err != nil {
		return fmt.Errorf("adult: thumbnail: %w", err)
	}

	m, err := s.mod.Get(r.Context(), domain.ModuleAdult)
	if err != nil {
		return fmt.Errorf("adult: get module config: %w", err)
	}
	cfg, err := parseAdultConfig(m.Config)
	if err != nil {
		return fmt.Errorf("adult: parse module config: %w", err)
	}
	thumbDir := resolveThumbnailDir(cfg)
	if !isCleanSubdir(thumbDir, thumbPath) {
		return fmt.Errorf("adult: thumbnail path escapes cache dir: %w", domain.ErrInvalid)
	}

	f, err := os.Open(thumbPath) // #nosec G304 -- path verified within thumbnail_cache_dir
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("adult: thumbnail missing: %w", domain.ErrNotFound)
		}
		return fmt.Errorf("adult: open thumbnail: %w", err)
	}
	defer gerr.CloseInto(f, &err, "adult: close thumbnail")

	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("adult: stat thumbnail: %w", err)
	}

	w.Header().Set("Content-Type", "image/webp")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	http.ServeContent(w, r, filepath.Base(thumbPath), fi.ModTime(), f)
	return nil
}
