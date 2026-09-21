// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/golusoris/golusoris/auth/apikey"
	"github.com/golusoris/golusoris/core/clock"
	gerr "github.com/golusoris/golusoris/core/errors"

	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// PgAPIKeyStore implements golusoris auth/apikey.Store on the api_keys table.
type PgAPIKeyStore struct {
	st  *store.Store
	clk clock.Clock
}

// NewPgAPIKeyStore builds the store.
func NewPgAPIKeyStore(st *store.Store, clk clock.Clock) *PgAPIKeyStore {
	return &PgAPIKeyStore{st: st, clk: clk}
}

// Save persists a freshly issued key.
func (p *PgAPIKeyStore) Save(ctx context.Context, k apikey.Key) error {
	owner, err := uuid.Parse(k.OwnerID)
	if err != nil {
		return fmt.Errorf("auth: api key owner id: %w", err)
	}
	if err := p.st.Q().SaveAPIKey(ctx, sqlcgen.SaveAPIKeyParams{
		ID: k.ID, OwnerID: owner, Scopes: k.Scopes, Hash: k.Hash, CreatedAt: k.CreatedAt, ExpiresAt: k.ExpiresAt,
	}); err != nil {
		return fmt.Errorf("auth: save api key: %w", err)
	}
	return nil
}

// FindByID returns the key or gerr.CodeNotFound.
func (p *PgAPIKeyStore) FindByID(ctx context.Context, keyID string) (apikey.Key, error) {
	row, err := p.st.Q().GetAPIKey(ctx, keyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return apikey.Key{}, gerr.NotFound("api key not found")
	}
	if err != nil {
		return apikey.Key{}, fmt.Errorf("auth: get api key: %w", err)
	}
	return keyFromRow(row), nil
}

// Revoke marks the key revoked.
func (p *PgAPIKeyStore) Revoke(ctx context.Context, keyID string) error {
	now := p.clk.Now()
	n, err := p.st.Q().RevokeAPIKey(ctx, sqlcgen.RevokeAPIKeyParams{ID: keyID, RevokedAt: &now})
	if err != nil {
		return fmt.Errorf("auth: revoke api key: %w", err)
	}
	if n == 0 {
		return gerr.NotFound("api key not found")
	}
	return nil
}

// ListByOwner returns the owner's live keys.
func (p *PgAPIKeyStore) ListByOwner(ctx context.Context, ownerID string) ([]apikey.Key, error) {
	owner, err := uuid.Parse(ownerID)
	if err != nil {
		return nil, fmt.Errorf("auth: api key owner id: %w", err)
	}
	rows, err := p.st.Q().ListAPIKeysByOwner(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("auth: list api keys: %w", err)
	}
	out := make([]apikey.Key, 0, len(rows))
	for _, r := range rows {
		out = append(out, keyFromRow(r))
	}
	return out, nil
}

func keyFromRow(r sqlcgen.ApiKey) apikey.Key {
	return apikey.Key{
		ID: r.ID, OwnerID: r.OwnerID.String(), Scopes: r.Scopes, Hash: r.Hash,
		CreatedAt: r.CreatedAt, ExpiresAt: r.ExpiresAt, RevokedAt: r.RevokedAt,
	}
}
