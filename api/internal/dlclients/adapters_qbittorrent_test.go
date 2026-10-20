// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/golusoris/goenvoy/downloadclient/qbit/v2"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

type qbittorrentAuthCase struct {
	name                   string
	username, secret       string
	loginCode, versionCode int
	loginBody              string
	wantLogin              int32
	wantError              string
}

type qbittorrentAuthCalls struct {
	logins, versions, torrents atomic.Int32
}

func TestQBittorrentAuthentication(t *testing.T) {
	t.Parallel()
	cases := []qbittorrentAuthCase{
		{"success", "admin", "pw", 200, 200, "Ok.", 1, ""},
		{"wrong password", "admin", "wrong", 200, 403, "Fails.", 1, "qbittorrent login failed: wrong username or password"},
		{"host validation", "admin", "pw", 401, 200, "Unauthorized", 1, "qBittorrent refused the request (HTTP 401): its WebUI Host header validation rejects this address; use the host and port qBittorrent serves on, or add this host to its WebUI server domains"},
		{"banned", "admin", "pw", 403, 200, "Forbidden", 1, "qBittorrent banned this IP after too many failed logins"},
		{"no credentials", "", "", 200, 200, "Ok.", 0, ""},
		{"no credentials forbidden", "", "", 200, 403, "Ok.", 0, "qbittorrent version: qbit: HTTP 403"},
		{"username only", "admin", "", 200, 403, "Fails.", 1, "qbittorrent login failed: wrong username or password"},
		{"password only", "", "pw", 200, 403, "Fails.", 1, "qbittorrent login failed: wrong username or password"},
		{"login server failure", "admin", "pw", 500, 200, "failure", 1, "qbittorrent login: qbit: HTTP 500"},
		{"version server failure", "admin", "pw", 200, 500, "Ok.", 1, "qbittorrent version: qbit: HTTP 500"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var calls qbittorrentAuthCalls
			srv := newQBittorrentAuthServer(t, tc, &calls)
			g := NewGoenvoy(clock.NewFake())
			ctx, cancel := context.WithTimeout(t.Context(), ObserveTimeout)
			defer cancel()
			obs, err := g.qbittorrent(ctx, domain.DownloadClient{BaseURL: srv.URL, Username: tc.username}, tc.secret)
			if got := calls.logins.Load(); got != tc.wantLogin {
				t.Errorf("login calls = %d, want %d", got, tc.wantLogin)
			}
			if tc.wantError == "" {
				if err != nil || obs.Version != "v5.2.1" || calls.versions.Load() != 1 || calls.torrents.Load() != 1 {
					t.Fatalf("observation = %+v, error = %v, version/torrent calls = %d/%d", obs, err, calls.versions.Load(), calls.torrents.Load())
				}
				return
			}
			assertQBittorrentAuthFailure(t, tc, err, &calls)
		})
	}
}

func newQBittorrentAuthServer(t *testing.T, tc qbittorrentAuthCase, calls *qbittorrentAuthCalls) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v2/auth/login", func(w http.ResponseWriter, r *http.Request) {
		calls.logins.Add(1)
		if r.FormValue("username") != tc.username || r.FormValue("password") != tc.secret {
			t.Error("login credentials differ from configuration")
		}
		if tc.loginBody == "Ok." {
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "session", Path: "/"})
		}
		w.WriteHeader(tc.loginCode)
		_, _ = io.WriteString(w, tc.loginBody)
	})
	mux.HandleFunc("GET /api/v2/app/version", func(w http.ResponseWriter, r *http.Request) {
		calls.versions.Add(1)
		if tc.wantLogin == 1 && tc.loginBody == "Ok." {
			cookie, err := r.Cookie("SID")
			if err != nil || cookie.Value != "session" {
				t.Errorf("version request missing SID: %v", err)
			}
		}
		w.WriteHeader(tc.versionCode)
		_, _ = io.WriteString(w, "v5.2.1")
	})
	mux.HandleFunc("GET /api/v2/torrents/info", func(w http.ResponseWriter, _ *http.Request) {
		calls.torrents.Add(1)
		_, _ = io.WriteString(w, "[]")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func assertQBittorrentAuthFailure(t *testing.T, tc qbittorrentAuthCase, err error, calls *qbittorrentAuthCalls) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), tc.wantError) {
		t.Errorf("error = %v, want substring %q", err, tc.wantError)
	}
	status := tc.versionCode
	wantVersions := int32(1)
	if tc.loginCode != http.StatusOK {
		status, wantVersions = tc.loginCode, 0
	}
	if apiErr, ok := errors.AsType[*qbit.APIError](err); !ok || apiErr.StatusCode != status {
		t.Errorf("error chain = %v, want qbit APIError %d", err, status)
	}
	if calls.versions.Load() != wantVersions || calls.torrents.Load() != 0 {
		t.Errorf("version/torrent calls = %d/%d, want %d/0", calls.versions.Load(), calls.torrents.Load(), wantVersions)
	}
}
