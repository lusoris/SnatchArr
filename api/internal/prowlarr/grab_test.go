// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package prowlarr_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/prowlarr"
	"github.com/lusoris/SnatchArr/api/internal/prowlarrclient"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

type grabProwlarrServer struct {
	mu            sync.Mutex
	requests      []*http.Request
	requestBodies [][]byte
	searchStatus  int
	grabStatus    int
	searchResults []map[string]any
	indexers      []map[string]any
	indexerStatus []map[string]any
	indexerStats  map[string]any
}

func newGrabProwlarr(t *testing.T) (*grabProwlarrServer, *httptest.Server) {
	t.Helper()
	fps := &grabProwlarrServer{
		searchStatus: http.StatusOK,
		grabStatus:   http.StatusOK,
		indexers: []map[string]any{
			{"id": 1, "name": "Indexer 1", "enable": true, "fields": []map[string]any{}},
			{"id": 2, "name": "Indexer 2", "enable": true, "fields": []map[string]any{}},
		},
		indexerStats: map[string]any{
			"indexers": []map[string]any{
				{"indexerId": 1, "numberOfQueries": 0, "numberOfGrabs": 0},
				{"indexerId": 2, "numberOfQueries": 0, "numberOfGrabs": 0},
			},
		},
		indexerStatus: []map[string]any{},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fps.mu.Lock()
		fps.requests = append(fps.requests, r)
		fps.requestBodies = append(fps.requestBodies, body)
		searchStatus := fps.searchStatus
		grabStatus := fps.grabStatus
		searchResults := fps.searchResults
		indexers := fps.indexers
		indexerStats := fps.indexerStats
		indexerStatus := fps.indexerStatus
		fps.mu.Unlock()

		if r.Header.Get("X-Api-Key") != "prowl-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/system/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"version": "1.15.0"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/indexer":
			_ = json.NewEncoder(w).Encode(indexers)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/indexerstats":
			_ = json.NewEncoder(w).Encode(indexerStats)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/indexerstatus":
			_ = json.NewEncoder(w).Encode(indexerStatus)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications":
			_ = json.NewEncoder(w).Encode([]any{})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/search":
			if searchStatus != http.StatusOK {
				w.WriteHeader(searchStatus)
				return
			}
			if searchResults == nil {
				_ = json.NewEncoder(w).Encode([]any{})
			} else {
				_ = json.NewEncoder(w).Encode(searchResults)
			}
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/search":
			if grabStatus != http.StatusOK {
				w.WriteHeader(grabStatus)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1})
		default:
			http.NotFound(w, r)
		}
	})

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return fps, srv
}

type grabFixture struct {
	svc    *prowlarr.Service
	fps    *grabProwlarrServer
	linkID uuid.UUID
}

func setupGrabFixture(t *testing.T) grabFixture {
	t.Helper()
	fps, srv := newGrabProwlarr(t)
	st := storetest.New(t)
	clk := clock.NewFake()
	enc, err := crypto.NewEncryptor(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("encryptor: %v", err)
	}
	prowlClient := prowlarrclient.New(5*time.Second, "SnatchArr-test")
	prober := arrclient.NewGoenvoyProber(arrclient.DefaultOptions())
	instSvc := instances.New(st, enc, clk, id.New(), prober, slog.New(slog.DiscardHandler))
	cfg := config.Options{
		Prowlarr: config.ProwlarrOptions{LimitThreshold: 0.9},
	}
	svc := prowlarr.New(st, enc, clk, id.New(), prowlClient, prober, instSvc, slog.New(slog.DiscardHandler), cfg)

	link, err := svc.Create(t.Context(), prowlarr.Input{
		Name:    "prowlarr-test",
		BaseURL: srv.URL,
		APIKey:  "prowl-key",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("create link: %v", err)
	}

	return grabFixture{
		svc:    svc,
		fps:    fps,
		linkID: link.ID,
	}
}

func TestGrabForItem_Table(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		indexers      []map[string]any
		indexerStatus []map[string]any
		indexerStats  map[string]any
		searchResults []map[string]any
		searchStatus  int
		grabStatus    int
		wantErr       bool
		wantNoRelease bool
		wantGrabbedID string
		wantGrabCalls int
	}{
		{
			name: "boundary exactly one release -> grabbed",
			searchResults: []map[string]any{
				{"guid": "release-one-only", "indexerId": 1, "title": "Game Single"},
			},
			wantGrabbedID: "release-one-only",
			wantGrabCalls: 1,
		},
		{
			name:          "search returns zero releases -> ErrNoRelease, zero grab calls",
			searchResults: []map[string]any{},
			wantErr:       true,
			wantNoRelease: true,
			wantGrabCalls: 0,
		},
		{
			name: "multiple releases: first indexer disabled, second enabled -> second grabbed",
			indexers: []map[string]any{
				{"id": 1, "name": "Disabled Indexer", "enable": false, "fields": []map[string]any{}},
				{"id": 2, "name": "Active Indexer", "enable": true, "fields": []map[string]any{}},
			},
			searchResults: []map[string]any{
				{"guid": "rel-disabled", "indexerId": 1, "title": "Game on Disabled"},
				{"guid": "rel-active", "indexerId": 2, "title": "Game on Active"},
			},
			wantGrabbedID: "rel-active",
			wantGrabCalls: 1,
		},
		{
			name: "multiple releases: first indexer rate-limited, second enabled -> second grabbed",
			indexerStatus: []map[string]any{
				{"id": 10, "indexerId": 1, "disabledTill": "2030-01-01T00:00:00Z"},
			},
			searchResults: []map[string]any{
				{"guid": "rel-rate-limited", "indexerId": 1, "title": "Game on RateLimited"},
				{"guid": "rel-healthy", "indexerId": 2, "title": "Game on Healthy"},
			},
			wantGrabbedID: "rel-healthy",
			wantGrabCalls: 1,
		},
		{
			name: "multiple releases: first indexer exhausted by limit, second enabled -> second grabbed",
			indexers: []map[string]any{
				{
					"id": 1, "name": "Exhausted Indexer", "enable": true,
					"fields": []map[string]any{
						{"name": "baseSettings.queryLimit", "value": 10},
					},
				},
				{"id": 2, "name": "Unlimited Indexer", "enable": true, "fields": []map[string]any{}},
			},
			indexerStats: map[string]any{
				"indexers": []map[string]any{
					{"indexerId": 1, "numberOfQueries": 10, "numberOfGrabs": 0},
					{"indexerId": 2, "numberOfQueries": 0, "numberOfGrabs": 0},
				},
			},
			searchResults: []map[string]any{
				{"guid": "rel-exhausted", "indexerId": 1, "title": "Game on Exhausted"},
				{"guid": "rel-free", "indexerId": 2, "title": "Game on Free"},
			},
			wantGrabbedID: "rel-free",
			wantGrabCalls: 1,
		},
		{
			name: "all releases from disabled indexers -> ErrNoRelease, zero grab calls",
			indexers: []map[string]any{
				{"id": 1, "name": "Disabled Indexer", "enable": false, "fields": []map[string]any{}},
			},
			searchResults: []map[string]any{
				{"guid": "rel-all-disabled", "indexerId": 1, "title": "Game on Disabled"},
			},
			wantErr:       true,
			wantNoRelease: true,
			wantGrabCalls: 0,
		},
		{
			name:         "stub 500 on search -> wrapped error",
			searchStatus: http.StatusInternalServerError,
			wantErr:      true,
		},
		{
			name: "stub 500 on grab -> wrapped error",
			searchResults: []map[string]any{
				{"guid": "rel-grab-fail", "indexerId": 1, "title": "Game Fail Grab"},
			},
			grabStatus:    http.StatusInternalServerError,
			wantErr:       true,
			wantGrabCalls: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := setupGrabFixture(t)
			f.fps.mu.Lock()
			if tc.indexers != nil {
				f.fps.indexers = tc.indexers
			}
			if tc.indexerStatus != nil {
				f.fps.indexerStatus = tc.indexerStatus
			}
			if tc.indexerStats != nil {
				f.fps.indexerStats = tc.indexerStats
			}
			f.fps.searchResults = tc.searchResults
			if tc.searchStatus != 0 {
				f.fps.searchStatus = tc.searchStatus
			}
			if tc.grabStatus != 0 {
				f.fps.grabStatus = tc.grabStatus
			}
			f.fps.mu.Unlock()

			q := prowlarrclient.SearchQuery{Query: "Pokemon"}
			rel, err := f.svc.GrabForItem(t.Context(), f.linkID, q)

			if (err != nil) != tc.wantErr {
				t.Fatalf("GrabForItem() error = %v, wantErr = %v", err, tc.wantErr)
			}
			if tc.wantNoRelease && !errors.Is(err, prowlarr.ErrNoRelease) {
				t.Errorf("expected ErrNoRelease, got %v", err)
			}
			if !tc.wantErr && rel.GUID != tc.wantGrabbedID {
				t.Errorf("grabbed GUID = %q, want %q", rel.GUID, tc.wantGrabbedID)
			}

			f.fps.mu.Lock()
			defer f.fps.mu.Unlock()
			grabCalls := 0
			for i, r := range f.fps.requests {
				if r.Method == http.MethodPost && r.URL.Path == "/api/v1/search" {
					grabCalls++
					if tc.wantGrabbedID != "" {
						var body map[string]any
						_ = json.Unmarshal(f.fps.requestBodies[i], &body)
						if body["guid"] != tc.wantGrabbedID {
							t.Errorf("POST /api/v1/search body guid = %v, want %q", body["guid"], tc.wantGrabbedID)
						}
						if r.Header.Get("X-Api-Key") != "prowl-key" {
							t.Errorf("POST /api/v1/search X-Api-Key = %q, want prowl-key", r.Header.Get("X-Api-Key"))
						}
					}
				}
			}
			if grabCalls != tc.wantGrabCalls {
				t.Errorf("grab calls = %d, want %d", grabCalls, tc.wantGrabCalls)
			}
		})
	}
}

func TestGrabForItem_NoDownloadClientCalls(t *testing.T) {
	t.Parallel()
	f := setupGrabFixture(t)
	f.fps.mu.Lock()
	f.fps.searchResults = []map[string]any{
		{"guid": "release-clean-path", "indexerId": 1, "title": "Clean Path Test"},
	}
	f.fps.mu.Unlock()

	q := prowlarrclient.SearchQuery{Query: "Clean Path"}
	rel, err := f.svc.GrabForItem(t.Context(), f.linkID, q)
	if err != nil {
		t.Fatalf("GrabForItem() err = %v", err)
	}
	if rel.GUID != "release-clean-path" {
		t.Errorf("grabbed GUID = %q, want %q", rel.GUID, "release-clean-path")
	}

	f.fps.mu.Lock()
	defer f.fps.mu.Unlock()

	bannedSubstrings := []string{
		"downloadclient",
		"torrents",
		"transmission",
		"deluge",
		"qbit",
		"sabnzbd",
		"nzbget",
	}

	for _, req := range f.fps.requests {
		// A test proves no request goes to any path outside /api/v1/ on the Prowlarr stub
		if !strings.HasPrefix(req.URL.Path, "/api/v1/") {
			t.Errorf("unexpected request to path outside /api/v1/: %s", req.URL.Path)
		}
		for _, banned := range bannedSubstrings {
			if strings.Contains(strings.ToLower(req.URL.Path), banned) {
				t.Errorf("forbidden download client call to path: %s", req.URL.Path)
			}
		}
	}
}

func TestGrabForItem_DisabledOrNotFoundLink(t *testing.T) {
	t.Parallel()
	f := setupGrabFixture(t)

	// Non-existent link
	missingID := uuid.New()
	_, err := f.svc.GrabForItem(t.Context(), missingID, prowlarrclient.SearchQuery{Query: "Test"})
	if err == nil {
		t.Error("expected error for non-existent link ID, got nil")
	}

	// Disabled link
	_, err = f.svc.Update(t.Context(), f.linkID, prowlarr.Input{
		Name:    "prowlarr-disabled",
		BaseURL: "http://127.0.0.1:9696",
		Enabled: false,
	})
	if err != nil {
		t.Fatalf("update link: %v", err)
	}

	_, err = f.svc.GrabForItem(t.Context(), f.linkID, prowlarrclient.SearchQuery{Query: "Test"})
	if err == nil {
		t.Error("expected error for disabled link, got nil")
	}
}
