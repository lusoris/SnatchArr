// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package webui_test

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/go-chi/chi/v5"

	"github.com/lusoris/SnatchArr/api/internal/webui"
)

const (
	shellBody = "<html>shell</html>"
	assetBody = "console.log(1)"
	immutable = "public, max-age=31536000, immutable"
)

func newRouter() http.Handler {
	r := chi.NewRouter()
	webui.Mount(r, fstest.MapFS{
		"index.html":                 {Data: []byte(shellBody)},
		"favicon.svg":                {Data: []byte("<svg/>")},
		"_app/immutable/entry.a1.js": {Data: []byte(assetBody)},
	})
	return r
}

func TestMount(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, path  string
		code        int
		body, cache string
	}{
		{"positive root serves the shell", "/", http.StatusOK, shellBody, "no-cache"},
		{"positive client route serves the shell", "/instances/42", http.StatusOK, shellBody, "no-cache"},
		{"positive hashed asset is cached for a year", "/_app/immutable/entry.a1.js", http.StatusOK, assetBody, immutable},
		{"positive static file", "/favicon.svg", http.StatusOK, "<svg/>", "no-cache"},
		{"negative missing hashed asset", "/_app/immutable/gone.js", http.StatusNotFound, "", ""},
		{"negative missing file is not the shell", "/missing.png", http.StatusNotFound, "", ""},
		{"negative unknown api path is not the shell", "/api/v1/nope", http.StatusNotFound, "", ""},
		{"negative probe path is not the shell", "/livez", http.StatusNotFound, "", ""},
		{"negative metrics path is not the shell", "/metrics", http.StatusNotFound, "", ""},
		{"boundary docs prefix is not the shell", "/docs/x", http.StatusNotFound, "", ""},
		{"boundary api without the slash is a client route", "/api", http.StatusOK, shellBody, "no-cache"},
	}
	h := newRouter()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rec.Code != tc.code {
				t.Fatalf("GET %s = %d, want %d", tc.path, rec.Code, tc.code)
			}
			if tc.code != http.StatusOK {
				if strings.Contains(rec.Body.String(), shellBody) {
					t.Errorf("GET %s answered the shell", tc.path)
				}
				return
			}
			if rec.Body.String() != tc.body || rec.Header().Get("Cache-Control") != tc.cache {
				t.Errorf("GET %s = %q with Cache-Control %q, want %q with %q", tc.path, rec.Body.String(), rec.Header().Get("Cache-Control"), tc.body, tc.cache)
			}
		})
	}
}

func TestFSHoldsTheShell(t *testing.T) {
	t.Parallel()
	fsys, err := webui.FS()
	if err != nil {
		t.Fatalf("FS() = %v", err)
	}
	if _, err := fs.Stat(fsys, "index.html"); err != nil {
		t.Errorf("embedded dist has no index.html at its root: %v", err)
	}
}
