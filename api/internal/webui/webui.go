// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package webui embeds the built SvelteKit SPA (adapter-static output copied to dist/)
// and serves it with immutable caching for hashed assets and an index.html fallback for
// client-side routes (ADR-0005).
package webui

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/golusoris/golusoris/httpx/static"
)

//go:embed all:dist
var distFS embed.FS

// FS returns the embedded dist/ tree rooted at its top level.
func FS() (fs.FS, error) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, fmt.Errorf("webui: dist fs: %w", err)
	}
	return sub, nil
}

// Mount attaches the SPA to r. API and probe prefixes are excluded so a typo in an
// API path returns a proper 404 instead of the SPA shell.
func Mount(r chi.Router, fsys fs.FS) {
	immutable := static.Handler(fsys, static.Options{
		CacheControl:    "public, max-age=31536000, immutable",
		NoIndexFallback: true,
	})
	shell := static.Handler(fsys, static.Options{
		CacheControl: "no-cache",
	})
	r.Handle("/_app/*", immutable)
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		if isNonUIPath(req.URL.Path) {
			http.NotFound(w, req)
			return
		}
		if strings.Contains(req.URL.Path, ".") {
			shell.ServeHTTP(w, req)
			return
		}
		req.URL.Path = "/"
		shell.ServeHTTP(w, req)
	})
}

func isNonUIPath(p string) bool {
	for _, prefix := range []string{"/api/", "/livez", "/readyz", "/startupz", "/metrics", "/docs"} {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}
