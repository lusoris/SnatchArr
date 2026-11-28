// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"net/http"
	"time"
)

type cancelRuleWriteKey struct{}

type cancelRuleWriteFunc func() error

// WithNativeRulesWriteCancellation must be installed directly as the native
// http.Server.Handler. It captures cancellation for the rules GET before
// middleware can wrap the writer, without changing the writer passed downstream.
func WithNativeRulesWriteCancellation(next http.Handler) http.Handler {
	if next == nil {
		next = http.DefaultServeMux
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isRulesGET(r) {
			// Only inspect the writer supplied by net/http at this boundary.
			// Do not unwrap or discover deadline setters in downstream handlers.
			if native, ok := w.(interface{ SetWriteDeadline(time.Time) error }); ok {
				cancelWrite := cancelRuleWriteFunc(func() error {
					return native.SetWriteDeadline(time.Unix(1, 0))
				})
				r = r.WithContext(context.WithValue(r.Context(), cancelRuleWriteKey{}, cancelWrite))
			}
		}
		next.ServeHTTP(w, r)
	})
}
