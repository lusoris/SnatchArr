// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/google/uuid"
	_ "golang.org/x/image/webp"

	"github.com/golusoris/golusoris/core/clock"
	gerr "github.com/golusoris/golusoris/core/errors"
	"github.com/golusoris/golusoris/leader/gate"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/metrics"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

const (
	// MaxImageSets is the upper bound on image sets scanned per pass.
	MaxImageSets = 10000
	// MaxItemsPerSet is the upper bound on items retained in a single image set.
	MaxItemsPerSet = 2000
	// MaxSetBytes is the scalar upper bound on the total size of an image set (2 GiB).
	MaxSetBytes = 2 * 1024 * 1024 * 1024
	// MaxImageDecodeBytes is the maximum allowed image file size for decoding (100 MiB).
	MaxImageDecodeBytes = 100 * 1024 * 1024
)

// ScanTicker periodically scans the image set directory while this replica leads.
type ScanTicker struct {
	*gate.Periodic
}

// NewScanTicker constructs the image set scan leader loop ticker.
func NewScanTicker(svc *Service, clk clock.Clock, m *metrics.Metrics) *ScanTicker {
	return &ScanTicker{gate.NewPeriodic("adult: image sets scan", BaseTickPeriod, clk, m, svc.ScanTick)}
}

// shouldScanTick verifies module enablement, directory presence, and interval elapsed.
func (s *Service) shouldScanTick(ctx context.Context) (bool, error) {
	enabled, err := s.mod.IsEnabled(ctx, domain.ModuleAdult)
	if err != nil {
		return false, fmt.Errorf("adult: check module enabled: %w", err)
	}
	if !enabled {
		return false, nil
	}

	m, err := s.mod.Get(ctx, domain.ModuleAdult)
	if err != nil {
		return false, fmt.Errorf("adult: get module config: %w", err)
	}
	cfg, err := parseAdultConfig(m.Config)
	if err != nil {
		return false, fmt.Errorf("adult: parse module config: %w", err)
	}
	if cfg.ImageSetDir == nil || *cfg.ImageSetDir == "" {
		return false, nil
	}

	interval := resolveSyncInterval(m.Config)
	now := s.clk.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.lastScanPass.IsZero() && now.Sub(s.lastScanPass) < interval {
		return false, nil
	}
	s.lastScanPass = now
	return true, nil
}

// ScanTick evaluates whether a scan pass is due and executes it.
func (s *Service) ScanTick(ctx context.Context) error {
	due, err := s.shouldScanTick(ctx)
	if err != nil || !due {
		return err
	}
	return s.ScanImageSets(ctx)
}

// ScanImageSets executes one sweep over image_set_dir if enabled and configured.
func (s *Service) ScanImageSets(ctx context.Context) error {
	enabled, err := s.mod.IsEnabled(ctx, domain.ModuleAdult)
	if err != nil {
		return fmt.Errorf("adult: scan check module enabled: %w", err)
	}
	if !enabled {
		return nil
	}

	m, err := s.mod.Get(ctx, domain.ModuleAdult)
	if err != nil {
		return fmt.Errorf("adult: scan get module config: %w", err)
	}
	cfg, err := parseAdultConfig(m.Config)
	if err != nil {
		return fmt.Errorf("adult: scan parse config: %w", err)
	}
	if cfg.ImageSetDir == nil || *cfg.ImageSetDir == "" {
		return nil
	}

	return s.scanDir(ctx, filepath.Clean(*cfg.ImageSetDir))
}

func (s *Service) scanDir(ctx context.Context, baseDir string) error {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return fmt.Errorf("adult: read image set dir: %w", err)
	}

	maxSets := s.maxImageSets
	if maxSets <= 0 {
		maxSets = MaxImageSets
	}

	var seenPaths []string
	setCount := 0
	truncated := false
	for _, entry := range entries {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if setCount >= maxSets {
			s.logger.WarnContext(ctx, "adult: max image sets limit reached", "limit", maxSets)
			truncated = true
			break
		}
		subPath := filepath.Join(baseDir, entry.Name())
		if !isValidSetDir(baseDir, subPath) {
			continue
		}
		setCount++
		seenPaths = append(seenPaths, subPath)
		if err := s.scanSingleSet(ctx, baseDir, subPath); err != nil {
			s.logger.ErrorContext(ctx, "adult: scan image set error", "error", logSafeErr(err))
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return s.maybeCleanVanishedSets(ctx, truncated, seenPaths)
}

func isValidSetDir(baseDir, subPath string) bool {
	if !isCleanSubdir(baseDir, subPath) {
		return false
	}
	fi, err := os.Lstat(subPath)
	return err == nil && fi.Mode()&os.ModeSymlink == 0 && fi.IsDir()
}

func (s *Service) maybeCleanVanishedSets(ctx context.Context, truncated bool, seenPaths []string) error {
	if truncated {
		return nil
	}
	return s.cleanVanishedSets(ctx, seenPaths)
}

func (s *Service) cleanVanishedSets(ctx context.Context, seenPaths []string) error {
	if len(seenPaths) > 0 {
		_, err := s.st.Q().DeleteAdultImageSetsNotInPaths(ctx, seenPaths)
		if err != nil {
			return fmt.Errorf("delete vanished sets: %w", err)
		}
		return nil
	}
	_, err := s.st.Q().DeleteAllAdultImageSets(ctx)
	if err != nil {
		return fmt.Errorf("delete all sets: %w", err)
	}
	return nil
}

func (s *Service) scanSingleSet(ctx context.Context, baseDir, setDir string) error {
	now := s.clk.Now()
	setID, err := s.ids.NewUUID()
	if err != nil {
		return fmt.Errorf("generate set id: %w", err)
	}
	set, err := s.st.Q().UpsertAdultImageSet(ctx, sqlcgen.UpsertAdultImageSetParams{
		ID:            setID,
		Title:         filepath.Base(setDir),
		DirectoryPath: setDir,
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if err != nil {
		return fmt.Errorf("upsert image set: %w", err)
	}

	existingList, err := s.st.Q().ListAdultImageItemsForSet(ctx, set.ID)
	if err != nil {
		return fmt.Errorf("list existing set items: %w", err)
	}
	existingMap := make(map[string]sqlcgen.AdultImageItem, len(existingList))
	for _, it := range existingList {
		existingMap[it.FilePath] = it
	}

	files, err := os.ReadDir(setDir)
	if err != nil {
		return fmt.Errorf("read set directory: %w", err)
	}
	slices.SortFunc(files, func(a, b os.DirEntry) int {
		return cmp.Compare(a.Name(), b.Name())
	})

	items, totalBytes := s.collectValidItems(ctx, set.ID, baseDir, setDir, files, existingMap)
	return s.syncSetItems(ctx, set.ID, items, totalBytes, now)
}

type scannedItem struct {
	filePath string
	fileName string
	size     int64
	mime     string
	checksum string
	width    *int32
	height   *int32
	mtime    *time.Time
}

type validEntry struct {
	filePath string
	size     int64
	mtime    time.Time
	ext      string
}

func validateFileEntry(baseDir, setDir string, f os.DirEntry) (validEntry, bool) {
	filePath := filepath.Join(setDir, f.Name())
	if !isCleanSubdir(baseDir, filePath) {
		return validEntry{}, false
	}
	fi, err := os.Lstat(filePath)
	if err != nil || fi.Mode()&os.ModeSymlink != 0 || fi.IsDir() {
		return validEntry{}, false
	}
	ext := filepath.Ext(f.Name())
	if !isAcceptedImageExt(ext) {
		return validEntry{}, false
	}
	return validEntry{
		filePath: filePath,
		size:     fi.Size(),
		mtime:    fi.ModTime().UTC().Truncate(time.Microsecond),
		ext:      ext,
	}, true
}

func (s *Service) collectValidItems(
	ctx context.Context,
	setID uuid.UUID,
	baseDir, setDir string,
	files []os.DirEntry,
	existingMap map[string]sqlcgen.AdultImageItem,
) ([]scannedItem, int64) {
	var (
		items      []scannedItem
		totalBytes int64
		count      int
	)
	for _, f := range files {
		if ctx.Err() != nil {
			break
		}
		entry, ok := validateFileEntry(baseDir, setDir, f)
		if !ok {
			continue
		}
		if entry.size > MaxImageDecodeBytes {
			s.logger.WarnContext(ctx, "adult: oversized image skipped", "image_set_id", setID, "size_bytes", entry.size)
			continue
		}
		if totalBytes+entry.size > MaxSetBytes {
			s.logger.WarnContext(ctx, "adult: max set bytes reached", "image_set_id", setID, "total_bytes", totalBytes+entry.size)
			break
		}
		if count >= MaxItemsPerSet {
			s.logger.WarnContext(ctx, "adult: max items per set limit reached", "image_set_id", setID, "item_count", count)
			break
		}
		item, err := s.resolveItem(entry, existingMap)
		if err != nil {
			continue
		}
		count++
		totalBytes += entry.size
		items = append(items, item)
	}
	return items, totalBytes
}

func (s *Service) resolveItem(
	entry validEntry,
	existingMap map[string]sqlcgen.AdultImageItem,
) (scannedItem, error) {
	if existing, ok := existingMap[entry.filePath]; ok && isItemUnchanged(existing, entry.size, entry.mtime) {
		mtime := entry.mtime
		return scannedItem{
			filePath: entry.filePath,
			fileName: filepath.Base(entry.filePath),
			size:     entry.size,
			mime:     existing.MimeType,
			checksum: existing.Sha256Checksum,
			width:    existing.Width,
			height:   existing.Height,
			mtime:    &mtime,
		}, nil
	}
	return s.inspectImageFile(entry.filePath, entry.size, entry.mtime, entry.ext)
}

func isItemUnchanged(existing sqlcgen.AdultImageItem, size int64, mtime time.Time) bool {
	if existing.FileSizeBytes != size || existing.FileMtime == nil {
		return false
	}
	return existing.FileMtime.UTC().Truncate(time.Microsecond).Equal(mtime)
}

func (s *Service) syncSetItems(
	ctx context.Context,
	setID uuid.UUID,
	items []scannedItem,
	totalBytes int64,
	now time.Time,
) error {
	activePaths := make([]string, 0, len(items))
	for i, item := range items {
		activePaths = append(activePaths, item.filePath)
		itemID, err := s.ids.NewUUID()
		if err != nil {
			return fmt.Errorf("generate item id: %w", err)
		}
		// #nosec G115 -- index i is bounded by MaxItemsPerSet (2000)
		sortOrder := int32(i)
		_, err = s.st.Q().UpsertAdultImageItem(ctx, sqlcgen.UpsertAdultImageItemParams{
			ID:             itemID,
			ImageSetID:     setID,
			FilePath:       item.filePath,
			FileName:       item.fileName,
			FileSizeBytes:  item.size,
			MimeType:       item.mime,
			Width:          item.width,
			Height:         item.height,
			Sha256Checksum: item.checksum,
			SortOrder:      sortOrder,
			CreatedAt:      now,
			FileMtime:      item.mtime,
		})
		if err != nil {
			return fmt.Errorf("upsert image item: %w", err)
		}
	}

	if err := s.cleanVanishedItems(ctx, setID, activePaths); err != nil {
		return err
	}

	var coverPath *string
	if len(items) > 0 {
		coverPath = &items[0].filePath
	}
	// #nosec G115 -- len(items) is bounded by MaxItemsPerSet (2000)
	itemCount := int32(len(items))
	_, err := s.st.Q().UpdateAdultImageSetSummary(ctx, sqlcgen.UpdateAdultImageSetSummaryParams{
		ID:             setID,
		ItemCount:      itemCount,
		TotalSizeBytes: totalBytes,
		CoverImagePath: coverPath,
		UpdatedAt:      now,
	})
	if err != nil {
		return fmt.Errorf("update set summary: %w", err)
	}
	return nil
}

func (s *Service) cleanVanishedItems(ctx context.Context, setID uuid.UUID, activePaths []string) error {
	if len(activePaths) > 0 {
		_, err := s.st.Q().DeleteAdultImageItemsNotInPaths(ctx, sqlcgen.DeleteAdultImageItemsNotInPathsParams{
			ImageSetID: setID,
			KeepPaths:  activePaths,
		})
		if err != nil {
			return fmt.Errorf("delete vanished items: %w", err)
		}
		return nil
	}
	_, err := s.st.Q().DeleteAllAdultImageItemsForSet(ctx, setID)
	if err != nil {
		return fmt.Errorf("delete all set items: %w", err)
	}
	return nil
}

func extractDimensions(f io.ReadSeeker) (*int32, *int32) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, nil
	}
	cfg, _, err := image.DecodeConfig(f)
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, nil
	}
	if cfg.Width > math.MaxInt32 || cfg.Height > math.MaxInt32 {
		return nil, nil
	}
	w := int32(cfg.Width)  // #nosec G115 -- bounds checked above
	h := int32(cfg.Height) // #nosec G115 -- bounds checked above
	return &w, &h
}

func (s *Service) inspectImageFile(filePath string, size int64, mtime time.Time, ext string) (_ scannedItem, err error) {
	openFn := s.openFile
	if openFn == nil {
		openFn = os.Open
	}
	f, err := openFn(filePath) // #nosec G304 -- path verified within imageSetDir
	if err != nil {
		return scannedItem{}, fmt.Errorf("open file: %w", err)
	}
	defer gerr.CloseInto(f, &err, "close image file")

	header := make([]byte, 512)
	n, err := io.ReadFull(f, header)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return scannedItem{}, fmt.Errorf("read header: %w", err)
	}
	mimeType := http.DetectContentType(header[:n])
	if !matchMimeWithExt(ext, mimeType) {
		return scannedItem{}, fmt.Errorf("mime mismatch: ext %s got %s", ext, mimeType)
	}

	hasher := sha256.New()
	if _, err := hasher.Write(header[:n]); err != nil {
		return scannedItem{}, fmt.Errorf("hash header: %w", err)
	}
	if _, err := io.Copy(hasher, f); err != nil {
		return scannedItem{}, fmt.Errorf("hash stream: %w", err)
	}
	checksum := hex.EncodeToString(hasher.Sum(nil))

	w, h := extractDimensions(f)
	mtimeCopy := mtime
	return scannedItem{
		filePath: filePath,
		fileName: filepath.Base(filePath),
		size:     size,
		mime:     mimeType,
		checksum: checksum,
		width:    w,
		height:   h,
		mtime:    &mtimeCopy,
	}, nil
}

func logSafeErr(err error) string {
	if err == nil {
		return ""
	}
	if le, ok := errors.AsType[*os.LinkError](err); ok {
		if le.Err != nil {
			return le.Op + ": " + le.Err.Error()
		}
		return le.Op
	}
	if pe, ok := errors.AsType[*fs.PathError](err); ok {
		if pe.Err != nil {
			return pe.Op + ": " + pe.Err.Error()
		}
		return pe.Op
	}
	return err.Error()
}
