// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package config_test

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	gconfig "github.com/golusoris/golusoris/core/config"

	"github.com/lusoris/SnatchArr/metadata-proxy/internal/config"
)

const (
	maxFuncLines  = 60
	testEnvPrefix = "SNATCHARR_MP_TEST_"
)

type compileCase struct {
	name    string
	mutate  func(*config.Options)
	wantErr error
}

func validMinimalOptions() config.Options {
	o := config.DefaultOptions()
	o.Listeners = []config.Listener{{App: "sonarr", Port: 8081}}
	return o
}

func runCompileCases(t *testing.T, cases []compileCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := validMinimalOptions()
			tc.mutate(&opts)
			_, err := config.Compile(opts)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("Compile() unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Compile() error = %v, want errors.Is(_, %v)", err, tc.wantErr)
			}
		})
	}
}

func TestCompileListenerAndPortValidation(t *testing.T) {
	t.Parallel()

	runCompileCases(t, []compileCase{
		{name: "positive: minimal log-mode config", mutate: func(*config.Options) {}},
		{
			name: "boundary: listener ports 1 and 65535 plus multiple target ports",
			mutate: func(o *config.Options) {
				o.Listeners = []config.Listener{{App: "sonarr", Port: 1}, {App: "radarr", Port: 65535}}
				o.Ports = []int{443, 8443}
			},
		},
		{name: "negative: no listener", mutate: func(o *config.Options) { o.Listeners = nil }, wantErr: config.ErrNoListeners},
		{name: "negative: empty listener app", mutate: func(o *config.Options) { o.Listeners = []config.Listener{{App: "", Port: 8081}} }, wantErr: config.ErrBadAppName},
		{
			name: "negative: duplicate listener app",
			mutate: func(o *config.Options) {
				o.Listeners = []config.Listener{{App: "sonarr", Port: 8081}, {App: "sonarr", Port: 8082}}
			},
			wantErr: config.ErrDuplicateApp,
		},
		{
			name: "negative: duplicate listener port",
			mutate: func(o *config.Options) {
				o.Listeners = []config.Listener{{App: "sonarr", Port: 8081}, {App: "radarr", Port: 8081}}
			},
			wantErr: config.ErrDuplicatePort,
		},
		{name: "boundary: listener port 0", mutate: func(o *config.Options) { o.Listeners = []config.Listener{{App: "sonarr", Port: 0}} }, wantErr: config.ErrBadPort},
		{name: "boundary: listener port 65536", mutate: func(o *config.Options) { o.Listeners = []config.Listener{{App: "sonarr", Port: 65536}} }, wantErr: config.ErrBadPort},
		{name: "boundary: target port 0", mutate: func(o *config.Options) { o.Ports = []int{0} }, wantErr: config.ErrBadPort},
		{name: "boundary: target port 65536", mutate: func(o *config.Options) { o.Ports = []int{65536} }, wantErr: config.ErrBadPort},
		{name: "negative: empty target ports", mutate: func(o *config.Options) { o.Ports = []int{} }, wantErr: config.ErrBadPort},
	})
}

func TestCompileHostAndPrefixValidation(t *testing.T) {
	t.Parallel()

	runCompileCases(t, []compileCase{
		{
			name: "positive: enforce mode with exact and suffix hosts plus CIDRs",
			mutate: func(o *config.Options) {
				o.Mode = config.ModeEnforce
				o.AllowHosts = []string{"SkyHook.Sonarr.TV.", ".radarr.video."}
				o.RefuseHosts = []string{"indexer.example", ".tracker.example"}
				o.DeniedPrefixes = []string{"10.96.0.0/12", "fd00::/8", "::ffff:100.128.0.0/112"}
			},
		},
		{name: "negative: unknown mode", mutate: func(o *config.Options) { o.Mode = "audit" }, wantErr: config.ErrUnknownMode},
		{name: "negative: enforce mode with empty allow_hosts", mutate: func(o *config.Options) { o.Mode = config.ModeEnforce; o.AllowHosts = nil }, wantErr: config.ErrEmptyAllowHosts},
		{name: "negative: bad allow_hosts wildcard pattern", mutate: func(o *config.Options) { o.AllowHosts = []string{"*.sonarr.tv"} }, wantErr: config.ErrBadHostPattern},
		{name: "negative: bad allow_hosts URL with scheme", mutate: func(o *config.Options) { o.AllowHosts = []string{"https://skyhook.sonarr.tv"} }, wantErr: config.ErrBadHostPattern},
		{name: "negative: bad refuse_hosts double dot", mutate: func(o *config.Options) { o.RefuseHosts = []string{"bad..example.com"} }, wantErr: config.ErrBadHostPattern},
		{name: "negative: bad refuse_hosts IP literal", mutate: func(o *config.Options) { o.RefuseHosts = []string{"127.0.0.1"} }, wantErr: config.ErrBadHostPattern},
		{name: "negative: bad CIDR in denied_prefixes", mutate: func(o *config.Options) { o.DeniedPrefixes = []string{"10.96.0.0/99"} }, wantErr: config.ErrBadCIDR},
		{name: "boundary: IPv4-mapped IPv6 prefix shorter than /96 rejected", mutate: func(o *config.Options) { o.DeniedPrefixes = []string{"::ffff:100.128.0.0/95"} }, wantErr: config.ErrBadCIDR},
		{name: "negative: IPv4-mapped IPv6 prefix with IPv4 mask length rejected", mutate: func(o *config.Options) { o.DeniedPrefixes = []string{"::ffff:10.96.0.0/12"} }, wantErr: config.ErrBadCIDR},
	})
}

func TestCompileTimeoutAndBoundValidation(t *testing.T) {
	t.Parallel()

	runCompileCases(t, []compileCase{
		{
			name:    "boundary: max_tunnels 1",
			mutate:  func(o *config.Options) { o.MaxTunnels = 1 },
			wantErr: nil,
		},
		{
			name:    "boundary: zero dial_timeout",
			mutate:  func(o *config.Options) { o.DialTimeout = 0 },
			wantErr: config.ErrNonPositiveTimeout,
		},
		{
			name:    "negative: negative idle_timeout",
			mutate:  func(o *config.Options) { o.IdleTimeout = -time.Second },
			wantErr: config.ErrNonPositiveTimeout,
		},
		{
			name:    "boundary: zero max_tunnel",
			mutate:  func(o *config.Options) { o.MaxTunnel = 0 },
			wantErr: config.ErrNonPositiveTimeout,
		},
		{
			name:    "boundary: zero max_tunnels",
			mutate:  func(o *config.Options) { o.MaxTunnels = 0 },
			wantErr: config.ErrNonPositiveBound,
		},
		{
			name:    "negative: negative max_tunnels",
			mutate:  func(o *config.Options) { o.MaxTunnels = -5 },
			wantErr: config.ErrNonPositiveBound,
		},
	})
}

func TestCompileDeniedPrefixesIPv4Mapped(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		raw    []string
		wantV4 []netip.Prefix
		wantV6 []netip.Prefix
	}{
		{
			name:   "positive: mapped /112 normalized to IPv4 /16 while ordinary IPv6 stays in DeniedV6",
			raw:    []string{"::ffff:100.128.0.7/112", "2c0f:fff0::/32"},
			wantV4: []netip.Prefix{netip.MustParsePrefix("100.128.0.0/16")},
			wantV6: []netip.Prefix{netip.MustParsePrefix("2c0f:fff0::/32")},
		},
		{
			name:   "boundary: mapped /96 and /128 normalize to /0 and /32",
			raw:    []string{"::ffff:0:0/96", "::ffff:198.51.100.9/128"},
			wantV4: []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("198.51.100.9/32")},
			wantV6: []netip.Prefix{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := validMinimalOptions()
			opts.DeniedPrefixes = tc.raw
			compiled, err := config.Compile(opts)
			if err != nil {
				t.Fatalf("Compile(%v) unexpected error: %v", tc.raw, err)
			}
			if !slices.Equal(compiled.DeniedV4, tc.wantV4) {
				t.Fatalf("DeniedV4 = %v, want %v", compiled.DeniedV4, tc.wantV4)
			}
			if !slices.Equal(compiled.DeniedV6, tc.wantV6) {
				t.Fatalf("DeniedV6 = %v, want %v", compiled.DeniedV6, tc.wantV6)
			}
		})
	}
}

func TestHostMatcher(t *testing.T) {
	t.Parallel()

	m, err := config.NewHostMatcher([]string{"SkyHook.Sonarr.TV.", ".Radarr.Video."})
	if err != nil {
		t.Fatalf("NewHostMatcher() error: %v", err)
	}

	cases := []struct {
		name      string
		host      string
		wantMatch string
		wantOK    bool
	}{
		{"positive: exact lowercase", "skyhook.sonarr.tv", "skyhook.sonarr.tv", true},
		{"positive: exact mixed-case trailing dot", "SKYHOOK.SONARR.TV.", "skyhook.sonarr.tv", true},
		{"negative: subdomain of exact pattern", "sub.skyhook.sonarr.tv", "", false},
		{"positive: suffix subdomain", "api.radarr.video", ".radarr.video", true},
		{"positive: suffix deep subdomain trailing dot", "img.api.RADARR.video.", ".radarr.video", true},
		{"boundary: apex does not match leading-dot suffix", "radarr.video", "", false},
		{"negative: lookalike suffix without dot boundary", "fakerradarr.video", "", false},
		{"boundary: empty host", "", "", false},
		{"negative: double trailing dot", "skyhook.sonarr.tv..", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := m.Match(tc.host)
			if ok != tc.wantOK || got != tc.wantMatch {
				t.Fatalf("Match(%q) = (%q, %v), want (%q, %v)", tc.host, got, ok, tc.wantMatch, tc.wantOK)
			}
		})
	}
}

func TestLoadExampleYAML(t *testing.T) {
	t.Parallel()

	examplePath := filepath.Join("..", "..", "config.example.yaml")
	coreCfg, err := gconfig.New(gconfig.Options{
		Files:        []string{examplePath},
		CompoundKeys: config.CompoundKeys(),
		DisableEnv:   true,
	})
	if err != nil {
		t.Fatalf("gconfig.New(%s): %v", examplePath, err)
	}
	compiled, err := config.Load(coreCfg)
	if err != nil {
		t.Fatalf("config.Load(%s): %v", examplePath, err)
	}
	if compiled.Options.Mode != config.ModeLog {
		t.Fatalf("Mode = %q, want %q", compiled.Options.Mode, config.ModeLog)
	}
	if len(compiled.Apps) == 0 {
		t.Fatal("expected non-empty listeners in config.example.yaml")
	}
	if !compiled.AllowMatcher.Matches("skyhook.sonarr.tv") {
		t.Fatal("expected skyhook.sonarr.tv in allow_hosts")
	}
}

func TestLoadFromYAMLAndEnvOverride(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "proxy.yaml")
	yamlBody := `metadata_proxy:
  mode: log
  listeners:
    - app: sonarr
      port: 18081
  allow_hosts:
    - skyhook.sonarr.tv
`
	if err := os.WriteFile(cfgFile, []byte(yamlBody), 0o600); err != nil {
		t.Fatalf("write temp yaml: %v", err)
	}

	// Simulate an ambient empty APP_METADATA_PROXY_ALLOW_HOSTS in the caller's shell.
	t.Setenv("APP_METADATA_PROXY_ALLOW_HOSTS", "")
	t.Setenv(config.ConfigFileEnv, cfgFile)
	t.Setenv(testEnvPrefix+"METADATA_PROXY_MODE", "enforce")
	t.Setenv(testEnvPrefix+"METADATA_PROXY_MAX_TUNNELS", "12")

	opts, err := config.DecorateCoreOptions(gconfig.Options{EnvPrefix: testEnvPrefix, Delimiter: "."})
	if err != nil {
		t.Fatalf("DecorateCoreOptions: %v", err)
	}
	coreCfg, err := gconfig.New(opts)
	if err != nil {
		t.Fatalf("gconfig.New: %v", err)
	}
	compiled, err := config.Load(coreCfg)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if compiled.Options.Mode != config.ModeEnforce {
		t.Fatalf("Mode = %q, want enforce", compiled.Options.Mode)
	}
	if compiled.Options.MaxTunnels != 12 {
		t.Fatalf("MaxTunnels = %d, want 12", compiled.Options.MaxTunnels)
	}
}

func TestLoadPortsFromEnv(t *testing.T) {
	cases := []struct {
		name      string
		envPorts  string
		setEnv    bool
		wantPorts []uint16
		wantErr   error
	}{
		{name: "positive: unset uses default 443", setEnv: false, wantPorts: []uint16{443}},
		{name: "positive: single port in env", envPorts: "8443", setEnv: true, wantPorts: []uint16{8443}},
		{name: "positive: multiple comma-separated ports in env", envPorts: "443, 8443", setEnv: true, wantPorts: []uint16{443, 8443}},
		{name: "boundary: min and max ports in env", envPorts: "1,65535", setEnv: true, wantPorts: []uint16{1, 65535}},
		{name: "negative: empty env ports", envPorts: "", setEnv: true, wantErr: config.ErrBadPort},
		{name: "negative: trailing comma in env ports", envPorts: "443,", setEnv: true, wantErr: config.ErrBadPort},
		{name: "negative: non-integer element in env ports", envPorts: "443,https", setEnv: true, wantErr: config.ErrBadPort},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfgFile := filepath.Join(t.TempDir(), "proxy.yaml")
			yamlBody := "metadata_proxy:\n  listeners:\n    - app: sonarr\n      port: 18081\n"
			if err := os.WriteFile(cfgFile, []byte(yamlBody), 0o600); err != nil {
				t.Fatalf("write temp yaml: %v", err)
			}
			t.Setenv(config.ConfigFileEnv, cfgFile)
			if tc.setEnv {
				t.Setenv(testEnvPrefix+"METADATA_PROXY_PORTS", tc.envPorts)
			}
			compiled, err := loadFromDecoratedEnv(t)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("Load() error = %v, want errors.Is(_, %v)", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}
			if !slices.Equal(compiled.PortList, tc.wantPorts) {
				t.Fatalf("PortList = %v, want %v", compiled.PortList, tc.wantPorts)
			}
		})
	}
}

func loadFromDecoratedEnv(t *testing.T) (*config.Compiled, error) {
	t.Helper()
	opts, err := config.DecorateCoreOptions(gconfig.Options{EnvPrefix: testEnvPrefix, Delimiter: "."})
	if err != nil {
		t.Fatalf("DecorateCoreOptions: %v", err)
	}
	coreCfg, err := gconfig.New(opts)
	if err != nil {
		t.Fatalf("gconfig.New: %v", err)
	}
	return config.Load(coreCfg)
}

func TestLoadRejectsFractionalAndMalformedPorts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		ext     string
		body    string
		wantErr error
	}{
		{
			name: "positive: integer listener and target ports in YAML",
			ext:  ".yaml",
			body: "metadata_proxy:\n  listeners:\n    - app: sonarr\n      port: 18081\n  ports: [443, 8443]\n",
		},
		{
			name: "positive: integer listener and target ports in JSON",
			ext:  ".json",
			body: `{"metadata_proxy":{"listeners":[{"app":"sonarr","port":18081}],"ports":[443,8443]}}`,
		},
		{
			name:    "negative: fractional listener port in YAML",
			ext:     ".yaml",
			body:    "metadata_proxy:\n  listeners:\n    - app: sonarr\n      port: 18081.5\n",
			wantErr: config.ErrBadPort,
		},
		{
			name:    "negative: fractional target port in YAML",
			ext:     ".yaml",
			body:    "metadata_proxy:\n  listeners:\n    - app: sonarr\n      port: 18081\n  ports: [443.5]\n",
			wantErr: config.ErrBadPort,
		},
		{
			name:    "negative: boolean target port in YAML",
			ext:     ".yaml",
			body:    "metadata_proxy:\n  listeners:\n    - app: sonarr\n      port: 18081\n  ports: [true]\n",
			wantErr: config.ErrBadPort,
		},
		{
			name:    "boundary: missing listener port key in YAML",
			ext:     ".yaml",
			body:    "metadata_proxy:\n  listeners:\n    - app: sonarr\n",
			wantErr: config.ErrBadPort,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := loadConfigSnippet(t, tc.ext, tc.body)
			if tc.wantErr == nil && err != nil {
				t.Fatalf("loadConfigSnippet unexpected error: %v", err)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("loadConfigSnippet error = %v, want errors.Is(_, %v)", err, tc.wantErr)
			}
		})
	}
}

func loadConfigSnippet(t *testing.T, ext, body string) error {
	t.Helper()
	cfgFile := filepath.Join(t.TempDir(), "cfg"+ext)
	if err := os.WriteFile(cfgFile, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", cfgFile, err)
	}
	coreCfg, err := gconfig.New(gconfig.Options{
		Files:        []string{cfgFile},
		CompoundKeys: config.CompoundKeys(),
		DisableEnv:   true,
	})
	if err != nil {
		return err
	}
	_, err = config.Load(coreCfg)
	return err
}

func TestDecorateCoreOptionsBoundedStat(t *testing.T) {
	existing := filepath.Join(t.TempDir(), "present.yaml")
	if err := os.WriteFile(existing, []byte("metadata_proxy: {}\n"), 0o600); err != nil {
		t.Fatalf("write present.yaml: %v", err)
	}
	missing := filepath.Join(t.TempDir(), "does-not-exist.yaml")

	t.Run("positive: existing explicit file succeeds within deadline", func(t *testing.T) {
		t.Setenv(config.ConfigFileEnv, existing)
		got, err := config.DecorateCoreOptions(gconfig.Options{EnvPrefix: testEnvPrefix})
		if err != nil {
			t.Fatalf("DecorateCoreOptions: %v", err)
		}
		if !slices.Contains(got.Files, existing) {
			t.Fatalf("Files = %v, want %q", got.Files, existing)
		}
	})

	t.Run("negative: missing explicit file returns ErrNotExist", func(t *testing.T) {
		t.Setenv(config.ConfigFileEnv, missing)
		if _, err := config.DecorateCoreOptions(gconfig.Options{EnvPrefix: testEnvPrefix}); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("DecorateCoreOptions error = %v, want errors.Is(_, os.ErrNotExist)", err)
		}
	})

	t.Run("negative: stalled explicit file stat times out", func(t *testing.T) {
		t.Setenv(config.ConfigFileEnv, existing)
		assertStalledStatTimesOut(t)
	})

	t.Run("boundary: expired context aborts stat immediately", func(t *testing.T) {
		t.Setenv(config.ConfigFileEnv, existing)
		ctx, cancel := context.WithDeadline(t.Context(), time.Unix(0, 0))
		defer cancel()
		if _, err := config.DecorateCoreOptionsContext(ctx, gconfig.Options{EnvPrefix: testEnvPrefix}); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("DecorateCoreOptionsContext error = %v, want context.DeadlineExceeded", err)
		}
	})
}

func assertStalledStatTimesOut(t *testing.T) {
	t.Helper()
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	_, err := config.DecorateCoreOptionsFX(config.CoreDecorateParams{
		Options: gconfig.Options{EnvPrefix: testEnvPrefix},
		Override: &config.CoreOptionsOverride{
			StatTimeout: 20 * time.Millisecond,
			StatFunc: func(path string) (os.FileInfo, error) {
				timer := time.NewTimer(250 * time.Millisecond)
				defer timer.Stop()
				select {
				case <-release:
				case <-timer.C:
				}
				return os.Stat(path)
			},
		},
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("DecorateCoreOptionsFX error = %v, want errors.Is(_, context.DeadlineExceeded)", err)
	}
}

func TestPackageFunctionsRespectMaxLines(t *testing.T) {
	t.Parallel()

	shortSrc := "package p\nfunc ok() {\n" + strings.Repeat("\t_ = 1\n", maxFuncLines-2) + "}\n"
	longSrc := "package p\nfunc tooLong() {\n" + strings.Repeat("\t_ = 1\n", maxFuncLines-1) + "}\n"
	if bad := findOversizedFuncs(t, "short.go", shortSrc); len(bad) != 0 {
		t.Fatalf("boundary %d-line func flagged: %v", maxFuncLines, bad)
	}
	if bad := findOversizedFuncs(t, "long.go", longSrc); len(bad) != 1 {
		t.Fatalf("boundary %d-line func violations = %v, want 1", maxFuncLines+1, bad)
	}

	for _, file := range []string{"config.go", "hostmatch.go", "config_test.go"} {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if bad := findOversizedFuncs(t, file, string(src)); len(bad) != 0 {
			t.Fatalf("%s exceeds %d-line HISS-04 function cap: %v", file, maxFuncLines, bad)
		}
	}
}

func findOversizedFuncs(t *testing.T, filename, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}
	var violations []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		lines := fset.Position(fn.End()).Line - fset.Position(fn.Pos()).Line + 1
		if lines > maxFuncLines {
			violations = append(violations, fmt.Sprintf("%s:%d lines", fn.Name.Name, lines))
		}
	}
	return violations
}
