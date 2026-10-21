// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/auth/apikey"
	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// keyIssuer abstracts apikey.Service for unit testing.
type keyIssuer interface {
	Issue(ctx context.Context, ownerID string, scopes []string) (string, apikey.Key, error)
	Revoke(ctx context.Context, id string) error
}

// APIKeys handles API key issuance, listing, revocation, and last-used tracking.
type APIKeys struct {
	q      sqlcgen.Querier
	keys   keyIssuer
	clk    clock.Clock
	logger *slog.Logger
}

// NewAPIKeys constructs the API key manager.
func NewAPIKeys(st *store.Store, keys *apikey.Service, clk clock.Clock, logger *slog.Logger) *APIKeys {
	return &APIKeys{
		q:      st.Q(),
		keys:   keys,
		clk:    clk,
		logger: logger,
	}
}

func newAPIKeysWithQuerier(q sqlcgen.Querier, keys keyIssuer, clk clock.Clock, logger *slog.Logger) *APIKeys {
	return &APIKeys{
		q:      q,
		keys:   keys,
		clk:    clk,
		logger: logger,
	}
}

func validateCreateInput(clk clock.Clock, name string, expiresAt *time.Time) (string, error) {
	trimmed, err := domain.ValidateAPIKeyName(name)
	if err != nil {
		return "", err
	}
	if expiresAt != nil && !expiresAt.After(clk.Now()) {
		return "", fmt.Errorf("%w: expires_at must be in the future", domain.ErrBadRequest)
	}
	return trimmed, nil
}

func (a *APIKeys) ensureQuota(ctx context.Context, ownerID uuid.UUID) error {
	count, err := a.q.CountActiveAPIKeysByOwner(ctx, ownerID)
	if err != nil {
		return fmt.Errorf("auth: count api keys: %w", store.MapError(err))
	}
	if count >= domain.MaxKeysPerOwner {
		return fmt.Errorf("%w: owner reached limit of %d live api keys", domain.ErrConflict, domain.MaxKeysPerOwner)
	}
	return nil
}

func (a *APIKeys) setMetaOrRollback(ctx context.Context, issuedID string, ownerID uuid.UUID, name string, expiresAt *time.Time) error {
	n, err := a.q.SetAPIKeyMeta(ctx, sqlcgen.SetAPIKeyMetaParams{
		ID:        issuedID,
		OwnerID:   ownerID,
		Name:      name,
		ExpiresAt: expiresAt,
	})
	if err == nil && n > 0 {
		return nil
	}
	if revErr := a.keys.Revoke(ctx, issuedID); revErr != nil {
		a.logger.WarnContext(ctx, "auth: rollback revoke api key failed",
			slog.String("id", issuedID),
			slog.String("error", revErr.Error()),
		)
	}
	if err == nil {
		err = errors.New("zero rows updated")
	}
	return fmt.Errorf("auth: set api key metadata: %w", store.MapError(err))
}

// Create issues a new API key, validates input, ensures max live key quota, and persists metadata.
func (a *APIKeys) Create(ctx context.Context, ownerID uuid.UUID, name string, expiresAt *time.Time, scopes ...string) (domain.CreatedAPIKey, error) {
	trimmed, err := validateCreateInput(a.clk, name, expiresAt)
	if err != nil {
		return domain.CreatedAPIKey{}, err
	}
	cleanScopes, err := domain.ValidateAPIKeyScopes(scopes)
	if err != nil {
		return domain.CreatedAPIKey{}, err
	}
	if err = a.ensureQuota(ctx, ownerID); err != nil {
		return domain.CreatedAPIKey{}, err
	}
	raw, issued, err := a.keys.Issue(ctx, ownerID.String(), cleanScopes)
	if err != nil {
		return domain.CreatedAPIKey{}, fmt.Errorf("auth: issue api key: %w", err)
	}
	if err = a.setMetaOrRollback(ctx, issued.ID, ownerID, trimmed, expiresAt); err != nil {
		return domain.CreatedAPIKey{}, err
	}
	return domain.CreatedAPIKey{
		ID:         issued.ID,
		OwnerID:    ownerID,
		Name:       trimmed,
		Scopes:     cleanScopes,
		CreatedAt:  issued.CreatedAt,
		ExpiresAt:  expiresAt,
		LastUsedAt: nil,
		Secret:     raw,
	}, nil
}

// List returns all active API keys for the owner (including expired ones).
func (a *APIKeys) List(ctx context.Context, ownerID uuid.UUID) ([]domain.APIKey, error) {
	rows, err := a.q.ListAPIKeysForOwner(ctx, ownerID)
	if err != nil {
		return nil, fmt.Errorf("auth: list api keys: %w", store.MapError(err))
	}
	out := make([]domain.APIKey, 0, len(rows))
	for _, r := range rows {
		scopes := r.Scopes
		if scopes == nil {
			scopes = []string{}
		}
		out = append(out, domain.APIKey{
			ID:         r.ID,
			OwnerID:    r.OwnerID,
			Name:       r.Name,
			Scopes:     scopes,
			CreatedAt:  r.CreatedAt,
			ExpiresAt:  r.ExpiresAt,
			LastUsedAt: r.LastUsedAt,
		})
	}
	return out, nil
}

// Revoke marks the owner's API key revoked. Returns domain.ErrNotFound if not found or already revoked.
func (a *APIKeys) Revoke(ctx context.Context, ownerID uuid.UUID, id string) error {
	now := a.clk.Now()
	n, err := a.q.RevokeAPIKeyForOwner(ctx, sqlcgen.RevokeAPIKeyForOwnerParams{
		ID:        id,
		OwnerID:   ownerID,
		RevokedAt: &now,
	})
	if err != nil {
		return fmt.Errorf("auth: revoke api key: %w", store.MapError(err))
	}
	if n == 0 {
		return fmt.Errorf("%w: api key not found", domain.ErrNotFound)
	}
	return nil
}

// Touch updates last_used_at throttled to at most once per minute.
func (a *APIKeys) Touch(ctx context.Context, id string, now time.Time) error {
	if err := a.q.TouchAPIKey(ctx, sqlcgen.TouchAPIKeyParams{
		ID:  id,
		Now: now,
	}); err != nil {
		return fmt.Errorf("auth: touch api key: %w", store.MapError(err))
	}
	return nil
}
