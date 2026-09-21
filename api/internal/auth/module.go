// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package auth

import (
	"fmt"
	"log/slog"

	"go.uber.org/fx"

	"github.com/golusoris/golusoris/auth/apikey"
	"github.com/golusoris/golusoris/auth/session"
	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/config"
)

// devAPIKeySecret keeps `make dev` bootable without secrets. Never used in prod: config
// validation rejects an empty secret there.
const devAPIKeySecret = "snatcharr-dev-only-api-key-secret" // #nosec G101 -- dev-profile fallback only; prod config validation rejects an empty secret

// cookieName is fixed because the OpenAPI security scheme names it; the Secure flag still
// follows config (`__Host-` prefixed names cannot be set over plain-http dev).
const cookieName = "snatcharr_session"

func newSessionManager(st *PgSessionStore, cfg config.Options) *session.Manager {
	return session.NewManager(st, session.Options{
		CookieName: cookieName,
		TTL:        cfg.Session.TTL,
		Secure:     cfg.Session.Secure,
	})
}

func newAPIKeyService(st *PgAPIKeyStore, cfg config.Options, clk clock.Clock, logger *slog.Logger) (*apikey.Service, error) {
	secret := cfg.APIKey.Secret
	if secret == "" {
		if cfg.Profile != config.ProfileDev {
			return nil, fmt.Errorf("auth: %w: snatcharr.apikey.secret", config.ErrMissingSecret)
		}
		logger.Warn("auth: using built-in dev API key secret; set APP_SNATCHARR_APIKEY_SECRET")
		secret = devAPIKeySecret
	}
	svc, err := apikey.New(st, apikey.Options{Prefix: "sk", HMACSecret: []byte(secret), Clock: clk})
	if err != nil {
		return nil, fmt.Errorf("auth: api key service: %w", err)
	}
	return svc, nil
}

// Module provides the auth service, the Postgres-backed session manager and the API-key
// service.
var Module = fx.Module("snatcharr.auth",
	fx.Provide(NewService, NewPgSessionStore, NewPgAPIKeyStore, newSessionManager, newAPIKeyService),
)
