// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package arrclient_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/queue"
)

func TestQueuePagingAndMapping(t *testing.T) {
	t.Parallel()

	kinds := []struct {
		kind domain.AppKind
		path string
	}{
		{domain.KindSonarr, "/api/v3/queue"},
		{domain.KindRadarr, "/api/v3/queue"},
		{domain.KindLidarr, "/api/v1/queue"},
		{domain.KindReadarr, "/api/v1/queue"},
	}

	for _, tc := range kinds {
		t.Run(string(tc.kind), func(t *testing.T) {
			t.Parallel()
			pagesRequested := make([]int, 0)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tc.path {
					http.NotFound(w, r)
					return
				}
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				pagesRequested = append(pagesRequested, page)
				w.Header().Set("Content-Type", "application/json")

				switch page {
				case 1:
					_, _ = w.Write([]byte(`{
						"page": 1,
						"pageSize": 2,
						"totalRecords": 3,
						"records": [
							{
								"id": 10,
								"title": "Release.10",
								"statusMessages": [
									{"title": "Release.10", "messages": ["Automatic import is not possible.", ""]},
									{"title": "", "messages": ["second"]}
								],
								"downloadId": "dl-10",
								"added": "2026-10-17T20:00:00Z",
								"status": "warning",
								"trackedDownloadStatus": "warning",
								"trackedDownloadState": "downloading",
								"sizeleft": 500.5,
								"customFormatScore": 12,
								"episode": {"monitored": true},
								"movie": {"monitored": true},
								"album": {"monitored": true},
								"book": {"monitored": true}
							},
							{
								"id": 11,
								"downloadId": "dl-11",
								"added": "2026-10-17T20:05:00Z",
								"status": "downloading",
								"trackedDownloadStatus": "ok",
								"trackedDownloadState": "downloading",
								"sizeleft": -5.0,
								"customFormatScore": 0,
								"episode": null,
								"movie": null,
								"album": null,
								"book": null
							}
						]
					}`))
				case 2:
					_, _ = w.Write([]byte(`{
						"page": 2,
						"pageSize": 2,
						"totalRecords": 3,
						"records": [
							{
								"id": 12,
								"downloadId": "dl-12",
								"added": "2026-10-17T20:10:00Z",
								"status": "queued",
								"trackedDownloadStatus": "ok",
								"trackedDownloadState": "downloading",
								"sizeleft": 0.0,
								"customFormatScore": 5,
								"episode": {"monitored": false},
								"movie": {"monitored": false},
								"album": {"monitored": false},
								"book": {"monitored": false}
							}
						]
					}`))
				default:
					_, _ = w.Write([]byte(`{"page": 3, "pageSize": 2, "totalRecords": 3, "records": []}`))
				}
			}))
			defer server.Close()

			prober := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: 2 * time.Second})
			items, err := prober.Queue(t.Context(), tc.kind, server.URL, "key", 10, 2)
			if err != nil {
				t.Fatalf("Queue() error: %v", err)
			}

			if len(items) != 3 {
				t.Fatalf("got %d items, want 3", len(items))
			}
			if len(pagesRequested) != 2 {
				t.Fatalf("got %d page requests, want 2 (stops at totalRecords)", len(pagesRequested))
			}

			// Item 1: monitored=true, sizeLeft=500
			if items[0].ID != 10 || !items[0].Monitored || items[0].SizeLeft != 500 || items[0].CustomFormatScore != 12 {
				t.Errorf("item 0 unexpected: %+v", items[0])
			}
			// Status messages flatten to title then messages, empty strings dropped.
			wantLines := []string{"Release.10", "Automatic import is not possible.", "second"}
			if items[0].Title != "Release.10" || !slices.Equal(items[0].StatusLines, wantLines) {
				t.Errorf("item 0 title/lines = %q %q, want Release.10 %q", items[0].Title, items[0].StatusLines, wantLines)
			}
			if len(items[1].StatusLines) != 0 {
				t.Errorf("item 1 lines = %q, want none", items[1].StatusLines)
			}
			// Item 2: nil entity -> monitored=true, negative size clamped to 0
			if items[1].ID != 11 || !items[1].Monitored || items[1].SizeLeft != 0 {
				t.Errorf("item 1 unexpected: %+v", items[1])
			}
			// Item 3: monitored=false
			if items[2].ID != 12 || items[2].Monitored {
				t.Errorf("item 2 unexpected: %+v", items[2])
			}
		})
	}
}

func TestQueueMaxPagesBoundHonoured(t *testing.T) {
	t.Parallel()

	pagesRequested := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pagesRequested++
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"page":         pagesRequested,
			"pageSize":     2,
			"totalRecords": 100,
			"records": []map[string]any{
				{
					"id":         pagesRequested,
					"downloadId": "dl",
					"added":      "2026-10-17T20:00:00Z",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	prober := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: 2 * time.Second})
	items, err := prober.Queue(t.Context(), domain.KindSonarr, server.URL, "key", 2, 2)
	if err != nil {
		t.Fatalf("Queue() error: %v", err)
	}

	if pagesRequested != 2 {
		t.Fatalf("got %d pages requested, want 2", pagesRequested)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
}

func TestQueueDropsUnparsableAdded(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"page": 1,
			"pageSize": 2,
			"totalRecords": 2,
			"records": [
				{
					"id": 1,
					"downloadId": "dl-valid",
					"added": "2026-10-17T20:00:00Z"
				},
				{
					"id": 2,
					"downloadId": "dl-invalid",
					"added": "not-a-valid-date"
				}
			]
		}`))
	}))
	defer server.Close()

	prober := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: 2 * time.Second})
	items, err := prober.Queue(t.Context(), domain.KindSonarr, server.URL, "key", 5, 10)
	if err != nil {
		t.Fatalf("Queue() error: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("got %d items, want 1 (dropped unparsable date)", len(items))
	}
	if items[0].ID != 1 {
		t.Errorf("got item ID %d, want 1", items[0].ID)
	}
}

func TestQueueWhisparrUnsupported(t *testing.T) {
	t.Parallel()

	prober := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: 2 * time.Second})
	for _, kind := range []domain.AppKind{domain.KindWhisparrV2, domain.KindWhisparrV3} {
		_, err := prober.Queue(t.Context(), kind, "http://localhost", "key", 5, 10)
		if !errors.Is(err, queue.ErrQueueUnsupported) {
			t.Errorf("kind %v got err %v, want ErrQueueUnsupported", kind, err)
		}

		err = prober.RemoveQueueItem(t.Context(), kind, "http://localhost", "key", 1, true, false)
		if !errors.Is(err, queue.ErrQueueUnsupported) {
			t.Errorf("kind %v RemoveQueueItem got err %v, want ErrQueueUnsupported", kind, err)
		}
	}
}

func TestRemoveQueueItem(t *testing.T) {
	t.Parallel()

	kinds := []struct {
		kind domain.AppKind
		path string
	}{
		{domain.KindSonarr, "/api/v3/queue/42"},
		{domain.KindRadarr, "/api/v3/queue/42"},
		{domain.KindLidarr, "/api/v1/queue/42"},
		{domain.KindReadarr, "/api/v1/queue/42"},
	}

	for _, tc := range kinds {
		for _, blocklist := range []bool{true, false} {
			for _, research := range []bool{true, false} {
				t.Run(string(tc.kind)+"_blocklist_"+strconv.FormatBool(blocklist)+"_research_"+strconv.FormatBool(research), func(t *testing.T) {
					t.Parallel()
					deleted := false
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.Method != http.MethodDelete {
							t.Errorf("got method %q, want DELETE", r.Method)
						}
						if r.URL.Path != tc.path {
							t.Errorf("got path %q, want %q", r.URL.Path, tc.path)
						}
						q := r.URL.Query()
						if q.Get("removeFromClient") != "true" {
							t.Errorf("removeFromClient query param = %q, want true", q.Get("removeFromClient"))
						}
						wantSkipRedownload := strconv.FormatBool(!research)
						if q.Get("skipRedownload") != wantSkipRedownload {
							t.Errorf("skipRedownload query param = %q, want %q", q.Get("skipRedownload"), wantSkipRedownload)
						}
						wantBlocklist := strconv.FormatBool(blocklist)
						if q.Get("blocklist") != wantBlocklist {
							t.Errorf("blocklist query param = %q, want %q", q.Get("blocklist"), wantBlocklist)
						}
						deleted = true
						w.WriteHeader(http.StatusOK)
					}))
					defer server.Close()

					prober := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: 2 * time.Second})
					err := prober.RemoveQueueItem(t.Context(), tc.kind, server.URL, "key", 42, blocklist, research)
					if err != nil {
						t.Fatalf("RemoveQueueItem error: %v", err)
					}
					if !deleted {
						t.Errorf("DELETE request was not received")
					}
				})
			}
		}
	}
}
