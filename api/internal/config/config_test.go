// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package config_test

import (
	"encoding/hex"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	gconfig "github.com/golusoris/golusoris/core/config"

	"github.com/lusoris/SnatchArr/api/internal/config"
)

func TestValidate(t *testing.T) {
	t.Parallel()
	prod := config.Default()
	prod.Worker.Token = "t"
	prod.APIKey.Secret = "s"
	tests := []struct {
		name    string
		mutate  func(*config.Options)
		wantErr error
		wantAny bool
	}{
		{"positive prod with secrets", func(*config.Options) {}, nil, false},
		{"positive dev without secrets", func(o *config.Options) {
			o.Profile = config.ProfileDev
			o.Worker.Token = ""
			o.APIKey.Secret = ""
		}, nil, false},
		{"negative prod missing worker token", func(o *config.Options) { o.Worker.Token = "" }, config.ErrMissingSecret, true},
		{"negative prod missing apikey secret", func(o *config.Options) { o.APIKey.Secret = "" }, config.ErrMissingSecret, true},
		{"negative unknown profile", func(o *config.Options) { o.Profile = "staging" }, nil, true},
		{"boundary ttl 1m ok", func(o *config.Options) { o.Session.TTL = time.Minute }, nil, false},
		{"boundary ttl 59s", func(o *config.Options) { o.Session.TTL = 59 * time.Second }, nil, true},
		{"boundary ttl 720h ok", func(o *config.Options) { o.Session.TTL = 720 * time.Hour }, nil, false},
		{"boundary ttl 721h", func(o *config.Options) { o.Session.TTL = 721 * time.Hour }, nil, true},
		{"negative secrets without config", func(o *config.Options) { o.Configarr.Secrets = "/s.yml" }, nil, true},
		{"positive configarr linked", func(o *config.Options) {
			o.Configarr.Config = "/c.yml"
			o.Configarr.Secrets = "/s.yml"
		}, nil, false},
		{"positive trusted proxy valid list", func(o *config.Options) {
			o.Proxy.Trusted = []string{"10.0.0.0/8", "172.16.0.0/12"}
		}, nil, false},
		{"negative trusted proxy malformed entry refused", func(o *config.Options) {
			o.Proxy.Trusted = []string{"10.0.0.0/8", "invalid-cidr"}
		}, nil, true},
		{"boundary trusted proxy empty trusts nothing", func(o *config.Options) {
			o.Proxy.Trusted = nil
		}, nil, false},
		{"positive trusted proxy whitespace and empty items around commas", func(o *config.Options) {
			o.Proxy.Trusted = []string{" 10.0.0.0/8 ", " ", "192.168.0.0/16"}
		}, nil, false},
		{"positive prowlarr default threshold", func(o *config.Options) {
			o.Prowlarr.LimitThreshold = 0.9
		}, nil, false},
		{"boundary prowlarr threshold 1.0", func(o *config.Options) {
			o.Prowlarr.LimitThreshold = 1.0
		}, nil, false},
		{"boundary prowlarr threshold 0.001", func(o *config.Options) {
			o.Prowlarr.LimitThreshold = 0.001
		}, nil, false},
		{"negative prowlarr threshold 0 refused", func(o *config.Options) {
			o.Prowlarr.LimitThreshold = 0.0
		}, nil, true},
		{"negative prowlarr threshold negative refused", func(o *config.Options) {
			o.Prowlarr.LimitThreshold = -0.1
		}, nil, true},
		{"negative prowlarr threshold greater than 1 refused", func(o *config.Options) {
			o.Prowlarr.LimitThreshold = 1.01
		}, nil, true},
		{"positive oidc valid prod", func(o *config.Options) {
			o.OIDC.Enabled = true
			o.OIDC.IssuerURL = "https://idp.example.com"
			o.OIDC.ClientID = "client-id"
			o.OIDC.RedirectURL = "https://snatcharr.example.com/api/v1/auth/oidc/callback"
			o.OIDC.AllowedSubjects = []string{"sub-1"}
		}, nil, false},
		{"boundary oidc dev allows http issuer", func(o *config.Options) {
			o.Profile = config.ProfileDev
			o.Worker.Token = ""
			o.APIKey.Secret = ""
			o.OIDC.Enabled = true
			o.OIDC.IssuerURL = "http://localhost:8080"
			o.OIDC.ClientID = "client-id"
			o.OIDC.RedirectURL = "http://localhost:8080/api/v1/auth/oidc/callback"
			o.OIDC.AllowedEmails = []string{"admin@example.com"}
		}, nil, false},
		{"negative oidc prod refuses http issuer", func(o *config.Options) {
			o.OIDC.Enabled = true
			o.OIDC.IssuerURL = "http://idp.example.com"
			o.OIDC.ClientID = "client-id"
			o.OIDC.RedirectURL = "https://snatcharr.example.com/api/v1/auth/oidc/callback"
			o.OIDC.AllowedSubjects = []string{"sub-1"}
		}, nil, true},
		{"negative oidc missing issuer", func(o *config.Options) {
			o.OIDC.Enabled = true
			o.OIDC.IssuerURL = ""
			o.OIDC.ClientID = "client-id"
			o.OIDC.RedirectURL = "https://snatcharr.example.com/api/v1/auth/oidc/callback"
			o.OIDC.AllowedSubjects = []string{"sub-1"}
		}, nil, true},
		{"negative oidc malformed issuer", func(o *config.Options) {
			o.OIDC.Enabled = true
			o.OIDC.IssuerURL = "://invalid"
			o.OIDC.ClientID = "client-id"
			o.OIDC.RedirectURL = "https://snatcharr.example.com/api/v1/auth/oidc/callback"
			o.OIDC.AllowedSubjects = []string{"sub-1"}
		}, nil, true},
		{"negative oidc missing client_id", func(o *config.Options) {
			o.OIDC.Enabled = true
			o.OIDC.IssuerURL = "https://idp.example.com"
			o.OIDC.ClientID = ""
			o.OIDC.RedirectURL = "https://snatcharr.example.com/api/v1/auth/oidc/callback"
			o.OIDC.AllowedSubjects = []string{"sub-1"}
		}, nil, true},
		{"negative oidc missing redirect_url", func(o *config.Options) {
			o.OIDC.Enabled = true
			o.OIDC.IssuerURL = "https://idp.example.com"
			o.OIDC.ClientID = "client-id"
			o.OIDC.RedirectURL = ""
			o.OIDC.AllowedSubjects = []string{"sub-1"}
		}, nil, true},
		{"negative oidc missing both allowed subjects and emails", func(o *config.Options) {
			o.OIDC.Enabled = true
			o.OIDC.IssuerURL = "https://idp.example.com"
			o.OIDC.ClientID = "client-id"
			o.OIDC.RedirectURL = "https://snatcharr.example.com/api/v1/auth/oidc/callback"
		}, nil, true},
		{"positive proxy auth valid", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote-User"
			o.ProxyAuth.TrustedCIDRs = []string{"10.0.0.0/8"}
			o.ProxyAuth.AllowedUsers = []string{"admin"}
		}, nil, false},
		{"negative proxy auth empty header", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = ""
			o.ProxyAuth.TrustedCIDRs = []string{"10.0.0.0/8"}
			o.ProxyAuth.AllowedUsers = []string{"admin"}
		}, nil, true},
		{"negative proxy auth invalid header token", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote User Header"
			o.ProxyAuth.TrustedCIDRs = []string{"10.0.0.0/8"}
			o.ProxyAuth.AllowedUsers = []string{"admin"}
		}, nil, true},
		{"negative proxy auth empty trusted cidrs", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote-User"
			o.ProxyAuth.TrustedCIDRs = nil
			o.ProxyAuth.AllowedUsers = []string{"admin"}
		}, nil, true},
		{"negative proxy auth catch-all ipv4 cidr refused", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote-User"
			o.ProxyAuth.TrustedCIDRs = []string{"0.0.0.0/0"}
			o.ProxyAuth.AllowedUsers = []string{"admin"}
		}, nil, true},
		{"negative proxy auth catch-all ipv6 cidr refused", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote-User"
			o.ProxyAuth.TrustedCIDRs = []string{"::/0"}
			o.ProxyAuth.AllowedUsers = []string{"admin"}
		}, nil, true},
		{"negative proxy auth invalid cidr format", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote-User"
			o.ProxyAuth.TrustedCIDRs = []string{"not-a-cidr"}
			o.ProxyAuth.AllowedUsers = []string{"admin"}
		}, nil, true},
		{"negative proxy auth empty allowed users", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote-User"
			o.ProxyAuth.TrustedCIDRs = []string{"10.0.0.0/8"}
			o.ProxyAuth.AllowedUsers = nil
		}, nil, true},
		{"negative proxy auth blank user in allowed users", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote-User"
			o.ProxyAuth.TrustedCIDRs = []string{"10.0.0.0/8"}
			o.ProxyAuth.AllowedUsers = []string{" "}
		}, nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			o := prod
			tc.mutate(&o)
			err := o.Validate()
			if (err != nil) != tc.wantAny {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantAny)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestLinked(t *testing.T) {
	t.Parallel()
	if (config.ConfigarrOptions{}).Linked() {
		t.Fatal("empty options must not be linked")
	}
	if !(config.ConfigarrOptions{Config: "/c.yml"}).Linked() {
		t.Fatal("config path must mean linked")
	}
}

// A key of the deployment's own, as `openssl rand -hex 32` prints it.
const ownKey = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

func TestCryptoKey(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		profile    config.Profile
		configured string
		wantKey    string
		wantDev    bool
		wantErr    error
	}{
		{"positive prod with its own key", config.ProfileProd, ownKey, ownKey, false, nil},
		{"positive dev without a key runs on the development key", config.ProfileDev, "", config.DevCryptoKey, true, nil},
		{"positive dev with its own key", config.ProfileDev, ownKey, ownKey, false, nil},
		{"boundary dev set to the development key still reports development", config.ProfileDev, config.DevCryptoKey, config.DevCryptoKey, true, nil},
		{"negative prod without a key", config.ProfileProd, "", "", false, config.ErrMissingSecret},
		{"negative prod with the development key", config.ProfileProd, config.DevCryptoKey, "", false, config.ErrDevelopmentKey},
		{"negative prod with the development key in upper case", config.ProfileProd, strings.ToUpper(config.DevCryptoKey), "", false, config.ErrDevelopmentKey},
		{"negative default profile without a key", config.Default().Profile, "", "", false, config.ErrMissingSecret},
		{"negative default profile with the development key", config.Default().Profile, config.DevCryptoKey, "", false, config.ErrDevelopmentKey},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			o := config.Default()
			o.Profile = tc.profile
			key, dev, err := o.CryptoKey(tc.configured)
			if !errors.Is(err, tc.wantErr) || key != tc.wantKey || dev != tc.wantDev {
				t.Fatalf("CryptoKey(%q) = %q, %v, %v; want %q, %v, %v", tc.configured, key, dev, err, tc.wantKey, tc.wantDev, tc.wantErr)
			}
		})
	}
}

// The development key is a valid AES-256 key: 64 hex digits.
func TestDevCryptoKeyIsAES256(t *testing.T) {
	t.Parallel()
	raw, err := hex.DecodeString(config.DevCryptoKey)
	if err != nil || len(raw) != 32 {
		t.Fatalf("DevCryptoKey decodes to %d bytes, %v; want 32", len(raw), err)
	}
}

func TestValidate_MalformedTrustedProxyNamesBadEntry(t *testing.T) {
	t.Parallel()
	o := config.Default()
	o.Profile = config.ProfileDev
	o.Proxy.Trusted = []string{"10.0.0.0/8", "malformed-cidr"}
	err := o.Validate()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "malformed-cidr") {
		t.Fatalf("expected error to name malformed-cidr, got: %v", err)
	}
}

func TestLoadTrustedProxies(t *testing.T) {
	cases := []struct {
		name      string
		envVal    string
		wantCIDRs []string
		wantErr   bool
		badEntry  string
	}{
		{
			name:      "positive valid list",
			envVal:    "10.0.0.0/8, 172.16.0.0/12",
			wantCIDRs: []string{"10.0.0.0/8", "172.16.0.0/12"},
		},
		{
			name:      "positive whitespace and empty items around commas",
			envVal:    " 10.0.0.0/8 , , 192.168.0.0/16 ",
			wantCIDRs: []string{"10.0.0.0/8", "192.168.0.0/16"},
		},
		{
			name:      "boundary empty default trusts nothing",
			envVal:    "",
			wantCIDRs: nil,
		},
		{
			name:     "negative malformed entry refused names bad entry",
			envVal:   "10.0.0.0/8, not-a-cidr",
			wantErr:  true,
			badEntry: "not-a-cidr",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envVal != "" {
				t.Setenv("APP_SNATCHARR_PROXY_TRUSTED", tc.envVal)
			}
			t.Setenv("APP_SNATCHARR_PROFILE", "dev")
			cfg, err := gconfig.New(gconfig.Options{EnvPrefix: "APP_"})
			if err != nil {
				t.Fatalf("gconfig.New: %v", err)
			}
			opts, err := config.Load(cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("config.Load expected error, got nil")
				}
				if tc.badEntry != "" && !strings.Contains(err.Error(), tc.badEntry) {
					t.Fatalf("config.Load error %q does not contain bad entry %q", err.Error(), tc.badEntry)
				}
				return
			}
			if err != nil {
				t.Fatalf("config.Load: %v", err)
			}
			if len(opts.Proxy.Trusted) != len(tc.wantCIDRs) {
				t.Fatalf("Proxy.Trusted = %v, want %v", opts.Proxy.Trusted, tc.wantCIDRs)
			}
			for i, cidr := range tc.wantCIDRs {
				if opts.Proxy.Trusted[i] != cidr {
					t.Fatalf("Proxy.Trusted[%d] = %q, want %q", i, opts.Proxy.Trusted[i], cidr)
				}
			}
		})
	}
}

func TestLoadSetupSecret(t *testing.T) {
	cases := []struct {
		name       string
		envVal     string
		wantSecret string
	}{
		{
			name:       "positive secret configured",
			envVal:     "super-secret-bootstrap-token",
			wantSecret: "super-secret-bootstrap-token",
		},
		{
			name:       "boundary empty default has no secret",
			envVal:     "",
			wantSecret: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envVal != "" {
				t.Setenv("APP_SNATCHARR_SETUP_SECRET", tc.envVal)
			}
			t.Setenv("APP_SNATCHARR_PROFILE", "dev")
			cfg, err := gconfig.New(gconfig.Options{EnvPrefix: "APP_"})
			if err != nil {
				t.Fatalf("gconfig.New: %v", err)
			}
			opts, err := config.Load(cfg)
			if err != nil {
				t.Fatalf("config.Load: %v", err)
			}
			if opts.Setup.Secret != tc.wantSecret {
				t.Fatalf("Setup.Secret = %q, want %q", opts.Setup.Secret, tc.wantSecret)
			}
		})
	}
}

func TestLoadCompoundKeysFromEnv(t *testing.T) {
	cases := []struct {
		name     string
		env      map[string]string
		assertFn func(t *testing.T, opts config.Options)
	}{
		{
			name: "oidc issuer_url",
			env:  map[string]string{"APP_SNATCHARR_OIDC_ISSUER_URL": "https://auth.example.com"},
			assertFn: func(t *testing.T, opts config.Options) {
				t.Helper()
				if opts.OIDC.IssuerURL != "https://auth.example.com" {
					t.Fatalf("OIDC.IssuerURL = %q, want %q", opts.OIDC.IssuerURL, "https://auth.example.com")
				}
			},
		},
		{
			name: "oidc client_id",
			env:  map[string]string{"APP_SNATCHARR_OIDC_CLIENT_ID": "snatcharr-client"},
			assertFn: func(t *testing.T, opts config.Options) {
				t.Helper()
				if opts.OIDC.ClientID != "snatcharr-client" {
					t.Fatalf("OIDC.ClientID = %q, want %q", opts.OIDC.ClientID, "snatcharr-client")
				}
			},
		},
		{
			name: "oidc client_secret",
			env:  map[string]string{"APP_SNATCHARR_OIDC_CLIENT_SECRET": "secret-xyz"},
			assertFn: func(t *testing.T, opts config.Options) {
				t.Helper()
				if opts.OIDC.ClientSecret != "secret-xyz" {
					t.Fatalf("OIDC.ClientSecret = %q, want %q", opts.OIDC.ClientSecret, "secret-xyz")
				}
			},
		},
		{
			name: "oidc redirect_url",
			env:  map[string]string{"APP_SNATCHARR_OIDC_REDIRECT_URL": "https://snatcharr.example.com/callback"},
			assertFn: func(t *testing.T, opts config.Options) {
				t.Helper()
				if opts.OIDC.RedirectURL != "https://snatcharr.example.com/callback" {
					t.Fatalf("OIDC.RedirectURL = %q, want %q", opts.OIDC.RedirectURL, "https://snatcharr.example.com/callback")
				}
			},
		},
		{
			name: "oidc allowed_subjects comma-split",
			env:  map[string]string{"APP_SNATCHARR_OIDC_ALLOWED_SUBJECTS": "sub1, sub2"},
			assertFn: func(t *testing.T, opts config.Options) {
				t.Helper()
				want := []string{"sub1", "sub2"}
				if !slices.Equal(opts.OIDC.AllowedSubjects, want) {
					t.Fatalf("OIDC.AllowedSubjects = %v, want %v", opts.OIDC.AllowedSubjects, want)
				}
			},
		},
		{
			name: "oidc allowed_emails comma-split",
			env:  map[string]string{"APP_SNATCHARR_OIDC_ALLOWED_EMAILS": "admin@example.com, test@example.com"},
			assertFn: func(t *testing.T, opts config.Options) {
				t.Helper()
				want := []string{"admin@example.com", "test@example.com"}
				if !slices.Equal(opts.OIDC.AllowedEmails, want) {
					t.Fatalf("OIDC.AllowedEmails = %v, want %v", opts.OIDC.AllowedEmails, want)
				}
			},
		},
		{
			name: "oidc discovery_timeout parsed duration",
			env:  map[string]string{"APP_SNATCHARR_OIDC_DISCOVERY_TIMEOUT": "15s"},
			assertFn: func(t *testing.T, opts config.Options) {
				t.Helper()
				if opts.OIDC.DiscoveryTimeout != 15*time.Second {
					t.Fatalf("OIDC.DiscoveryTimeout = %v, want 15s", opts.OIDC.DiscoveryTimeout)
				}
			},
		},
		{
			name: "proxy_auth enabled",
			env: map[string]string{
				"APP_SNATCHARR_PROXY_AUTH_ENABLED":       "true",
				"APP_SNATCHARR_PROXY_AUTH_TRUSTED_CIDRS": "10.0.0.0/8",
				"APP_SNATCHARR_PROXY_AUTH_ALLOWED_USERS": "admin",
			},
			assertFn: func(t *testing.T, opts config.Options) {
				t.Helper()
				if !opts.ProxyAuth.Enabled {
					t.Fatalf("ProxyAuth.Enabled = false, want true")
				}
			},
		},
		{
			name: "proxy_auth header",
			env:  map[string]string{"APP_SNATCHARR_PROXY_AUTH_HEADER": "X-Forwarded-User"},
			assertFn: func(t *testing.T, opts config.Options) {
				t.Helper()
				if opts.ProxyAuth.Header != "X-Forwarded-User" {
					t.Fatalf("ProxyAuth.Header = %q, want %q", opts.ProxyAuth.Header, "X-Forwarded-User")
				}
			},
		},
		{
			name: "proxy_auth trusted_cidrs comma-split",
			env:  map[string]string{"APP_SNATCHARR_PROXY_AUTH_TRUSTED_CIDRS": "10.0.0.0/8, 172.16.0.0/12"},
			assertFn: func(t *testing.T, opts config.Options) {
				t.Helper()
				want := []string{"10.0.0.0/8", "172.16.0.0/12"}
				if !slices.Equal(opts.ProxyAuth.TrustedCIDRs, want) {
					t.Fatalf("ProxyAuth.TrustedCIDRs = %v, want %v", opts.ProxyAuth.TrustedCIDRs, want)
				}
			},
		},
		{
			name: "proxy_auth allowed_users comma-split",
			env:  map[string]string{"APP_SNATCHARR_PROXY_AUTH_ALLOWED_USERS": "admin, operator"},
			assertFn: func(t *testing.T, opts config.Options) {
				t.Helper()
				want := []string{"admin", "operator"}
				if !slices.Equal(opts.ProxyAuth.AllowedUsers, want) {
					t.Fatalf("ProxyAuth.AllowedUsers = %v, want %v", opts.ProxyAuth.AllowedUsers, want)
				}
			},
		},
		{
			name: "configarr download_clients",
			env:  map[string]string{"APP_SNATCHARR_CONFIGARR_DOWNLOAD_CLIENTS": "/etc/snatcharr/download-clients.yml"},
			assertFn: func(t *testing.T, opts config.Options) {
				t.Helper()
				if opts.Configarr.DownloadClients != "/etc/snatcharr/download-clients.yml" {
					t.Fatalf("Configarr.DownloadClients = %q, want %q", opts.Configarr.DownloadClients, "/etc/snatcharr/download-clients.yml")
				}
			},
		},
		{
			name: "configarr policies",
			env:  map[string]string{"APP_SNATCHARR_CONFIGARR_POLICIES": "/etc/snatcharr/policies.yml"},
			assertFn: func(t *testing.T, opts config.Options) {
				t.Helper()
				if opts.Configarr.Policies != "/etc/snatcharr/policies.yml" {
					t.Fatalf("Configarr.Policies = %q, want %q", opts.Configarr.Policies, "/etc/snatcharr/policies.yml")
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APP_SNATCHARR_PROFILE", "dev")
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			cfg, err := gconfig.New(gconfig.Options{
				EnvPrefix:    "APP_",
				CompoundKeys: config.CompoundKeys(),
			})
			if err != nil {
				t.Fatalf("gconfig.New: %v", err)
			}
			opts, err := config.Load(cfg)
			if err != nil {
				t.Fatalf("config.Load: %v", err)
			}
			tc.assertFn(t, opts)
		})
	}
}

func TestLoadWithoutCompoundKeys_Fails(t *testing.T) {
	t.Setenv("APP_SNATCHARR_PROFILE", "dev")
	t.Setenv("APP_SNATCHARR_PROXY_AUTH_ENABLED", "true")
	t.Setenv("APP_SNATCHARR_PROXY_AUTH_TRUSTED_CIDRS", "10.0.0.0/8")
	t.Setenv("APP_SNATCHARR_PROXY_AUTH_ALLOWED_USERS", "admin")

	cfg, err := gconfig.New(gconfig.Options{EnvPrefix: "APP_"})
	if err != nil {
		t.Fatalf("gconfig.New: %v", err)
	}
	opts, err := config.Load(cfg)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if opts.ProxyAuth.Enabled {
		t.Fatalf("ProxyAuth.Enabled = true without CompoundKeys; expected false because env was split into snatcharr.proxy.auth.enabled")
	}
}
