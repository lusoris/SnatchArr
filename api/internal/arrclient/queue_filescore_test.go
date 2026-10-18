// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package arrclient_test

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// fileScoreServer serves one queue page of records and answers file reads
// under filePath with the score from scores; a file id missing from scores
// answers 500, a nil score answers a file without customFormatScore.
func fileScoreServer(t *testing.T, records []map[string]any, filePath string, scores map[int]*int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if id, ok := strings.CutPrefix(r.URL.Path, filePath); ok {
			reads.Add(1)
			n, _ := strconv.Atoi(id)
			score, known := scores[n]
			if !known {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			body := map[string]any{"id": n}
			if score != nil {
				body["customFormatScore"] = *score
			}
			_ = json.NewEncoder(w).Encode(body)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"page": 1, "pageSize": len(records), "totalRecords": len(records), "records": records})
	}))
	t.Cleanup(server.Close)
	return server, &reads
}

func queueRecord(id int, nested string, file map[string]any) map[string]any {
	return map[string]any{"id": id, "downloadId": "dl-" + strconv.Itoa(id), "added": "2026-10-17T20:00:00Z", "customFormatScore": 100, nested: file}
}

func TestQueueFileScores(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		kind      domain.AppKind
		filePath  string
		records   []map[string]any
		scores    map[int]*int
		want      []*int32
		wantReads int32
	}{
		{
			name:     "positive sonarr episode file score",
			kind:     domain.KindSonarr,
			filePath: "/api/v3/episodefile/",
			records:  []map[string]any{queueRecord(1, "episode", map[string]any{"monitored": true, "episodeFileId": 7})},
			scores:   map[int]*int{7: new(150)},
			want:     []*int32{new(int32(150))}, wantReads: 1,
		},
		{
			name:     "positive radarr movie file score",
			kind:     domain.KindRadarr,
			filePath: "/api/v3/moviefile/",
			records:  []map[string]any{queueRecord(1, "movie", map[string]any{"monitored": true, "movieFileId": 9})},
			scores:   map[int]*int{9: new(80)},
			want:     []*int32{new(int32(80))}, wantReads: 1,
		},
		{
			name:     "negative no file on disk reads nothing",
			kind:     domain.KindSonarr,
			filePath: "/api/v3/episodefile/",
			records:  []map[string]any{queueRecord(1, "episode", map[string]any{"monitored": true})},
			want:     []*int32{nil}, wantReads: 0,
		},
		{
			name:     "negative failed read leaves the score unknown",
			kind:     domain.KindSonarr,
			filePath: "/api/v3/episodefile/",
			records:  []map[string]any{queueRecord(1, "episode", map[string]any{"monitored": true, "episodeFileId": 7})},
			want:     []*int32{nil}, wantReads: 1,
		},
		{
			name:     "negative radarr file without score stays unknown",
			kind:     domain.KindRadarr,
			filePath: "/api/v3/moviefile/",
			records:  []map[string]any{queueRecord(1, "movie", map[string]any{"monitored": true, "movieFileId": 9})},
			scores:   map[int]*int{9: nil},
			want:     []*int32{nil}, wantReads: 1,
		},
		{
			name:     "negative lidarr has no file lookup",
			kind:     domain.KindLidarr,
			filePath: "/api/v1/trackfile/",
			records:  []map[string]any{queueRecord(1, "album", map[string]any{"monitored": true})},
			want:     []*int32{nil}, wantReads: 0,
		},
		{
			name:     "boundary two items on one file read it once",
			kind:     domain.KindSonarr,
			filePath: "/api/v3/episodefile/",
			records: []map[string]any{
				queueRecord(1, "episode", map[string]any{"monitored": true, "episodeFileId": 7}),
				queueRecord(2, "episode", map[string]any{"monitored": true, "episodeFileId": 7}),
			},
			scores: map[int]*int{7: new(0)},
			want:   []*int32{new(int32(0)), new(int32(0))}, wantReads: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server, reads := fileScoreServer(t, tt.records, tt.filePath, tt.scores)
			prober := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: 2 * time.Second})
			items, err := prober.Queue(t.Context(), tt.kind, server.URL, "key", 1, len(tt.records))
			if err != nil {
				t.Fatalf("Queue() error: %v", err)
			}
			if len(items) != len(tt.want) {
				t.Fatalf("got %d items, want %d", len(items), len(tt.want))
			}
			for i, want := range tt.want {
				got := items[i].ExistingScore
				if (got == nil) != (want == nil) || (got != nil && *got != *want) {
					t.Errorf("item %d ExistingScore = %v, want %v", i, deref(got), deref(want))
				}
			}
			if n := reads.Load(); n != tt.wantReads {
				t.Errorf("file reads = %d, want %d", n, tt.wantReads)
			}
		})
	}
}

// TestQueueFileScoreLookupBound pins the per-read bound: 51 distinct files
// give 50 reads and leave the last item's score unknown.
func TestQueueFileScoreLookupBound(t *testing.T) {
	t.Parallel()
	const files = 51
	records := make([]map[string]any, 0, files)
	scores := make(map[int]*int, files)
	for i := 1; i <= files; i++ {
		records = append(records, queueRecord(i, "episode", map[string]any{"monitored": true, "episodeFileId": i}))
		scores[i] = new(i)
	}
	server, reads := fileScoreServer(t, records, "/api/v3/episodefile/", scores)
	prober := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: 2 * time.Second})
	items, err := prober.Queue(t.Context(), domain.KindSonarr, server.URL, "key", 1, files)
	if err != nil {
		t.Fatalf("Queue() error: %v", err)
	}
	if n := reads.Load(); n != files-1 {
		t.Fatalf("file reads = %d, want %d", n, files-1)
	}
	if got := items[files-2].ExistingScore; got == nil || *got != files-1 {
		t.Errorf("item %d ExistingScore = %v, want %d", files-2, deref(got), files-1)
	}
	if got := items[files-1].ExistingScore; got != nil {
		t.Errorf("item %d ExistingScore = %d, want unknown past the bound", files-1, *got)
	}
}

func deref(p *int32) any {
	if p == nil {
		return nil
	}
	return *p
}

// TestQueueMediaID pins the media item each kind groups duplicates by.
func TestQueueMediaID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		kind   domain.AppKind
		record map[string]any
		want   int32
	}{
		{"positive sonarr episode", domain.KindSonarr, map[string]any{"episodeId": 11}, 11},
		{"positive radarr movie", domain.KindRadarr, map[string]any{"movieId": 12}, 12},
		{"positive lidarr album", domain.KindLidarr, map[string]any{"albumId": 13}, 13},
		{"positive readarr book", domain.KindReadarr, map[string]any{"bookId": 14}, 14},
		{"negative sonarr without episode id", domain.KindSonarr, map[string]any{"seriesId": 3}, 0},
		{"boundary radarr ignores a foreign field", domain.KindRadarr, map[string]any{"episodeId": 11}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			record := queueRecord(1, "unused", map[string]any{})
			maps.Copy(record, tt.record)
			server, _ := fileScoreServer(t, []map[string]any{record}, "/unused/", nil)
			prober := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: 2 * time.Second})
			items, err := prober.Queue(t.Context(), tt.kind, server.URL, "key", 1, 1)
			if err != nil {
				t.Fatalf("Queue() error: %v", err)
			}
			if len(items) != 1 || items[0].MediaID != tt.want {
				t.Fatalf("items = %+v, want one item with MediaID %d", items, tt.want)
			}
		})
	}
}
