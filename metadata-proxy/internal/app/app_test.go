// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package app_test

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/fx"

	"github.com/golusoris/golusoris/httpx/server"
	"github.com/golusoris/golusoris/k8s/health"

	"github.com/lusoris/SnatchArr/metadata-proxy/internal/app"
	"github.com/lusoris/SnatchArr/metadata-proxy/internal/config"
	"github.com/lusoris/SnatchArr/metadata-proxy/internal/proxy"
)

const maxFunctionLines = 60

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

func startTestAdminApp(t *testing.T) string {
	t.Helper()
	adminPort := reservePort(t)
	cfgFile := filepath.Join(t.TempDir(), "config.yaml")
	yamlBody := "metadata_proxy:\n  mode: log\n  listeners:\n    - app: sonarr\n      port: 18081\n  allow_hosts:\n    - skyhook.sonarr.tv\n"
	if err := os.WriteFile(cfgFile, []byte(yamlBody), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	adminAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(adminPort))
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

	startCtx, startCancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer startCancel()
	if err := fxApp.Start(startCtx); err != nil {
		t.Fatalf("fxApp.Start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = fxApp.Stop(stopCtx)
	})
	return adminAddr
}

func TestAppLifecycleAndAdminProbes(t *testing.T) {
	t.Parallel()

	adminAddr := startTestAdminApp(t)
	client := &http.Client{Timeout: 5 * time.Second}
	for _, path := range []string{"/livez", "/readyz", "/startupz", "/metrics"} {
		wantBody := ""
		if path == "/metrics" {
			wantBody = "app_check_status"
		}
		assertAdminProbeOK(t, client, adminAddr, path, wantBody)
	}
}

func assertAdminProbeOK(t *testing.T, client *http.Client, adminAddr, path, wantBody string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
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
	if wantBody != "" && !strings.Contains(string(body), wantBody) {
		t.Fatalf("GET %s missing %q:\n%s", path, wantBody, string(body))
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

type appExplicitFileCase struct {
	name        string
	unsetEnv    bool
	mutateAfter func(t *testing.T, path string)
	wantMode    config.Mode
	wantErr     error
	wantStats   int
}

func TestAppExplicitConfigFileOnStart(t *testing.T) {
	cases := []appExplicitFileCase{
		{name: "positive: explicit file present and unchanged across load", wantMode: config.ModeEnforce, wantStats: 2},
		{
			name: "negative: explicit file removed after decorate stat",
			mutateAfter: func(t *testing.T, path string) {
				t.Helper()
				if err := os.Remove(path); err != nil {
					t.Errorf("remove %s: %v", path, err)
				}
			},
			wantErr:   config.ErrConfigFileChanged,
			wantStats: 2,
		},
		{
			name: "negative: explicit file replaced with new inode between stats",
			mutateAfter: func(t *testing.T, path string) {
				t.Helper()
				rep := filepath.Join(filepath.Dir(path), "rep.yaml")
				if err := os.WriteFile(rep, []byte("metadata_proxy:\n  mode: log\n"), 0o600); err != nil {
					t.Errorf("write %s: %v", rep, err)
					return
				}
				if err := os.Rename(rep, path); err != nil {
					t.Errorf("rename %s -> %s: %v", rep, path, err)
				}
			},
			wantErr:   config.ErrConfigFileChanged,
			wantStats: 2,
		},
		{name: "boundary: ConfigFileEnv unset skips stat and keeps default optional", unsetEnv: true, wantMode: config.ModeLog, wantStats: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files, explicitFile, envVal := prepareAppExplicitFiles(t, tc)
			t.Setenv(config.ConfigFileEnv, envVal)
			if tc.unsetEnv {
				t.Setenv("APP_METADATA_PROXY_LISTENERS", "sonarr:18081")
				t.Setenv("APP_METADATA_PROXY_ALLOW_HOSTS", "skyhook.sonarr.tv")
			}
			runAppExplicitFileCase(t, tc, files, explicitFile)
		})
	}
}

func prepareAppExplicitFiles(t *testing.T, tc appExplicitFileCase) ([]string, string, string) {
	t.Helper()
	dir := t.TempDir()
	if tc.unsetEnv {
		return []string{filepath.Join(dir, "missing-default.yaml")}, "", ""
	}
	defaultFile := filepath.Join(dir, "default.yaml")
	defaultYAML := "metadata_proxy:\n  mode: log\n  listeners:\n    - app: sonarr\n      port: 18081\n  allow_hosts:\n    - skyhook.sonarr.tv\n"
	if err := os.WriteFile(defaultFile, []byte(defaultYAML), 0o600); err != nil {
		t.Fatalf("write default config: %v", err)
	}
	explicitFile := filepath.Join(dir, "explicit.yaml")
	if err := os.WriteFile(explicitFile, []byte("metadata_proxy:\n  mode: enforce\n"), 0o600); err != nil {
		t.Fatalf("write explicit config: %v", err)
	}
	return []string{defaultFile}, explicitFile, explicitFile
}

func runAppExplicitFileCase(t *testing.T, tc appExplicitFileCase, files []string, explicitFile string) {
	t.Helper()
	var statCalls atomic.Int32
	var compiled *config.Compiled
	fxApp := fx.New(
		fx.NopLogger,
		app.Module,
		fx.Supply(&config.CoreOptionsOverride{
			Files:        files,
			DisableWatch: true,
			StatFunc: func(path string) (os.FileInfo, error) {
				call := statCalls.Add(1)
				info, err := os.Stat(path)
				if err == nil && call == 1 && tc.mutateAfter != nil {
					tc.mutateAfter(t, path)
				}
				return info, err
			},
		}),
		fx.Populate(&compiled),
	)
	err := fxApp.Err()
	if tc.wantErr != nil {
		if !errors.Is(err, tc.wantErr) || !strings.Contains(err.Error(), explicitFile) {
			t.Fatalf("fxApp.Err() = %v, want %v mentioning %q", err, tc.wantErr, explicitFile)
		}
		if got := int(statCalls.Load()); got != tc.wantStats {
			t.Fatalf("stat calls = %d, want %d", got, tc.wantStats)
		}
		return
	}
	if err != nil {
		t.Fatalf("fxApp.Err() unexpected error: %v", err)
	}
	if got := int(statCalls.Load()); got != tc.wantStats {
		t.Fatalf("stat calls = %d, want %d", got, tc.wantStats)
	}
	if compiled.Options.Mode != tc.wantMode {
		t.Fatalf("Mode = %q, want %q", compiled.Options.Mode, tc.wantMode)
	}
}

func checkAppTestSource(filename string, src any) error {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		return fmt.Errorf("parse %s: %w", filename, err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fn.Name.Name == "itoa" {
			return errors.New("custom itoa helper declared; use strconv.Itoa")
		}
		lines := fset.Position(fn.End()).Line - fset.Position(fn.Pos()).Line + 1
		if lines > maxFunctionLines {
			return fmt.Errorf("function %s spans %d lines, max %d", fn.Name.Name, lines, maxFunctionLines)
		}
	}
	return nil
}

func TestAppTestFileConventions(t *testing.T) {
	t.Parallel()

	sixtyLineBody := "package p\nfunc f() {\n" + strings.Repeat("\t_ = 1\n", maxFunctionLines-2) + "}\n"
	sixtyOneLineBody := "package p\nfunc f() {\n" + strings.Repeat("\t_ = 1\n", maxFunctionLines-1) + "}\n"

	cases := []struct {
		name    string
		file    string
		src     any
		wantErr bool
	}{
		{name: "positive: app_test.go obeys function length and stdlib port formatting", file: "app_test.go"},
		{name: "negative: function exceeding 60 lines rejected", file: "long.go", src: sixtyOneLineBody, wantErr: true},
		{name: "negative: custom itoa helper rejected", file: "itoa.go", src: "package p\nfunc itoa(n int) string { return \"\" }\n", wantErr: true},
		{name: "boundary: function of exactly 60 lines accepted", file: "edge.go", src: sixtyLineBody},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := checkAppTestSource(tc.file, tc.src)
			if tc.wantErr && err == nil {
				t.Fatalf("checkAppTestSource(%s) = nil, want error", tc.file)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("checkAppTestSource(%s) = %v, want nil", tc.file, err)
			}
		})
	}
}
