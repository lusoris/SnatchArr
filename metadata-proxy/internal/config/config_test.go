// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	gconfig "github.com/golusoris/golusoris/core/config"

	"github.com/lusoris/SnatchArr/metadata-proxy/internal/config"
)

func validMinimalOptions() config.Options {
	o := config.DefaultOptions()
	o.Listeners = []config.Listener{{App: "sonarr", Port: 8081}}
	return o
}

func TestCompileValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		mutate  func(*config.Options)
		wantErr error
	}{
		{
			name:    "positive: minimal log-mode config",
			mutate:  func(*config.Options) {},
			wantErr: nil,
		},
		{
			name: "positive: enforce mode with exact and suffix hosts plus CIDRs",
			mutate: func(o *config.Options) {
				o.Mode = config.ModeEnforce
				o.Listeners = []config.Listener{{App: "sonarr", Port: 1}, {App: "radarr", Port: 65535}}
				o.AllowHosts = []string{"SkyHook.Sonarr.TV.", ".radarr.video."}
				o.RefuseHosts = []string{"indexer.example", ".tracker.example"}
				o.Ports = []int{443, 8443}
				o.DeniedPrefixes = []string{"10.96.0.0/12", "fd00::/8"}
				o.MaxTunnels = 1
			},
			wantErr: nil,
		},
		{
			name:    "negative: no listener",
			mutate:  func(o *config.Options) { o.Listeners = nil },
			wantErr: config.ErrNoListeners,
		},
		{
			name:    "negative: empty listener app",
			mutate:  func(o *config.Options) { o.Listeners = []config.Listener{{App: "", Port: 8081}} },
			wantErr: config.ErrBadAppName,
		},
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
		{
			name:    "boundary: listener port 0",
			mutate:  func(o *config.Options) { o.Listeners = []config.Listener{{App: "sonarr", Port: 0}} },
			wantErr: config.ErrBadPort,
		},
		{
			name:    "boundary: listener port 65536",
			mutate:  func(o *config.Options) { o.Listeners = []config.Listener{{App: "sonarr", Port: 65536}} },
			wantErr: config.ErrBadPort,
		},
		{
			name:    "boundary: target port 0",
			mutate:  func(o *config.Options) { o.Ports = []int{0} },
			wantErr: config.ErrBadPort,
		},
		{
			name:    "boundary: target port 65536",
			mutate:  func(o *config.Options) { o.Ports = []int{65536} },
			wantErr: config.ErrBadPort,
		},
		{
			name:    "negative: empty target ports",
			mutate:  func(o *config.Options) { o.Ports = []int{} },
			wantErr: config.ErrBadPort,
		},
		{
			name:    "negative: unknown mode",
			mutate:  func(o *config.Options) { o.Mode = "audit" },
			wantErr: config.ErrUnknownMode,
		},
		{
			name:    "negative: enforce mode with empty allow_hosts",
			mutate:  func(o *config.Options) { o.Mode = config.ModeEnforce; o.AllowHosts = nil },
			wantErr: config.ErrEmptyAllowHosts,
		},
		{
			name:    "negative: bad allow_hosts wildcard pattern",
			mutate:  func(o *config.Options) { o.AllowHosts = []string{"*.sonarr.tv"} },
			wantErr: config.ErrBadHostPattern,
		},
		{
			name:    "negative: bad allow_hosts URL with scheme",
			mutate:  func(o *config.Options) { o.AllowHosts = []string{"https://skyhook.sonarr.tv"} },
			wantErr: config.ErrBadHostPattern,
		},
		{
			name:    "negative: bad refuse_hosts double dot",
			mutate:  func(o *config.Options) { o.RefuseHosts = []string{"bad..example.com"} },
			wantErr: config.ErrBadHostPattern,
		},
		{
			name:    "negative: bad refuse_hosts IP literal",
			mutate:  func(o *config.Options) { o.RefuseHosts = []string{"127.0.0.1"} },
			wantErr: config.ErrBadHostPattern,
		},
		{
			name:    "negative: bad CIDR in denied_prefixes",
			mutate:  func(o *config.Options) { o.DeniedPrefixes = []string{"10.96.0.0/99"} },
			wantErr: config.ErrBadCIDR,
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
	}

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

	t.Setenv(config.ConfigFileEnv, cfgFile)
	t.Setenv("APP_METADATA_PROXY_MODE", "enforce")
	t.Setenv("APP_METADATA_PROXY_MAX_TUNNELS", "12")

	opts, err := config.DecorateCoreOptions(gconfig.Options{EnvPrefix: "APP_", Delimiter: "."})
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

func TestDecorateCoreOptionsMissingExplicitFile(t *testing.T) {
	t.Setenv(config.ConfigFileEnv, filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if _, err := config.DecorateCoreOptions(gconfig.Options{EnvPrefix: "APP_"}); err == nil {
		t.Fatal("expected error for missing explicit config file, got nil")
	}
}
