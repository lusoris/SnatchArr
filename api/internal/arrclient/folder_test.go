// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package arrclient_test

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestListFolder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind      domain.AppKind
		path      string
		withFiles bool
		want      []string
	}{
		{domain.KindSonarr, "/api/v3/filesystem", true, []string{"job", "note"}},
		{domain.KindRadarr, "/api/v3/filesystem", true, []string{"job", "note"}},
		{domain.KindLidarr, "/api/v1/filesystem", true, []string{"job", "note"}},
		{domain.KindReadarr, "/api/v1/filesystem", false, []string{"job"}},
		{domain.KindWhisparrV2, "/api/v3/filesystem", false, []string{"job"}},
		{domain.KindWhisparrV3, "/api/v3/filesystem", false, []string{"job"}},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			t.Parallel()
			server := newFolderServer(tc.path, tc.withFiles)
			defer server.Close()
			p := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: time.Second})
			names, err := p.ListFolder(t.Context(), tc.kind, server.URL, "test-key", "/data/job space#/")
			if err != nil || !slices.Equal(names, tc.want) {
				t.Fatalf("ListFolder() = %q, %v; want %q", names, err, tc.want)
			}
		})
	}
}

func newFolderServer(path string, withFiles bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path || r.URL.Query().Get("path") != "/data/job space#/" {
			http.NotFound(w, r)
			return
		}
		if r.URL.RawQuery == "path=%2Fdata%2Fjob+space%23%2F&includeFiles=true" != withFiles {
			http.Error(w, "unexpected query", http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("includeFiles") == "true" {
			_, _ = w.Write([]byte(`{"directories":[{"name":"job"}],"files":[{"name":"note"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"directories":[{"name":"job"}],"files":[]}`))
	}))
}

func TestListFolderFailsClosed(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusInternalServerError)
	}))
	defer server.Close()
	p := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: time.Second})
	if _, err := p.ListFolder(t.Context(), domain.KindSonarr, server.URL, "test-key", "/data/"); err == nil {
		t.Fatal("ListFolder() error = nil, want HTTP error")
	}
	malformed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"directories":"bad","files":[]}`))
	}))
	defer malformed.Close()
	if _, err := p.ListFolder(t.Context(), domain.KindWhisparrV2, malformed.URL, "test-key", "/data/"); err == nil {
		t.Fatal("ListFolder() error = nil, want malformed shape error")
	}
}

func TestListFolderDeadline(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(200 * time.Millisecond):
			http.Error(w, "late", http.StatusGatewayTimeout)
		}
	}))
	defer server.Close()
	p := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: 50 * time.Millisecond})
	if _, err := p.ListFolder(t.Context(), domain.KindSonarr, server.URL, "test-key", "/data/"); err == nil {
		t.Fatal("ListFolder() error = nil, want deadline error")
	}
}

func TestListFolderEntryCap(t *testing.T) {
	t.Parallel()
	const maxEntries = 10000
	for _, tc := range []struct {
		kind domain.AppKind
		path string
	}{{domain.KindSonarr, "/api/v3/filesystem"}, {domain.KindWhisparrV3, "/api/v3/filesystem"}} {
		t.Run(string(tc.kind), func(t *testing.T) {
			t.Parallel()
			for _, count := range []int{maxEntries, maxEntries + 1} {
				wantOK := count == maxEntries
				t.Run(strconv.Itoa(count), func(t *testing.T) {
					t.Parallel()
					entries := strings.TrimSuffix(strings.Repeat(`{"name":"job"},`, count), ",")
					payload := `{"directories":[` + entries + `],"files":[]}`
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.URL.Path != tc.path {
							http.NotFound(w, r)
							return
						}
						_, _ = w.Write([]byte(payload))
					}))
					defer server.Close()
					p := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: time.Second})
					names, err := p.ListFolder(t.Context(), tc.kind, server.URL, "test-key", "/data/")
					if wantOK {
						if err != nil || len(names) != count {
							t.Fatalf("ListFolder() count = %d, err = %v; want %d names", len(names), err, count)
						}
						for i := range min(len(names), maxEntries) {
							if names[i] != "job" {
								t.Fatalf("names[%d] = %q, want job", i, names[i])
							}
						}
					} else if err == nil || names != nil || !strings.Contains(err.Error(), "10001") || !strings.Contains(err.Error(), "10000") {
						t.Fatalf("ListFolder() = %d names, %v; want capped listing error", len(names), err)
					}
				})
			}
		})
	}
}
