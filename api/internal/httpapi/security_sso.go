// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"

	"github.com/lusoris/SnatchArr/api/internal/auth"
	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/sso"
)

type ssoConfig struct {
	OIDC      config.OIDCOptions
	ProxyAuth config.ProxyAuthOptions
	Jellyfin  config.JellyfinOptions
}

// ssoGroups returns the groups to resolve and the configured admin, requester and viewer groups for origin,
// or a non-empty refusal reason.
func ssoGroups(ctx context.Context, cfg ssoConfig, origin auth.SSOOrigin) (groups, admin, requester, viewer []string, reason string) {
	switch origin.Source {
	case "oidc":
		if !cfg.OIDC.Enabled {
			return nil, nil, nil, nil, "oidc_disabled"
		}
		return origin.Groups, cfg.OIDC.AdminGroups, cfg.OIDC.RequesterGroups, cfg.OIDC.ViewerGroups, ""
	case "proxy":
		if !cfg.ProxyAuth.Enabled {
			return nil, nil, nil, nil, "proxy_disabled"
		}
		liveUser, ok := sso.ProxyUserFromContext(ctx)
		if !ok {
			// No trusted proxy headers on this request: judge the groups stored at sign-in.
			return origin.Groups, cfg.ProxyAuth.AdminGroups, cfg.ProxyAuth.RequesterGroups, cfg.ProxyAuth.ViewerGroups, ""
		}
		if liveUser != origin.Subject {
			return nil, nil, nil, nil, "subject_changed"
		}
		liveGroups, _ := sso.ProxyGroupsFromContext(ctx)
		return liveGroups, cfg.ProxyAuth.AdminGroups, cfg.ProxyAuth.RequesterGroups, cfg.ProxyAuth.ViewerGroups, ""
	default:
		return nil, nil, nil, nil, "unknown_source"
	}
}

// checkSSOOrigin re-resolves the role of a single sign-on session against the current
// configuration. It returns "" when the session still holds its role, else the refusal reason.
func checkSSOOrigin(ctx context.Context, cfg ssoConfig, origin auth.SSOOrigin) string {
	if origin.Source == "jellyfin" {
		if !cfg.Jellyfin.Enabled {
			return "jellyfin_disabled"
		}
		return ""
	}
	groups, admin, requester, viewer, reason := ssoGroups(ctx, cfg, origin)
	if reason != "" {
		return reason
	}
	role, ok := sso.ResolveRole(groups, admin, requester, viewer)
	if !ok {
		return "not_authorized"
	}
	if role != origin.Role {
		return "role_changed"
	}
	return ""
}
