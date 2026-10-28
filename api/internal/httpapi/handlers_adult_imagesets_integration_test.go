// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

func createTestPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func enableAdultWithDir(t *testing.T, f *routerFixture, adminCsrf, dir string) {
	t.Helper()
	payload := map[string]any{
		"enabled": true,
		"config": map[string]any{
			"image_set_dir":         dir,
			"sync_interval_minutes": 360,
		},
	}
	res := doHTTP(t, f.client, http.MethodPut, f.srv.URL+PathPrefix+"/system/modules/"+domain.ModuleAdult, adminCsrf, payload)
	if res.statusCode != http.StatusOK {
		t.Fatalf("enable adult module status = %d, want 200; body: %s", res.statusCode, string(res.body))
	}
}

func TestAdultImageSets_Positive_ListGetAndFileStream(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	adminCsrf := getAdminCsrfToken(t, f)

	rootDir := t.TempDir()
	setDir := filepath.Join(rootDir, "sample-image-set-1")
	if err := os.MkdirAll(setDir, 0o755); err != nil {
		t.Fatalf("mkdir set: %v", err)
	}
	pngBytes := createTestPNG(t)
	filePath := filepath.Join(setDir, "001.png")
	if err := os.WriteFile(filePath, pngBytes, 0o644); err != nil {
		t.Fatalf("write png: %v", err)
	}

	enableAdultWithDir(t, f, adminCsrf, rootDir)

	scanRes := doHTTP(t, f.client, http.MethodPost, f.srv.URL+PathPrefix+"/adult/scan", adminCsrf, nil)
	if scanRes.statusCode != http.StatusOK {
		t.Fatalf("scan status = %d, want 200; body: %s", scanRes.statusCode, string(scanRes.body))
	}
	var scanSummary map[string]any
	if err := json.Unmarshal(scanRes.body, &scanSummary); err != nil {
		t.Fatalf("unmarshal scan summary: %v", err)
	}
	if scanSummary["sets_found"] != float64(1) || scanSummary["items_found"] != float64(1) || scanSummary["ignore_file_present"] != true {
		t.Fatalf("unexpected scan summary: %+v", scanSummary)
	}

	stashIgnorePath := filepath.Join(rootDir, ".stashignore")
	if _, err := os.Stat(stashIgnorePath); err != nil {
		t.Fatalf(".stashignore missing at root: %v", err)
	}

	listRes := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/image-sets", "", nil)
	if listRes.statusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200; body: %s", listRes.statusCode, string(listRes.body))
	}
	var sets []map[string]any
	if err := json.Unmarshal(listRes.body, &sets); err != nil {
		t.Fatalf("unmarshal sets: %v", err)
	}
	if len(sets) != 1 {
		t.Fatalf("sets len = %d, want 1", len(sets))
	}
	setID := sets[0]["id"].(string)

	getRes := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/image-sets/"+setID, "", nil)
	if getRes.statusCode != http.StatusOK {
		t.Fatalf("get status = %d, want 200; body: %s", getRes.statusCode, string(getRes.body))
	}
	var detail map[string]any
	if err := json.Unmarshal(getRes.body, &detail); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	if detail["ignore_file_present"] != true {
		t.Errorf("detail ignore_file_present = false, want true")
	}
	rawItems, ok := detail["items"].([]any)
	if !ok || len(rawItems) != 1 {
		t.Fatalf("items count = %d, want 1", len(rawItems))
	}
	firstItem := rawItems[0].(map[string]any)
	itemID := firstItem["id"].(string)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.srv.URL+PathPrefix+"/adult/image-items/"+itemID+"/file", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("file status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !bytes.Equal(body, pngBytes) {
		t.Fatalf("file body len = %d, want %d", len(body), len(pngBytes))
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}
	if nosniff := resp.Header.Get("X-Content-Type-Options"); nosniff != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", nosniff)
	}
}

func TestAdultImageSets_Negative_NotFoundAndForbidden(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	adminCsrf := getAdminCsrfToken(t, f)

	rootDir := t.TempDir()
	enableAdultWithDir(t, f, adminCsrf, rootDir)

	unknownID := uuid.New().String()
	getSetRes := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/image-sets/"+unknownID, "", nil)
	assertProblemStatusAndType(t, getSetRes, http.StatusNotFound)

	getFileRes := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/image-items/"+unknownID+"/file", "", nil)
	assertProblemStatusAndType(t, getFileRes, http.StatusNotFound)

	viewerClient, viewerCsrf := loginViewerSession(t, f)
	scanRes := doHTTP(t, viewerClient, http.MethodPost, f.srv.URL+PathPrefix+"/adult/scan", viewerCsrf, nil)
	assertProblemStatusAndType(t, scanRes, http.StatusForbidden)
}

func TestAdultImageSets_Negative_ModuleOffReturns404(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	adminCsrf := getAdminCsrfToken(t, f)

	disableRes := doHTTP(t, f.client, http.MethodPut, f.srv.URL+PathPrefix+"/system/modules/"+domain.ModuleAdult, adminCsrf, map[string]any{"enabled": false})
	if disableRes.statusCode != http.StatusOK {
		t.Fatalf("disable module status = %d, want 200", disableRes.statusCode)
	}

	dummyID := uuid.New().String()
	endpoints := []struct {
		name   string
		method string
		path   string
	}{
		{"list image sets", http.MethodGet, "/adult/image-sets"},
		{"get image set", http.MethodGet, "/adult/image-sets/" + dummyID},
		{"trigger scan", http.MethodPost, "/adult/scan"},
		{"get item file", http.MethodGet, "/adult/image-items/" + dummyID + "/file"},
	}
	for _, ep := range endpoints {
		t.Run(ep.name, func(t *testing.T) {
			t.Parallel()
			res := doHTTP(t, f.client, ep.method, f.srv.URL+PathPrefix+ep.path, adminCsrf, nil)
			assertProblemStatusAndType(t, res, http.StatusNotFound)
		})
	}
}

func TestAdultImageSets_Negative_FileOutsideRootRefused(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	adminCsrf := getAdminCsrfToken(t, f)

	rootDir := t.TempDir()
	enableAdultWithDir(t, f, adminCsrf, rootDir)

	setID := uuid.New()
	if _, err := f.st.Q().UpsertAdultImageSet(t.Context(), sqlcgen.UpsertAdultImageSetParams{
		ID:            setID,
		Title:         "Escaped Set",
		DirectoryPath: filepath.Join(rootDir, "escaped"),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}); err != nil {
		t.Fatalf("insert set: %v", err)
	}

	outsidePath := filepath.Join(t.TempDir(), "secret.png")
	if writeErr := os.WriteFile(outsidePath, createTestPNG(t), 0o644); writeErr != nil {
		t.Fatalf("write outside file: %v", writeErr)
	}

	itemID := uuid.New()
	if _, err := f.st.Q().UpsertAdultImageItem(t.Context(), sqlcgen.UpsertAdultImageItemParams{
		ID:             itemID,
		ImageSetID:     setID,
		FilePath:       outsidePath,
		FileName:       "secret.png",
		FileSizeBytes:  100,
		MimeType:       "image/png",
		Sha256Checksum: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		SortOrder:      0,
		CreatedAt:      time.Now(),
	}); err != nil {
		t.Fatalf("insert item: %v", err)
	}

	fileRes := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/image-items/"+itemID.String()+"/file", "", nil)
	assertProblemClientRejected(t, fileRes)
}

func TestAdultImageSets_Boundary_EmptyDirZeroItemsAndPagination(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	adminCsrf := getAdminCsrfToken(t, f)

	rootDir := t.TempDir()
	enableAdultWithDir(t, f, adminCsrf, rootDir)

	// 1. Boundary: empty dir
	scanRes := doHTTP(t, f.client, http.MethodPost, f.srv.URL+PathPrefix+"/adult/scan", adminCsrf, nil)
	if scanRes.statusCode != http.StatusOK {
		t.Fatalf("scan empty dir status = %d, want 200", scanRes.statusCode)
	}
	var emptySummary map[string]any
	_ = json.Unmarshal(scanRes.body, &emptySummary)
	if emptySummary["sets_found"] != float64(0) || emptySummary["items_found"] != float64(0) {
		t.Errorf("empty dir expected 0 sets and items, got %+v", emptySummary)
	}

	listEmpty := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/image-sets", "", nil)
	if listEmpty.statusCode != http.StatusOK {
		t.Fatalf("list empty status = %d, want 200", listEmpty.statusCode)
	}
	var emptyList []any
	_ = json.Unmarshal(listEmpty.body, &emptyList)
	if len(emptyList) != 0 {
		t.Errorf("expected 0 image sets, got %d", len(emptyList))
	}

	// 2. Boundary: set with zero image items
	set1Dir := filepath.Join(rootDir, "image-set-zero")
	if err := os.MkdirAll(set1Dir, 0o755); err != nil {
		t.Fatalf("mkdir set1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(set1Dir, "notes.txt"), []byte("not an image"), 0o644); err != nil {
		t.Fatalf("write txt: %v", err)
	}

	// And a second set with 1 image for pagination boundary
	set2Dir := filepath.Join(rootDir, "image-set-one")
	if err := os.MkdirAll(set2Dir, 0o755); err != nil {
		t.Fatalf("mkdir set2: %v", err)
	}
	if err := os.WriteFile(filepath.Join(set2Dir, "001.png"), createTestPNG(t), 0o644); err != nil {
		t.Fatalf("write png: %v", err)
	}

	scanRes2 := doHTTP(t, f.client, http.MethodPost, f.srv.URL+PathPrefix+"/adult/scan", adminCsrf, nil)
	if scanRes2.statusCode != http.StatusOK {
		t.Fatalf("second scan status = %d, want 200", scanRes2.statusCode)
	}

	// 3. Boundary: limit=1 pagination
	page1 := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/image-sets?limit=1&offset=0", "", nil)
	if page1.statusCode != http.StatusOK {
		t.Fatalf("page 1 status = %d, want 200", page1.statusCode)
	}
	var p1Sets []map[string]any
	_ = json.Unmarshal(page1.body, &p1Sets)
	if len(p1Sets) != 1 {
		t.Fatalf("page 1 sets = %d, want 1", len(p1Sets))
	}

	page2 := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/image-sets?limit=1&offset=1", "", nil)
	if page2.statusCode != http.StatusOK {
		t.Fatalf("page 2 status = %d, want 200", page2.statusCode)
	}
	var p2Sets []map[string]any
	_ = json.Unmarshal(page2.body, &p2Sets)
	if len(p2Sets) != 1 {
		t.Fatalf("page 2 sets = %d, want 1", len(p2Sets))
	}
	if p1Sets[0]["id"] == p2Sets[0]["id"] {
		t.Errorf("page 1 and page 2 returned same set: %v", p1Sets[0]["id"])
	}
}
