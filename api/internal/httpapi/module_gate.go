// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"errors"
	"fmt"

	"github.com/ogen-go/ogen/middleware"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

type moduleChecker interface {
	IsEnabled(ctx context.Context, id string) (bool, error)
}

// ModuleGateMiddleware returns an ogen middleware that checks whether the optional module
// owning the requested operation is enabled. If disabled, it returns domain.ErrNotFound
// so the centralized error handler renders an RFC 9457 HTTP 404 Problem JSON response.
func ModuleGateMiddleware(ops map[string]string, svc moduleChecker) oas.Middleware {
	return func(req middleware.Request, next middleware.Next) (middleware.Response, error) {
		modID, ok := ops[req.OperationID]
		if !ok {
			return next(req)
		}
		if svc == nil {
			return middleware.Response{}, errors.New("module gate: module checker is nil")
		}
		enabled, err := svc.IsEnabled(req.Context, modID)
		if err != nil {
			return middleware.Response{}, fmt.Errorf("module gate %s: %w", modID, err)
		}
		if !enabled {
			return middleware.Response{}, fmt.Errorf("%w: module %s is disabled", domain.ErrNotFound, modID)
		}
		return next(req)
	}
}
