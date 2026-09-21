// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/auth/apikey"
	"github.com/golusoris/golusoris/auth/session"

	"github.com/lusoris/SnatchArr/api/internal/auth"
	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// sessionUserKey is the session entry holding the user id.
const sessionUserKey = "user_id"

// Security resolves cookie sessions and bearer API keys to users.
type Security struct {
	sessions *session.Manager
	keys     *apikey.Service
	users    *auth.Service
}

// NewSecurity wires the handler.
func NewSecurity(sessions *session.Manager, keys *apikey.Service, users *auth.Service) *Security {
	return &Security{sessions: sessions, keys: keys, users: users}
}

// HandleCookieAuth loads the session named by the cookie and attaches its user.
func (s *Security) HandleCookieAuth(ctx context.Context, _ oas.OperationName, _ oas.CookieAuth) (context.Context, error) {
	r, ok := requestFrom(ctx)
	if !ok {
		return ctx, fmt.Errorf("%w: no request in context", domain.ErrUnauthorized)
	}
	sess, err := s.sessions.Load(r)
	if err != nil {
		return ctx, fmt.Errorf("%w: %w", domain.ErrUnauthorized, err)
	}
	raw, _ := sess.Get(sessionUserKey).(string)
	userID, err := uuid.Parse(raw)
	if err != nil {
		return ctx, fmt.Errorf("%w: no user in session", domain.ErrUnauthorized)
	}
	u, err := s.users.User(ctx, userID)
	if err != nil {
		return ctx, fmt.Errorf("%w: %w", domain.ErrUnauthorized, err)
	}
	return withUser(ctx, u), nil
}

// HandleBearerAuth verifies an API key and attaches its owner.
func (s *Security) HandleBearerAuth(ctx context.Context, _ oas.OperationName, t oas.BearerAuth) (context.Context, error) {
	key, err := s.keys.Verify(ctx, t.Token)
	if err != nil {
		return ctx, fmt.Errorf("%w: invalid api key", domain.ErrUnauthorized)
	}
	ownerID, err := uuid.Parse(key.OwnerID)
	if err != nil {
		return ctx, fmt.Errorf("%w: bad key owner", domain.ErrUnauthorized)
	}
	u, err := s.users.User(ctx, ownerID)
	if err != nil {
		return ctx, fmt.Errorf("%w: %w", domain.ErrUnauthorized, err)
	}
	return withUser(ctx, u), nil
}
