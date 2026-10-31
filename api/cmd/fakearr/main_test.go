// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/queue"
)

const testAPIKey = "fake-api-key-0123456789"

func newTestServer(t *testing.T, kind string, importBlocked bool) *httptest.Server {
	t.Helper()
	f := newFake(kind, testAPIKey, 10, 5, importBlocked, nil)
	mux := http.NewServeMux()
	f.routes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func doJSON(t *testing.T, srv *httptest.Server, method, path string, body any, dest any) int {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		bodyReader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, srv.URL+path, bodyReader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("X-Api-Key", testAPIKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	if dest != nil {
		if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
			t.Fatalf("decode JSON response: %v", err)
		}
	}
	return resp.StatusCode
}

func parseQueueRecords(t *testing.T, raw map[string]any) (int, []map[string]any) {
	t.Helper()
	total, ok := raw["totalRecords"].(float64)
	if !ok {
		t.Fatalf("totalRecords is not float64: %v", raw["totalRecords"])
	}
	recsRaw, ok := raw["records"].([]any)
	if !ok {
		t.Fatalf("records is not []any: %v", raw["records"])
	}
	recs := make([]map[string]any, 0, len(recsRaw))
	for _, r := range recsRaw {
		recMap, ok := r.(map[string]any)
		if !ok {
			t.Fatalf("record is not map[string]any: %v", r)
		}
		recs = append(recs, recMap)
	}
	return int(total), recs
}

func assertQueueRecord(t *testing.T, rec map[string]any, isMovie bool) {
	t.Helper()
	if rec["downloadId"] != "fake-dl-1" {
		t.Errorf("downloadId = %v, want fake-dl-1", rec["downloadId"])
	}
	if rec["trackedDownloadStatus"] != "warning" {
		t.Errorf("trackedDownloadStatus = %v, want warning", rec["trackedDownloadStatus"])
	}
	if rec["trackedDownloadState"] != "importBlocked" {
		t.Errorf("trackedDownloadState = %v, want importBlocked", rec["trackedDownloadState"])
	}
	if isMovie {
		if rec["movieId"] != float64(10) {
			t.Errorf("movieId = %v, want 10", rec["movieId"])
		}
	} else if rec["seriesId"] != float64(10) || rec["episodeId"] != float64(101) {
		t.Errorf("seriesId = %v, episodeId = %v; want 10, 101", rec["seriesId"], rec["episodeId"])
	}
}

func assertCandidate(t *testing.T, cand map[string]any, isMovie, hasIndexerFlags bool) {
	t.Helper()
	if cand["downloadId"] != "fake-dl-1" {
		t.Errorf("cand downloadId = %v, want fake-dl-1", cand["downloadId"])
	}
	rejs, ok := cand["rejections"].([]any)
	if !ok || len(rejs) != 0 {
		t.Errorf("rejections = %v, want empty []", cand["rejections"])
	}
	if isMovie {
		if cand["movie"] == nil {
			t.Error("cand movie mapping missing")
		}
		if hasIndexerFlags && cand["indexerFlags"] != float64(0) {
			t.Errorf("indexerFlags = %v, want 0", cand["indexerFlags"])
		}
	} else if cand["series"] == nil || cand["episodes"] == nil {
		t.Errorf("cand series/episodes mapping missing: series=%v, episodes=%v", cand["series"], cand["episodes"])
	}
}

func TestQueueAndCandidatePositive(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		kind            string
		isMovie         bool
		hasIndexerFlags bool
	}{
		{name: "sonarr", kind: "sonarr", isMovie: false, hasIndexerFlags: false},
		{name: "radarr", kind: "radarr", isMovie: true, hasIndexerFlags: false},
		{name: "whisparr-v2", kind: "whisparr-v2", isMovie: false, hasIndexerFlags: false},
		{name: "whisparr-v3", kind: "whisparr-v3", isMovie: true, hasIndexerFlags: true},
		{name: "whisparr_v2", kind: "whisparr_v2", isMovie: false, hasIndexerFlags: false},
		{name: "whisparr_v3", kind: "whisparr_v3", isMovie: true, hasIndexerFlags: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := newTestServer(t, tt.kind, true)

			var qResp map[string]any
			status := doJSON(t, srv, http.MethodGet, "/api/v3/queue", nil, &qResp)
			if status != http.StatusOK {
				t.Fatalf("GET /queue status = %d, want %d", status, http.StatusOK)
			}
			total, recs := parseQueueRecords(t, qResp)
			if total != 1 || len(recs) != 1 {
				t.Fatalf("queue totalRecords = %d, len(records) = %d; want 1 and 1", total, len(recs))
			}
			assertQueueRecord(t, recs[0], tt.isMovie)

			var cands []map[string]any
			status = doJSON(t, srv, http.MethodGet, "/api/v3/manualimport?downloadId=fake-dl-1", nil, &cands)
			if status != http.StatusOK {
				t.Fatalf("GET /manualimport status = %d, want %d", status, http.StatusOK)
			}
			if len(cands) != 1 {
				t.Fatalf("len(cands) = %d, want 1", len(cands))
			}
			assertCandidate(t, cands[0], tt.isMovie, tt.hasIndexerFlags)
		})
	}
}

func TestQueueAndCandidateNegative(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		kind          string
		importBlocked bool
		urlPath       string
		checkQueue    bool
		wantTotal     int
		wantRecords   int
		wantCands     int
	}{
		{
			name:          "flag_off_queue_empty",
			kind:          "sonarr",
			importBlocked: false,
			urlPath:       "/api/v3/queue",
			checkQueue:    true,
			wantTotal:     3,
			wantRecords:   0,
		},
		{
			name:          "flag_off_manualimport_empty",
			kind:          "sonarr",
			importBlocked: false,
			urlPath:       "/api/v3/manualimport?downloadId=fake-dl-1",
			checkQueue:    false,
			wantCands:     0,
		},
		{
			name:          "flag_on_unknown_download_id",
			kind:          "sonarr",
			importBlocked: true,
			urlPath:       "/api/v3/manualimport?downloadId=unknown-download-id",
			checkQueue:    false,
			wantCands:     0,
		},
		{
			name:          "flag_on_empty_download_id",
			kind:          "radarr",
			importBlocked: true,
			urlPath:       "/api/v3/manualimport",
			checkQueue:    false,
			wantCands:     0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := newTestServer(t, tt.kind, tt.importBlocked)

			if tt.checkQueue {
				var qResp map[string]any
				status := doJSON(t, srv, http.MethodGet, tt.urlPath, nil, &qResp)
				if status != http.StatusOK {
					t.Fatalf("GET /queue status = %d, want %d", status, http.StatusOK)
				}
				total, recs := parseQueueRecords(t, qResp)
				if total != tt.wantTotal || len(recs) != tt.wantRecords {
					t.Fatalf("queue totalRecords = %d, len = %d; want %d and %d", total, len(recs), tt.wantTotal, tt.wantRecords)
				}
				return
			}

			var cands []map[string]any
			status := doJSON(t, srv, http.MethodGet, tt.urlPath, nil, &cands)
			if status != http.StatusOK {
				t.Fatalf("GET /manualimport status = %d, want %d", status, http.StatusOK)
			}
			if len(cands) != tt.wantCands {
				t.Fatalf("len(cands) = %d, want %d", len(cands), tt.wantCands)
			}
		})
	}
}

func TestManualImportCommandBoundary(t *testing.T) {
	t.Parallel()
	kinds := []string{"sonarr", "radarr", "whisparr-v2", "whisparr-v3"}
	for _, k := range kinds {
		t.Run(k, func(t *testing.T) {
			t.Parallel()
			srv := newTestServer(t, k, true)

			var qResp map[string]any
			status := doJSON(t, srv, http.MethodGet, "/api/v3/queue", nil, &qResp)
			total, recs := parseQueueRecords(t, qResp)
			if status != http.StatusOK || len(recs) != 1 || total != 1 {
				t.Fatalf("before command: total=%d, len(records) = %d, want 1 and 1", total, len(recs))
			}

			var otherCmd map[string]any
			status = doJSON(t, srv, http.MethodPost, "/api/v3/command", map[string]any{"name": "RescanMovie"}, &otherCmd)
			if status != http.StatusCreated {
				t.Fatalf("POST /command (other) status = %d, want %d", status, http.StatusCreated)
			}
			status = doJSON(t, srv, http.MethodGet, "/api/v3/queue", nil, &qResp)
			total, recs = parseQueueRecords(t, qResp)
			if status != http.StatusOK || len(recs) != 1 || total != 1 {
				t.Fatalf("after other command: total=%d, len(records) = %d, want 1 and 1", total, len(recs))
			}

			var cmd map[string]any
			status = doJSON(t, srv, http.MethodPost, "/api/v3/command", map[string]any{"name": "ManualImport"}, &cmd)
			if status != http.StatusCreated {
				t.Fatalf("POST /command (ManualImport) status = %d, want %d", status, http.StatusCreated)
			}

			var recorded []map[string]any
			status = doJSON(t, srv, http.MethodGet, "/_fake/commands", nil, &recorded)
			if status != http.StatusOK || len(recorded) < 2 {
				t.Fatalf("GET /_fake/commands len = %d, want >= 2", len(recorded))
			}
			last := recorded[len(recorded)-1]
			if last["name"] != "ManualImport" {
				t.Errorf("last command name = %v, want ManualImport", last["name"])
			}

			status = doJSON(t, srv, http.MethodGet, "/api/v3/queue", nil, &qResp)
			if status != http.StatusOK {
				t.Fatalf("GET /queue status = %d, want %d", status, http.StatusOK)
			}
			total, recs = parseQueueRecords(t, qResp)
			if total != 0 || len(recs) != 0 {
				t.Fatalf("after ManualImport: totalRecords = %d, len(records) = %d; want 0 and 0", total, len(recs))
			}
		})
	}
}

func TestArrClientAndSweepIntegration(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name domain.AppKind
		kind string
	}{
		{name: domain.KindSonarr, kind: "sonarr"},
		{name: domain.KindRadarr, kind: "radarr"},
		{name: domain.KindWhisparrV2, kind: "whisparr-v2"},
		{name: domain.KindWhisparrV3, kind: "whisparr-v3"},
	}
	for _, tc := range cases {
		t.Run(string(tc.name), func(t *testing.T) {
			t.Parallel()
			srv := newTestServer(t, tc.kind, true)

			prober := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: 5 * time.Second})

			items, err := prober.Queue(t.Context(), tc.name, srv.URL, testAPIKey, 1, 10)
			if err != nil {
				t.Fatalf("prober.Queue: %v", err)
			}
			if len(items) != 1 {
				t.Fatalf("len(items) = %d, want 1", len(items))
			}
			item := items[0]
			if item.DownloadID != "fake-dl-1" {
				t.Errorf("item.DownloadID = %q, want fake-dl-1", item.DownloadID)
			}

			settings := queue.DefaultSettings()
			settings.Enabled = true
			settings.ForceImport = true
			fakeNow, err := time.Parse(time.RFC3339, "2026-10-27T12:00:00Z")
			if err != nil {
				t.Fatalf("parse fakeNow: %v", err)
			}
			decisions := queue.Sweep(settings, items, nil, fakeNow)
			if len(decisions) != 1 {
				t.Fatalf("len(decisions) = %d, want 1", len(decisions))
			}
			dec := decisions[0]
			if dec.Rule != queue.RuleImportFixable {
				t.Fatalf("dec.Rule = %q, want %q (status lines: %v)", dec.Rule, queue.RuleImportFixable, item.StatusLines)
			}

			n, err := prober.ForceImport(t.Context(), tc.name, srv.URL, testAPIKey, "fake-dl-1", queue.ImportModeFixable)
			if err != nil {
				t.Fatalf("prober.ForceImport: %v", err)
			}
			if n != 1 {
				t.Errorf("imported files = %d, want 1", n)
			}

			itemsAfter, err := prober.Queue(t.Context(), tc.name, srv.URL, testAPIKey, 1, 10)
			if err != nil {
				t.Fatalf("prober.Queue after import: %v", err)
			}
			if len(itemsAfter) != 0 {
				t.Errorf("items after import = %d, want 0", len(itemsAfter))
			}
		})
	}
}
