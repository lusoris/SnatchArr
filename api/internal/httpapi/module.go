// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"fmt"
	"log/slog"

	"github.com/go-chi/chi/v5"
	"go.uber.org/fx"

	"github.com/golusoris/golusoris/httpx/middleware"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
)

// PathPrefix is where the OpenAPI server is mounted.
const PathPrefix = "/api/v1"

type mountParams struct {
	fx.In
	Router   chi.Router
	Logger   *slog.Logger
	Handlers *Handlers
	Security *Security
	CSRF     middleware.Middleware
}

// Mount builds the ogen server and attaches it under PathPrefix with the request-context
// bridge and CSRF protection for cookie traffic.
func Mount(p mountParams) error {
	srv, err := oas.NewServer(p.Handlers, p.Security,
		oas.WithPathPrefix(PathPrefix),
		oas.WithErrorHandler(ErrorHandler(p.Logger)),
	)
	if err != nil {
		return fmt.Errorf("httpapi: new server: %w", err)
	}
	p.Router.With(withHTTP, csrfUnlessBearer(p.CSRF)).Handle(PathPrefix+"/*", srv)
	return nil
}

// Module provides the handlers and mounts the API.
var Module = fx.Module("snatcharr.httpapi",
	fx.Provide(NewHandlers, NewSecurity),
	fx.Invoke(Mount),
)
