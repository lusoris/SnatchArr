// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"
)

// TestFakeServerUnreachableSurvivesPortReuse holds the unreachable fakes to their word: a
// parallel test may bind the port a closed fake freed, and a link seeded after setUnreachable
// must still fail to connect instead of reaching that other server.
func TestFakeServerUnreachableSurvivesPortReuse(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		unreachable func(t *testing.T) (closedURL, linkURL string)
	}{
		{"prowlarr", func(t *testing.T) (string, string) {
			t.Helper()
			s := newFakeProwlarrServer(t)
			closed := s.URL
			s.setUnreachable()
			return closed, s.URL
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			closedURL, linkURL := tc.unreachable(t)
			reclaimPort(t, closedURL)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, linkURL+"/api/status", http.NoBody)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err == nil {
				_ = resp.Body.Close()
				t.Fatalf("GET %s after setUnreachable answered %d; want a refused connection", linkURL, resp.StatusCode)
			}
		})
	}
}

// reclaimPort binds the address of a closed fake again, as a parallel test's server may, and
// answers every request with 200.
func reclaimPort(t *testing.T, closedURL string) {
	t.Helper()
	u, err := url.Parse(closedURL)
	if err != nil {
		t.Fatalf("parse %s: %v", closedURL, err)
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", u.Host)
	if err != nil {
		t.Fatalf("reclaim %s: %v", u.Host, err)
	}
	srv := &http.Server{
		Handler:           http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
		ReadHeaderTimeout: time.Second,
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
}
