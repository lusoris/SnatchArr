// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package prowlarrclient_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/prowlarrclient"
)

func fakeProwlarr(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Api-Key") != "prowl-key" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			next(w, r)
		}
	}
	write := func(w http.ResponseWriter, v any) {
		if err := json.NewEncoder(w).Encode(v); err != nil {
			t.Errorf("encode: %v", err)
		}
	}
	mux.HandleFunc("GET /api/v1/system/status", auth(func(w http.ResponseWriter, _ *http.Request) {
		write(w, map[string]any{"version": "1.15.0.4361"})
	}))
	mux.HandleFunc("GET /api/v1/indexer", auth(func(w http.ResponseWriter, _ *http.Request) {
		write(w, []map[string]any{
			{
				"id":     1,
				"name":   "Indexer Day",
				"enable": true,
				"fields": []map[string]any{
					{"name": "baseSettings.queryLimit", "value": 100},
					{"name": "baseSettings.grabLimit", "value": 50},
					{"name": "baseSettings.limitsUnit", "value": 0},
				},
			},
			{
				"id":     2,
				"name":   "Indexer Hour",
				"enable": true,
				"fields": []map[string]any{
					{"name": "baseSettings.queryLimit", "value": "20"},
					{"name": "baseSettings.limitsUnit", "value": 1},
				},
			},
			{
				"id":     3,
				"name":   "Indexer Unlimited",
				"enable": true,
				"fields": []map[string]any{},
			},
		})
	}))
	mux.HandleFunc("GET /api/v1/indexerstats", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("startDate") == "" || r.URL.Query().Get("endDate") == "" {
			t.Errorf("missing startDate or endDate: %s", r.URL.RawQuery)
		}
		write(w, map[string]any{
			"indexers": []map[string]any{
				{"indexerId": 1, "numberOfQueries": 80, "numberOfRssQueries": 10, "numberOfGrabs": 5},
				{"indexerId": 2, "numberOfQueries": 15, "numberOfRssQueries": 0, "numberOfGrabs": 2},
			},
		})
	}))
	mux.HandleFunc("GET /api/v1/indexerstatus", auth(func(w http.ResponseWriter, _ *http.Request) {
		write(w, []map[string]any{
			{"id": 99, "indexerId": 2, "disabledTill": "2026-10-16T22:00:00Z"},
			{"id": 100, "indexerId": 3, "disabledTill": nil},
		})
	}))
	mux.HandleFunc("GET /api/v1/applications", auth(func(w http.ResponseWriter, _ *http.Request) {
		write(w, []map[string]any{
			{
				"id":   1,
				"name": "Sonarr",
				"fields": []map[string]any{
					{"name": "prowlarrUrl", "value": "http://prowlarr:9696"},
				},
			},
		})
	}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestHTTPClient_Status(t *testing.T) {
	t.Parallel()
	srv := fakeProwlarr(t)
	c := prowlarrclient.New(5*time.Second, "SnatchArr-test")
	ctx := context.Background()

	tests := []struct {
		name    string
		url     string
		key     string
		wantVer string
		wantErr bool
	}{
		{"positive status 200", srv.URL, "prowl-key", "1.15.0.4361", false},
		{"negative bad key 401", srv.URL, "bad-key", "", true},
		{"boundary unreachable url", "http://127.0.0.1:1", "prowl-key", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			st, err := c.Status(ctx, tc.url, tc.key)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Status() error = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && st.Version != tc.wantVer {
				t.Errorf("Status() version = %q, want %q", st.Version, tc.wantVer)
			}
		})
	}
}

func TestHTTPClient_Indexers(t *testing.T) {
	t.Parallel()
	srv := fakeProwlarr(t)
	c := prowlarrclient.New(5*time.Second, "SnatchArr-test")
	ctx := context.Background()

	indexers, err := c.Indexers(ctx, srv.URL, "prowl-key")
	if err != nil {
		t.Fatalf("Indexers() err = %v", err)
	}
	if len(indexers) != 3 {
		t.Fatalf("Indexers() len = %d, want 3", len(indexers))
	}
	// Indexer 1: day limited
	if indexers[0].ID != 1 || !indexers[0].Enabled || indexers[0].QueryLimit == nil || *indexers[0].QueryLimit != 100 ||
		indexers[0].GrabLimit == nil || *indexers[0].GrabLimit != 50 || indexers[0].LimitsUnit != 0 {
		t.Errorf("Indexer 0 unexpected: %+v", indexers[0])
	}
	// Indexer 2: hour limited parsed from string
	if indexers[1].ID != 2 || !indexers[1].Enabled || indexers[1].QueryLimit == nil || *indexers[1].QueryLimit != 20 ||
		indexers[1].GrabLimit != nil || indexers[1].LimitsUnit != 1 {
		t.Errorf("Indexer 1 unexpected: %+v", indexers[1])
	}
	// Indexer 3: unlimited
	if indexers[2].ID != 3 || !indexers[2].Enabled || indexers[2].QueryLimit != nil || indexers[2].GrabLimit != nil {
		t.Errorf("Indexer 2 unexpected: %+v", indexers[2])
	}
}

func TestHTTPClient_Usage(t *testing.T) {
	t.Parallel()
	srv := fakeProwlarr(t)
	c := prowlarrclient.New(5*time.Second, "SnatchArr-test")
	ctx := context.Background()

	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	usage, err := c.Usage(ctx, srv.URL, "prowl-key", now.Add(-24*time.Hour), now)
	if err != nil {
		t.Fatalf("Usage() err = %v", err)
	}
	if len(usage) != 2 {
		t.Fatalf("Usage() len = %d, want 2", len(usage))
	}
	if u1 := usage[1]; u1.Queries != 90 || u1.Grabs != 5 {
		t.Errorf("Usage[1] = %+v, want queries=90, grabs=5", u1)
	}
	if u2 := usage[2]; u2.Queries != 15 || u2.Grabs != 2 {
		t.Errorf("Usage[2] = %+v, want queries=15, grabs=2", u2)
	}
}

// TestHTTPClient_Usage_Table verifies that Usage rejects payloads missing a
// non-null indexers array with domain.ErrInvalid and accepts an empty slice.
func TestHTTPClient_Usage_Table(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		payload   any
		wantErr   bool
		wantCount int
	}{
		{
			name:    "negative empty object missing indexers",
			payload: map[string]any{},
			wantErr: true,
		},
		{
			name:    "negative null indexers field",
			payload: map[string]any{"indexers": nil},
			wantErr: true,
		},
		{
			name:      "boundary empty indexers slice returns empty map",
			payload:   map[string]any{"indexers": []any{}},
			wantErr:   false,
			wantCount: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(tc.payload)
			}))
			t.Cleanup(srv.Close)

			c := prowlarrclient.New(5*time.Second, "SnatchArr-test")
			got, err := c.Usage(ctx, srv.URL, "any", now.Add(-time.Hour), now)
			if tc.wantErr {
				if err == nil || !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("Usage() err = %v, want wrapping domain.ErrInvalid", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Usage() unexpected error: %v", err)
			}
			if len(got) != tc.wantCount {
				t.Errorf("Usage() count = %d, want %d", len(got), tc.wantCount)
			}
		})
	}
}

func TestHTTPClient_Statuses(t *testing.T) {
	t.Parallel()
	srv := fakeProwlarr(t)
	c := prowlarrclient.New(5*time.Second, "SnatchArr-test")
	ctx := context.Background()

	statuses, err := c.Statuses(ctx, srv.URL, "prowl-key")
	if err != nil {
		t.Fatalf("Statuses() err = %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("Statuses() len = %d, want 1", len(statuses))
	}
	// Status row had id=99, indexerId=2: prove indexerId is used, not status row id
	if _, hasRowID := statuses[99]; hasRowID {
		t.Error("Statuses() contained status row ID 99, expected indexerId 2")
	}
	dt2, ok := statuses[2]
	if !ok {
		t.Fatal("expected status for indexer 2")
	}
	expected := time.Date(2026, 10, 16, 22, 0, 0, 0, time.UTC)
	if !dt2.Equal(expected) {
		t.Errorf("disabledTill = %v, want %v", dt2, expected)
	}
}

// TestHTTPClient_Statuses_Table verifies Statuses handling of null and empty
// responses, malformed disabledTill timestamps, and non-positive indexer IDs.
func TestHTTPClient_Statuses_Table(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name      string
		payload   any
		wantErr   bool
		wantCount int
		wantTimes map[int]time.Time
	}{
		{
			name:    "negative null response returns error",
			payload: nil,
			wantErr: true,
		},
		{
			name:      "boundary empty slice returns empty map",
			payload:   []map[string]any{},
			wantCount: 0,
		},
		{
			name: "negative bad disabledTill string returns error",
			payload: []map[string]any{
				{"id": 1, "indexerId": 10, "disabledTill": "invalid-timestamp"},
			},
			wantErr: true,
		},
		{
			name: "negative indexerId 0 with garbage disabledTill returns error",
			payload: []map[string]any{
				{"id": 1, "indexerId": 0, "disabledTill": "garbage"},
			},
			wantErr: true,
		},
		{
			name: "boundary indexerId 0 with valid disabledTill skipped",
			payload: []map[string]any{
				{"id": 1, "indexerId": 0, "disabledTill": "2026-10-16T22:00:00Z"},
			},
			wantCount: 0,
		},
		{
			name: "boundary empty disabledTill produces no entry",
			payload: []map[string]any{
				{"id": 1, "indexerId": 11, "disabledTill": ""},
				{"id": 2, "indexerId": 0, "disabledTill": "2026-10-16T22:00:00Z"},
			},
			wantCount: 0,
		},
		{
			name: "positive RFC3339 with fraction parses",
			payload: []map[string]any{
				{"id": 1, "indexerId": 12, "disabledTill": "2026-10-16T22:00:00.123456Z"},
			},
			wantCount: 1,
			wantTimes: map[int]time.Time{
				12: time.Date(2026, 10, 16, 22, 0, 0, 123456000, time.UTC),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(tc.payload)
			}))
			t.Cleanup(srv.Close)

			c := prowlarrclient.New(5*time.Second, "SnatchArr-test")
			got, err := c.Statuses(ctx, srv.URL, "any")
			if tc.wantErr {
				if err == nil || !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("Statuses() err = %v, want wrapping domain.ErrInvalid", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Statuses() unexpected error: %v", err)
			}
			if len(got) != tc.wantCount {
				t.Errorf("Statuses() count = %d, want %d", len(got), tc.wantCount)
			}
			for id, wantTime := range tc.wantTimes {
				if gotTime, ok := got[id]; !ok || !gotTime.Equal(wantTime) {
					t.Errorf("Statuses()[%d] = %v, want %v", id, gotTime, wantTime)
				}
			}
		})
	}
}

func TestHTTPClient_ProwlarrURLs(t *testing.T) {
	t.Parallel()
	srv := fakeProwlarr(t)
	c := prowlarrclient.New(5*time.Second, "SnatchArr-test")
	ctx := context.Background()

	urls, err := c.ProwlarrURLs(ctx, srv.URL, "prowl-key")
	if err != nil {
		t.Fatalf("ProwlarrURLs() err = %v", err)
	}
	if len(urls) != 1 || urls[0] != "http://prowlarr:9696" {
		t.Errorf("ProwlarrURLs() = %v, want [http://prowlarr:9696]", urls)
	}
}

func TestHTTPClient_InvalidShapes(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/indexerstats", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]any{})
	})
	mux.HandleFunc("GET /api/v1/indexerstatus", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"statuses": []any{}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := prowlarrclient.New(5*time.Second, "SnatchArr-test")
	ctx := context.Background()
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

	if _, err := c.Usage(ctx, srv.URL, "any", now.Add(-time.Hour), now); err == nil {
		t.Error("Usage() expected error on bare array shape, got nil")
	}
	if _, err := c.Statuses(ctx, srv.URL, "any"); err == nil {
		t.Error("Statuses() expected error on object shape, got nil")
	}
}

func TestHTTPClient_Grab(t *testing.T) {
	t.Parallel()

	type recordedReq struct {
		apiKey string
		body   map[string]any
	}

	tests := []struct {
		name       string
		release    prowlarrclient.Release
		key        string
		serverCode int
		wantErr    bool
		wantReq    bool
	}{
		{
			name: "positive grab sends POST /api/v1/search with guid and indexerId",
			release: prowlarrclient.Release{
				GUID:      "release-guid-123",
				IndexerID: 2,
				Title:     "Game Release",
			},
			key:        "prowl-key",
			serverCode: http.StatusOK,
			wantErr:    false,
			wantReq:    true,
		},
		{
			name: "negative empty guid refused before HTTP call",
			release: prowlarrclient.Release{
				GUID:      "",
				IndexerID: 2,
			},
			key:     "prowl-key",
			wantErr: true,
			wantReq: false,
		},
		{
			name: "negative whitespace guid refused before HTTP call",
			release: prowlarrclient.Release{
				GUID:      "   ",
				IndexerID: 2,
			},
			key:     "prowl-key",
			wantErr: true,
			wantReq: false,
		},
		{
			name: "negative indexerId 0 refused before HTTP call",
			release: prowlarrclient.Release{
				GUID:      "release-guid-123",
				IndexerID: 0,
			},
			key:     "prowl-key",
			wantErr: true,
			wantReq: false,
		},
		{
			name: "negative indexerId negative refused before HTTP call",
			release: prowlarrclient.Release{
				GUID:      "release-guid-123",
				IndexerID: -1,
			},
			key:     "prowl-key",
			wantErr: true,
			wantReq: false,
		},
		{
			name: "negative stub 400 returns wrapped error",
			release: prowlarrclient.Release{
				GUID:      "release-guid-123",
				IndexerID: 2,
			},
			key:        "prowl-key",
			serverCode: http.StatusBadRequest,
			wantErr:    true,
			wantReq:    true,
		},
		{
			name: "negative stub 500 returns wrapped error",
			release: prowlarrclient.Release{
				GUID:      "release-guid-123",
				IndexerID: 2,
			},
			key:        "prowl-key",
			serverCode: http.StatusInternalServerError,
			wantErr:    true,
			wantReq:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var recorded []recordedReq
			var mu sync.Mutex
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/search" || r.Method != http.MethodPost {
					http.NotFound(w, r)
					return
				}
				var b map[string]any
				_ = json.NewDecoder(r.Body).Decode(&b)
				mu.Lock()
				recorded = append(recorded, recordedReq{
					apiKey: r.Header.Get("X-Api-Key"),
					body:   b,
				})
				mu.Unlock()
				if tc.serverCode != 0 && tc.serverCode != http.StatusOK {
					w.WriteHeader(tc.serverCode)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"id": 1})
			}))
			defer srv.Close()

			c := prowlarrclient.New(5*time.Second, "SnatchArr-test")
			err := c.Grab(t.Context(), srv.URL, tc.key, tc.release)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Grab() err = %v, wantErr = %v", err, tc.wantErr)
			}
			mu.Lock()
			reqCount := len(recorded)
			mu.Unlock()
			if tc.wantReq && reqCount != 1 {
				t.Fatalf("Grab() recorded %d requests, want 1", reqCount)
			}
			if !tc.wantReq && reqCount != 0 {
				t.Fatalf("Grab() recorded %d requests, want 0", reqCount)
			}
			if tc.wantReq && !tc.wantErr {
				mu.Lock()
				req := recorded[0]
				mu.Unlock()
				if req.apiKey != tc.key {
					t.Errorf("X-Api-Key = %q, want %q", req.apiKey, tc.key)
				}
				guid, _ := req.body["guid"].(string)
				if guid != tc.release.GUID {
					t.Errorf("body guid = %v, want %q", req.body["guid"], tc.release.GUID)
				}
				idxID, _ := req.body["indexerId"].(float64)
				if int(idxID) != tc.release.IndexerID {
					t.Errorf("body indexerId = %v, want %d", req.body["indexerId"], tc.release.IndexerID)
				}
			}
		})
	}
}

func TestHTTPClient_Search(t *testing.T) {
	t.Parallel()

	type searchReq struct {
		query  string
		apiKey string
	}

	tests := []struct {
		name       string
		q          prowlarrclient.SearchQuery
		key        string
		serverCode int
		releases   []map[string]any
		wantErr    bool
		wantCount  int
	}{
		{
			name: "positive search returns mapped releases",
			q: prowlarrclient.SearchQuery{
				Query:      "Chrono Trigger",
				Type:       "search",
				IndexerIDs: []int{1, 2},
				Categories: []int{1000},
				Limit:      10,
				Offset:     0,
			},
			key:        "prowl-key",
			serverCode: http.StatusOK,
			releases: []map[string]any{
				{
					"guid":        "guid-1",
					"indexerId":   1,
					"title":       "Chrono Trigger SNES",
					"size":        4194304,
					"indexer":     "RetroGamer",
					"downloadUrl": "http://prowlarr/dl/1",
					"infoHash":    "abc1",
					"categories":  []map[string]any{{"id": 1000, "name": "Console"}},
				},
				{
					"guid":        "guid-2",
					"indexerId":   2,
					"title":       "Chrono Trigger DS",
					"size":        67108864,
					"indexer":     "RomVault",
					"downloadUrl": "http://prowlarr/dl/2",
					"infoHash":    "abc2",
					"categories":  []map[string]any{{"id": 1000, "name": "Console"}},
				},
			},
			wantErr:   false,
			wantCount: 2,
		},
		{
			name: "boundary empty search returns zero releases",
			q: prowlarrclient.SearchQuery{
				Query: "nonexistent",
			},
			key:        "prowl-key",
			serverCode: http.StatusOK,
			releases:   []map[string]any{},
			wantErr:    false,
			wantCount:  0,
		},
		{
			name: "negative search 500 error",
			q: prowlarrclient.SearchQuery{
				Query: "error-query",
			},
			key:        "prowl-key",
			serverCode: http.StatusInternalServerError,
			wantErr:    true,
			wantCount:  0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var recorded searchReq
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/search" || r.Method != http.MethodGet {
					http.NotFound(w, r)
					return
				}
				recorded = searchReq{
					query:  r.URL.Query().Get("query"),
					apiKey: r.Header.Get("X-Api-Key"),
				}
				if tc.serverCode != 0 && tc.serverCode != http.StatusOK {
					w.WriteHeader(tc.serverCode)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(tc.releases)
			}))
			defer srv.Close()

			c := prowlarrclient.New(5*time.Second, "SnatchArr-test")
			rels, err := c.Search(t.Context(), srv.URL, tc.key, tc.q)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Search() err = %v, wantErr = %v", err, tc.wantErr)
			}
			if !tc.wantErr {
				if len(rels) != tc.wantCount {
					t.Fatalf("Search() len = %d, want %d", len(rels), tc.wantCount)
				}
				if recorded.apiKey != tc.key {
					t.Errorf("apiKey = %q, want %q", recorded.apiKey, tc.key)
				}
				if tc.q.Query != "" && recorded.query != tc.q.Query {
					t.Errorf("query = %q, want %q", recorded.query, tc.q.Query)
				}
				if len(rels) > 0 {
					if rels[0].GUID != "guid-1" || rels[0].IndexerID != 1 || rels[0].Title != "Chrono Trigger SNES" {
						t.Errorf("rels[0] mismatch: %+v", rels[0])
					}
					if len(rels[0].Categories) != 1 || rels[0].Categories[0] != 1000 {
						t.Errorf("rels[0].Categories mismatch: %+v", rels[0].Categories)
					}
				}
			}
		})
	}
}
