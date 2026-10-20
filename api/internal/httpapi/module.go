// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/fx"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/httpx/middleware"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
)

// PathPrefix is where the OpenAPI server is mounted.
const PathPrefix = "/api/v1"

// MountParams are the dependencies Mount needs. The app calls Mount from its single
// route-registration invoke so chi sees every Use() before the first route.
type MountParams struct {
	fx.In
	Logger   *slog.Logger
	Clock    clock.Clock
	Handlers *Handlers
	Security *Security
	CSRF     middleware.Middleware
}

// Mount builds the ogen server and attaches it under PathPrefix with the request-context
// bridge and CSRF protection for cookie traffic.
func Mount(r chi.Router, p MountParams) error {
	srv, err := oas.NewServer(p.Handlers, p.Security,
		oas.WithPathPrefix(PathPrefix),
		oas.WithErrorHandler(ErrorHandler(p.Logger)),
	)
	if err != nil {
		return fmt.Errorf("httpapi: new server: %w", err)
	}
	r.With(withHTTP, csrfUnlessBearer(p.CSRF)).Handle(PathPrefix+"/*", srv)
	return nil
}

// StreamPath serves the Server-Sent Events stream. It lives outside the generated router.
const StreamPath = PathPrefix + "/events/stream"

// MountStream serves the event stream behind the session or API-key check of the
// OpenAPI operations.
func MountStream(r chi.Router, p MountParams, stream http.Handler) error {
	h, err := KeepAlive(stream, p.Clock, StreamKeepAliveInterval, p.Logger)
	if err != nil {
		return fmt.Errorf("httpapi: mount event stream: %w", err)
	}
	r.With(p.Security.RequireUser(p.Logger)).Handle(StreamPath, h)
	return nil
}

// LogStreamPath serves the live log tail. It lives outside the generated router.
const LogStreamPath = PathPrefix + "/logs/stream"

// MountLogStream serves the log tail to administrators only: the session or API-key
// check of MountStream, then the admin check of listLogs.
func MountLogStream(r chi.Router, p MountParams, stream http.Handler) error {
	h, err := KeepAlive(stream, p.Clock, StreamKeepAliveInterval, p.Logger)
	if err != nil {
		return fmt.Errorf("httpapi: mount log stream: %w", err)
	}
	r.With(p.Security.RequireUser(p.Logger), adminOnly(p.Logger)).Handle(LogStreamPath, h)
	return nil
}

// adminOnly answers a request whose attached user is not an administrator with the
// problem+json error of requireAdmin.
func adminOnly(logger *slog.Logger) func(http.Handler) http.Handler {
	onErr := ErrorHandler(logger)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if err := requireAdmin(req.Context()); err != nil {
				onErr(req.Context(), w, req, err)
				return
			}
			next.ServeHTTP(w, req)
		})
	}
}

// Module provides the handlers and security resolver. Mounting is the app's job.
var Module = fx.Module("snatcharr.httpapi",
	fx.Provide(NewHandlers, NewSecurity),
)
