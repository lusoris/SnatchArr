// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"go.uber.org/fx"

	gconfig "github.com/golusoris/golusoris/core/config"

	"github.com/lusoris/SnatchArr/api/internal/buildinfo"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

func proxyTestConfig(t *testing.T, trustedCIDRs []string) *gconfig.Config {
	t.Helper()
	cc := storetest.New(t).Pool().Config().ConnConfig
	dsn := url.URL{
		Scheme: "postgres", User: url.UserPassword(cc.User, cc.Password),
		Host: net.JoinHostPort(cc.Host, strconv.Itoa(int(cc.Port))), Path: "/" + cc.Database, RawQuery: "sslmode=disable",
	}
	cfgMap := map[string]any{
		"snatcharr": map[string]any{
			"profile": "dev",
			"worker":  map[string]any{"token": "proxy-test-token"},
			"proxy":   map[string]any{"trusted": trustedCIDRs},
		},
		"db":   map[string]any{"dsn": dsn.String()},
		"http": map[string]any{"addr": anyLoopback},
		"grpc": map[string]any{"listen": anyLoopback},
	}
	raw, err := json.Marshal(cfgMap)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "config.json")
	if err = os.WriteFile(file, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := gconfig.New(gconfig.Options{Files: []string{file}, EnvPrefix: "SNATCHARR_PROXY_TEST_UNSET_"})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

func startTestControlPlane(t *testing.T, trustedCIDRs []string) (chi.Router, *store.Store) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	cfg := proxyTestConfig(t, trustedCIDRs)
	var router chi.Router
	var st *store.Store
	app := fx.New(
		fx.Supply(buildinfo.Info{Version: "test"}),
		Module,
		fx.Decorate(func(*gconfig.Config) *gconfig.Config { return cfg }),
		fx.Populate(&router, &st),
	)
	if err := app.Start(ctx); err != nil {
		t.Fatalf("app start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
		defer stopCancel()
		_ = app.Stop(stopCtx)
	})
	return router, st
}

func TestLoginProxyTrust(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		trustedCIDRs  []string
		remoteAddr    string
		xff           string
		wantLockedKey string
		wantNotKey    string
	}{
		{
			name:          "positive peer in trusted CIDR with X-Forwarded-For uses forwarded client IP",
			trustedCIDRs:  []string{"10.0.0.0/8"},
			remoteAddr:    "10.0.0.1:1234",
			xff:           "203.0.113.195",
			wantLockedKey: "ip:203.0.113.195",
			wantNotKey:    "ip:10.0.0.1",
		},
		{
			name:          "negative untrusted peer with spoofed X-Forwarded-For ignores header",
			trustedCIDRs:  []string{"10.0.0.0/8"},
			remoteAddr:    "198.51.100.1:1234",
			xff:           "203.0.113.195",
			wantLockedKey: "ip:198.51.100.1",
			wantNotKey:    "ip:203.0.113.195",
		},
		{
			name:          "boundary empty config uses RemoteAddr",
			trustedCIDRs:  nil,
			remoteAddr:    "10.0.0.1:1234",
			xff:           "203.0.113.195",
			wantLockedKey: "ip:10.0.0.1",
			wantNotKey:    "ip:203.0.113.195",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			router, st := startTestControlPlane(t, tc.trustedCIDRs)

			body := bytes.NewBufferString(`{"username":"unknown-user","password":"wrong-password"}`)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", body)
			req.Header.Set("Content-Type", "application/json")
			req.RemoteAddr = tc.remoteAddr
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			// Failed login should record a lockout under wantLockedKey
			row, err := st.Q().GetLoginLockout(t.Context(), tc.wantLockedKey)
			if err != nil {
				t.Fatalf("expected lockout for key %q, got err: %v", tc.wantLockedKey, err)
			}
			if row.Key != tc.wantLockedKey {
				t.Fatalf("locked key = %q, want %q", row.Key, tc.wantLockedKey)
			}

			// wantNotKey must NOT have a lockout row
			_, err = st.Q().GetLoginLockout(t.Context(), tc.wantNotKey)
			if !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("expected key %q to have no lockout row, got err: %v", tc.wantNotKey, err)
			}
		})
	}
}
