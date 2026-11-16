// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package tvdbprovider

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

type redirectCase struct {
	name, path, target string
	status             int
	wantRequests       int64
}

func TestRedirectRefused(t *testing.T) {
	t.Parallel()
	cases := []redirectCase{
		{"cross-host login 307", "/login", "cross-host", http.StatusTemporaryRedirect, 1},
		{"cross-host data 302", "/series/81189", "cross-host", http.StatusFound, 2},
		{"same-host data 307", "/series/81189", "same-host", http.StatusTemporaryRedirect, 2},
		{"same-host another port data 308", "/series/81189", "same-port", http.StatusPermanentRedirect, 2},
		{"data 304 without location", "/series/81189", "none", http.StatusNotModified, 2},
		{"data 302 without location", "/series/81189", "none", http.StatusFound, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runRedirectCase(t, tc)
		})
	}
}

func runRedirectCase(t *testing.T, tc redirectCase) {
	t.Helper()
	s := newStub(t)
	var remoteHits, loginBodyHits, authHits, localHits atomic.Int64
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remoteHits.Add(1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if bytes.Contains(body, []byte(plantedKey)) && bytes.Contains(body, []byte(plantedPIN)) {
			loginBodyHits.Add(1)
		}
		if r.Header.Get("Authorization") != "" {
			authHits.Add(1)
		}
		if r.URL.Path == "/login" {
			_, _ = w.Write([]byte(`{"status":"success","data":{"token":"stub-token"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"id":81189,"name":"en","aliases":[]}}`))
	}))
	t.Cleanup(remote.Close)
	location := redirectLocation(tc.target, s.srv.URL, remote.URL, tc.path)
	s.redirect = func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == "/redirect-target" {
			localHits.Add(1)
			w.WriteHeader(http.StatusOK)
			return true
		}
		if r.URL.Path != tc.path {
			return false
		}
		if location != "" {
			w.Header().Set("Location", location)
		}
		w.WriteHeader(tc.status)
		return true
	}
	var logs bytes.Buffer
	p := s.provider(t, plantedKey, plantedPIN, &logs)
	_, err := p.TitlesFor(t.Context(), sonarrRef(), "de")
	if n := remoteHits.Load(); n != 0 {
		t.Fatalf("redirect target received %d requests; login body received %d times; Authorization received %d times", n, loginBodyHits.Load(), authHits.Load())
	}
	if n := localHits.Load(); n != 0 {
		t.Fatalf("same-host redirect target received %d requests", n)
	}
	if n := s.total.Load(); n != tc.wantRequests {
		t.Fatalf("TVDB requests = %d, want %d (one attempt per call)", n, tc.wantRequests)
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("TitlesFor() error = %v, want ErrUnavailable", err)
	}
	assertNoSecret(t, err.Error(), logs.String())
}

func redirectLocation(target, baseURL, remoteURL, path string) string {
	switch target {
	case "cross-host":
		return strings.Replace(remoteURL, "127.0.0.1", "localhost", 1) + path + "?key=" + plantedKey
	case "same-host":
		return baseURL + "/redirect-target?key=" + plantedKey
	case "same-port":
		return remoteURL + path + "?key=" + plantedKey
	default:
		return ""
	}
}
