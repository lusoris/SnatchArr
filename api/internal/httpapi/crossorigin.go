// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/golusoris/golusoris/httpx/middleware"
)

// CrossOrigin is the CSRF protection of cookie traffic: Go's same-origin check on the
// Sec-Fetch-Site and Origin headers, the algorithm golusoris httpx/csrf delegates to
// (filippo.io/csrf, which ignores token values). httpx/csrf turns into a no-op when
// http.csrf.secret is empty; this check has no secret and is never off. A refused request
// gets a 403 problem+json, as every other error does.
func CrossOrigin(logger *slog.Logger) middleware.Middleware {
	guard := http.NewCrossOriginProtection()
	guard.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeProblem(r.Context(), logger, w, problem{
			Type: "about:blank", Title: http.StatusText(http.StatusForbidden), Status: http.StatusForbidden,
			Detail: "cross-origin request refused", Instance: r.URL.Path,
		})
	}))
	return guard.Handler
}
