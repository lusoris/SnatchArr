// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package cleanuparrclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/cleanuparrclient"
)

func fakeCleanuparr(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Api-Key") != "clean-key" {
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
	mux.HandleFunc("GET /api/status", auth(func(w http.ResponseWriter, _ *http.Request) {
		write(w, map[string]any{
			"application":   map[string]any{"version": "2.4.1.0", "startTime": "2026-09-20T08:00:00.1234567+00:00", "upTime": "1.02:03:04"},
			"mediaManagers": map[string]any{"Sonarr": map[string]any{"instanceCount": 2}, "Radarr": map[string]any{"instanceCount": 1}},
		})
	}))
	mux.HandleFunc("GET /api/strikes/recent", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("count") != "5" {
			t.Errorf("count=%q", r.URL.Query().Get("count"))
		}
		write(w, []map[string]any{
			{"id": "a", "type": "Stalled", "createdAt": "2026-09-21T10:00:00Z", "downloadId": "ABC", "title": "Some.Release", "isDryRun": false},
			{"id": "b", "type": "SlowSpeed", "createdAt": "2026-09-21T09:00:00Z", "downloadId": "DEF", "title": "Other", "isDryRun": true},
		})
	}))
	mux.HandleFunc("GET /api/strikes", auth(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		items := []map[string]any{{"isMarkedForRemoval": true, "isRemoved": false, "isReturning": false}}
		if page == "2" {
			items = []map[string]any{{"isMarkedForRemoval": false, "isRemoved": true, "isReturning": true}}
		}
		write(w, map[string]any{"items": items, "page": page, "pageSize": 500, "totalCount": 2, "totalPages": 2})
	}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestHTTPClient(t *testing.T) {
	t.Parallel()
	srv := fakeCleanuparr(t)
	c := cleanuparrclient.New(5*time.Second, "SnatchArr-test")
	ctx := context.Background()

	st, err := c.Status(ctx, srv.URL, "clean-key")
	if err != nil || st.Version != "2.4.1.0" || st.StartedAt == nil || st.MediaManagers["Sonarr"] != 2 {
		t.Fatalf("status: %+v %v", st, err)
	}
	if _, badErr := c.Status(ctx, srv.URL, "nope"); badErr == nil {
		t.Fatal("bad key must fail")
	}
	recent, err := c.RecentStrikes(ctx, srv.URL, "clean-key", 5)
	if err != nil || len(recent) != 2 || recent[0].Type != "Stalled" || recent[1].DryRun != true || recent[0].CreatedAt.IsZero() {
		t.Fatalf("recent: %+v %v", recent, err)
	}
	sum, err := c.Summary(ctx, srv.URL, "clean-key")
	if err != nil || sum.StruckDownloads != 2 || sum.MarkedForRemoval != 1 || sum.RemovedDownloads != 1 || sum.ReturningDownload != 1 {
		t.Fatalf("summary: %+v %v", sum, err)
	}
}
