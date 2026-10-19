// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package arrclient_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/queue"
)

// forceImportLiveEnv names the file tools/e2e/forceimport_live.sh writes: the real Sonarr
// and Radarr it started and the ID-matched download it left in each queue.
const forceImportLiveEnv = "SNATCHARR_FORCEIMPORT_LIVE"

// liveImport is one ID-matched download on a real *arr. Files is the number of files the
// import must send. ExtraLine and SkipImport are the harness's planted cases: an extra
// status line must keep the item out of id_matched, and an import never sent must leave
// the item in the queue.
type liveImport struct {
	Kind       domain.AppKind `json:"kind"`
	URL        string         `json:"url"`
	APIKey     string         `json:"api_key"`
	DownloadID string         `json:"download_id"`
	Files      int            `json:"files"`
	ExtraLine  string         `json:"extra_line"`
	SkipImport bool           `json:"skip_import"`
}

// TestLiveForceImport drives the production write path of rule id_matched (ADR-0013)
// against real Sonarr and Radarr: queue read, sweep in act mode, ManualImport, read-back.
// It runs in `make arr-forceimport-live`, which starts the apps and fails when this test
// skipped.
func TestLiveForceImport(t *testing.T) {
	t.Parallel()
	path := os.Getenv(forceImportLiveEnv)
	if path == "" {
		t.Skip(forceImportLiveEnv + " is not set; run make arr-forceimport-live")
	}
	raw, err := os.ReadFile(path) //nolint:gosec // G304 -- the harness names its own file.
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var cases []liveImport
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	seen := map[domain.AppKind]bool{}
	p := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: 30 * time.Second})
	for _, c := range cases {
		seen[c.Kind] = true
		t.Run(string(c.Kind), func(t *testing.T) {
			t.Parallel()
			checkLiveImport(t, p, c)
		})
	}
	for _, kind := range []domain.AppKind{domain.KindSonarr, domain.KindRadarr} {
		if !seen[kind] {
			t.Errorf("%s: no ID-matched download in %s", kind, path)
		}
	}
}

func checkLiveImport(t *testing.T, p *arrclient.GoenvoyProber, c liveImport) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	items := liveItems(ctx, t, p, c)
	if len(items) == 0 {
		t.Fatalf("download %s is not in the queue", c.DownloadID)
	}
	for i := range items {
		if c.ExtraLine != "" {
			items[i].StatusLines = append(items[i].StatusLines, c.ExtraLine)
		}
	}
	s := queue.DefaultSettings()
	s.Enabled, s.Grace, s.ForceImport = true, 0, queue.SupportsForceImport(c.Kind)
	s.Rules[queue.RuleIDMatched] = queue.RuleSettings{Mode: queue.ModeAct, Threshold: 1, Blocklist: nil}
	ds := queue.Sweep(s, items, nil, time.Now())
	if len(ds) == 0 || !ds[0].Import {
		t.Fatalf("download %s is not id-matched: decisions %+v, status lines %q", c.DownloadID, ds, items[0].StatusLines)
	}
	if !c.SkipImport {
		// The queue's own downloadId, as the service sends it: the *arr matches it
		// case-sensitively and lists the hash in upper case.
		n, err := p.ForceImport(ctx, c.Kind, c.URL, c.APIKey, ds[0].DownloadID, queue.ImportModeClean)
		if err != nil {
			t.Fatalf("force import %s: %v", ds[0].DownloadID, err)
		}
		if n != c.Files {
			t.Errorf("force import sent %d files, want %d", n, c.Files)
		}
	}
	waitImported(ctx, t, p, c)
}

// waitImported reads the queue back until the download has left it: the command runs
// asynchronously, so only the read-back proves the import.
func waitImported(ctx context.Context, t *testing.T, p *arrclient.GoenvoyProber, c liveImport) {
	t.Helper()
	const polls, every = 40, 3 * time.Second
	for range polls {
		if len(liveItems(ctx, t, p, c)) == 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("download %s: %v", c.DownloadID, ctx.Err())
		case <-time.After(every):
		}
	}
	t.Fatalf("download %s is still in the queue %s after the import", c.DownloadID, polls*every)
}

func liveItems(ctx context.Context, t *testing.T, p *arrclient.GoenvoyProber, c liveImport) []queue.Item {
	t.Helper()
	all, err := p.Queue(ctx, c.Kind, c.URL, c.APIKey, 5, 50)
	if err != nil {
		t.Fatalf("read queue: %v", err)
	}
	var out []queue.Item
	for _, it := range all {
		if strings.EqualFold(it.DownloadID, c.DownloadID) {
			out = append(out, it)
		}
	}
	return out
}
