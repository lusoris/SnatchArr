// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package arrclient_test

import (
	"context"
	"errors"
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
		kind         domain.AppKind
		path         string
		includeFiles string
		want         []string
	}{
		{domain.KindSonarr, "/api/v3/filesystem", "false", []string{"job"}},
		{domain.KindRadarr, "/api/v3/filesystem", "false", []string{"job"}},
		{domain.KindLidarr, "/api/v1/filesystem", "false", []string{"job"}},
		{domain.KindReadarr, "/api/v1/filesystem", "", []string{"job"}},
		{domain.KindWhisparrV2, "/api/v3/filesystem", "", []string{"job"}},
		{domain.KindWhisparrV3, "/api/v3/filesystem", "", []string{"job"}},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			t.Parallel()
			server := newFolderServer(tc.path, tc.includeFiles, `{"directories":[{"name":"job"}],"files":[{"name":"note"}]}`)
			defer server.Close()
			p := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: time.Second})
			names, err := p.ListFolder(t.Context(), tc.kind, server.URL, "test-key", "/data/job space#/")
			if err != nil || !slices.Equal(names, tc.want) {
				t.Fatalf("ListFolder() = %q, %v; want %q", names, err, tc.want)
			}
		})
	}
}

func TestListFolderWithoutFilesKey(t *testing.T) {
	t.Parallel()
	for _, kind := range []domain.AppKind{domain.KindWhisparrV2, domain.KindWhisparrV3} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			server := newFolderServer("/api/v3/filesystem", "", `{"directories":[{"name":"job"}]}`)
			defer server.Close()
			p := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: time.Second})
			names, err := p.ListFolder(t.Context(), kind, server.URL, "test-key", "/data/job space#/")
			if err != nil || !slices.Equal(names, []string{"job"}) {
				t.Fatalf("ListFolder() = %q, %v; want [job]", names, err)
			}
		})
	}
}

func newFolderServer(path, includeFiles, payload string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path || r.URL.Query().Get("path") != "/data/job space#/" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("includeFiles") != includeFiles {
			http.Error(w, "unexpected query", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(payload))
	}))
}

func TestListFolderIgnoresFiles(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind         domain.AppKind
		path         string
		includeFiles string
	}{
		{domain.KindSonarr, "/api/v3/filesystem", "false"},
		{domain.KindRadarr, "/api/v3/filesystem", "false"},
		{domain.KindLidarr, "/api/v1/filesystem", "false"},
		{domain.KindReadarr, "/api/v1/filesystem", ""},
		{domain.KindWhisparrV2, "/api/v3/filesystem", ""},
		{domain.KindWhisparrV3, "/api/v3/filesystem", ""},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			t.Parallel()
			server := newFolderServer(tc.path, tc.includeFiles, `{"directories":[],"files":[{"name":"job"}]}`)
			defer server.Close()
			p := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: time.Second})
			names, err := p.ListFolder(t.Context(), tc.kind, server.URL, "test-key", "/data/job space#/")
			if err != nil || len(names) != 0 {
				t.Fatalf("ListFolder() = %q, %v; want no directory names", names, err)
			}
		})
	}
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
	if _, err := p.ListFolder(t.Context(), domain.KindSonarr, server.URL, "test-key", "/data/"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ListFolder() error = %v, want context deadline exceeded", err)
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
					payload := `{"directories":[` + entries + `],"files":[{"name":"file-entry"}]}`
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

func TestListFolderBodyCap(t *testing.T) {
	t.Parallel()
	const maxBody = 8 << 20
	payload := `{"directories":[{"name":"` + strings.Repeat("a", maxBody) + `"}],"files":[]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	defer server.Close()
	p := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: time.Second})
	if _, err := p.ListFolder(t.Context(), domain.KindSonarr, server.URL, "test-key", "/data/"); err == nil {
		t.Fatal("ListFolder() error = nil, want body limit error")
	}
}
