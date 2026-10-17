// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/dlclients"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// liveEnv names the file tools/e2e/dlclients_live.sh writes: the real clients it started.
const liveEnv = "SNATCHARR_DLCLIENTS_LIVE"

// unknownVersion lists the clients whose version the adapter cannot read yet, with the reason.
// A release that fixes one turns the live run red until its entry goes.
var unknownVersion = map[domain.DownloadClientKind]string{
	domain.ClientDeluge: "goenvoy asks Deluge 2 with daemon.info: golusoris/goenvoy#101",
}

// liveClient is one running download client. Auth says whether the client refuses a wrong
// secret (rTorrent's XML-RPC endpoint has no login of its own).
type liveClient struct {
	Kind     domain.DownloadClientKind `json:"kind"`
	URL      string                    `json:"url"`
	Username string                    `json:"username"`
	Secret   string                    `json:"secret"`
	Auth     bool                      `json:"auth"`
	Paused   bool                      `json:"paused"`
}

// TestLiveDownloadClients observes each real client through the adapter that production uses.
// It runs in `make dlclients-live`, which starts the clients and fails when this test skipped.
func TestLiveDownloadClients(t *testing.T) {
	t.Parallel()
	path := os.Getenv(liveEnv)
	if path == "" {
		t.Skip(liveEnv + " is not set; run make dlclients-live")
	}
	clients := readLiveClients(t, path)
	seen := map[domain.DownloadClientKind]bool{}
	g := dlclients.NewGoenvoy(clock.NewFake())
	for _, c := range clients {
		seen[c.Kind] = true
		t.Run(string(c.Kind), func(t *testing.T) {
			t.Parallel()
			checkLiveClient(t, g, c)
		})
	}
	for _, kind := range domain.AllDownloadClientKinds {
		if !seen[kind] {
			t.Errorf("%s: no running client in %s", kind, path)
		}
	}
}

func readLiveClients(t *testing.T, path string) []liveClient {
	t.Helper()
	raw, err := os.ReadFile(path) // #nosec G304 -- the path is the test's own input file
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var clients []liveClient
	if err = json.Unmarshal(raw, &clients); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return clients
}

func checkLiveClient(t *testing.T, g *dlclients.Goenvoy, c liveClient) {
	t.Helper()
	dc := domain.DownloadClient{Kind: c.Kind, Name: string(c.Kind), BaseURL: c.URL, Username: c.Username}
	snap := g.Snapshot(t.Context(), dc, c.Secret)
	if !snap.Reachable {
		t.Fatalf("not reachable with the right secret: %s", snap.Error)
	}
	if issue, unknown := unknownVersion[c.Kind]; unknown != (snap.Version == "") {
		t.Errorf("version = %q; want it known = %t (%s)", snap.Version, !unknown, issue)
	}
	if snap.Active != 0 || snap.Queued != 0 {
		t.Errorf("an empty client reports active %d, queued %d; want 0, 0", snap.Active, snap.Queued)
	}
	if snap.Paused != c.Paused {
		t.Errorf("paused = %t; the client was left with paused = %t", snap.Paused, c.Paused)
	}
	if !c.Auth {
		return
	}
	if wrong := g.Snapshot(t.Context(), dc, c.Secret+"-wrong"); wrong.Reachable || wrong.Error == "" {
		t.Errorf("a wrong secret was accepted: reachable %t, error %q", wrong.Reachable, wrong.Error)
	}
}
