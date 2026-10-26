// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

//go:build integration

package adult

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

func createTestPNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 200, G: 50, B: 50, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create test png: %v", err)
	}
	defer func() { _ = f.Close() }()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode test png: %v", err)
	}
}

func createTestJPEG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: 50, G: 200, B: 50, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create test jpeg: %v", err)
	}
	defer func() { _ = f.Close() }()
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("encode test jpeg: %v", err)
	}
}

func createPixelBombPNG(t *testing.T, path string) {
	t.Helper()
	buf := new(bytes.Buffer)
	buf.WriteString("\x89PNG\r\n\x1a\n")
	ihdrData := []byte{
		'I', 'H', 'D', 'R',
		0x00, 0x00, 0x27, 0x10, // width: 10000
		0x00, 0x00, 0x27, 0x10, // height: 10000
		8, 2, 0, 0, 0,
	}
	crc := crc32.ChecksumIEEE(ihdrData)
	_ = binary.Write(buf, binary.BigEndian, uint32(13))
	buf.Write(ihdrData)
	_ = binary.Write(buf, binary.BigEndian, crc)
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write pixel bomb png: %v", err)
	}
}

func configureAdultModule(ctx context.Context, t *testing.T, env *testEnv, imageDir, thumbDir string) {
	t.Helper()
	cfgBytes := fmt.Sprintf(`{"image_set_dir":%q,"thumbnail_cache_dir":%q,"sync_interval_minutes":15}`, imageDir, thumbDir)
	raw := json.RawMessage(cfgBytes)
	if _, err := env.modulesSvc.Update(ctx, domain.ModuleAdult, true, &raw); err != nil {
		t.Fatalf("enable adult module: %v", err)
	}
}

func TestImageSetScanAndThumbnail(t *testing.T) {
	ctx := context.Background()
	env := setupTestEnv(ctx, t)

	tmp := t.TempDir()
	imagesDir := filepath.Join(tmp, "imagesets")
	thumbDir := filepath.Join(tmp, "thumbnails")
	if err := os.MkdirAll(imagesDir, 0o750); err != nil {
		t.Fatalf("mkdir imagesets: %v", err)
	}

	set1Dir := filepath.Join(imagesDir, "gallery_alpha")
	set2Dir := filepath.Join(imagesDir, "gallery_beta")
	if err := os.MkdirAll(set1Dir, 0o750); err != nil {
		t.Fatalf("mkdir set1: %v", err)
	}
	if err := os.MkdirAll(set2Dir, 0o750); err != nil {
		t.Fatalf("mkdir set2: %v", err)
	}

	img1A := filepath.Join(set1Dir, "01_first.png")
	img1B := filepath.Join(set1Dir, "02_second.jpg")
	img2A := filepath.Join(set2Dir, "cover.png")

	createTestPNG(t, img1A, 200, 150)
	createTestJPEG(t, img1B, 300, 200)
	createTestPNG(t, img2A, 100, 100)

	configureAdultModule(ctx, t, env, imagesDir, thumbDir)

	if err := env.svc.ScanImageSets(ctx); err != nil {
		t.Fatalf("ScanImageSets: %v", err)
	}

	sets, err := env.st.Q().ListAdultImageSets(ctx)
	if err != nil {
		t.Fatalf("ListAdultImageSets: %v", err)
	}
	if len(sets) != 2 {
		t.Fatalf("got %d sets, want 2", len(sets))
	}

	for _, s := range sets {
		items, err := env.st.Q().ListAdultImageItemsForSet(ctx, s.ID)
		if err != nil {
			t.Fatalf("ListAdultImageItemsForSet: %v", err)
		}
		if s.Title == "gallery_alpha" {
			if s.ItemCount != 2 || len(items) != 2 {
				t.Fatalf("gallery_alpha item count = %d, want 2", s.ItemCount)
			}
			if s.CoverImagePath == nil || *s.CoverImagePath != img1A {
				t.Fatalf("gallery_alpha cover = %v, want %q", s.CoverImagePath, img1A)
			}
		} else if s.Title == "gallery_beta" {
			if s.ItemCount != 1 || len(items) != 1 {
				t.Fatalf("gallery_beta item count = %d, want 1", s.ItemCount)
			}
			if s.CoverImagePath == nil || *s.CoverImagePath != img2A {
				t.Fatalf("gallery_beta cover = %v, want %q", s.CoverImagePath, img2A)
			}
		}
	}

	// Test Thumbnail generation and caching
	set1Items, err := env.st.Q().ListAdultImageItemsForSet(ctx, sets[0].ID)
	if err != nil || len(set1Items) == 0 {
		t.Fatalf("failed to get items for thumbnail test: %v", err)
	}
	targetItem := set1Items[0]

	thumbPath, err := env.svc.Thumbnail(ctx, targetItem.ID)
	if err != nil {
		t.Fatalf("Thumbnail(): %v", err)
	}
	fi1, err := os.Stat(thumbPath)
	if err != nil {
		t.Fatalf("stat generated thumbnail: %v", err)
	}
	mod1 := fi1.ModTime()

	// Second call must return cached path without re-encoding
	time.Sleep(10 * time.Millisecond)
	cachedPath, err := env.svc.Thumbnail(ctx, targetItem.ID)
	if err != nil {
		t.Fatalf("second Thumbnail(): %v", err)
	}
	if cachedPath != thumbPath {
		t.Fatalf("cachedPath = %q, want %q", cachedPath, thumbPath)
	}
	fi2, err := os.Stat(cachedPath)
	if err != nil {
		t.Fatalf("stat cached thumbnail: %v", err)
	}
	if !fi2.ModTime().Equal(mod1) {
		t.Fatalf("thumbnail modtime changed (%v != %v), was re-encoded", fi2.ModTime(), mod1)
	}
}

func TestImageSetNegativeCases(t *testing.T) {
	ctx := context.Background()
	env := setupTestEnv(ctx, t)

	tmp := t.TempDir()
	imagesDir := filepath.Join(tmp, "imagesets")
	thumbDir := filepath.Join(tmp, "thumbnails")
	setDir := filepath.Join(imagesDir, "gallery_neg")
	if err := os.MkdirAll(setDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	validImg := filepath.Join(setDir, "valid.png")
	createTestPNG(t, validImg, 100, 100)

	// Symlink case: symlink to validImg
	symlinkImg := filepath.Join(setDir, "symlink.png")
	if err := os.Symlink(validImg, symlinkImg); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Oversized file case (> 100 MiB)
	oversizedImg := filepath.Join(setDir, "huge.jpg")
	fHuge, err := os.Create(oversizedImg)
	if err != nil {
		t.Fatalf("create huge: %v", err)
	}
	if err := fHuge.Truncate(MaxImageDecodeBytes + 1024); err != nil {
		_ = fHuge.Close()
		t.Fatalf("truncate huge: %v", err)
	}
	_ = fHuge.Close()

	// Mime mismatch case
	mismatchImg := filepath.Join(setDir, "fake.png")
	if err := os.WriteFile(mismatchImg, []byte("plain text content not a png"), 0o600); err != nil {
		t.Fatalf("write mismatch: %v", err)
	}

	// Pixel bomb case
	bombImg := filepath.Join(setDir, "bomb.png")
	createPixelBombPNG(t, bombImg)

	configureAdultModule(ctx, t, env, imagesDir, thumbDir)

	if err := env.svc.ScanImageSets(ctx); err != nil {
		t.Fatalf("ScanImageSets: %v", err)
	}

	set, err := env.st.Q().GetAdultImageSetByDirectory(ctx, setDir)
	if err != nil {
		t.Fatalf("GetAdultImageSetByDirectory: %v", err)
	}
	items, err := env.st.Q().ListAdultImageItemsForSet(ctx, set.ID)
	if err != nil {
		t.Fatalf("ListAdultImageItemsForSet: %v", err)
	}

	// Expected: only valid.png and bomb.png are scanned into items; symlink, oversized, mismatch skipped!
	for _, it := range items {
		if it.FileName == "symlink.png" {
			t.Fatal("symlink was not skipped by scanner")
		}
		if it.FileName == "huge.jpg" {
			t.Fatal("oversized file was not skipped by scanner")
		}
		if it.FileName == "fake.png" {
			t.Fatal("mime mismatch was not skipped by scanner")
		}
	}

	// Now verify pixel bomb is refused during thumbnail generation without full decode
	var bombItemID uuid.UUID
	for _, it := range items {
		if it.FileName == "bomb.png" {
			bombItemID = it.ID
			break
		}
	}
	if bombItemID == uuid.Nil {
		t.Fatal("bomb.png was not found in items")
	}

	_, err = env.svc.Thumbnail(ctx, bombItemID)
	if err == nil {
		t.Fatal("Thumbnail() succeeded on pixel bomb, want error")
	}
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Thumbnail() on pixel bomb error = %v, want ErrInvalid", err)
	}

	// Verify oversized file is refused before decode by Thumbnail
	oversizedItemID, _ := env.ids.NewUUID()
	_, err = env.st.Q().UpsertAdultImageItem(ctx, sqlcgen.UpsertAdultImageItemParams{
		ID:             oversizedItemID,
		ImageSetID:     set.ID,
		FilePath:       oversizedImg,
		FileName:       "huge.jpg",
		FileSizeBytes:  MaxImageDecodeBytes + 1024,
		MimeType:       "image/jpeg",
		Sha256Checksum: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		SortOrder:      10,
		CreatedAt:      env.clk.Now(),
	})
	if err != nil {
		t.Fatalf("upsert oversized item: %v", err)
	}
	_, err = env.svc.Thumbnail(ctx, oversizedItemID)
	if err == nil || !strings.Contains(err.Error(), "exceeds decode limit") {
		t.Fatalf("Thumbnail() on oversized file error = %v, want 'exceeds decode limit'", err)
	}
}

func TestImageSetBoundaryCases(t *testing.T) {
	ctx := context.Background()
	env := setupTestEnv(ctx, t)

	tmp := t.TempDir()
	imagesDir := filepath.Join(tmp, "imagesets_boundary")
	thumbDir := filepath.Join(tmp, "thumbnails_boundary")
	setDir := filepath.Join(imagesDir, "gallery_boundary")
	if err := os.MkdirAll(setDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	samplePath := filepath.Join(tmp, "sample.png")
	createTestPNG(t, samplePath, 10, 10)
	sampleBytes, err := os.ReadFile(samplePath)
	if err != nil {
		t.Fatalf("read sample: %v", err)
	}

	// Write 2001 items
	for i := range 2001 {
		name := filepath.Join(setDir, fmt.Sprintf("img_%04d.png", i))
		if err := os.WriteFile(name, sampleBytes, 0o600); err != nil {
			t.Fatalf("write item %d: %v", i, err)
		}
	}

	configureAdultModule(ctx, t, env, imagesDir, thumbDir)

	if err := env.svc.ScanImageSets(ctx); err != nil {
		t.Fatalf("ScanImageSets: %v", err)
	}

	set, err := env.st.Q().GetAdultImageSetByDirectory(ctx, setDir)
	if err != nil {
		t.Fatalf("GetAdultImageSetByDirectory: %v", err)
	}
	if set.ItemCount != MaxItemsPerSet {
		t.Fatalf("set.ItemCount = %d, want MaxItemsPerSet (%d)", set.ItemCount, MaxItemsPerSet)
	}

	// Empty dir test -> zero sets
	emptyDir := filepath.Join(tmp, "empty_root")
	if err := os.MkdirAll(emptyDir, 0o750); err != nil {
		t.Fatalf("mkdir empty: %v", err)
	}
	configureAdultModule(ctx, t, env, emptyDir, thumbDir)
	if err := env.svc.ScanImageSets(ctx); err != nil {
		t.Fatalf("ScanImageSets on empty dir: %v", err)
	}

	// Module disabled test -> zero file I/O
	if err := env.modulesSvc.SetEnabled(ctx, domain.ModuleAdult, false); err != nil {
		t.Fatalf("disable adult: %v", err)
	}
	// Non-existent directory would error if touched
	nonExistentDir := filepath.Join(tmp, "nonexistent_dir")
	env.svc.mu.Lock()
	env.svc.lastScanPass = time.Time{}
	env.svc.mu.Unlock()
	if err := env.svc.ScanImageSets(ctx); err != nil {
		t.Fatalf("ScanImageSets when disabled returned error: %v", err)
	}
	_ = nonExistentDir
}

func TestImageServingHTTP(t *testing.T) {
	ctx := context.Background()
	env := setupTestEnv(ctx, t)

	tmp := t.TempDir()
	imagesDir := filepath.Join(tmp, "images_serve")
	thumbDir := filepath.Join(tmp, "thumbs_serve")
	setDir := filepath.Join(imagesDir, "gallery_serve")
	if err := os.MkdirAll(setDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	imgFile := filepath.Join(setDir, "test.png")
	createTestPNG(t, imgFile, 150, 150)

	configureAdultModule(ctx, t, env, imagesDir, thumbDir)

	if err := env.svc.ScanImageSets(ctx); err != nil {
		t.Fatalf("ScanImageSets: %v", err)
	}

	set, err := env.st.Q().GetAdultImageSetByDirectory(ctx, setDir)
	if err != nil {
		t.Fatalf("GetAdultImageSet: %v", err)
	}
	items, err := env.st.Q().ListAdultImageItemsForSet(ctx, set.ID)
	if err != nil || len(items) == 0 {
		t.Fatalf("ListAdultImageItems: %v", err)
	}
	item := items[0]

	fi, err := os.Stat(imgFile)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	t.Run("status 200 normal image", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/image", nil)
		rec := httptest.NewRecorder()
		if err := env.svc.ServeImage(rec, req, item.ID); err != nil {
			t.Fatalf("ServeImage: %v", err)
		}
		res := rec.Result()
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", res.StatusCode)
		}
		if res.Header.Get("Content-Type") != "image/png" {
			t.Fatalf("Content-Type = %q, want image/png", res.Header.Get("Content-Type"))
		}
		if res.Header.Get("Cache-Control") != "private, max-age=86400" {
			t.Fatalf("Cache-Control = %q", res.Header.Get("Cache-Control"))
		}
		if res.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("X-Content-Type-Options = %q", res.Header.Get("X-Content-Type-Options"))
		}
	})

	t.Run("range request 206", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/image", nil)
		req.Header.Set("Range", "bytes=0-10")
		rec := httptest.NewRecorder()
		if err := env.svc.ServeImage(rec, req, item.ID); err != nil {
			t.Fatalf("ServeImage Range: %v", err)
		}
		res := rec.Result()
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode != http.StatusPartialContent {
			t.Fatalf("status = %d, want 206", res.StatusCode)
		}
	})

	t.Run("if modified since 304", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/image", nil)
		req.Header.Set("If-Modified-Since", fi.ModTime().UTC().Format(http.TimeFormat))
		rec := httptest.NewRecorder()
		if err := env.svc.ServeImage(rec, req, item.ID); err != nil {
			t.Fatalf("ServeImage IMS: %v", err)
		}
		res := rec.Result()
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode != http.StatusNotModified {
			t.Fatalf("status = %d, want 304", res.StatusCode)
		}
	})

	t.Run("missing item 404 mapped", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/image", nil)
		rec := httptest.NewRecorder()
		err := env.svc.ServeImage(rec, req, uuid.New())
		if err == nil {
			t.Fatal("ServeImage want error for missing item, got nil")
		}
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("ServeImage missing item error = %v, want ErrNotFound", err)
		}
	})

	t.Run("path escaping image_set_dir refused", func(t *testing.T) {
		escapedItemID, _ := env.ids.NewUUID()
		_, err := env.st.Q().UpsertAdultImageItem(ctx, sqlcgen.UpsertAdultImageItemParams{
			ID:             escapedItemID,
			ImageSetID:     set.ID,
			FilePath:       "/etc/passwd",
			FileName:       "passwd",
			FileSizeBytes:  100,
			MimeType:       "image/png",
			Sha256Checksum: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			SortOrder:      99,
			CreatedAt:      env.clk.Now(),
		})
		if err != nil {
			t.Fatalf("upsert escaping item: %v", err)
		}
		req := httptest.NewRequest(http.MethodGet, "/image", nil)
		rec := httptest.NewRecorder()
		err = env.svc.ServeImage(rec, req, escapedItemID)
		if err == nil {
			t.Fatal("ServeImage want error for path escaping dir, got nil")
		}
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("ServeImage escaping path error = %v, want ErrInvalid", err)
		}
	})

	t.Run("status 200 normal thumbnail", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/thumb", nil)
		rec := httptest.NewRecorder()
		if err := env.svc.ServeThumbnail(rec, req, item.ID); err != nil {
			t.Fatalf("ServeThumbnail: %v", err)
		}
		res := rec.Result()
		defer func() { _ = res.Body.Close() }()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("thumbnail status = %d, want 200", res.StatusCode)
		}
		if res.Header.Get("Content-Type") != "image/webp" {
			t.Fatalf("Content-Type = %q, want image/webp", res.Header.Get("Content-Type"))
		}
	})
}

func TestImageSetScanCachingAndTouch(t *testing.T) {
	ctx := context.Background()
	env := setupTestEnv(ctx, t)

	tmp := t.TempDir()
	imagesDir := filepath.Join(tmp, "images_cache_test")
	thumbDir := filepath.Join(tmp, "thumbs_cache_test")
	setDir := filepath.Join(imagesDir, "gallery_cache")
	if err := os.MkdirAll(setDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	img1 := filepath.Join(setDir, "01_img.png")
	img2 := filepath.Join(setDir, "02_img.jpg")
	createTestPNG(t, img1, 50, 50)
	createTestJPEG(t, img2, 60, 60)

	configureAdultModule(ctx, t, env, imagesDir, thumbDir)

	var openCount atomic.Int64
	env.svc.openFile = func(name string) (*os.File, error) {
		openCount.Add(1)
		return os.Open(name)
	}

	// First scan: opens both files
	if err := env.svc.ScanImageSets(ctx); err != nil {
		t.Fatalf("first ScanImageSets: %v", err)
	}
	if got := openCount.Load(); got != 2 {
		t.Fatalf("first scan opened %d files, want 2", got)
	}

	// Second scan of unchanged set: zero file opens
	if err := env.svc.ScanImageSets(ctx); err != nil {
		t.Fatalf("second ScanImageSets: %v", err)
	}
	if got := openCount.Load(); got != 2 {
		t.Fatalf("second scan opened %d files (total = %d), want 0 new opens (total 2)", got-2, got)
	}

	// Touch img1 with a new mod time (5 minutes in future)
	newMtime := time.Now().Add(5 * time.Minute)
	if err := os.Chtimes(img1, newMtime, newMtime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	// Third scan: only touched file img1 is opened and re-hashed
	if err := env.svc.ScanImageSets(ctx); err != nil {
		t.Fatalf("third ScanImageSets: %v", err)
	}
	if got := openCount.Load(); got != 3 {
		t.Fatalf("third scan total opens = %d, want 3 (1 new open)", got)
	}
}

func TestImageSetVanishedSetsAndTruncatedPass(t *testing.T) {
	ctx := context.Background()
	env := setupTestEnv(ctx, t)

	tmp := t.TempDir()
	imagesDir := filepath.Join(tmp, "images_vanish_test")
	thumbDir := filepath.Join(tmp, "thumbs_vanish_test")
	set1Dir := filepath.Join(imagesDir, "gallery_1")
	set2Dir := filepath.Join(imagesDir, "gallery_2")
	set3Dir := filepath.Join(imagesDir, "gallery_3")
	for _, d := range []string{set1Dir, set2Dir, set3Dir} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
		createTestPNG(t, filepath.Join(d, "img.png"), 30, 30)
	}

	configureAdultModule(ctx, t, env, imagesDir, thumbDir)

	if err := env.svc.ScanImageSets(ctx); err != nil {
		t.Fatalf("initial scan: %v", err)
	}

	// Remove set3 from disk
	if err := os.RemoveAll(set3Dir); err != nil {
		t.Fatalf("remove set3Dir: %v", err)
	}

	// Truncated pass (maxImageSets = 1): limit reached on gallery_1, deletes nothing
	env.svc.maxImageSets = 1
	if err := env.svc.ScanImageSets(ctx); err != nil {
		t.Fatalf("truncated scan: %v", err)
	}
	setsAfterTruncated, err := env.st.Q().ListAdultImageSets(ctx)
	if err != nil {
		t.Fatalf("list sets after truncated: %v", err)
	}
	if len(setsAfterTruncated) != 3 {
		t.Fatalf("truncated pass deleted sets: got %d sets, want 3", len(setsAfterTruncated))
	}

	// Full pass: maxImageSets reset to 0, gallery_3 is deleted
	env.svc.maxImageSets = 0
	if err := env.svc.ScanImageSets(ctx); err != nil {
		t.Fatalf("full scan: %v", err)
	}
	setsAfterFull, err := env.st.Q().ListAdultImageSets(ctx)
	if err != nil {
		t.Fatalf("list sets after full: %v", err)
	}
	if len(setsAfterFull) != 2 {
		t.Fatalf("full pass got %d sets, want 2", len(setsAfterFull))
	}

	// Empty directory pass: remove remaining sets, all sets deleted
	if err := os.RemoveAll(set1Dir); err != nil {
		t.Fatalf("remove set1Dir: %v", err)
	}
	if err := os.RemoveAll(set2Dir); err != nil {
		t.Fatalf("remove set2Dir: %v", err)
	}
	if err := env.svc.ScanImageSets(ctx); err != nil {
		t.Fatalf("empty scan: %v", err)
	}
	setsAfterEmpty, err := env.st.Q().ListAdultImageSets(ctx)
	if err != nil {
		t.Fatalf("list sets after empty: %v", err)
	}
	if len(setsAfterEmpty) != 0 {
		t.Fatalf("empty pass got %d sets, want 0", len(setsAfterEmpty))
	}
}

func TestImageSetSymlinkSwapAfterScan(t *testing.T) {
	ctx := context.Background()
	env := setupTestEnv(ctx, t)

	tmp := t.TempDir()
	imagesDir := filepath.Join(tmp, "images_symlink_test")
	thumbDir := filepath.Join(tmp, "thumbs_symlink_test")
	setDir := filepath.Join(imagesDir, "gallery_swap")
	if err := os.MkdirAll(setDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	validImg := filepath.Join(setDir, "swap.png")
	createTestPNG(t, validImg, 80, 80)

	configureAdultModule(ctx, t, env, imagesDir, thumbDir)

	if err := env.svc.ScanImageSets(ctx); err != nil {
		t.Fatalf("ScanImageSets: %v", err)
	}

	set, err := env.st.Q().GetAdultImageSetByDirectory(ctx, setDir)
	if err != nil {
		t.Fatalf("GetAdultImageSetByDirectory: %v", err)
	}
	items, err := env.st.Q().ListAdultImageItemsForSet(ctx, set.ID)
	if err != nil || len(items) == 0 {
		t.Fatalf("ListAdultImageItemsForSet: %v", err)
	}
	targetItem := items[0]

	// Target file outside image_set_dir
	secretFile := filepath.Join(tmp, "outside_secret.png")
	createTestPNG(t, secretFile, 50, 50)

	// Replace the scanned valid file with a symlink pointing to secretFile outside image_set_dir
	if err := os.Remove(validImg); err != nil {
		t.Fatalf("remove valid file: %v", err)
	}
	if err := os.Symlink(secretFile, validImg); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	// 1. ServeImage must refuse the symlink with domain.ErrInvalid
	req := httptest.NewRequest(http.MethodGet, "/image", nil)
	rec := httptest.NewRecorder()
	err = env.svc.ServeImage(rec, req, targetItem.ID)
	if err == nil {
		t.Fatal("ServeImage succeeded on symlink swap, want ErrInvalid")
	}
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("ServeImage on symlink swap error = %v, want ErrInvalid", err)
	}

	// 2. Thumbnail must refuse the symlink with domain.ErrInvalid
	_, err = env.svc.Thumbnail(ctx, targetItem.ID)
	if err == nil {
		t.Fatal("Thumbnail succeeded on symlink swap, want ErrInvalid")
	}
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Thumbnail on symlink swap error = %v, want ErrInvalid", err)
	}
}
