// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/lusoris/SnatchArr/api/internal/auth"
	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/jellyfin"
)

// JellyfinLogin authenticates a user using Jellyfin credentials.
func (h *Handlers) JellyfinLogin(ctx context.Context, req *oas.JellyfinLoginRequest) (oas.JellyfinLoginRes, error) {
	if !h.cfg.Jellyfin.Enabled {
		return nil, domain.ErrNotFound
	}
	ip := remoteIP(ctx)
	m, err := h.auth.LoginJellyfin(ctx, h.jellyfin, req.Username, req.Password, ip)
	if err != nil {
		if errors.Is(err, domain.ErrTooManyRequests) {
			if w, ok := writerFrom(ctx); ok {
				w.Header().Set("Retry-After", strconv.Itoa(int(auth.LockoutCooldown.Seconds())))
			}
		}
		return nil, err
	}
	return h.startJellyfinSession(ctx, m)
}

// JellyfinQuickConnectInitiate initiates a Quick Connect flow on Jellyfin.
func (h *Handlers) JellyfinQuickConnectInitiate(ctx context.Context) (*oas.JellyfinQuickConnect, error) {
	if !h.cfg.Jellyfin.Enabled {
		return nil, domain.ErrNotFound
	}
	enabled, err := h.jellyfin.QuickConnectEnabled(ctx)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, domain.ErrNotFound
	}
	code, secret, err := h.jellyfin.InitiateQuickConnect(ctx)
	if err != nil {
		return nil, err
	}
	return &oas.JellyfinQuickConnect{
		Code:   code,
		Secret: secret,
	}, nil
}

// JellyfinQuickConnectAuthenticate completes an authorized Quick Connect session.
func (h *Handlers) JellyfinQuickConnectAuthenticate(ctx context.Context, req *oas.JellyfinQuickConnectAuthenticateRequest) (*oas.Session, error) {
	if !h.cfg.Jellyfin.Enabled {
		return nil, domain.ErrNotFound
	}
	authorized, err := h.jellyfin.QuickConnectAuthorized(ctx, req.Secret)
	if err != nil {
		return nil, err
	}
	if !authorized {
		h.logger.InfoContext(ctx, "auth: jellyfin quick connect refused", slog.String("reason", "not_yet_authorized"))
		return nil, fmt.Errorf("%w: quick connect not yet authorized", domain.ErrConflict)
	}
	m, err := h.jellyfin.SignInQuickConnect(ctx, req.Secret)
	if err != nil {
		return nil, err
	}
	if m.Disabled {
		h.logger.InfoContext(ctx, "auth: jellyfin login refused", slog.String("reason", "user_disabled"), slog.String("subject", m.ID))
		return nil, fmt.Errorf("%w: user disabled", domain.ErrUnauthorized)
	}
	return h.startJellyfinSession(ctx, m)
}

func (h *Handlers) startJellyfinSession(ctx context.Context, m jellyfin.Member) (*oas.Session, error) {
	role := domain.RoleForJellyfinMember(m.Admin, h.cfg.Jellyfin.DefaultRole)
	origin := auth.SSOOrigin{
		Source:  "jellyfin",
		Subject: m.ID,
		Role:    role,
	}
	if role == domain.RoleAdmin {
		admin, err := h.auth.FirstAdmin(ctx)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				h.logger.WarnContext(ctx, "auth: jellyfin login refused", slog.String("reason", "no_admin_configured"), slog.String("subject", m.ID))
				return nil, fmt.Errorf("%w: no admin user configured", domain.ErrConflict)
			}
			return nil, err
		}
		return h.startSession(ctx, admin, &origin)
	}
	externalUser, err := h.auth.ProvisionJellyfinUser(ctx, m, role)
	if err != nil {
		return nil, err
	}
	return h.startSession(ctx, externalUser, &origin)
}
