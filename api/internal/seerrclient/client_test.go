// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package seerrclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/seerrclient"
)

func fakeSeerr(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Api-Key") != "seerr-key" {
				w.WriteHeader(http.StatusForbidden)
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
	mux.HandleFunc("GET /api/v1/status", auth(func(w http.ResponseWriter, _ *http.Request) { write(w, map[string]any{"version": "2.5.1"}) }))
	mux.HandleFunc("GET /api/v1/request", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("filter") != "processing" {
			t.Errorf("unexpected filter %q", r.URL.Query().Get("filter"))
		}
		write(w, map[string]any{
			"pageInfo": map[string]any{"pages": 1, "results": 2},
			"results": []map[string]any{
				{
					"id": 11, "type": "movie", "status": 2, "is4k": false, "serverId": 0, "createdAt": "2026-09-20T10:00:00.000Z",
					"requestedBy": map[string]any{"displayName": "Alice"},
					"media":       map[string]any{"id": 5, "mediaType": "movie", "tmdbId": 603, "tvdbId": nil, "status": 3},
				},
				{
					"id": 12, "type": "tv", "status": 2, "is4k": true, "serverId": 1,
					"requestedBy": map[string]any{"username": "bob"},
					"seasons":     []map[string]any{{"seasonNumber": 1}, {"seasonNumber": 2}},
					"media":       map[string]any{"id": 6, "mediaType": "tv", "tmdbId": 1396, "tvdbId": 81189, "status": 4},
				},
			},
		})
	}))
	mux.HandleFunc("GET /api/v1/settings/sonarr", auth(func(w http.ResponseWriter, _ *http.Request) {
		write(w, []map[string]any{{"id": 1, "name": "Sonarr 4K", "hostname": "sonarr.lan", "port": 8989, "apiKey": "sk", "useSsl": false, "baseUrl": "/sonarr/", "is4k": true, "isDefault": true}})
	}))
	mux.HandleFunc("GET /api/v1/settings/radarr", auth(func(w http.ResponseWriter, _ *http.Request) {
		write(w, []map[string]any{{"id": 0, "name": "Radarr", "hostname": "radarr.lan", "port": 7878, "apiKey": "rk", "useSsl": true, "baseUrl": "", "is4k": false, "isDefault": true}})
	}))
	mux.HandleFunc("GET /api/v1/movie/603", auth(func(w http.ResponseWriter, _ *http.Request) { write(w, map[string]any{"title": "The Matrix"}) }))
	mux.HandleFunc("GET /api/v1/tv/1396", auth(func(w http.ResponseWriter, _ *http.Request) { write(w, map[string]any{"name": "Breaking Bad"}) }))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestHTTPClient(t *testing.T) {
	t.Parallel()
	srv := fakeSeerr(t)
	c := seerrclient.New(5*time.Second, "SnatchArr-test")
	ctx := context.Background()

	st, err := c.Status(ctx, srv.URL, "seerr-key")
	if err != nil || st.Version != "2.5.1" {
		t.Fatalf("status: %+v %v", st, err)
	}
	if _, badErr := c.Status(ctx, srv.URL, "wrong"); badErr == nil {
		t.Fatal("bad key must fail")
	}

	reqs, err := c.ProcessingRequests(ctx, srv.URL, "seerr-key")
	if err != nil || len(reqs) != 2 {
		t.Fatalf("requests: %d %v", len(reqs), err)
	}
	if reqs[0].Type != "movie" || reqs[0].Media.TmdbID != 603 || reqs[0].Requester() != "Alice" || reqs[0].ServerID == nil {
		t.Fatalf("movie request: %+v", reqs[0])
	}
	if reqs[1].Type != "tv" || reqs[1].Media.TvdbID == nil || *reqs[1].Media.TvdbID != 81189 || len(reqs[1].Seasons) != 2 || !reqs[1].Is4K || reqs[1].Requester() != "bob" {
		t.Fatalf("tv request: %+v", reqs[1])
	}

	servers, err := c.Servers(ctx, srv.URL, "seerr-key")
	if err != nil || len(servers) != 2 {
		t.Fatalf("servers: %+v %v", servers, err)
	}
	if servers[0].Kind != domain.KindSonarr || servers[0].BaseURL != "http://sonarr.lan:8989/sonarr" || servers[0].APIKey != "sk" || !servers[0].Is4K {
		t.Fatalf("sonarr server: %+v", servers[0])
	}
	if servers[1].Kind != domain.KindRadarr || servers[1].BaseURL != "https://radarr.lan:7878" || servers[1].ID != 0 {
		t.Fatalf("radarr server: %+v", servers[1])
	}

	if title, err := c.Title(ctx, srv.URL, "seerr-key", domain.SeerrMovie, 603); err != nil || title != "The Matrix" {
		t.Fatalf("movie title: %q %v", title, err)
	}
	if title, err := c.Title(ctx, srv.URL, "seerr-key", domain.SeerrTV, 1396); err != nil || title != "Breaking Bad" {
		t.Fatalf("tv title: %q %v", title, err)
	}
}

func TestServerURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		host string
		port int
		ssl  bool
		base string
		want string
	}{
		{"sonarr", 8989, false, "", "http://sonarr:8989"},
		{"sonarr", 8989, true, "/sonarr/", "https://sonarr:8989/sonarr"},
		{"10.0.0.5", 0, false, "arr", "http://10.0.0.5/arr"},
	}
	for _, tc := range cases {
		if got := seerrclient.ServerURL(tc.host, tc.port, tc.ssl, tc.base); got != tc.want {
			t.Errorf("ServerURL(%q,%d,%v,%q) = %q, want %q", tc.host, tc.port, tc.ssl, tc.base, got, tc.want)
		}
	}
}
