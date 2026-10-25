// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func scopesToOAS(in []string) []oas.ApiKeyScope {
	out := make([]oas.ApiKeyScope, len(in))
	for i, s := range in {
		out[i] = oas.ApiKeyScope(s)
	}
	return out
}

func scopesFromOAS(in []oas.ApiKeyScope) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = string(s)
	}
	return out
}

// ListAPIKeys returns all active API keys for the current session user.
func (h *Handlers) ListAPIKeys(ctx context.Context) (oas.ListAPIKeysRes, error) {
	u, ok := UserFrom(ctx)
	if !ok {
		return nil, domain.ErrUnauthorized
	}
	keys, err := h.apiKeys.List(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	out := make(oas.ListAPIKeysOKApplicationJSON, 0, len(keys))
	for _, k := range keys {
		item := oas.ApiKey{
			ID:        k.ID,
			Name:      k.Name,
			Scopes:    scopesToOAS(k.Scopes),
			CreatedAt: k.CreatedAt,
		}
		if k.ExpiresAt != nil {
			item.ExpiresAt = oas.NewOptDateTime(*k.ExpiresAt)
		}
		if k.LastUsedAt != nil {
			item.LastUsedAt = oas.NewOptDateTime(*k.LastUsedAt)
		}
		out = append(out, item)
	}
	return &out, nil
}

// CreateAPIKey generates a new API key for the current session user.
func (h *Handlers) CreateAPIKey(ctx context.Context, req *oas.CreateApiKeyRequest) (oas.CreateAPIKeyRes, error) {
	if err := requireRequester(ctx); err != nil {
		return nil, err
	}
	u, ok := UserFrom(ctx)
	if !ok {
		return nil, domain.ErrUnauthorized
	}
	var expiresAt *time.Time
	if req.ExpiresAt.IsSet() {
		exp := req.ExpiresAt.Value
		expiresAt = &exp
	}
	created, err := h.apiKeys.Create(ctx, u.ID, req.Name, expiresAt, scopesFromOAS(req.Scopes)...)
	if err != nil {
		return nil, err
	}
	out := &oas.CreatedApiKey{
		ID:        created.ID,
		Name:      created.Name,
		Scopes:    scopesToOAS(created.Scopes),
		CreatedAt: created.CreatedAt,
		Secret:    created.Secret,
	}
	if created.ExpiresAt != nil {
		out.ExpiresAt = oas.NewOptDateTime(*created.ExpiresAt)
	}
	if created.LastUsedAt != nil {
		out.LastUsedAt = oas.NewOptDateTime(*created.LastUsedAt)
	}
	return out, nil
}

// RevokeAPIKey marks an API key revoked.
func (h *Handlers) RevokeAPIKey(ctx context.Context, params oas.RevokeAPIKeyParams) (oas.RevokeAPIKeyRes, error) {
	if err := requireRequester(ctx); err != nil {
		return nil, err
	}
	u, ok := UserFrom(ctx)
	if !ok {
		return nil, domain.ErrUnauthorized
	}
	if err := h.apiKeys.Revoke(ctx, u.ID, params.ID); err != nil {
		return nil, err
	}
	return &oas.RevokeAPIKeyNoContent{}, nil
}
