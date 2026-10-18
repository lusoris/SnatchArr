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
	"sync"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/queue"
)

// Manual-import candidates in the shape Sonarr and Radarr return them.
const (
	sonarrClean    = `{"path":"/dl/a.mkv","series":{"id":5},"episodes":[{"id":51},{"id":52}],"quality":{"quality":{"id":3}},"downloadId":"dl-1","releaseType":"multiEpisode","rejections":[]}`
	sonarrRejected = `{"path":"/dl/b.mkv","series":{"id":5},"episodes":[{"id":53}],"rejections":[{"reason":"Not an upgrade","type":"permanent"}]}`
	sonarrUnmapped = `{"path":"/dl/c.mkv","series":{"id":5},"episodes":[],"rejections":[]}`
	radarrClean    = `{"path":"/dl/m.mkv","movie":{"id":9},"quality":{"quality":{"id":7}},"downloadId":"dl-1","rejections":[]}`
	radarrRejected = `{"path":"/dl/n.mkv","movie":{"id":9},"rejections":[{"reason":"Sample","type":"permanent"}]}`
	radarrUnmapped = `{"path":"/dl/o.mkv","rejections":[]}`
)

// fakeImportArr serves GET /api/v3/manualimport and POST /api/v3/command and
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
	case r.Method == http.MethodGet && r.URL.Path == "/api/v3/manualimport":
		f.mu.Lock()
		f.query = r.URL.RawQuery
		f.mu.Unlock()
		if f.getStatus != 0 {
			w.WriteHeader(f.getStatus)
			return
		}
		_, _ = io.WriteString(w, "["+f.candidates+"]")
	case r.Method == http.MethodPost && r.URL.Path == "/api/v3/command":
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
		{name: "positive/sonarr_only_clean_of_mixed", kind: domain.KindSonarr, candidates: sonarrRejected + "," + sonarrClean + "," + sonarrUnmapped, wantN: 1, wantPaths: []string{"/dl/a.mkv"}},
		{name: "positive/radarr_only_clean_of_mixed", kind: domain.KindRadarr, candidates: radarrUnmapped + "," + radarrClean + "," + radarrRejected, wantN: 1, wantPaths: []string{"/dl/m.mkv"}},
		{name: "negative/sonarr_rejected_refused", kind: domain.KindSonarr, candidates: sonarrRejected, wantErr: queue.ErrNoCleanCandidate},
		{name: "negative/sonarr_unmapped_refused", kind: domain.KindSonarr, candidates: sonarrUnmapped, wantErr: queue.ErrNoCleanCandidate},
		{name: "negative/radarr_rejected_refused", kind: domain.KindRadarr, candidates: radarrRejected, wantErr: queue.ErrNoCleanCandidate},
		{name: "negative/radarr_unmapped_refused", kind: domain.KindRadarr, candidates: radarrUnmapped, wantErr: queue.ErrNoCleanCandidate},
		{name: "negative/lidarr_unsupported", kind: domain.KindLidarr, candidates: sonarrClean, wantErr: queue.ErrQueueUnsupported},
		{name: "negative/unknown_kind_invalid", kind: domain.AppKind("bogus"), candidates: sonarrClean, wantErr: domain.ErrInvalid},
		{name: "negative/candidates_error", kind: domain.KindSonarr, getStatus: http.StatusInternalServerError, wantAnyErr: true},
		{name: "negative/command_error", kind: domain.KindRadarr, candidates: radarrClean, postStatus: http.StatusInternalServerError, wantAnyErr: true, wantPaths: []string{"/dl/m.mkv"}},
		{name: "boundary/no_candidates_refused", kind: domain.KindRadarr, candidates: "", wantErr: queue.ErrNoCleanCandidate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			arr := &fakeImportArr{candidates: tt.candidates, getStatus: tt.getStatus, postStatus: tt.postStatus}
			srv := httptest.NewServer(arr)
			t.Cleanup(srv.Close)
			p := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: 2 * time.Second})
			n, err := p.ForceImport(t.Context(), tt.kind, srv.URL, "k", "dl-1")
			checkForceImportErr(t, err, tt.wantErr, tt.wantAnyErr)
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
	if arr.query != "downloadId=dl-1&filterExistingFiles=true" {
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
