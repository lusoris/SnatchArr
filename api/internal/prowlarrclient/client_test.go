// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package prowlarrclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
