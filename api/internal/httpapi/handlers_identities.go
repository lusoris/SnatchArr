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
	"github.com/lusoris/SnatchArr/api/internal/sso"
)

// ListIdentities lists linked external identities for the authenticated user.
func (h *Handlers) ListIdentities(ctx context.Context) (oas.ListIdentitiesRes, error) {
	u, ok := UserFrom(ctx)
	if !ok {
		return nil, domain.ErrUnauthorized
	}
	identities, err := h.auth.ListIdentities(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	items := make([]oas.UserIdentity, 0, len(identities))
	for _, id := range identities {
		items = append(items, oas.UserIdentity{
			Provider: oas.UserIdentityProvider(id.Provider),
			LinkedAt: id.CreatedAt,
		})
	}
	return &oas.UserIdentities{Items: items}, nil
}

// LinkJellyfinIdentity links a Jellyfin account to the authenticated user.
func (h *Handlers) LinkJellyfinIdentity(ctx context.Context, req *oas.JellyfinLoginRequest) (oas.LinkJellyfinIdentityRes, error) {
	if !h.cfg.Jellyfin.Enabled {
		return nil, domain.ErrNotFound
	}
	u, ok := UserFrom(ctx)
	if !ok {
		return nil, domain.ErrUnauthorized
	}
	ip := remoteIP(ctx)
	m, err := h.auth.LoginJellyfin(ctx, h.jellyfin, req.Username, req.Password, ip)
	if err != nil {
		if errors.Is(err, domain.ErrTooManyRequests) {
			if w, ok := writerFrom(ctx); ok {
				w.Header().Set("Retry-After", strconv.Itoa(int(auth.LockoutCooldown.Seconds())))
			}
		}
		if errors.Is(err, auth.ErrMemberDisabled) {
			return nil, fmt.Errorf("%w: member disabled", domain.ErrForbidden)
		}
		return nil, err
	}
	if m.Admin {
		h.logger.WarnContext(ctx, "auth: jellyfin identity link refused", slog.String("reason", "admin_identity"), slog.String("subject", m.ID))
		return nil, fmt.Errorf("%w: jellyfin administrators cannot be linked", domain.ErrConflict)
	}
	if err := h.auth.LinkIdentity(ctx, u.ID, domain.IdentityProviderJellyfin, m.ID); err != nil {
		return nil, err
	}
	return &oas.LinkJellyfinIdentityNoContent{}, nil
}

// LinkProxyIdentity links the current reverse proxy identity to the authenticated user.
func (h *Handlers) LinkProxyIdentity(ctx context.Context) (oas.LinkProxyIdentityRes, error) {
	if !h.cfg.ProxyAuth.Enabled {
		return nil, domain.ErrNotFound
	}
	u, ok := UserFrom(ctx)
	if !ok {
		return nil, domain.ErrUnauthorized
	}
	user, ok := sso.ProxyUserFromContext(ctx)
	if !ok || user == "" {
		return nil, domain.ErrUnauthorized
	}
	groups, _ := sso.ProxyGroupsFromContext(ctx)
	role, ok := sso.ResolveRole(groups, h.cfg.ProxyAuth.AdminGroups, h.cfg.ProxyAuth.RequesterGroups, h.cfg.ProxyAuth.ViewerGroups)
	if !ok {
		h.logger.WarnContext(ctx, "sso: proxy link refused",
			slog.String("reason", "not_authorized"),
			slog.String("subject", user),
			slog.Int("groups", len(groups)),
		)
		return nil, domain.ErrForbidden
	}
	if role == domain.RoleAdmin {
		h.logger.WarnContext(ctx, "sso: proxy link refused",
			slog.String("reason", "admin_identity"),
			slog.String("subject", user),
		)
		return nil, fmt.Errorf("%w: proxy administrators cannot be linked", domain.ErrConflict)
	}
	if err := h.auth.LinkIdentity(ctx, u.ID, domain.IdentityProviderProxy, user); err != nil {
		return nil, err
	}
	return &oas.LinkProxyIdentityNoContent{}, nil
}

// UnlinkJellyfinIdentity removes a linked Jellyfin identity from the authenticated user.
func (h *Handlers) UnlinkJellyfinIdentity(ctx context.Context) (oas.UnlinkJellyfinIdentityRes, error) {
	if err := h.unlinkIdentity(ctx, domain.IdentityProviderJellyfin); err != nil {
		return nil, err
	}
	return &oas.UnlinkJellyfinIdentityNoContent{}, nil
}

// UnlinkProxyIdentity removes a linked reverse proxy identity from the authenticated user.
func (h *Handlers) UnlinkProxyIdentity(ctx context.Context) (oas.UnlinkProxyIdentityRes, error) {
	if err := h.unlinkIdentity(ctx, domain.IdentityProviderProxy); err != nil {
		return nil, err
	}
	return &oas.UnlinkProxyIdentityNoContent{}, nil
}

func (h *Handlers) unlinkIdentity(ctx context.Context, provider string) error {
	u, ok := UserFrom(ctx)
	if !ok {
		return domain.ErrUnauthorized
	}
	return h.auth.UnlinkIdentity(ctx, u.ID, provider)
}
