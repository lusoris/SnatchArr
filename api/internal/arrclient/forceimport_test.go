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
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/queue"
)

// Manual-import candidates in the shape each *arr returns them.
const (
	sonarrClean        = `{"path":"/dl/a.mkv","series":{"id":5},"episodes":[{"id":51},{"id":52}],"quality":{"quality":{"id":3}},"downloadId":"dl-1","releaseType":"multiEpisode","rejections":[]}`
	sonarrRejected     = `{"path":"/dl/b.mkv","series":{"id":5},"episodes":[{"id":53}],"rejections":[{"reason":"Not an upgrade","type":"permanent"}]}`
	sonarrUnmapped     = `{"path":"/dl/c.mkv","series":{"id":5},"episodes":[],"rejections":[]}`
	radarrClean        = `{"path":"/dl/m.mkv","movie":{"id":9},"quality":{"quality":{"id":7}},"downloadId":"dl-1","rejections":[]}`
	radarrRejected     = `{"path":"/dl/n.mkv","movie":{"id":9},"rejections":[{"reason":"Sample","type":"permanent"}]}`
	radarrUnmapped     = `{"path":"/dl/o.mkv","rejections":[]}`
	lidarrClean        = `{"path":"/dl/t1.flac","artist":{"id":3},"album":{"id":31},"tracks":[{"id":301}],"quality":{"quality":{"id":1}},"downloadId":"dl-1","rejections":[]}`
	lidarrRejected     = `{"path":"/dl/t2.flac","artist":{"id":3},"album":{"id":31},"tracks":[{"id":302}],"rejections":[{"reason":"Sample","type":"permanent"}]}`
	lidarrUnmapped     = `{"path":"/dl/t3.flac","artist":{"id":3},"album":{"id":31},"tracks":[],"rejections":[]}`
	readarrClean       = `{"path":"/dl/b.epub","author":{"id":4},"book":{"id":41},"quality":{"quality":{"id":2}},"downloadId":"dl-1","rejections":[]}`
	readarrRejected    = `{"path":"/dl/b2.epub","author":{"id":4},"book":{"id":41},"rejections":[{"reason":"Sample","type":"permanent"}]}`
	readarrUnmapped    = `{"path":"/dl/b3.epub","author":{"id":4},"rejections":[]}`
	whisparrV2Clean    = `{"path":"/dl/w2.mkv","series":{"id":7},"episodes":[{"id":71}],"quality":{"quality":{"id":4}},"downloadId":"dl-1","rejections":[]}`
	whisparrV2Rejected = `{"path":"/dl/w2_rej.mkv","series":{"id":7},"episodes":[{"id":71}],"rejections":[{"reason":"Sample","type":"permanent"}]}`
	whisparrV2Unmapped = `{"path":"/dl/w2_unmap.mkv","series":{"id":7},"episodes":[],"rejections":[]}`
	whisparrV3Clean    = `{"path":"/dl/w3.mkv","movie":{"id":8},"quality":{"quality":{"id":5}},"downloadId":"dl-1","indexerFlags":0,"rejections":[]}`
	whisparrV3Rejected = `{"path":"/dl/w3_rej.mkv","movie":{"id":8},"rejections":[{"reason":"Sample","type":"permanent"}]}`
	whisparrV3Unmapped = `{"path":"/dl/w3_unmap.mkv","rejections":[]}`
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
		{name: "positive/whisparr_v2_clean", kind: domain.KindWhisparrV2, candidates: whisparrV2Clean, wantN: 1, wantPaths: []string{"/dl/w2.mkv"}},
		{name: "positive/whisparr_v3_clean", kind: domain.KindWhisparrV3, candidates: whisparrV3Clean, wantN: 1, wantPaths: []string{"/dl/w3.mkv"}},
		{name: "positive/sonarr_only_clean_of_mixed", kind: domain.KindSonarr, candidates: sonarrRejected + "," + sonarrClean + "," + sonarrUnmapped, wantN: 1, wantPaths: []string{"/dl/a.mkv"}},
		{name: "positive/radarr_only_clean_of_mixed", kind: domain.KindRadarr, candidates: radarrUnmapped + "," + radarrClean + "," + radarrRejected, wantN: 1, wantPaths: []string{"/dl/m.mkv"}},
		{name: "positive/lidarr_only_clean_of_mixed", kind: domain.KindLidarr, candidates: lidarrRejected + "," + lidarrClean + "," + lidarrUnmapped, wantN: 1, wantPaths: []string{"/dl/t1.flac"}},
		{name: "positive/readarr_only_clean_of_mixed", kind: domain.KindReadarr, candidates: readarrUnmapped + "," + readarrClean + "," + readarrRejected, wantN: 1, wantPaths: []string{"/dl/b.epub"}},
		{name: "positive/whisparr_v2_only_clean_of_mixed", kind: domain.KindWhisparrV2, candidates: whisparrV2Rejected + "," + whisparrV2Clean + "," + whisparrV2Unmapped, wantN: 1, wantPaths: []string{"/dl/w2.mkv"}},
		{name: "positive/whisparr_v3_only_clean_of_mixed", kind: domain.KindWhisparrV3, candidates: whisparrV3Unmapped + "," + whisparrV3Clean + "," + whisparrV3Rejected, wantN: 1, wantPaths: []string{"/dl/w3.mkv"}},
		{name: "negative/sonarr_rejected_refused", kind: domain.KindSonarr, candidates: sonarrRejected, wantErr: queue.ErrNoCleanCandidate},
		{name: "negative/sonarr_unmapped_refused", kind: domain.KindSonarr, candidates: sonarrUnmapped, wantErr: queue.ErrNoCleanCandidate},
		{name: "negative/radarr_rejected_refused", kind: domain.KindRadarr, candidates: radarrRejected, wantErr: queue.ErrNoCleanCandidate},
		{name: "negative/radarr_unmapped_refused", kind: domain.KindRadarr, candidates: radarrUnmapped, wantErr: queue.ErrNoCleanCandidate},
		{name: "negative/lidarr_rejected_refused", kind: domain.KindLidarr, candidates: lidarrRejected, wantErr: queue.ErrNoCleanCandidate},
		{name: "negative/lidarr_unmapped_refused", kind: domain.KindLidarr, candidates: lidarrUnmapped, wantErr: queue.ErrNoCleanCandidate},
		{name: "negative/readarr_rejected_refused", kind: domain.KindReadarr, candidates: readarrRejected, wantErr: queue.ErrNoCleanCandidate},
		{name: "negative/readarr_unmapped_refused", kind: domain.KindReadarr, candidates: readarrUnmapped, wantErr: queue.ErrNoCleanCandidate},
		{name: "negative/whisparr_v2_rejected_refused", kind: domain.KindWhisparrV2, candidates: whisparrV2Rejected, wantErr: queue.ErrNoCleanCandidate},
		{name: "negative/whisparr_v2_unmapped_refused", kind: domain.KindWhisparrV2, candidates: whisparrV2Unmapped, wantErr: queue.ErrNoCleanCandidate},
		{name: "negative/whisparr_v3_rejected_refused", kind: domain.KindWhisparrV3, candidates: whisparrV3Rejected, wantErr: queue.ErrNoCleanCandidate},
		{name: "negative/whisparr_v3_unmapped_refused", kind: domain.KindWhisparrV3, candidates: whisparrV3Unmapped, wantErr: queue.ErrNoCleanCandidate},
		{name: "negative/unknown_kind_invalid", kind: domain.AppKind("bogus"), candidates: sonarrClean, wantErr: domain.ErrInvalid},
		{name: "negative/candidates_error", kind: domain.KindSonarr, getStatus: http.StatusInternalServerError, wantAnyErr: true},
		{name: "negative/whisparr_v2_candidates_error", kind: domain.KindWhisparrV2, getStatus: http.StatusInternalServerError, wantAnyErr: true},
		{name: "negative/whisparr_v3_candidates_error", kind: domain.KindWhisparrV3, getStatus: http.StatusInternalServerError, wantAnyErr: true},
		{name: "negative/command_error", kind: domain.KindRadarr, candidates: radarrClean, postStatus: http.StatusInternalServerError, wantAnyErr: true, wantPaths: []string{"/dl/m.mkv"}},
		{name: "boundary/radarr_no_candidates_refused", kind: domain.KindRadarr, candidates: "", wantErr: queue.ErrNoCleanCandidate},
		{name: "boundary/lidarr_no_candidates_refused", kind: domain.KindLidarr, candidates: "", wantErr: queue.ErrNoCleanCandidate},
		{name: "boundary/readarr_no_candidates_refused", kind: domain.KindReadarr, candidates: "", wantErr: queue.ErrNoCleanCandidate},
		{name: "boundary/whisparr_v2_no_candidates_refused", kind: domain.KindWhisparrV2, candidates: "", wantErr: queue.ErrNoCleanCandidate},
		{name: "boundary/whisparr_v3_no_candidates_refused", kind: domain.KindWhisparrV3, candidates: "", wantErr: queue.ErrNoCleanCandidate},
		{name: "boundary/whisparr_v3_indexer_flags_zero_preserved", kind: domain.KindWhisparrV3, candidates: whisparrV3Clean, wantN: 1, wantPaths: []string{"/dl/w3.mkv"}},
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
	whisparrV2Fixable := `{"path":"/dl/w2_fix.mkv","series":{"id":7},"episodes":[{"id":71}],"quality":{"quality":{"id":4}},"downloadId":"dl-1","rejections":[{"reason":"Single episode split into multiple files","type":"permanent"}]}`
	whisparrV3Fixable := `{"path":"/dl/w3_fix.mkv","movie":{"id":8},"quality":{"quality":{"id":5}},"downloadId":"dl-1","indexerFlags":0,"rejections":[{"reason":"Found matching movie via grab history, but release was matched to movie by ID. Manual Import required.","type":"permanent"}]}`

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
			name:       "positive/whisparr_v2_fixable_qualifies",
			kind:       domain.KindWhisparrV2,
			candidates: whisparrV2Fixable,
			wantN:      1,
			wantPaths:  []string{"/dl/w2_fix.mkv"},
		},
		{
			name:       "positive/whisparr_v3_fixable_qualifies",
			kind:       domain.KindWhisparrV3,
			candidates: whisparrV3Fixable,
			wantN:      1,
			wantPaths:  []string{"/dl/w3_fix.mkv"},
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
		m, _ := f.(map[string]any)
		if m["path"] != wantPaths[i] {
			t.Fatalf("file %d = %v, want path %s", i, f, wantPaths[i])
		}
		if m["path"] == "/dl/w3.mkv" {
			flags, ok := m["indexerFlags"]
			if !ok {
				t.Fatalf("file %d omitted indexerFlags: %v", i, f)
			}
			if flags != float64(0) {
				t.Fatalf("file %d indexerFlags = %v, want 0", i, flags)
			}
			if m["movieId"] != float64(8) {
				t.Fatalf("file %d movieId = %v, want 8", i, m["movieId"])
			}
		}
		if m["path"] == "/dl/w2.mkv" {
			if m["seriesId"] != float64(7) {
				t.Fatalf("file %d seriesId = %v, want 7", i, m["seriesId"])
			}
			eps, _ := m["episodeIds"].([]any)
			if len(eps) != 1 || eps[0] != float64(71) {
				t.Fatalf("file %d episodeIds = %v, want [71]", i, m["episodeIds"])
			}
		}
	}
}

func TestForceImportRejectionReasons(t *testing.T) {
	t.Parallel()
	longReason := strings.Repeat("A", 250)
	truncatedLong := strings.Repeat("A", 200)

	tests := []struct {
		name        string
		kind        domain.AppKind
		candidates  string
		wantReasons []string
	}{
		{
			name:        "boundary/zero_reasons_unmapped",
			kind:        domain.KindSonarr,
			candidates:  sonarrUnmapped,
			wantReasons: nil,
		},
		{
			name:        "positive/single_reason",
			kind:        domain.KindRadarr,
			candidates:  radarrRejected,
			wantReasons: []string{"Sample"},
		},
		{
			name:        "positive/whisparr_v2_single_reason",
			kind:        domain.KindWhisparrV2,
			candidates:  whisparrV2Rejected,
			wantReasons: []string{"Sample"},
		},
		{
			name:        "positive/whisparr_v3_single_reason",
			kind:        domain.KindWhisparrV3,
			candidates:  whisparrV3Rejected,
			wantReasons: []string{"Sample"},
		},
		{
			name: "positive/dedup_single_reason_across_candidates",
			kind: domain.KindSonarr,
			candidates: `{"path":"/dl/1.mkv","series":{"id":5},"rejections":[{"reason":"Sample"}]},` +
				`{"path":"/dl/2.mkv","series":{"id":5},"rejections":[{"reason":"Sample"}]}`,
			wantReasons: []string{"Sample"},
		},
		{
			name: "boundary/six_reasons_capped_at_five_and_truncated",
			kind: domain.KindSonarr,
			candidates: `{"path":"/dl/1.mkv","series":{"id":5},"rejections":[{"reason":"Reason 1"}]},` +
				`{"path":"/dl/2.mkv","series":{"id":5},"rejections":[{"reason":"Reason 1"},{"reason":"Reason 2"}]},` +
				`{"path":"/dl/3.mkv","series":{"id":5},"rejections":[{"reason":"` + longReason + `"}]},` +
				`{"path":"/dl/4.mkv","series":{"id":5},"rejections":[{"reason":"Reason 4"}]},` +
				`{"path":"/dl/5.mkv","series":{"id":5},"rejections":[{"reason":"Reason 5"}]},` +
				`{"path":"/dl/6.mkv","series":{"id":5},"rejections":[{"reason":"Reason 6"}]}`,
			wantReasons: []string{"Reason 1", "Reason 2", truncatedLong, "Reason 4", "Reason 5"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			arr := &fakeImportArr{candidates: tt.candidates}
			srv := httptest.NewServer(arr)
			t.Cleanup(srv.Close)
			p := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: 2 * time.Second})
			_, err := p.ForceImport(t.Context(), tt.kind, srv.URL, "k", "dl-1", queue.ImportModeClean)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !errors.Is(err, queue.ErrNoCleanCandidate) {
				t.Fatalf("errors.Is(err, ErrNoCleanCandidate) = false, got %v", err)
			}
			var nce *queue.NoCleanCandidateError
			if !errors.As(err, &nce) {
				t.Fatalf("errors.As(err, &nce) = false, got %v", err)
			}
			if !reflect.DeepEqual(nce.Reasons, tt.wantReasons) {
				t.Fatalf("reasons = %q, want %q", nce.Reasons, tt.wantReasons)
			}
		})
	}
}

func TestForceImportTimeout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		opts    arrclient.Options
		delay   time.Duration
		wantErr bool
		wantN   int
	}{
		{
			name:    "positive: import timeout allows slow response",
			opts:    arrclient.Options{Timeout: 50 * time.Millisecond, ImportTimeout: 2 * time.Second},
			delay:   200 * time.Millisecond,
			wantErr: false,
			wantN:   1,
		},
		{
			name:    "negative: exceeded import timeout errors",
			opts:    arrclient.Options{Timeout: 50 * time.Millisecond, ImportTimeout: 100 * time.Millisecond},
			delay:   400 * time.Millisecond,
			wantErr: true,
			wantN:   0,
		},
		{
			name:    "boundary: import timeout raised to timeout",
			opts:    arrclient.Options{Timeout: time.Second, ImportTimeout: 10 * time.Millisecond},
			delay:   100 * time.Millisecond,
			wantErr: false,
			wantN:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(timeoutImportHandler(tt.delay))
			t.Cleanup(srv.Close)

			p := arrclient.NewGoenvoyProber(tt.opts)
			n, err := p.ForceImport(t.Context(), domain.KindLidarr, srv.URL, "k", "dl-1", queue.ImportModeClean)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if errors.Is(err, queue.ErrNoCleanCandidate) {
					t.Fatalf("expected non-ErrNoCleanCandidate error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if n != tt.wantN {
				t.Fatalf("n = %d, want %d", n, tt.wantN)
			}
		})
	}
}

func timeoutImportHandler(delay time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && (r.URL.Path == "/api/v1/manualimport" || r.URL.Path == "/api/v3/manualimport") {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-r.Context().Done():
				return
			case <-timer.C:
			}
			_, _ = io.WriteString(w, "["+lidarrClean+"]")
			return
		}
		if r.Method == http.MethodPost && (r.URL.Path == "/api/v1/command" || r.URL.Path == "/api/v3/command") {
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":1,"name":"ManualImport","status":"queued"}`)
			return
		}
		http.NotFound(w, r)
	})
}
