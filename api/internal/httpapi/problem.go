// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/ogen-go/ogen/ogenerrors"

	"github.com/golusoris/golusoris/ogenkit"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/queue"
	"github.com/lusoris/SnatchArr/api/internal/sso"
)

// problemOptions maps errors to RFC 9457 problems; problem_contract_test.go pins every entry.
func problemOptions() []ogenkit.Option {
	return []ogenkit.Option{
		ogenkit.WithPathInstance(),
		ogenkit.WithDecodeDetail("malformed request"),
		ogenkit.WithInvalidParams(),
		ogenkit.WithValidationType("about:blank"),
		ogenkit.WithHTTPStatus(domain.ErrInvalid, http.StatusUnprocessableEntity, ""),
		ogenkit.WithHTTPStatus(queue.ErrInvalid, http.StatusUnprocessableEntity, ""),
		ogenkit.WithSecurityStatus(http.StatusUnauthorized, "authentication required"),
		ogenkit.WithHTTPStatus(domain.ErrUnauthorized, http.StatusUnauthorized, "authentication required"),
		ogenkit.WithHTTPStatus(sso.ErrProxySecret, http.StatusUnauthorized, "authentication required"),
		ogenkit.WithHTTPStatus(domain.ErrForbidden, http.StatusForbidden, ""),
		ogenkit.WithHTTPStatus(errCrossOrigin, http.StatusForbidden, "cross-origin request refused"),
		ogenkit.WithHTTPStatus(domain.ErrNotFound, http.StatusNotFound, ""),
		ogenkit.WithHTTPStatus(domain.ErrConflict, http.StatusConflict, ""),
		ogenkit.WithHTTPStatus(domain.ErrBadRequest, http.StatusBadRequest, ""),
		ogenkit.WithHTTPStatus(domain.ErrUnreachable, http.StatusBadGateway, ""),
		ogenkit.WithHTTPStatus(domain.ErrTooManyRequests, http.StatusTooManyRequests, "too many requests"),
		ogenkit.WithFallbackDetail("internal error"),
	}
}

var resolver = ogenkit.NewResolver(problemOptions()...)

// ErrorHandler renders every ogen-level or handler error as application/problem+json.
func ErrorHandler(logger *slog.Logger) ogenerrors.ErrorHandler {
	return ogenkit.ErrorHandlerWith(logger, problemOptions()...)
}
