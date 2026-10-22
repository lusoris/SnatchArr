// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/golusoris/golusoris/httpx/middleware"
)

var errCrossOrigin = errors.New("cross-origin request refused")

// CrossOrigin is the CSRF protection of cookie traffic: Go's same-origin check on the
// Sec-Fetch-Site and Origin headers, the algorithm golusoris httpx/csrf delegates to
// (filippo.io/csrf, which ignores token values). httpx/csrf turns into a no-op when
// http.csrf.secret is empty; this check has no secret and is never off. A refused request
// gets a 403 problem+json, as every other error does.
func CrossOrigin(logger *slog.Logger) middleware.Middleware {
	guard := http.NewCrossOriginProtection()
	onErr := ErrorHandler(logger)
	guard.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		onErr(r.Context(), w, r, errCrossOrigin)
	}))
	return guard.Handler
}
