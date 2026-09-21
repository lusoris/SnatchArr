// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/ogen-go/ogen/ogenerrors"
	"github.com/ogen-go/ogen/validate"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// problem is the RFC 9457 body. `invalid-params` follows the common extension used by
// sveltesentio's problemToFieldErrors.
type problem struct {
	Type          string         `json:"type"`
	Title         string         `json:"title"`
	Status        int            `json:"status"`
	Detail        string         `json:"detail,omitempty"`
	Instance      string         `json:"instance,omitempty"`
	InvalidParams []invalidParam `json:"invalid-params,omitempty"` //nolint:tagliatelle // RFC 9457 extension member name is hyphenated
}

type invalidParam struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// statusOf maps errors from every layer to an HTTP status and a public detail.
func statusOf(err error) (status int, detail string, params []invalidParam) {
	var secErr *ogenerrors.SecurityError
	var decErr *ogenerrors.DecodeRequestError
	var paramErr *ogenerrors.DecodeParamsError
	var valErr *validate.Error
	switch {
	case errors.As(err, &secErr), errors.Is(err, domain.ErrUnauthorized):
		return http.StatusUnauthorized, "authentication required", nil
	case errors.Is(err, domain.ErrForbidden):
		return http.StatusForbidden, err.Error(), nil
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound, err.Error(), nil
	case errors.Is(err, domain.ErrConflict):
		return http.StatusConflict, err.Error(), nil
	case errors.As(err, &valErr):
		return http.StatusUnprocessableEntity, "request validation failed", validationParams(valErr)
	case errors.Is(err, domain.ErrInvalid):
		return http.StatusUnprocessableEntity, err.Error(), nil
	case errors.As(err, &decErr), errors.As(err, &paramErr):
		return http.StatusBadRequest, "malformed request", nil
	case errors.Is(err, domain.ErrUnreachable):
		return http.StatusBadGateway, err.Error(), nil
	default:
		return http.StatusInternalServerError, "internal error", nil
	}
}

func validationParams(v *validate.Error) []invalidParam {
	out := make([]invalidParam, 0, len(v.Fields))
	for _, f := range v.Fields {
		out = append(out, invalidParam{Name: f.Name, Reason: f.Error.Error()})
	}
	return out
}

// ErrorHandler renders every ogen-level or handler error as application/problem+json.
func ErrorHandler(logger *slog.Logger) ogenerrors.ErrorHandler {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, err error) {
		status, detail, params := statusOf(err)
		if status >= http.StatusInternalServerError {
			logger.ErrorContext(ctx, "httpapi: unhandled error", slog.String("path", r.URL.Path), slog.String("error", err.Error()))
		}
		writeProblem(ctx, logger, w, problem{
			Type: "about:blank", Title: http.StatusText(status), Status: status,
			Detail: detail, Instance: r.URL.Path, InvalidParams: params,
		})
	}
}

func writeProblem(ctx context.Context, logger *slog.Logger, w http.ResponseWriter, p problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(p.Status)
	if err := json.NewEncoder(w).Encode(p); err != nil {
		logger.WarnContext(ctx, "httpapi: write problem body", slog.String("error", err.Error()))
	}
}
