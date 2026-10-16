// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package sso implements OIDC single sign-on and trusted proxy-header authentication.
// golusoris auth/oidc is not used because its AuthURL sends no nonce and Exchange discards the parsed ID token.
package sso

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/lusoris/SnatchArr/api/internal/config"
)

type proxyUserKey struct{}

// WithProxyUser stores the verified proxy user in ctx.
func WithProxyUser(ctx context.Context, user string) context.Context {
	return context.WithValue(ctx, proxyUserKey{}, user)
}

// ProxyUserFromContext extracts the verified proxy user from ctx, if present.
func ProxyUserFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(proxyUserKey{}).(string)
	return v, ok && v != ""
}

// ProxyHeaderMiddleware extracts and verifies the trusted reverse-proxy user header.
// It is mounted in the HTTP pipeline before TrustProxy so it inspects the direct peer address.
func ProxyHeaderMiddleware(cfg config.ProxyAuthOptions, logger *slog.Logger) func(http.Handler) http.Handler {
	headerName := cfg.Header
	if headerName == "" {
		headerName = "Remote-User"
	}
	nets := parseCIDRs(cfg.TrustedCIDRs)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawVal := r.Header.Get(headerName)
			if rawVal == "" {
				next.ServeHTTP(w, r)
				return
			}
			r.Header.Del(headerName)
			if !cfg.Enabled || len(nets) == 0 || !isPeerTrusted(r.RemoteAddr, nets) {
				logger.DebugContext(r.Context(), "sso: proxy auth untrusted source", slog.String("reason", "untrusted_source"))
				next.ServeHTTP(w, r)
				return
			}
			trimmed := strings.TrimSpace(rawVal)
			if trimmed == "" {
				next.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithProxyUser(r.Context(), trimmed)))
		})
	}
}

func isPeerTrusted(remoteAddr string, nets []*net.IPNet) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func parseCIDRs(cidrs []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err == nil {
			out = append(out, n)
		}
	}
	return out
}
