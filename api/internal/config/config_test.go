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
	"github.com/lusoris/SnatchArr/api/internal/domain"
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
		{"positive jellyfin default role default requester", func(o *config.Options) {}, nil, false},
		{"positive jellyfin default role viewer", func(o *config.Options) {
			o.Jellyfin.DefaultRole = domain.RoleViewer
		}, nil, false},
		{"positive jellyfin default role normalized whitespace", func(o *config.Options) {
			o.Jellyfin.DefaultRole = "  Viewer  "
		}, nil, false},
		{"negative jellyfin default role admin refused", func(o *config.Options) {
			o.Jellyfin.DefaultRole = domain.RoleAdmin
		}, nil, true},
		{"negative jellyfin default role owner refused", func(o *config.Options) {
			o.Jellyfin.DefaultRole = "owner"
		}, nil, true},
		{"negative jellyfin default role empty refused", func(o *config.Options) {
			o.Jellyfin.DefaultRole = ""
		}, nil, true},
		{"positive jellyfin enabled valid", func(o *config.Options) {
			o.Jellyfin.Enabled = true
			o.Jellyfin.URL = "http://jellyfin.local:8096"
			o.Jellyfin.APIKey = "valid-api-key"
			o.Jellyfin.Timeout = 5 * time.Second
		}, nil, false},
		{"positive item search cooldown default", func(o *config.Options) {}, nil, false},
		{"positive item search cooldown 30", func(o *config.Options) {
			o.ItemSearch.CooldownSeconds = 30
		}, nil, false},
		{"negative item search cooldown above max refused", func(o *config.Options) {
			o.ItemSearch.CooldownSeconds = config.MaxItemSearchCooldownSeconds + 1
		}, nil, true},
		{"negative item search cooldown negative refused", func(o *config.Options) {
			o.ItemSearch.CooldownSeconds = -1
		}, nil, true},
		{"negative jellyfin enabled no url", func(o *config.Options) {
			o.Jellyfin.Enabled = true
			o.Jellyfin.URL = ""
			o.Jellyfin.APIKey = "valid-key"
		}, nil, true},
		{"negative jellyfin enabled bad scheme", func(o *config.Options) {
			o.Jellyfin.Enabled = true
			o.Jellyfin.URL = "ftp://jellyfin.local"
			o.Jellyfin.APIKey = "valid-key"
		}, nil, true},
		{"negative jellyfin enabled empty key", func(o *config.Options) {
			o.Jellyfin.Enabled = true
			o.Jellyfin.URL = "http://jellyfin.local:8096"
			o.Jellyfin.APIKey = ""
		}, nil, true},
		{"boundary jellyfin disabled with empty url passes", func(o *config.Options) {
			o.Jellyfin.Enabled = false
			o.Jellyfin.URL = ""
			o.Jellyfin.APIKey = ""
		}, nil, false},
		{"boundary jellyfin key of only whitespace refused", func(o *config.Options) {
			o.Jellyfin.Enabled = true
			o.Jellyfin.URL = "http://jellyfin.local:8096"
			o.Jellyfin.APIKey = "   "
		}, nil, true},
		{"boundary jellyfin timeout zero refused", func(o *config.Options) {
			o.Jellyfin.Enabled = true
			o.Jellyfin.URL = "http://jellyfin.local:8096"
			o.Jellyfin.APIKey = "valid-key"
			o.Jellyfin.Timeout = 0
		}, nil, true},
		{"positive oidc valid prod", func(o *config.Options) {
			o.OIDC.Enabled = true
			o.OIDC.IssuerURL = "https://idp.example.com"
			o.OIDC.ClientID = "client-id"
			o.OIDC.RedirectURL = "https://snatcharr.example.com/api/v1/auth/oidc/callback"
			o.OIDC.AdminGroups = []string{"admin"}
		}, nil, false},
		{"positive oidc with viewer groups", func(o *config.Options) {
			o.OIDC.Enabled = true
			o.OIDC.IssuerURL = "https://idp.example.com"
			o.OIDC.ClientID = "client-id"
			o.OIDC.RedirectURL = "https://snatcharr.example.com/api/v1/auth/oidc/callback"
			o.OIDC.AdminGroups = []string{"admin"}
			o.OIDC.ViewerGroups = []string{"viewer"}
		}, nil, false},
		{"positive oidc with requester groups", func(o *config.Options) {
			o.OIDC.Enabled = true
			o.OIDC.IssuerURL = "https://idp.example.com"
			o.OIDC.ClientID = "client-id"
			o.OIDC.RedirectURL = "https://snatcharr.example.com/api/v1/auth/oidc/callback"
			o.OIDC.AdminGroups = []string{"admin"}
			o.OIDC.RequesterGroups = []string{"requester"}
			o.OIDC.ViewerGroups = []string{"viewer"}
		}, nil, false},
		{"boundary oidc dev allows http issuer", func(o *config.Options) {
			o.Profile = config.ProfileDev
			o.Worker.Token = ""
			o.APIKey.Secret = ""
			o.OIDC.Enabled = true
			o.OIDC.IssuerURL = "http://localhost:8080"
			o.OIDC.ClientID = "client-id"
			o.OIDC.RedirectURL = "http://localhost:8080/api/v1/auth/oidc/callback"
			o.OIDC.AdminGroups = []string{"admin"}
		}, nil, false},
		{"negative oidc prod refuses http issuer", func(o *config.Options) {
			o.OIDC.Enabled = true
			o.OIDC.IssuerURL = "http://idp.example.com"
			o.OIDC.ClientID = "client-id"
			o.OIDC.RedirectURL = "https://snatcharr.example.com/api/v1/auth/oidc/callback"
			o.OIDC.AdminGroups = []string{"admin"}
		}, nil, true},
		{"negative oidc missing issuer", func(o *config.Options) {
			o.OIDC.Enabled = true
			o.OIDC.IssuerURL = ""
			o.OIDC.ClientID = "client-id"
			o.OIDC.RedirectURL = "https://snatcharr.example.com/api/v1/auth/oidc/callback"
			o.OIDC.AdminGroups = []string{"admin"}
		}, nil, true},
		{"negative oidc malformed issuer", func(o *config.Options) {
			o.OIDC.Enabled = true
			o.OIDC.IssuerURL = "://invalid"
			o.OIDC.ClientID = "client-id"
			o.OIDC.RedirectURL = "https://snatcharr.example.com/api/v1/auth/oidc/callback"
			o.OIDC.AdminGroups = []string{"admin"}
		}, nil, true},
		{"negative oidc missing client_id", func(o *config.Options) {
			o.OIDC.Enabled = true
			o.OIDC.IssuerURL = "https://idp.example.com"
			o.OIDC.ClientID = ""
			o.OIDC.RedirectURL = "https://snatcharr.example.com/api/v1/auth/oidc/callback"
			o.OIDC.AdminGroups = []string{"admin"}
		}, nil, true},
		{"negative oidc missing redirect_url", func(o *config.Options) {
			o.OIDC.Enabled = true
			o.OIDC.IssuerURL = "https://idp.example.com"
			o.OIDC.ClientID = "client-id"
			o.OIDC.RedirectURL = ""
			o.OIDC.AdminGroups = []string{"admin"}
		}, nil, true},
		{"negative oidc missing admin groups", func(o *config.Options) {
			o.OIDC.Enabled = true
			o.OIDC.IssuerURL = "https://idp.example.com"
			o.OIDC.ClientID = "client-id"
			o.OIDC.RedirectURL = "https://snatcharr.example.com/api/v1/auth/oidc/callback"
			o.OIDC.AdminGroups = nil
		}, nil, true},
		{"negative oidc legacy allowed subjects refused", func(o *config.Options) {
			o.OIDC.Enabled = true
			o.OIDC.IssuerURL = "https://idp.example.com"
			o.OIDC.ClientID = "client-id"
			o.OIDC.RedirectURL = "https://snatcharr.example.com/api/v1/auth/oidc/callback"
			o.OIDC.AdminGroups = []string{"admin"}
			o.OIDC.AllowedSubjects = []string{"sub-1"}
		}, nil, true},
		{"negative oidc legacy allowed emails refused", func(o *config.Options) {
			o.OIDC.Enabled = true
			o.OIDC.IssuerURL = "https://idp.example.com"
			o.OIDC.ClientID = "client-id"
			o.OIDC.RedirectURL = "https://snatcharr.example.com/api/v1/auth/oidc/callback"
			o.OIDC.AdminGroups = []string{"admin"}
			o.OIDC.AllowedEmails = []string{"user@example.com"}
		}, nil, true},
		{"negative oidc group on both admin and viewer lists", func(o *config.Options) {
			o.OIDC.Enabled = true
			o.OIDC.IssuerURL = "https://idp.example.com"
			o.OIDC.ClientID = "client-id"
			o.OIDC.RedirectURL = "https://snatcharr.example.com/api/v1/auth/oidc/callback"
			o.OIDC.AdminGroups = []string{"ops"}
			o.OIDC.ViewerGroups = []string{"ops"}
		}, nil, true},
		{"negative oidc group on both admin and requester lists", func(o *config.Options) {
			o.OIDC.Enabled = true
			o.OIDC.IssuerURL = "https://idp.example.com"
			o.OIDC.ClientID = "client-id"
			o.OIDC.RedirectURL = "https://snatcharr.example.com/api/v1/auth/oidc/callback"
			o.OIDC.AdminGroups = []string{"ops"}
			o.OIDC.RequesterGroups = []string{"ops"}
		}, nil, true},
		{"negative oidc group on both requester and viewer lists", func(o *config.Options) {
			o.OIDC.Enabled = true
			o.OIDC.IssuerURL = "https://idp.example.com"
			o.OIDC.ClientID = "client-id"
			o.OIDC.RedirectURL = "https://snatcharr.example.com/api/v1/auth/oidc/callback"
			o.OIDC.AdminGroups = []string{"admin"}
			o.OIDC.RequesterGroups = []string{"family"}
			o.OIDC.ViewerGroups = []string{"family"}
		}, nil, true},
		{"positive proxy auth valid", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote-User"
			o.ProxyAuth.GroupsHeader = "X-authentik-groups"
			o.ProxyAuth.Secret = strings.Repeat("s", 32)
			o.ProxyAuth.TrustedCIDRs = []string{"10.0.0.0/8"}
			o.ProxyAuth.AdminGroups = []string{"admin"}
		}, nil, false},
		{"positive proxy auth with viewer groups", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote-User"
			o.ProxyAuth.GroupsHeader = "X-authentik-groups"
			o.ProxyAuth.Secret = strings.Repeat("s", 32)
			o.ProxyAuth.TrustedCIDRs = []string{"10.0.0.0/8"}
			o.ProxyAuth.AdminGroups = []string{"admin"}
			o.ProxyAuth.ViewerGroups = []string{"viewer"}
		}, nil, false},
		{"positive proxy auth with requester groups", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote-User"
			o.ProxyAuth.GroupsHeader = "X-authentik-groups"
			o.ProxyAuth.Secret = strings.Repeat("s", 32)
			o.ProxyAuth.TrustedCIDRs = []string{"10.0.0.0/8"}
			o.ProxyAuth.AdminGroups = []string{"admin"}
			o.ProxyAuth.RequesterGroups = []string{"requester"}
			o.ProxyAuth.ViewerGroups = []string{"viewer"}
		}, nil, false},
		{"negative proxy auth group on both admin and requester lists", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote-User"
			o.ProxyAuth.GroupsHeader = "X-authentik-groups"
			o.ProxyAuth.Secret = strings.Repeat("s", 32)
			o.ProxyAuth.TrustedCIDRs = []string{"10.0.0.0/8"}
			o.ProxyAuth.AdminGroups = []string{"admin"}
			o.ProxyAuth.RequesterGroups = []string{"admin"}
		}, nil, true},
		{"negative proxy auth group on both requester and viewer lists", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote-User"
			o.ProxyAuth.GroupsHeader = "X-authentik-groups"
			o.ProxyAuth.Secret = strings.Repeat("s", 32)
			o.ProxyAuth.TrustedCIDRs = []string{"10.0.0.0/8"}
			o.ProxyAuth.AdminGroups = []string{"admin"}
			o.ProxyAuth.RequesterGroups = []string{"viewer"}
			o.ProxyAuth.ViewerGroups = []string{"viewer"}
		}, nil, true},
		{"negative proxy auth empty header", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = ""
			o.ProxyAuth.GroupsHeader = "X-authentik-groups"
			o.ProxyAuth.Secret = strings.Repeat("s", 32)
			o.ProxyAuth.TrustedCIDRs = []string{"10.0.0.0/8"}
			o.ProxyAuth.AdminGroups = []string{"admin"}
		}, nil, true},
		{"negative proxy auth invalid header token", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote User Header"
			o.ProxyAuth.GroupsHeader = "X-authentik-groups"
			o.ProxyAuth.Secret = strings.Repeat("s", 32)
			o.ProxyAuth.TrustedCIDRs = []string{"10.0.0.0/8"}
			o.ProxyAuth.AdminGroups = []string{"admin"}
		}, nil, true},
		{"negative proxy auth empty groups header", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote-User"
			o.ProxyAuth.GroupsHeader = ""
			o.ProxyAuth.Secret = strings.Repeat("s", 32)
			o.ProxyAuth.TrustedCIDRs = []string{"10.0.0.0/8"}
			o.ProxyAuth.AdminGroups = []string{"admin"}
		}, nil, true},
		{"negative proxy auth invalid groups header token", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote-User"
			o.ProxyAuth.GroupsHeader = "Invalid Header"
			o.ProxyAuth.Secret = strings.Repeat("s", 32)
			o.ProxyAuth.TrustedCIDRs = []string{"10.0.0.0/8"}
			o.ProxyAuth.AdminGroups = []string{"admin"}
		}, nil, true},
		{"negative proxy auth empty trusted cidrs", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote-User"
			o.ProxyAuth.GroupsHeader = "X-authentik-groups"
			o.ProxyAuth.Secret = strings.Repeat("s", 32)
			o.ProxyAuth.TrustedCIDRs = nil
			o.ProxyAuth.AdminGroups = []string{"admin"}
		}, nil, true},
		{"negative proxy auth catch-all ipv4 cidr refused", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote-User"
			o.ProxyAuth.GroupsHeader = "X-authentik-groups"
			o.ProxyAuth.Secret = strings.Repeat("s", 32)
			o.ProxyAuth.TrustedCIDRs = []string{"0.0.0.0/0"}
			o.ProxyAuth.AdminGroups = []string{"admin"}
		}, nil, true},
		{"negative proxy auth catch-all ipv6 cidr refused", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote-User"
			o.ProxyAuth.GroupsHeader = "X-authentik-groups"
			o.ProxyAuth.Secret = strings.Repeat("s", 32)
			o.ProxyAuth.TrustedCIDRs = []string{"::/0"}
			o.ProxyAuth.AdminGroups = []string{"admin"}
		}, nil, true},
		{"negative proxy auth invalid cidr format", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote-User"
			o.ProxyAuth.GroupsHeader = "X-authentik-groups"
			o.ProxyAuth.Secret = strings.Repeat("s", 32)
			o.ProxyAuth.TrustedCIDRs = []string{"not-a-cidr"}
			o.ProxyAuth.AdminGroups = []string{"admin"}
		}, nil, true},
		{"negative proxy auth empty admin groups", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote-User"
			o.ProxyAuth.GroupsHeader = "X-authentik-groups"
			o.ProxyAuth.Secret = strings.Repeat("s", 32)
			o.ProxyAuth.TrustedCIDRs = []string{"10.0.0.0/8"}
			o.ProxyAuth.AdminGroups = nil
		}, nil, true},
		{"negative proxy auth legacy allowed users refused", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote-User"
			o.ProxyAuth.GroupsHeader = "X-authentik-groups"
			o.ProxyAuth.Secret = strings.Repeat("s", 32)
			o.ProxyAuth.TrustedCIDRs = []string{"10.0.0.0/8"}
			o.ProxyAuth.AdminGroups = []string{"admin"}
			o.ProxyAuth.AllowedUsers = []string{"admin"}
		}, nil, true},
		{"negative proxy auth group on both admin and viewer lists", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote-User"
			o.ProxyAuth.GroupsHeader = "X-authentik-groups"
			o.ProxyAuth.Secret = strings.Repeat("s", 32)
			o.ProxyAuth.TrustedCIDRs = []string{"10.0.0.0/8"}
			o.ProxyAuth.AdminGroups = []string{"ops"}
			o.ProxyAuth.ViewerGroups = []string{"ops"}
		}, nil, true},
		{"positive proxy auth enabled and valid secret", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote-User"
			o.ProxyAuth.GroupsHeader = "X-authentik-groups"
			o.ProxyAuth.SecretHeader = "X-SnatchArr-Proxy-Secret"
			o.ProxyAuth.Secret = strings.Repeat("s", 32)
			o.ProxyAuth.TrustedCIDRs = []string{"10.0.0.0/8"}
			o.ProxyAuth.AdminGroups = []string{"admin"}
		}, nil, false},
		{"negative proxy auth missing secret", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote-User"
			o.ProxyAuth.GroupsHeader = "X-authentik-groups"
			o.ProxyAuth.Secret = ""
			o.ProxyAuth.TrustedCIDRs = []string{"10.0.0.0/8"}
			o.ProxyAuth.AdminGroups = []string{"admin"}
		}, nil, true},
		{"negative proxy auth 31-byte secret", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote-User"
			o.ProxyAuth.GroupsHeader = "X-authentik-groups"
			o.ProxyAuth.Secret = strings.Repeat("s", 31)
			o.ProxyAuth.TrustedCIDRs = []string{"10.0.0.0/8"}
			o.ProxyAuth.AdminGroups = []string{"admin"}
		}, nil, true},
		{"boundary proxy auth 32-byte secret ok", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote-User"
			o.ProxyAuth.GroupsHeader = "X-authentik-groups"
			o.ProxyAuth.Secret = strings.Repeat("s", 32)
			o.ProxyAuth.TrustedCIDRs = []string{"10.0.0.0/8"}
			o.ProxyAuth.AdminGroups = []string{"admin"}
		}, nil, false},
		{"negative proxy auth whitespace-only secret", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote-User"
			o.ProxyAuth.GroupsHeader = "X-authentik-groups"
			o.ProxyAuth.Secret = "   \t\n   "
			o.ProxyAuth.TrustedCIDRs = []string{"10.0.0.0/8"}
			o.ProxyAuth.AdminGroups = []string{"admin"}
		}, nil, true},
		{"negative proxy auth invalid secret_header token", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote-User"
			o.ProxyAuth.GroupsHeader = "X-authentik-groups"
			o.ProxyAuth.SecretHeader = "Invalid Secret Header"
			o.ProxyAuth.Secret = strings.Repeat("s", 32)
			o.ProxyAuth.TrustedCIDRs = []string{"10.0.0.0/8"}
			o.ProxyAuth.AdminGroups = []string{"admin"}
		}, nil, true},
		{"negative proxy auth secret_header equal to header in other case", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote-User"
			o.ProxyAuth.GroupsHeader = "X-authentik-groups"
			o.ProxyAuth.SecretHeader = "remote-user"
			o.ProxyAuth.Secret = strings.Repeat("s", 32)
			o.ProxyAuth.TrustedCIDRs = []string{"10.0.0.0/8"}
			o.ProxyAuth.AdminGroups = []string{"admin"}
		}, nil, true},
		{"negative proxy auth secret_header equal to groups_header in other case", func(o *config.Options) {
			o.ProxyAuth.Enabled = true
			o.ProxyAuth.Header = "Remote-User"
			o.ProxyAuth.GroupsHeader = "X-authentik-groups"
			o.ProxyAuth.SecretHeader = "x-AUTHENTIK-groups"
			o.ProxyAuth.Secret = strings.Repeat("s", 32)
			o.ProxyAuth.TrustedCIDRs = []string{"10.0.0.0/8"}
			o.ProxyAuth.AdminGroups = []string{"admin"}
		}, nil, true},
		{"positive metrics addr empty ok", func(o *config.Options) { o.Metrics.Addr = "" }, nil, false},
		{"positive metrics addr port only ok", func(o *config.Options) { o.Metrics.Addr = ":9090" }, nil, false},
		{"positive metrics addr host and port ok", func(o *config.Options) { o.Metrics.Addr = "127.0.0.1:9090" }, nil, false},
		{"boundary metrics addr port 1 ok", func(o *config.Options) { o.Metrics.Addr = ":1" }, nil, false},
		{"boundary metrics addr port 65535 ok", func(o *config.Options) { o.Metrics.Addr = ":65535" }, nil, false},
		{"negative metrics addr port 0 refused", func(o *config.Options) { o.Metrics.Addr = ":0" }, nil, true},
		{"negative metrics addr port 65536 refused", func(o *config.Options) { o.Metrics.Addr = ":65536" }, nil, true},
		{"negative metrics addr non-numeric port refused", func(o *config.Options) { o.Metrics.Addr = ":abc" }, nil, true},
		{"negative metrics addr missing port refused", func(o *config.Options) { o.Metrics.Addr = "localhost" }, nil, true},
		{"negative metrics addr numeric without colon refused", func(o *config.Options) { o.Metrics.Addr = "9090" }, nil, true},
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
			name: "oidc admin_groups comma-split",
			env:  map[string]string{"APP_SNATCHARR_OIDC_ADMIN_GROUPS": "admin, ops"},
			assertFn: func(t *testing.T, opts config.Options) {
				t.Helper()
				want := []string{"admin", "ops"}
				if !slices.Equal(opts.OIDC.AdminGroups, want) {
					t.Fatalf("OIDC.AdminGroups = %v, want %v", opts.OIDC.AdminGroups, want)
				}
			},
		},
		{
			name: "oidc requester_groups comma-split",
			env:  map[string]string{"APP_SNATCHARR_OIDC_REQUESTER_GROUPS": "requester, family"},
			assertFn: func(t *testing.T, opts config.Options) {
				t.Helper()
				want := []string{"requester", "family"}
				if !slices.Equal(opts.OIDC.RequesterGroups, want) {
					t.Fatalf("OIDC.RequesterGroups = %v, want %v", opts.OIDC.RequesterGroups, want)
				}
			},
		},
		{
			name: "oidc viewer_groups comma-split",
			env:  map[string]string{"APP_SNATCHARR_OIDC_VIEWER_GROUPS": "viewer, guest"},
			assertFn: func(t *testing.T, opts config.Options) {
				t.Helper()
				want := []string{"viewer", "guest"}
				if !slices.Equal(opts.OIDC.ViewerGroups, want) {
					t.Fatalf("OIDC.ViewerGroups = %v, want %v", opts.OIDC.ViewerGroups, want)
				}
			},
		},
		{
			name: "boundary group name with inner space kept whole",
			env:  map[string]string{"APP_SNATCHARR_OIDC_VIEWER_GROUPS": " SnatchArr Viewers ,Admins"},
			assertFn: func(t *testing.T, opts config.Options) {
				t.Helper()
				want := []string{"SnatchArr Viewers", "Admins"}
				if !slices.Equal(opts.OIDC.ViewerGroups, want) {
					t.Fatalf("OIDC.ViewerGroups = %v, want %v", opts.OIDC.ViewerGroups, want)
				}
			},
		},
		{
			name: "oidc groups_claim custom",
			env:  map[string]string{"APP_SNATCHARR_OIDC_GROUPS_CLAIM": "roles"},
			assertFn: func(t *testing.T, opts config.Options) {
				t.Helper()
				if opts.OIDC.GroupsClaim != "roles" {
					t.Fatalf("OIDC.GroupsClaim = %q, want roles", opts.OIDC.GroupsClaim)
				}
			},
		},
		{
			name: "proxy_auth enabled",
			env: map[string]string{
				"APP_SNATCHARR_PROXY_AUTH_ENABLED":       "true",
				"APP_SNATCHARR_PROXY_AUTH_TRUSTED_CIDRS": "10.0.0.0/8",
				"APP_SNATCHARR_PROXY_AUTH_ADMIN_GROUPS":  "admin",
				"APP_SNATCHARR_PROXY_AUTH_SECRET":        strings.Repeat("s", 32),
			},
			assertFn: func(t *testing.T, opts config.Options) {
				t.Helper()
				if !opts.ProxyAuth.Enabled {
					t.Fatalf("ProxyAuth.Enabled = false, want true")
				}
			},
		},
		{
			name: "proxy_auth secret_header custom",
			env:  map[string]string{"APP_SNATCHARR_PROXY_AUTH_SECRET_HEADER": "X-Custom-Secret"},
			assertFn: func(t *testing.T, opts config.Options) {
				t.Helper()
				if opts.ProxyAuth.SecretHeader != "X-Custom-Secret" {
					t.Fatalf("ProxyAuth.SecretHeader = %q, want X-Custom-Secret", opts.ProxyAuth.SecretHeader)
				}
			},
		},
		{
			name: "proxy_auth secret",
			env:  map[string]string{"APP_SNATCHARR_PROXY_AUTH_SECRET": strings.Repeat("s", 32)},
			assertFn: func(t *testing.T, opts config.Options) {
				t.Helper()
				if opts.ProxyAuth.Secret != strings.Repeat("s", 32) {
					t.Fatalf("ProxyAuth.Secret = %q, want 32 's'", opts.ProxyAuth.Secret)
				}
			},
		},
		{
			name: "proxy_auth groups_header custom",
			env:  map[string]string{"APP_SNATCHARR_PROXY_AUTH_GROUPS_HEADER": "X-Custom-Groups"},
			assertFn: func(t *testing.T, opts config.Options) {
				t.Helper()
				if opts.ProxyAuth.GroupsHeader != "X-Custom-Groups" {
					t.Fatalf("ProxyAuth.GroupsHeader = %q, want X-Custom-Groups", opts.ProxyAuth.GroupsHeader)
				}
			},
		},
		{
			name: "proxy_auth admin_groups comma-split",
			env:  map[string]string{"APP_SNATCHARR_PROXY_AUTH_ADMIN_GROUPS": "admin, ops"},
			assertFn: func(t *testing.T, opts config.Options) {
				t.Helper()
				want := []string{"admin", "ops"}
				if !slices.Equal(opts.ProxyAuth.AdminGroups, want) {
					t.Fatalf("ProxyAuth.AdminGroups = %v, want %v", opts.ProxyAuth.AdminGroups, want)
				}
			},
		},
		{
			name: "proxy_auth requester_groups comma-split",
			env:  map[string]string{"APP_SNATCHARR_PROXY_AUTH_REQUESTER_GROUPS": "requester, family"},
			assertFn: func(t *testing.T, opts config.Options) {
				t.Helper()
				want := []string{"requester", "family"}
				if !slices.Equal(opts.ProxyAuth.RequesterGroups, want) {
					t.Fatalf("ProxyAuth.RequesterGroups = %v, want %v", opts.ProxyAuth.RequesterGroups, want)
				}
			},
		},
		{
			name: "proxy_auth viewer_groups comma-split",
			env:  map[string]string{"APP_SNATCHARR_PROXY_AUTH_VIEWER_GROUPS": "viewer, guest"},
			assertFn: func(t *testing.T, opts config.Options) {
				t.Helper()
				want := []string{"viewer", "guest"}
				if !slices.Equal(opts.ProxyAuth.ViewerGroups, want) {
					t.Fatalf("ProxyAuth.ViewerGroups = %v, want %v", opts.ProxyAuth.ViewerGroups, want)
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
		{
			name: "jellyfin default_role",
			env:  map[string]string{"APP_SNATCHARR_JELLYFIN_DEFAULT_ROLE": "viewer"},
			assertFn: func(t *testing.T, opts config.Options) {
				t.Helper()
				if opts.Jellyfin.DefaultRole != domain.RoleViewer {
					t.Fatalf("Jellyfin.DefaultRole = %q, want %q", opts.Jellyfin.DefaultRole, domain.RoleViewer)
				}
			},
		},
		{
			name: "jellyfin full env",
			env: map[string]string{
				"APP_SNATCHARR_JELLYFIN_ENABLED":      "true",
				"APP_SNATCHARR_JELLYFIN_URL":          "https://jf.example.com",
				"APP_SNATCHARR_JELLYFIN_API_KEY":      "secret-key\n",
				"APP_SNATCHARR_JELLYFIN_TIMEOUT":      "5s",
				"APP_SNATCHARR_JELLYFIN_DEFAULT_ROLE": "viewer",
			},
			assertFn: func(t *testing.T, opts config.Options) {
				t.Helper()
				if !opts.Jellyfin.Enabled {
					t.Fatal("expected Jellyfin.Enabled = true")
				}
				if opts.Jellyfin.URL != "https://jf.example.com" {
					t.Fatalf("Jellyfin.URL = %q, want %q", opts.Jellyfin.URL, "https://jf.example.com")
				}
				if opts.Jellyfin.APIKey != "secret-key" {
					t.Fatalf("Jellyfin.APIKey = %q, want %q", opts.Jellyfin.APIKey, "secret-key")
				}
				if opts.Jellyfin.Timeout != 5*time.Second {
					t.Fatalf("Jellyfin.Timeout = %v, want 5s", opts.Jellyfin.Timeout)
				}
				if opts.Jellyfin.DefaultRole != domain.RoleViewer {
					t.Fatalf("Jellyfin.DefaultRole = %q, want %q", opts.Jellyfin.DefaultRole, domain.RoleViewer)
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

// golusoris core v0.12 Unmarshal infers underscore paths from the koanf struct tags, so
// underscore keys resolve from env even without CompoundKeys (golusoris/golusoris#937).
func TestLoadWithoutCompoundKeys_ResolvesUnderscoreKeys(t *testing.T) {
	t.Setenv("APP_SNATCHARR_PROFILE", "dev")
	t.Setenv("APP_SNATCHARR_PROXY_AUTH_ENABLED", "true")
	t.Setenv("APP_SNATCHARR_PROXY_AUTH_TRUSTED_CIDRS", "10.0.0.0/8")
	t.Setenv("APP_SNATCHARR_PROXY_AUTH_ADMIN_GROUPS", "admins")
	t.Setenv("APP_SNATCHARR_PROXY_AUTH_SECRET", strings.Repeat("s", 32))

	cfg, err := gconfig.New(gconfig.Options{EnvPrefix: "APP_"})
	if err != nil {
		t.Fatalf("gconfig.New: %v", err)
	}
	opts, err := config.Load(cfg)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if !opts.ProxyAuth.Enabled {
		t.Fatal("ProxyAuth.Enabled = false; want true from APP_SNATCHARR_PROXY_AUTH_ENABLED")
	}
	if len(opts.ProxyAuth.AdminGroups) != 1 || opts.ProxyAuth.AdminGroups[0] != "admins" {
		t.Fatalf("ProxyAuth.AdminGroups = %v, want [admins]", opts.ProxyAuth.AdminGroups)
	}
}

func TestLoadWithoutCompoundKeys_RefusesDeprecatedAllowedUsers(t *testing.T) {
	t.Setenv("APP_SNATCHARR_PROFILE", "dev")
	t.Setenv("APP_SNATCHARR_PROXY_AUTH_ENABLED", "true")
	t.Setenv("APP_SNATCHARR_PROXY_AUTH_TRUSTED_CIDRS", "10.0.0.0/8")
	t.Setenv("APP_SNATCHARR_PROXY_AUTH_ALLOWED_USERS", "admin")
	t.Setenv("APP_SNATCHARR_PROXY_AUTH_SECRET", strings.Repeat("s", 32))

	cfg, err := gconfig.New(gconfig.Options{EnvPrefix: "APP_"})
	if err != nil {
		t.Fatalf("gconfig.New: %v", err)
	}
	if _, err := config.Load(cfg); err == nil || !strings.Contains(err.Error(), "allowed_users is deprecated") {
		t.Fatalf("config.Load error = %v, want allowed_users deprecation", err)
	}
}

func TestLoadProxyAuthSecretTrimmed(t *testing.T) {
	cases := []struct {
		name       string
		envVal     string
		wantSecret string
		wantErr    bool
	}{
		{"positive secret kept", strings.Repeat("s", 32), strings.Repeat("s", 32), false},
		{"boundary trailing newline from a secret file trimmed", strings.Repeat("s", 32) + "\n", strings.Repeat("s", 32), false},
		{"negative 31 bytes plus newline refused", strings.Repeat("s", 31) + "\n", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APP_SNATCHARR_PROFILE", "dev")
			t.Setenv("APP_SNATCHARR_PROXY_AUTH_ENABLED", "true")
			t.Setenv("APP_SNATCHARR_PROXY_AUTH_TRUSTED_CIDRS", "10.0.0.0/8")
			t.Setenv("APP_SNATCHARR_PROXY_AUTH_ADMIN_GROUPS", "admins")
			t.Setenv("APP_SNATCHARR_PROXY_AUTH_SECRET", tc.envVal)
			cfg, err := gconfig.New(gconfig.Options{EnvPrefix: "APP_"})
			if err != nil {
				t.Fatalf("gconfig.New: %v", err)
			}
			opts, err := config.Load(cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatal("config.Load accepted a short secret")
				}
				return
			}
			if err != nil {
				t.Fatalf("config.Load: %v", err)
			}
			if opts.ProxyAuth.Secret != tc.wantSecret {
				t.Fatalf("ProxyAuth.Secret length = %d, want %d", len(opts.ProxyAuth.Secret), len(tc.wantSecret))
			}
		})
	}
}

func TestMetricsAddr_Load(t *testing.T) {
	cases := []struct {
		name     string
		envVal   string
		wantAddr string
		wantErr  bool
	}{
		{"positive empty default ok", "", "", false},
		{"positive custom port ok", ":9090", ":9090", false},
		{"positive host and port ok", "127.0.0.1:9090", "127.0.0.1:9090", false},
		{"boundary port 1 ok", ":1", ":1", false},
		{"boundary port 65535 ok", ":65535", ":65535", false},
		{"negative port 0 refused", ":0", "", true},
		{"negative port 65536 refused", ":65536", "", true},
		{"negative non numeric port refused", ":abc", "", true},
		{"negative missing port refused", "localhost", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APP_SNATCHARR_PROFILE", "dev")
			if tc.envVal != "" {
				t.Setenv("APP_SNATCHARR_METRICS_ADDR", tc.envVal)
			}
			cfg, err := gconfig.New(gconfig.Options{EnvPrefix: "APP_"})
			if err != nil {
				t.Fatalf("gconfig.New: %v", err)
			}
			opts, err := config.Load(cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("config.Load accepted invalid metrics addr %q", tc.envVal)
				}
				return
			}
			if err != nil {
				t.Fatalf("config.Load: %v", err)
			}
			if opts.Metrics.Addr != tc.wantAddr {
				t.Fatalf("Metrics.Addr = %q, want %q", opts.Metrics.Addr, tc.wantAddr)
			}
		})
	}
}

func TestLoad_JellyfinDefaultRole(t *testing.T) {
	cases := []struct {
		name     string
		setEnv   bool
		envVal   string
		wantRole domain.Role
		wantErr  bool
	}{
		{
			name:     "positive default yields requester",
			setEnv:   false,
			wantRole: domain.RoleRequester,
			wantErr:  false,
		},
		{
			name:     "positive viewer accepted",
			setEnv:   true,
			envVal:   "viewer",
			wantRole: domain.RoleViewer,
			wantErr:  false,
		},
		{
			name:     "positive Viewer with whitespace accepted and normalized",
			setEnv:   true,
			envVal:   "  Viewer  ",
			wantRole: domain.RoleViewer,
			wantErr:  false,
		},
		{
			name:    "negative admin refused at boot",
			setEnv:  true,
			envVal:  "admin",
			wantErr: true,
		},
		{
			name:    "negative owner refused at boot",
			setEnv:  true,
			envVal:  "owner",
			wantErr: true,
		},
		{
			name:    "negative explicit empty refused at boot",
			setEnv:  true,
			envVal:  "",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APP_SNATCHARR_PROFILE", "dev")
			if tc.setEnv {
				t.Setenv("APP_SNATCHARR_JELLYFIN_DEFAULT_ROLE", tc.envVal)
			}
			cfg, err := gconfig.New(gconfig.Options{
				EnvPrefix:    "APP_",
				CompoundKeys: config.CompoundKeys(),
			})
			if err != nil {
				t.Fatalf("gconfig.New: %v", err)
			}
			opts, err := config.Load(cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("config.Load accepted invalid jellyfin default role %q", tc.envVal)
				}
				msg := err.Error()
				if !strings.Contains(msg, "snatcharr.jellyfin.default_role") {
					t.Errorf("error %q should mention config key snatcharr.jellyfin.default_role", msg)
				}
				if !strings.Contains(msg, "APP_SNATCHARR_JELLYFIN_DEFAULT_ROLE") {
					t.Errorf("error %q should mention env var APP_SNATCHARR_JELLYFIN_DEFAULT_ROLE", msg)
				}
				return
			}
			if err != nil {
				t.Fatalf("config.Load: %v", err)
			}
			if opts.Jellyfin.DefaultRole != tc.wantRole {
				t.Fatalf("Jellyfin.DefaultRole = %q, want %q", opts.Jellyfin.DefaultRole, tc.wantRole)
			}
		})
	}
}

func TestValidate_JellyfinDefaultRoleErrorMessage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		role domain.Role
	}{
		{"negative admin refused", domain.RoleAdmin},
		{"negative owner refused", "owner"},
		{"negative empty refused", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := config.Default()
			opts.Worker.Token = "t"
			opts.APIKey.Secret = "s"
			opts.Jellyfin.DefaultRole = tc.role
			err := opts.Validate()
			if err == nil {
				t.Fatalf("opts.Validate() expected error for role %q, got nil", tc.role)
			}
			msg := err.Error()
			if !strings.Contains(msg, "snatcharr.jellyfin.default_role") {
				t.Errorf("error %q should mention config key snatcharr.jellyfin.default_role", msg)
			}
			if !strings.Contains(msg, "APP_SNATCHARR_JELLYFIN_DEFAULT_ROLE") {
				t.Errorf("error %q should mention env var APP_SNATCHARR_JELLYFIN_DEFAULT_ROLE", msg)
			}
		})
	}
}

func TestItemSearchCooldownConfig(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		val         int
		wantErr     bool
		errContains []string
	}{
		{name: "positive cooldown 60s", val: 60, wantErr: false},
		{name: "boundary cooldown 0s turns it off", val: 0, wantErr: false},
		{name: "boundary cooldown 3600s maximum", val: 3600, wantErr: false},
		{
			name:        "negative cooldown 3601s refused",
			val:         3601,
			wantErr:     true,
			errContains: []string{"snatcharr.item_search.cooldown_seconds", "APP_SNATCHARR_ITEM_SEARCH_COOLDOWN_SECONDS"},
		},
		{
			name:        "negative cooldown -1s refused",
			val:         -1,
			wantErr:     true,
			errContains: []string{"snatcharr.item_search.cooldown_seconds", "APP_SNATCHARR_ITEM_SEARCH_COOLDOWN_SECONDS"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := config.Default()
			opts.Worker.Token = "t"
			opts.APIKey.Secret = "s"
			opts.ItemSearch.CooldownSeconds = tc.val
			err := opts.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("opts.Validate() expected error for cooldown %d, got nil", tc.val)
				}
				for _, substr := range tc.errContains {
					if !strings.Contains(err.Error(), substr) {
						t.Errorf("error %q should contain %q", err.Error(), substr)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("opts.Validate() unexpected error: %v", err)
			}
		})
	}
}

func TestLoadItemSearchCooldownFromEnv(t *testing.T) {
	t.Run("positive env var sets cooldown", func(t *testing.T) {
		t.Setenv("APP_SNATCHARR_PROFILE", "dev")
		t.Setenv("APP_SNATCHARR_ITEM_SEARCH_COOLDOWN_SECONDS", "45")

		cfg, err := gconfig.New(gconfig.Options{EnvPrefix: "APP_"})
		if err != nil {
			t.Fatalf("gconfig.New: %v", err)
		}
		opts, err := config.Load(cfg)
		if err != nil {
			t.Fatalf("config.Load: %v", err)
		}
		if opts.ItemSearch.CooldownSeconds != 45 {
			t.Fatalf("opts.ItemSearch.CooldownSeconds = %d, want 45", opts.ItemSearch.CooldownSeconds)
		}
	})

	t.Run("positive default is 60 when unset", func(t *testing.T) {
		t.Setenv("APP_SNATCHARR_PROFILE", "dev")

		cfg, err := gconfig.New(gconfig.Options{EnvPrefix: "APP_"})
		if err != nil {
			t.Fatalf("gconfig.New: %v", err)
		}
		opts, err := config.Load(cfg)
		if err != nil {
			t.Fatalf("config.Load: %v", err)
		}
		if opts.ItemSearch.CooldownSeconds != config.DefaultItemSearchCooldownSeconds {
			t.Fatalf("opts.ItemSearch.CooldownSeconds = %d, want %d", opts.ItemSearch.CooldownSeconds, config.DefaultItemSearchCooldownSeconds)
		}
	})

	t.Run("negative env var 3601 refused", func(t *testing.T) {
		t.Setenv("APP_SNATCHARR_PROFILE", "dev")
		t.Setenv("APP_SNATCHARR_ITEM_SEARCH_COOLDOWN_SECONDS", "3601")

		cfg, err := gconfig.New(gconfig.Options{EnvPrefix: "APP_"})
		if err != nil {
			t.Fatalf("gconfig.New: %v", err)
		}
		_, err = config.Load(cfg)
		if err == nil {
			t.Fatal("config.Load expected error for cooldown 3601, got nil")
		}
		if !strings.Contains(err.Error(), "snatcharr.item_search.cooldown_seconds") {
			t.Errorf("error %q should mention snatcharr.item_search.cooldown_seconds", err.Error())
		}
		if !strings.Contains(err.Error(), "APP_SNATCHARR_ITEM_SEARCH_COOLDOWN_SECONDS") {
			t.Errorf("error %q should mention APP_SNATCHARR_ITEM_SEARCH_COOLDOWN_SECONDS", err.Error())
		}
	})
}
