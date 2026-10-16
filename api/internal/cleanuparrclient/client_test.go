// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package cleanuparrclient_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestSeekerConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantSearch bool
		wantProact bool
		wantInsts  int
		wantErr    bool
	}{
		{
			name: "positive active",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"searchEnabled":          true,
					"searchInterval":         15,
					"proactiveSearchEnabled": true,
					"instances": []map[string]any{
						{
							"arrInstanceId":      "b828cb0f-f1ee-48c2-be16-8be52e7beea7",
							"instanceName":       "Radarr 4K",
							"instanceType":       "Radarr",
							"enabled":            true,
							"arrInstanceEnabled": true,
						},
					},
				})
			},
			wantSearch: true,
			wantProact: true,
			wantInsts:  1,
			wantErr:    false,
		},
		{
			name: "negative proactive off",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"searchEnabled":          true,
					"proactiveSearchEnabled": false,
					"instances":              []map[string]any{},
				})
			},
			wantSearch: true,
			wantProact: false,
			wantInsts:  0,
			wantErr:    false,
		},
		{
			name: "boundary 404 endpoint not found",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.NotFound(w, nil)
			},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/configuration/seeker" {
					tc.handler(w, r)
					return
				}
				http.NotFound(w, r)
			}))
			t.Cleanup(srv.Close)

			c := cleanuparrclient.New(5*time.Second, "SnatchArr-test")
			cfg, err := c.SeekerConfig(context.Background(), srv.URL, "key")
			if (err != nil) != tc.wantErr {
				t.Fatalf("SeekerConfig() err = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if cfg.SearchEnabled != tc.wantSearch || cfg.ProactiveSearchEnabled != tc.wantProact || len(cfg.Instances) != tc.wantInsts {
				t.Fatalf("SeekerConfig() = %+v, want search=%v proact=%v insts=%d", cfg, tc.wantSearch, tc.wantProact, tc.wantInsts)
			}
		})
	}
}

func TestGeneralDryRun(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		dryRun  bool
		status  int
		wantErr bool
	}{
		{"positive dry run on", true, http.StatusOK, false},
		{"negative dry run off", false, http.StatusOK, false},
		{"boundary 500 error", false, http.StatusInternalServerError, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/configuration/general" {
					http.NotFound(w, r)
					return
				}
				if tc.status != http.StatusOK {
					w.WriteHeader(tc.status)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"dryRun": tc.dryRun})
			}))
			t.Cleanup(srv.Close)

			c := cleanuparrclient.New(5*time.Second, "SnatchArr-test")
			got, err := c.GeneralDryRun(context.Background(), srv.URL, "key")
			if (err != nil) != tc.wantErr {
				t.Fatalf("GeneralDryRun() err = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.dryRun {
				t.Fatalf("GeneralDryRun() = %v, want %v", got, tc.dryRun)
			}
		})
	}
}

func TestArrInstancesAllowList(t *testing.T) {
	t.Parallel()
	const secretAPIKey = "secret-arr-api-key-do-not-leak"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Real upstream response shape from ArrConfigController.GetArrConfig
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":                     "00000000-0000-0000-0000-000000000001",
			"type":                   "Sonarr",
			"failedImportMaxStrikes": -1,
			"instances": []map[string]any{
				{
					"id":          "11111111-1111-1111-1111-111111111111",
					"enabled":     true,
					"version":     4.0,
					"name":        "Sonarr TV",
					"url":         "http://sonarr:8989",
					"apiKey":      secretAPIKey,
					"externalUrl": "https://sonarr.example.com",
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	c := cleanuparrclient.New(5*time.Second, "SnatchArr-test")
	refs, err := c.ArrInstances(context.Background(), srv.URL, "clean-key", "sonarr")
	if err != nil {
		t.Fatalf("ArrInstances() err = %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("ArrInstances() len = %d, want 1", len(refs))
	}
	ref := refs[0]
	if ref.ID != "11111111-1111-1111-1111-111111111111" || ref.Name != "Sonarr TV" || ref.URL != "http://sonarr:8989" || !ref.Enabled {
		t.Fatalf("ArrInstances() ref = %+v", ref)
	}

	// CRITICAL: Assert apiKey is absent from every SnatchArr struct, string, and serialized representation
	formatted := fmt.Sprintf("%+v", ref)
	if strings.Contains(formatted, secretAPIKey) {
		t.Fatalf("CRITICAL SECURITY VIOLATION: struct contains raw apiKey: %s", formatted)
	}
	marshaled, err := json.Marshal(ref)
	if err != nil {
		t.Fatalf("json.Marshal(ref) err = %v", err)
	}
	if strings.Contains(string(marshaled), secretAPIKey) {
		t.Fatalf("CRITICAL SECURITY VIOLATION: JSON output contains apiKey: %s", string(marshaled))
	}
}
