// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/auth"
	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/sso"
)

func TestCheckSSOOrigin_Table(t *testing.T) {
	t.Parallel()

	baseCfg := ssoConfig{
		OIDC: config.OIDCOptions{
			Enabled:      true,
			AdminGroups:  []string{"oidc-admins"},
			ViewerGroups: []string{"oidc-viewers", "SnatchArr Viewers"},
		},
		ProxyAuth: config.ProxyAuthOptions{
			Enabled:      true,
			AdminGroups:  []string{"proxy-admins"},
			ViewerGroups: []string{"proxy-viewers", "SnatchArr Viewers"},
		},
	}

	cases := []struct {
		name       string
		cfgMod     func(*ssoConfig)
		ctxMod     func(context.Context) context.Context
		origin     auth.SSOOrigin
		wantReason string
	}{
		{
			name: "oidc allowed",
			origin: auth.SSOOrigin{
				Source:  "oidc",
				Subject: "sub-1",
				Groups:  []string{"oidc-viewers"},
				Role:    domain.RoleViewer,
			},
			wantReason: "",
		},
		{
			name: "oidc group removed from config",
			cfgMod: func(c *ssoConfig) {
				c.OIDC.ViewerGroups = []string{"other-viewers"}
			},
			origin: auth.SSOOrigin{
				Source:  "oidc",
				Subject: "sub-1",
				Groups:  []string{"oidc-viewers"},
				Role:    domain.RoleViewer,
			},
			wantReason: "not_authorized",
		},
		{
			name: "oidc disabled",
			cfgMod: func(c *ssoConfig) {
				c.OIDC.Enabled = false
			},
			origin: auth.SSOOrigin{
				Source:  "oidc",
				Subject: "sub-1",
				Groups:  []string{"oidc-viewers"},
				Role:    domain.RoleViewer,
			},
			wantReason: "oidc_disabled",
		},
		{
			name: "role changed (stored viewer, groups now admin)",
			origin: auth.SSOOrigin{
				Source:  "oidc",
				Subject: "sub-1",
				Groups:  []string{"oidc-admins"},
				Role:    domain.RoleViewer,
			},
			wantReason: "role_changed",
		},
		{
			name: "proxy live groups still allowed",
			ctxMod: func(ctx context.Context) context.Context {
				ctx = sso.WithProxyUser(ctx, "proxy-user-1")
				return sso.WithProxyGroups(ctx, []string{"proxy-viewers"})
			},
			origin: auth.SSOOrigin{
				Source:  "proxy",
				Subject: "proxy-user-1",
				Groups:  []string{"old-group"},
				Role:    domain.RoleViewer,
			},
			wantReason: "",
		},
		{
			name: "proxy live groups lost (removed identity)",
			ctxMod: func(ctx context.Context) context.Context {
				ctx = sso.WithProxyUser(ctx, "proxy-user-1")
				return sso.WithProxyGroups(ctx, []string{"unauthorized-group"})
			},
			origin: auth.SSOOrigin{
				Source:  "proxy",
				Subject: "proxy-user-1",
				Groups:  []string{"proxy-viewers"},
				Role:    domain.RoleViewer,
			},
			wantReason: "not_authorized",
		},
		{
			name: "proxy subject changed",
			ctxMod: func(ctx context.Context) context.Context {
				return sso.WithProxyUser(ctx, "different-user")
			},
			origin: auth.SSOOrigin{
				Source:  "proxy",
				Subject: "proxy-user-1",
				Groups:  []string{"proxy-viewers"},
				Role:    domain.RoleViewer,
			},
			wantReason: "subject_changed",
		},
		{
			name: "proxy disabled",
			cfgMod: func(c *ssoConfig) {
				c.ProxyAuth.Enabled = false
			},
			origin: auth.SSOOrigin{
				Source:  "proxy",
				Subject: "proxy-user-1",
				Groups:  []string{"proxy-viewers"},
				Role:    domain.RoleViewer,
			},
			wantReason: "proxy_disabled",
		},
		{
			name: "proxy no live headers uses stored groups",
			origin: auth.SSOOrigin{
				Source:  "proxy",
				Subject: "proxy-user-1",
				Groups:  []string{"proxy-viewers"},
				Role:    domain.RoleViewer,
			},
			wantReason: "",
		},
		{
			name: "unknown source",
			origin: auth.SSOOrigin{
				Source:  "saml",
				Subject: "sub-1",
				Groups:  []string{"proxy-viewers"},
				Role:    domain.RoleViewer,
			},
			wantReason: "unknown_source",
		},
		{
			name: "boundary: group name with inner space SnatchArr Viewers matches exactly",
			origin: auth.SSOOrigin{
				Source:  "proxy",
				Subject: "proxy-user-1",
				Groups:  []string{"SnatchArr Viewers"},
				Role:    domain.RoleViewer,
			},
			wantReason: "",
		},
		{
			name: "case mismatch snatcharr viewers refused",
			origin: auth.SSOOrigin{
				Source:  "proxy",
				Subject: "proxy-user-1",
				Groups:  []string{"snatcharr viewers"},
				Role:    domain.RoleViewer,
			},
			wantReason: "not_authorized",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := baseCfg
			if tc.cfgMod != nil {
				tc.cfgMod(&cfg)
			}
			ctx := t.Context()
			if tc.ctxMod != nil {
				ctx = tc.ctxMod(ctx)
			}
			if reason := checkSSOOrigin(ctx, cfg, tc.origin); reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", reason, tc.wantReason)
			}
		})
	}
}
