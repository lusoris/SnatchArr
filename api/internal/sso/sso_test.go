// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package sso_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/sso"
)

func TestProxyHeaderMiddleware(t *testing.T) {
	t.Parallel()
	testSecret := strings.Repeat("s", 32)
	baseCfg := config.ProxyAuthOptions{
		Enabled:      true,
		Header:       "Remote-User",
		GroupsHeader: "X-authentik-groups",
		SecretHeader: "X-SnatchArr-Proxy-Secret",
		Secret:       testSecret,
		TrustedCIDRs: []string{"10.0.0.0/8"},
	}

	tests := []struct {
		name         string
		cfgMod       func(*config.ProxyAuthOptions)
		remoteAddr   string
		headers      map[string]string
		wantNext     bool
		wantOnErr    bool
		wantErr      error
		wantStatus   int
		wantUser     string
		wantGroups   []string
		wantStripped bool
	}{
		{
			name:       "positive trusted peer and correct secret",
			remoteAddr: "10.1.2.3:12345",
			headers: map[string]string{
				"Remote-User":              "alice",
				"X-Authentik-Groups":       "admin|viewer",
				"X-SnatchArr-Proxy-Secret": testSecret,
			},
			wantNext:     true,
			wantOnErr:    false,
			wantStatus:   http.StatusOK,
			wantUser:     "alice",
			wantGroups:   []string{"admin", "viewer"},
			wantStripped: true,
		},
		{
			name:       "negative trusted peer and missing secret",
			remoteAddr: "10.1.2.3:12345",
			headers: map[string]string{
				"Remote-User":        "alice",
				"X-Authentik-Groups": "admin",
			},
			wantNext:   false,
			wantOnErr:  true,
			wantErr:    sso.ErrProxySecret,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "negative trusted peer and wrong secret same length",
			remoteAddr: "10.1.2.3:12345",
			headers: map[string]string{
				"Remote-User":              "alice",
				"X-Authentik-Groups":       "admin",
				"X-SnatchArr-Proxy-Secret": strings.Repeat("w", 32),
			},
			wantNext:   false,
			wantOnErr:  true,
			wantErr:    sso.ErrProxySecret,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "negative wrong secret shorter",
			remoteAddr: "10.1.2.3:12345",
			headers: map[string]string{
				"Remote-User":              "alice",
				"X-Authentik-Groups":       "admin",
				"X-SnatchArr-Proxy-Secret": "short",
			},
			wantNext:   false,
			wantOnErr:  true,
			wantErr:    sso.ErrProxySecret,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "negative wrong secret longer",
			remoteAddr: "10.1.2.3:12345",
			headers: map[string]string{
				"Remote-User":              "alice",
				"X-Authentik-Groups":       "admin",
				"X-SnatchArr-Proxy-Secret": strings.Repeat("s", 64),
			},
			wantNext:   false,
			wantOnErr:  true,
			wantErr:    sso.ErrProxySecret,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "negative empty header value",
			remoteAddr: "10.1.2.3:12345",
			headers: map[string]string{
				"Remote-User":              "alice",
				"X-Authentik-Groups":       "admin",
				"X-SnatchArr-Proxy-Secret": "",
			},
			wantNext:   false,
			wantOnErr:  true,
			wantErr:    sso.ErrProxySecret,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "boundary header name sent in lowercase with correct value",
			remoteAddr: "10.1.2.3:12345",
			headers: map[string]string{
				"Remote-User":              "alice",
				"X-Authentik-Groups":       "admin",
				"x-snatcharr-proxy-secret": testSecret,
			},
			wantNext:     true,
			wantOnErr:    false,
			wantStatus:   http.StatusOK,
			wantUser:     "alice",
			wantGroups:   []string{"admin"},
			wantStripped: true,
		},
		{
			name:       "positive untrusted peer and correct secret",
			remoteAddr: "192.168.1.100:12345",
			headers: map[string]string{
				"Remote-User":              "alice",
				"X-Authentik-Groups":       "admin",
				"X-SnatchArr-Proxy-Secret": testSecret,
			},
			wantNext:     true,
			wantOnErr:    false,
			wantStatus:   http.StatusOK,
			wantUser:     "",
			wantGroups:   nil,
			wantStripped: true,
		},
		{
			name:         "positive no proxy headers",
			remoteAddr:   "10.1.2.3:12345",
			headers:      map[string]string{},
			wantNext:     true,
			wantOnErr:    false,
			wantStatus:   http.StatusOK,
			wantUser:     "",
			wantGroups:   nil,
			wantStripped: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := baseCfg
			if tc.cfgMod != nil {
				tc.cfgMod(&cfg)
			}
			logger := slog.New(slog.DiscardHandler)

			var onErrCalled bool
			var onErrErr error
			onErr := func(_ context.Context, w http.ResponseWriter, _ *http.Request, err error) {
				onErrCalled = true
				onErrErr = err
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(http.StatusUnauthorized)
			}

			var nextCalled bool
			var gotUser string
			var gotGroups []string
			var downstreamSecretHeader string

			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
				if u, ok := sso.ProxyUserFromContext(r.Context()); ok {
					gotUser = u
				}
				if g, ok := sso.ProxyGroupsFromContext(r.Context()); ok {
					gotGroups = g
				}
				downstreamSecretHeader = r.Header.Get("X-Snatcharr-Proxy-Secret")
				w.WriteHeader(http.StatusOK)
			})

			mw := sso.ProxyHeaderMiddleware(cfg, logger, onErr)
			handler := mw(next)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.RemoteAddr = tc.remoteAddr
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if nextCalled != tc.wantNext {
				t.Fatalf("nextCalled = %v, want %v", nextCalled, tc.wantNext)
			}
			if onErrCalled != tc.wantOnErr {
				t.Fatalf("onErrCalled = %v, want %v", onErrCalled, tc.wantOnErr)
			}
			if tc.wantErr != nil && !errors.Is(onErrErr, tc.wantErr) {
				t.Fatalf("onErr error = %v, want %v", onErrErr, tc.wantErr)
			}
			if rec.Code != tc.wantStatus {
				t.Fatalf("HTTP status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if gotUser != tc.wantUser {
				t.Fatalf("user = %q, want %q", gotUser, tc.wantUser)
			}
			if !slices.Equal(gotGroups, tc.wantGroups) {
				t.Fatalf("groups = %v, want %v", gotGroups, tc.wantGroups)
			}
			if tc.wantStripped && downstreamSecretHeader != "" {
				t.Fatalf("downstream secret header was not stripped: %q", downstreamSecretHeader)
			}
		})
	}
}
