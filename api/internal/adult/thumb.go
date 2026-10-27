// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult

import (
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"

	"github.com/gen2brain/webp"
	"github.com/google/uuid"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	gerr "github.com/golusoris/golusoris/core/errors"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

const (
	// MaxThumbnailWidth is the bounding box max width in pixels.
	MaxThumbnailWidth = 400
	// MaxThumbnailHeight is the bounding box max height in pixels.
	MaxThumbnailHeight = 400
	// MaxImagePixels is the decompression bomb ceiling (50 MP).
	MaxImagePixels = 50_000_000
	// WebPThumbnailQuality is the default encoding quality for thumbnails.
	WebPThumbnailQuality = 80
)

func fitAspect(srcW, srcH, maxW, maxH int) (int, int) {
	if srcW <= 0 || srcH <= 0 {
		return maxW, maxH
	}
	if srcW <= maxW && srcH <= maxH {
		return srcW, srcH
	}
	ratioW := float64(maxW) / float64(srcW)
	ratioH := float64(maxH) / float64(srcH)
	ratio := min(ratioW, ratioH)
	w := max(1, int(float64(srcW)*ratio))
	h := max(1, int(float64(srcH)*ratio))
	return w, h
}

// Thumbnail returns the path to the cached WebP thumbnail for the specified item,
// generating it on a cache miss under strict resource and dimension bounds.
func (s *Service) Thumbnail(ctx context.Context, itemID uuid.UUID) (string, error) {
	item, err := s.st.Q().GetAdultImageItem(ctx, itemID)
	if err != nil {
		return "", fmt.Errorf("adult: get image item: %w", store.MapError(err))
	}

	m, err := s.mod.Get(ctx, domain.ModuleAdult)
	if err != nil {
		return "", fmt.Errorf("adult: get module config: %w", err)
	}
	cfg, err := parseAdultConfig(m.Config)
	if err != nil {
		return "", fmt.Errorf("adult: parse module config: %w", err)
	}

	thumbDir := resolveThumbnailDir(cfg)
	destPath := filepath.Join(thumbDir, item.Sha256Checksum+".webp")

	// Fast cache hit check
	if _, err := os.Stat(destPath); err == nil {
		return destPath, nil
	}

	// Acquire semaphore slot (bounded concurrency = 2)
	select {
	case s.thumbSem <- struct{}{}:
		defer func() { <-s.thumbSem }()
	case <-ctx.Done():
		return "", ctx.Err()
	}

	// Double check cache hit after acquiring semaphore
	if _, err := os.Stat(destPath); err == nil {
		return destPath, nil
	}

	cleanBase, err := thumbSourceBase(cfg, item)
	if err != nil {
		return "", err
	}
	return destPath, s.generateThumbnail(cleanBase, item.FilePath, destPath, thumbDir)
}

// thumbSourceBase returns the clean image_set_dir that must contain the item.
// File type and size are checked on the rooted open in generateThumbnail.
func thumbSourceBase(cfg adultSettings, item sqlcgen.AdultImageItem) (string, error) {
	if cfg.ImageSetDir == nil || *cfg.ImageSetDir == "" {
		return "", fmt.Errorf("adult: image_set_dir not configured: %w", domain.ErrInvalid)
	}
	cleanBase := filepath.Clean(*cfg.ImageSetDir)
	if !isCleanSubdir(cleanBase, item.FilePath) {
		return "", fmt.Errorf("adult: file path escapes image set dir: %w", domain.ErrInvalid)
	}
	return cleanBase, nil
}

func (s *Service) generateThumbnail(baseDir, srcPath, destPath, thumbDir string) (err error) {
	f, fi, err := openRegularInRoot(baseDir, srcPath)
	if err != nil {
		if errors.Is(err, domain.ErrInvalid) {
			return err
		}
		return mapFileErr(err, "open")
	}
	defer gerr.CloseInto(f, &err, "adult: close image")

	if fi.Size() > MaxImageDecodeBytes {
		return fmt.Errorf("adult: file size %d exceeds decode limit %d: %w", fi.Size(), MaxImageDecodeBytes, domain.ErrInvalid)
	}

	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return fmt.Errorf("adult: decode config: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > MaxImagePixels {
		return fmt.Errorf("adult: image dimensions %dx%d exceed pixel limit %d: %w", cfg.Width, cfg.Height, MaxImagePixels, domain.ErrInvalid)
	}

	if _, seekErr := f.Seek(0, io.SeekStart); seekErr != nil {
		return fmt.Errorf("adult: seek image: %w", seekErr)
	}
	srcImg, _, err := image.Decode(f)
	if err != nil {
		return fmt.Errorf("adult: decode image: %w", err)
	}

	dst := scaleCatmullRom(srcImg)
	return writeWebPAtomic(dst, destPath, thumbDir)
}

func scaleCatmullRom(srcImg image.Image) *image.RGBA {
	srcW := srcImg.Bounds().Dx()
	srcH := srcImg.Bounds().Dy()
	targetW, targetH := fitAspect(srcW, srcH, MaxThumbnailWidth, MaxThumbnailHeight)
	dst := image.NewRGBA(image.Rect(0, 0, targetW, targetH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), srcImg, srcImg.Bounds(), draw.Over, nil)
	return dst
}

func writeWebPAtomic(dst image.Image, destPath, thumbDir string) (err error) {
	err = os.MkdirAll(thumbDir, 0o750)
	if err != nil {
		return fmt.Errorf("adult: mkdir thumb dir: %w", err)
	}
	tmpFile, err := os.CreateTemp(thumbDir, "thumb-*.tmp")
	if err != nil {
		return fmt.Errorf("adult: create temp thumb: %w", err)
	}
	tmpName := tmpFile.Name()
	defer func() {
		if err != nil {
			err = errors.Join(err, removeIfPresent(tmpName))
		}
	}()

	opts := webp.Options{Quality: WebPThumbnailQuality}
	if err := webp.Encode(tmpFile, dst, opts); err != nil {
		return errors.Join(fmt.Errorf("adult: encode webp: %w", err), tmpFile.Close())
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("adult: close temp thumb: %w", err)
	}
	if err := os.Rename(tmpName, destPath); err != nil {
		return fmt.Errorf("adult: rename thumb: %w", err)
	}
	return nil
}

// removeIfPresent deletes a leftover temporary file; a file that is already
// gone is not an error.
func removeIfPresent(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("adult: remove temp thumb: %w", err)
	}
	return nil
}
