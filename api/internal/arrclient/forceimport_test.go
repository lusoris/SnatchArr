// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package arrclient_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/queue"
)

// Manual-import candidates in the shape each *arr returns them.
const (
	sonarrClean     = `{"path":"/dl/a.mkv","series":{"id":5},"episodes":[{"id":51},{"id":52}],"quality":{"quality":{"id":3}},"downloadId":"dl-1","releaseType":"multiEpisode","rejections":[]}`
	sonarrRejected  = `{"path":"/dl/b.mkv","series":{"id":5},"episodes":[{"id":53}],"rejections":[{"reason":"Not an upgrade","type":"permanent"}]}`
	sonarrUnmapped  = `{"path":"/dl/c.mkv","series":{"id":5},"episodes":[],"rejections":[]}`
	radarrClean     = `{"path":"/dl/m.mkv","movie":{"id":9},"quality":{"quality":{"id":7}},"downloadId":"dl-1","rejections":[]}`
	radarrRejected  = `{"path":"/dl/n.mkv","movie":{"id":9},"rejections":[{"reason":"Sample","type":"permanent"}]}`
	radarrUnmapped  = `{"path":"/dl/o.mkv","rejections":[]}`
	lidarrClean     = `{"path":"/dl/t1.flac","artist":{"id":3},"album":{"id":31},"tracks":[{"id":301}],"quality":{"quality":{"id":1}},"downloadId":"dl-1","rejections":[]}`
	lidarrRejected  = `{"path":"/dl/t2.flac","artist":{"id":3},"album":{"id":31},"tracks":[{"id":302}],"rejections":[{"reason":"Sample","type":"permanent"}]}`
	lidarrUnmapped  = `{"path":"/dl/t3.flac","artist":{"id":3},"album":{"id":31},"tracks":[],"rejections":[]}`
	readarrClean    = `{"path":"/dl/b.epub","author":{"id":4},"book":{"id":41},"quality":{"quality":{"id":2}},"downloadId":"dl-1","rejections":[]}`
	readarrRejected = `{"path":"/dl/b2.epub","author":{"id":4},"book":{"id":41},"rejections":[{"reason":"Sample","type":"permanent"}]}`
	readarrUnmapped = `{"path":"/dl/b3.epub","author":{"id":4},"rejections":[]}`
)

// fakeImportArr serves GET /api/v{1,3}/manualimport and POST /api/v{1,3}/command and
// records the command bodies.
type fakeImportArr struct {
	candidates string
	getStatus  int
	postStatus int

	mu       sync.Mutex
	query    string
	commands []map[string]any
}

func (f *fakeImportArr) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodGet && (r.URL.Path == "/api/v3/manualimport" || r.URL.Path == "/api/v1/manualimport"):
		f.mu.Lock()
		f.query = r.URL.RawQuery
		f.mu.Unlock()
		if f.getStatus != 0 {
			w.WriteHeader(f.getStatus)
			return
		}
		_, _ = io.WriteString(w, "["+f.candidates+"]")
	case r.Method == http.MethodPost && (r.URL.Path == "/api/v3/command" || r.URL.Path == "/api/v1/command"):
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.commands = append(f.commands, body)
		f.mu.Unlock()
		if f.postStatus != 0 {
			w.WriteHeader(f.postStatus)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":1,"name":"ManualImport","status":"queued"}`)
	default:
		http.NotFound(w, r)
	}
}

func TestForceImport(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		kind       domain.AppKind
		candidates string
		postStatus int
		getStatus  int
		wantN      int
		wantPaths  []string
		wantErr    error
		wantAnyErr bool
	}{
		{name: "positive/sonarr_clean", kind: domain.KindSonarr, candidates: sonarrClean, wantN: 1, wantPaths: []string{"/dl/a.mkv"}},
		{name: "positive/radarr_clean", kind: domain.KindRadarr, candidates: radarrClean, wantN: 1, wantPaths: []string{"/dl/m.mkv"}},
		{name: "positive/lidarr_clean", kind: domain.KindLidarr, candidates: lidarrClean, wantN: 1, wantPaths: []string{"/dl/t1.flac"}},
		{name: "positive/readarr_clean", kind: domain.KindReadarr, candidates: readarrClean, wantN: 1, wantPaths: []string{"/dl/b.epub"}},
		{name: "positive/sonarr_only_clean_of_mixed", kind: domain.KindSonarr, candidates: sonarrRejected + "," + sonarrClean + "," + sonarrUnmapped, wantN: 1, wantPaths: []string{"/dl/a.mkv"}},
		{name: "positive/radarr_only_clean_of_mixed", kind: domain.KindRadarr, candidates: radarrUnmapped + "," + radarrClean + "," + radarrRejected, wantN: 1, wantPaths: []string{"/dl/m.mkv"}},
		{name: "positive/lidarr_only_clean_of_mixed", kind: domain.KindLidarr, candidates: lidarrRejected + "," + lidarrClean + "," + lidarrUnmapped, wantN: 1, wantPaths: []string{"/dl/t1.flac"}},
		{name: "positive/readarr_only_clean_of_mixed", kind: domain.KindReadarr, candidates: readarrUnmapped + "," + readarrClean + "," + readarrRejected, wantN: 1, wantPaths: []string{"/dl/b.epub"}},
		{name: "negative/sonarr_rejected_refused", kind: domain.KindSonarr, candidates: sonarrRejected, wantErr: queue.ErrNoCleanCandidate},
		{name: "negative/sonarr_unmapped_refused", kind: domain.KindSonarr, candidates: sonarrUnmapped, wantErr: queue.ErrNoCleanCandidate},
		{name: "negative/radarr_rejected_refused", kind: domain.KindRadarr, candidates: radarrRejected, wantErr: queue.ErrNoCleanCandidate},
		{name: "negative/radarr_unmapped_refused", kind: domain.KindRadarr, candidates: radarrUnmapped, wantErr: queue.ErrNoCleanCandidate},
		{name: "negative/lidarr_rejected_refused", kind: domain.KindLidarr, candidates: lidarrRejected, wantErr: queue.ErrNoCleanCandidate},
		{name: "negative/lidarr_unmapped_refused", kind: domain.KindLidarr, candidates: lidarrUnmapped, wantErr: queue.ErrNoCleanCandidate},
		{name: "negative/readarr_rejected_refused", kind: domain.KindReadarr, candidates: readarrRejected, wantErr: queue.ErrNoCleanCandidate},
		{name: "negative/readarr_unmapped_refused", kind: domain.KindReadarr, candidates: readarrUnmapped, wantErr: queue.ErrNoCleanCandidate},
		{name: "negative/unknown_kind_invalid", kind: domain.AppKind("bogus"), candidates: sonarrClean, wantErr: domain.ErrInvalid},
		{name: "negative/candidates_error", kind: domain.KindSonarr, getStatus: http.StatusInternalServerError, wantAnyErr: true},
		{name: "negative/command_error", kind: domain.KindRadarr, candidates: radarrClean, postStatus: http.StatusInternalServerError, wantAnyErr: true, wantPaths: []string{"/dl/m.mkv"}},
		{name: "boundary/radarr_no_candidates_refused", kind: domain.KindRadarr, candidates: "", wantErr: queue.ErrNoCleanCandidate},
		{name: "boundary/lidarr_no_candidates_refused", kind: domain.KindLidarr, candidates: "", wantErr: queue.ErrNoCleanCandidate},
		{name: "boundary/readarr_no_candidates_refused", kind: domain.KindReadarr, candidates: "", wantErr: queue.ErrNoCleanCandidate},
		{name: "boundary/whisparr_v2_unsupported", kind: domain.KindWhisparrV2, candidates: sonarrClean, wantErr: queue.ErrQueueUnsupported},
		{name: "boundary/whisparr_v3_unsupported", kind: domain.KindWhisparrV3, candidates: radarrClean, wantErr: queue.ErrQueueUnsupported},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			arr := &fakeImportArr{candidates: tt.candidates, getStatus: tt.getStatus, postStatus: tt.postStatus}
			srv := httptest.NewServer(arr)
			t.Cleanup(srv.Close)
			p := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: 2 * time.Second})
			n, err := p.ForceImport(t.Context(), tt.kind, srv.URL, "k", "dl-1", queue.ImportModeClean)
			checkForceImportErr(t, err, tt.wantErr, tt.wantAnyErr)
			if err == nil && n != tt.wantN {
				t.Fatalf("n = %d, want %d", n, tt.wantN)
			}
			checkCommands(t, arr, tt.wantPaths)
		})
	}
}

func TestForceImportFixableMode(t *testing.T) {
	t.Parallel()

	sonarrFixable := `{"path":"/dl/fix.mkv","series":{"id":5},"episodes":[{"id":51}],"quality":{"quality":{"id":3}},"downloadId":"dl-1","rejections":[{"reason":"Single episode split into multiple files","type":"permanent"}]}`
	sonarrUnknownRejection := `{"path":"/dl/unk.mkv","series":{"id":5},"episodes":[{"id":52}],"quality":{"quality":{"id":3}},"downloadId":"dl-1","rejections":[{"reason":"Unknown weird rejection reason","type":"permanent"}]}`
	sonarrBrokenRejection := `{"path":"/dl/brk.mkv","series":{"id":5},"episodes":[{"id":53}],"quality":{"quality":{"id":3}},"downloadId":"dl-1","rejections":[{"reason":"Sample","type":"permanent"}]}`
	lidarrFixable := `{"path":"/dl/fix.flac","artist":{"id":3},"album":{"id":31},"tracks":[{"id":301}],"quality":{"quality":{"id":1}},"downloadId":"dl-1","rejections":[{"reason":"Has missing tracks","type":"permanent"}]}`

	tests := []struct {
		name       string
		kind       domain.AppKind
		candidates string
		wantN      int
		wantPaths  []string
		wantErr    error
	}{
		{
			name:       "positive/sonarr_fixable_qualifies",
			kind:       domain.KindSonarr,
			candidates: sonarrFixable,
			wantN:      1,
			wantPaths:  []string{"/dl/fix.mkv"},
		},
		{
			name:       "positive/lidarr_fixable_qualifies",
			kind:       domain.KindLidarr,
			candidates: lidarrFixable,
			wantN:      1,
			wantPaths:  []string{"/dl/fix.flac"},
		},
		{
			name:       "negative/sonarr_unknown_rejection_skipped",
			kind:       domain.KindSonarr,
			candidates: sonarrUnknownRejection,
			wantErr:    queue.ErrNoCleanCandidate,
		},
		{
			name:       "negative/sonarr_broken_rejection_skipped",
			kind:       domain.KindSonarr,
			candidates: sonarrBrokenRejection,
			wantErr:    queue.ErrNoCleanCandidate,
		},
		{
			name:       "positive/mixed_fixable_and_unknown_picks_only_fixable",
			kind:       domain.KindSonarr,
			candidates: sonarrUnknownRejection + "," + sonarrFixable,
			wantN:      1,
			wantPaths:  []string{"/dl/fix.mkv"},
		},
		{
			name:       "negative/whisparr_v2_unsupported_in_fixable_mode",
			kind:       domain.KindWhisparrV2,
			candidates: sonarrFixable,
			wantErr:    queue.ErrQueueUnsupported,
		},
		{
			name:       "negative/whisparr_v3_unsupported_in_fixable_mode",
			kind:       domain.KindWhisparrV3,
			candidates: sonarrFixable,
			wantErr:    queue.ErrQueueUnsupported,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			arr := &fakeImportArr{candidates: tt.candidates}
			srv := httptest.NewServer(arr)
			t.Cleanup(srv.Close)
			p := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: 2 * time.Second})
			n, err := p.ForceImport(t.Context(), tt.kind, srv.URL, "k", "dl-1", queue.ImportModeFixable)
			checkForceImportErr(t, err, tt.wantErr, false)
			if err == nil && n != tt.wantN {
				t.Fatalf("n = %d, want %d", n, tt.wantN)
			}
			checkCommands(t, arr, tt.wantPaths)
		})
	}
}

func checkForceImportErr(t *testing.T, err, want error, wantAny bool) {
	t.Helper()
	switch {
	case want != nil && !errors.Is(err, want):
		t.Fatalf("err = %v, want %v", err, want)
	case wantAny && (err == nil || errors.Is(err, queue.ErrNoCleanCandidate)):
		t.Fatalf("err = %v, want a transport error", err)
	case want == nil && !wantAny && err != nil:
		t.Fatalf("err = %v, want nil", err)
	}
}

// checkCommands asserts one ManualImport command in auto mode with exactly
// wantPaths, or no command at all when wantPaths is empty.
func checkCommands(t *testing.T, arr *fakeImportArr, wantPaths []string) {
	t.Helper()
	arr.mu.Lock()
	defer arr.mu.Unlock()
	if len(wantPaths) == 0 {
		if len(arr.commands) != 0 {
			t.Fatalf("commands = %v, want none", arr.commands)
		}
		return
	}
	vals, err := url.ParseQuery(arr.query)
	if err != nil || vals.Get("downloadId") != "dl-1" || vals.Get("filterExistingFiles") != "true" {
		t.Fatalf("candidate query = %q", arr.query)
	}
	if len(arr.commands) != 1 {
		t.Fatalf("commands = %d, want 1", len(arr.commands))
	}
	cmd := arr.commands[0]
	files, _ := cmd["files"].([]any)
	if cmd["name"] != "ManualImport" || cmd["importMode"] != "auto" || len(files) != len(wantPaths) {
		t.Fatalf("command = %v, want ManualImport auto with %v", cmd, wantPaths)
	}
	for i, f := range files {
		if m, _ := f.(map[string]any); m["path"] != wantPaths[i] {
			t.Fatalf("file %d = %v, want path %s", i, f, wantPaths[i])
		}
	}
}
