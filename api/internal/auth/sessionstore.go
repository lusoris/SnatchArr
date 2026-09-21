// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/golusoris/golusoris/core/clock"
	gerr "github.com/golusoris/golusoris/core/errors"

	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// PgSessionStore implements golusoris auth/session.Store on the sessions table.
type PgSessionStore struct {
	st  *store.Store
	clk clock.Clock
}

// NewPgSessionStore builds the store.
func NewPgSessionStore(st *store.Store, clk clock.Clock) *PgSessionStore {
	return &PgSessionStore{st: st, clk: clk}
}

// Load returns the session data or gerr.CodeNotFound when absent or expired.
func (p *PgSessionStore) Load(ctx context.Context, sessID string) (map[string]any, error) {
	raw, err := p.st.Q().LoadSession(ctx, sqlcgen.LoadSessionParams{ID: sessID, ExpiresAt: p.clk.Now()})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, gerr.NotFound("session not found")
	}
	if err != nil {
		return nil, fmt.Errorf("auth: load session: %w", err)
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("auth: decode session: %w", err)
	}
	return data, nil
}

// Save upserts the session with an absolute expiry.
func (p *PgSessionStore) Save(ctx context.Context, sessID string, data map[string]any, ttl time.Duration) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("auth: encode session: %w", err)
	}
	if err := p.st.Q().SaveSession(ctx, sqlcgen.SaveSessionParams{ID: sessID, Data: raw, ExpiresAt: p.clk.Now().Add(ttl)}); err != nil {
		return fmt.Errorf("auth: save session: %w", err)
	}
	return nil
}

// Delete removes a session; deleting an unknown id is not an error.
func (p *PgSessionStore) Delete(ctx context.Context, sessID string) error {
	if err := p.st.Q().DeleteSession(ctx, sessID); err != nil {
		return fmt.Errorf("auth: delete session: %w", err)
	}
	return nil
}

// PurgeExpired removes expired rows; meant for a periodic job.
func (p *PgSessionStore) PurgeExpired(ctx context.Context) (int64, error) {
	n, err := p.st.Q().PurgeExpiredSessions(ctx, p.clk.Now())
	if err != nil {
		return 0, fmt.Errorf("auth: purge sessions: %w", err)
	}
	return n, nil
}
