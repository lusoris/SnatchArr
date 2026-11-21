// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package app_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/fx"

	"github.com/golusoris/golusoris/httpx/server"
	"github.com/golusoris/golusoris/k8s/health"

	"github.com/lusoris/SnatchArr/metadata-proxy/internal/app"
	"github.com/lusoris/SnatchArr/metadata-proxy/internal/config"
	"github.com/lusoris/SnatchArr/metadata-proxy/internal/proxy"
)

func reservePort(t *testing.T) int {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reservePort: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

func TestModuleGraphValidates(t *testing.T) {
	t.Parallel()
	if err := fx.ValidateApp(fx.Supply(app.BuildInfo{Version: "test"}), app.Module); err != nil {
		t.Fatalf("fx.ValidateApp: %v", err)
	}
}

func TestAppLifecycleAndAdminProbes(t *testing.T) {
	t.Parallel()

	adminPort := reservePort(t)
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	yamlBody := `metadata_proxy:
  mode: log
  listeners:
    - app: sonarr
      port: 18081
  allow_hosts:
    - skyhook.sonarr.tv
`
	if err := os.WriteFile(cfgFile, []byte(yamlBody), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	adminAddr := net.JoinHostPort("127.0.0.1", strings.TrimSpace(http.CanonicalHeaderKey(""))+itoa(adminPort))
	fxApp := fx.New(
		fx.NopLogger,
		fx.Supply(app.BuildInfo{Version: "test", Commit: "abc1234", Date: "2026-11-15"}),
		fx.Supply([]proxy.Option{proxy.WithBindHost("127.0.0.1"), proxy.WithEphemeralPorts()}),
		app.Module,
		fx.Supply(&config.CoreOptionsOverride{
			Files:        []string{cfgFile},
			DisableEnv:   true,
			DisableWatch: true,
		}),
		fx.Decorate(func(o server.Options) server.Options {
			o.Addr = adminAddr
			return o
		}),
		fx.Decorate(func(o health.Options) health.Options {
			o.Drain.Delay = 0
			return o
		}),
	)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := fxApp.Start(ctx); err != nil {
		t.Fatalf("fxApp.Start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = fxApp.Stop(stopCtx)
	})

	client := &http.Client{Timeout: 5 * time.Second}
	for _, path := range []string{"/livez", "/readyz", "/startupz", "/metrics"} {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+adminAddr+path, nil)
		if err != nil {
			t.Fatalf("new request %s: %v", path, err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET %s status = %d, body = %s", path, resp.StatusCode, string(body))
		}
		if path == "/metrics" && !strings.Contains(string(body), "app_check_status") {
			t.Fatalf("/metrics missing status gauges:\n%s", string(body))
		}
	}
}

func TestAppRefusesInvalidConfigOnStart(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(cfgFile, []byte("metadata_proxy:\n  mode: enforce\n  listeners:\n    - app: sonarr\n      port: 18081\n"), 0o600); err != nil {
		t.Fatalf("write bad config: %v", err)
	}

	fxApp := fx.New(
		fx.NopLogger,
		app.Module,
		fx.Supply(&config.CoreOptionsOverride{
			Files:        []string{cfgFile},
			DisableEnv:   true,
			DisableWatch: true,
		}),
	)
	if err := fxApp.Err(); err == nil || !strings.Contains(err.Error(), config.ErrEmptyAllowHosts.Error()) {
		t.Fatalf("fxApp.Err() = %v, want %v", err, config.ErrEmptyAllowHosts)
	}
}

func itoa(n int) string {
	var buf [20]byte
	i := len(buf)
	for n >= 10 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	i--
	buf[i] = byte('0' + n)
	return string(buf[i:])
}
