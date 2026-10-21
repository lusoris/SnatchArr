// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/auth/apikey"
	"github.com/golusoris/golusoris/auth/session"
	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/auth"
	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// Security resolves cookie sessions and bearer API keys to users.
type Security struct {
	sessions *session.Manager
	ssoCfg   ssoConfig
	keys     *apikey.Service
	users    *auth.Service
	apiKeys  *auth.APIKeys
	clk      clock.Clock
	logger   *slog.Logger
}

// NewSecurity wires the handler.
func NewSecurity(sessions *session.Manager, cfg config.Options, keys *apikey.Service, users *auth.Service, apiKeys *auth.APIKeys, clk clock.Clock, logger *slog.Logger) *Security {
	return &Security{
		sessions: sessions,
		ssoCfg: ssoConfig{
			OIDC:      cfg.OIDC,
			ProxyAuth: cfg.ProxyAuth,
		},
		keys:    keys,
		users:   users,
		apiKeys: apiKeys,
		clk:     clk,
		logger:  logger,
	}
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
	raw, _ := sess.Get(auth.SessionUserKey).(string)
	userID, err := uuid.Parse(raw)
	if err != nil {
		return ctx, fmt.Errorf("%w: no user in session", domain.ErrUnauthorized)
	}
	u, err := s.users.User(ctx, userID)
	if err != nil {
		return ctx, fmt.Errorf("%w: %w", domain.ErrUnauthorized, err)
	}
	origin, hasSSO, ssoErr := auth.SSOOriginFrom(sess)
	if ssoErr != nil {
		source, _ := sess.Get(auth.SessionSSOSourceKey).(string)
		s.revokeSSOSession(ctx, sess.ID, "malformed_origin", source)
		return ctx, fmt.Errorf("%w: sso session revoked", domain.ErrUnauthorized)
	}
	if hasSSO {
		if reason := checkSSOOrigin(ctx, s.ssoCfg, origin); reason != "" {
			s.revokeSSOSession(ctx, sess.ID, reason, origin.Source)
			return ctx, fmt.Errorf("%w: sso session revoked", domain.ErrUnauthorized)
		}
	}
	return withUser(ctx, u), nil
}

func (s *Security) revokeSSOSession(ctx context.Context, sessID, reason, source string) {
	if revErr := s.users.RevokeSession(ctx, sessID); revErr != nil {
		if s.logger != nil {
			s.logger.WarnContext(ctx, "httpapi: revoke session failed",
				slog.String("reason", reason),
				slog.String("error", revErr.Error()),
			)
		}
	}
	if s.logger != nil {
		s.logger.WarnContext(ctx, "httpapi: sso session revoked",
			slog.String("reason", reason),
			slog.String("source", source),
		)
	}
}

// VerifyBearer verifies a raw bearer API key token, loads its owner user, and touches last_used_at.
func (s *Security) VerifyBearer(ctx context.Context, token string) (domain.User, apikey.Key, error) {
	key, err := s.keys.Verify(ctx, token)
	if err != nil {
		return domain.User{}, apikey.Key{}, fmt.Errorf("%w: invalid api key", domain.ErrUnauthorized)
	}
	ownerID, err := uuid.Parse(key.OwnerID)
	if err != nil {
		return domain.User{}, apikey.Key{}, fmt.Errorf("%w: bad key owner", domain.ErrUnauthorized)
	}
	u, err := s.users.User(ctx, ownerID)
	if err != nil {
		return domain.User{}, apikey.Key{}, fmt.Errorf("%w: %w", domain.ErrUnauthorized, err)
	}
	s.touchAPIKey(ctx, key.ID)
	return u, key, nil
}

// VerifyBearerRequest extracts and verifies a Bearer API key from r, ignoring session cookies.
func (s *Security) VerifyBearerRequest(ctx context.Context, r *http.Request) (domain.User, apikey.Key, error) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return domain.User{}, apikey.Key{}, fmt.Errorf("%w: bearer token required", domain.ErrUnauthorized)
	}
	return s.VerifyBearer(ctx, strings.TrimSpace(token))
}

// HandleBearerAuth verifies an API key and attaches its owner.
func (s *Security) HandleBearerAuth(ctx context.Context, _ oas.OperationName, t oas.BearerAuth) (context.Context, error) {
	u, _, err := s.VerifyBearer(ctx, t.Token)
	if err != nil {
		return ctx, err
	}
	return withUser(ctx, u), nil
}

func (s *Security) touchAPIKey(ctx context.Context, id string) {
	if s.apiKeys == nil || s.clk == nil {
		return
	}
	now := s.clk.Now()
	touchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := s.apiKeys.Touch(touchCtx, id, now); err != nil {
		if s.logger != nil {
			s.logger.WarnContext(touchCtx, "httpapi: touch api key failed",
				slog.String("id", id),
				slog.String("error", err.Error()),
			)
		}
	}
}

// RequireUser guards a route served outside the generated router with the same session
// or API-key check the OpenAPI operations get. A request without valid credentials gets a
// 401 problem+json and never reaches next.
func (s *Security) RequireUser(logger *slog.Logger) func(http.Handler) http.Handler {
	onErr := ErrorHandler(logger)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, err := s.authenticate(r)
			if err != nil {
				onErr(r.Context(), w, r, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(withUser(r.Context(), u)))
		})
	}
}

// authenticate resolves the user of a bearer API key when the request carries one, else
// of the session cookie. The scheme match is case-insensitive, as in the generated router.
func (s *Security) authenticate(r *http.Request) (domain.User, error) {
	ctx := context.WithValue(r.Context(), ctxKeyRequest, r)
	var err error
	if scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " "); ok && strings.EqualFold(scheme, "Bearer") {
		ctx, err = s.HandleBearerAuth(ctx, "", oas.BearerAuth{Token: token})
	} else {
		ctx, err = s.HandleCookieAuth(ctx, "", oas.CookieAuth{})
	}
	if err != nil {
		return domain.User{}, err
	}
	u, ok := UserFrom(ctx)
	if !ok {
		return domain.User{}, fmt.Errorf("%w: no user attached", domain.ErrUnauthorized)
	}
	return u, nil
}
