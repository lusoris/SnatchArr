// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/golusoris/golusoris/httpx/middleware"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

type ctxKey int

const (
	ctxKeyWriter ctxKey = iota
	ctxKeyRequest
	ctxKeyUser
)

// withHTTP stores the raw writer and request in the context so ogen handlers (which
// only receive ctx) can set cookies and read the session cookie.
func withHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), ctxKeyWriter, w)
		ctx = context.WithValue(ctx, ctxKeyRequest, r)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func writerFrom(ctx context.Context) (http.ResponseWriter, bool) {
	w, ok := ctx.Value(ctxKeyWriter).(http.ResponseWriter)
	return w, ok
}

func requestFrom(ctx context.Context) (*http.Request, bool) {
	r, ok := ctx.Value(ctxKeyRequest).(*http.Request)
	return r, ok
}

func withUser(ctx context.Context, u domain.User) context.Context {
	return context.WithValue(ctx, ctxKeyUser, u)
}

// UserFrom returns the authenticated user, if any.
func UserFrom(ctx context.Context) (domain.User, bool) {
	u, ok := ctx.Value(ctxKeyUser).(domain.User)
	return u, ok
}

// csrfUnlessBearer applies the CSRF middleware to browser (cookie) traffic only. API-key
// callers are not subject to cross-site request forgery.
func csrfUnlessBearer(csrfMW middleware.Middleware) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		protected := csrfMW(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				next.ServeHTTP(w, r)
				return
			}
			protected.ServeHTTP(w, r)
		})
	}
}
