// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package sso implements OIDC single sign-on via golusoris auth/oidc and trusted proxy-header authentication.
package sso

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/ogen-go/ogen/ogenerrors"

	"github.com/lusoris/SnatchArr/api/internal/config"
)

// ErrProxySecret is returned when a trusted reverse-proxy presents an invalid or missing shared secret.
var ErrProxySecret = errors.New("sso: proxy auth secret refused")

type (
	proxyUserKey   struct{}
	proxyGroupsKey struct{}
)

// WithProxyUser stores the verified proxy user in ctx.
func WithProxyUser(ctx context.Context, user string) context.Context {
	return context.WithValue(ctx, proxyUserKey{}, user)
}

// ProxyUserFromContext extracts the verified proxy user from ctx, if present.
func ProxyUserFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(proxyUserKey{}).(string)
	return v, ok && v != ""
}

// WithProxyGroups stores the verified proxy groups in ctx.
func WithProxyGroups(ctx context.Context, groups []string) context.Context {
	return context.WithValue(ctx, proxyGroupsKey{}, groups)
}

// ProxyGroupsFromContext extracts the verified proxy groups from ctx, if present.
func ProxyGroupsFromContext(ctx context.Context) ([]string, bool) {
	v, ok := ctx.Value(proxyGroupsKey{}).([]string)
	return v, ok
}

func stripProxyHeaders(r *http.Request, userHeader, groupsHeader, secretHeader string) (string, string, string) {
	rawUser := r.Header.Get(userHeader)
	rawGroups := r.Header.Get(groupsHeader)
	rawSecret := r.Header.Get(secretHeader)
	if rawUser != "" {
		r.Header.Del(userHeader)
	}
	if rawGroups != "" {
		r.Header.Del(groupsHeader)
	}
	r.Header.Del(secretHeader)
	return rawUser, rawGroups, rawSecret
}

func attachProxyContext(ctx context.Context, rawUser, rawGroups string) context.Context {
	if trimmedUser := strings.TrimSpace(rawUser); trimmedUser != "" {
		ctx = WithProxyUser(ctx, trimmedUser)
	}
	if rawGroups != "" {
		ctx = WithProxyGroups(ctx, parseGroupsHeader(rawGroups))
	}
	return ctx
}

// ProxyHeaderMiddleware extracts and verifies the trusted reverse-proxy user and groups headers.
// It is mounted in the HTTP pipeline before TrustProxy so it inspects the direct peer address.
func ProxyHeaderMiddleware(cfg config.ProxyAuthOptions, logger *slog.Logger, onErr ogenerrors.ErrorHandler) func(http.Handler) http.Handler {
	userHeader := cfg.Header
	if userHeader == "" {
		userHeader = "Remote-User"
	}
	groupsHeader := cfg.GroupsHeader
	if groupsHeader == "" {
		groupsHeader = "X-authentik-groups"
	}
	secretHeader := cfg.SecretHeader
	if secretHeader == "" {
		secretHeader = config.DefaultProxySecretHeader
	}
	nets := parseCIDRs(cfg.TrustedCIDRs)
	checkSecret := newSecretCheck(cfg.Secret)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawUser, rawGroups, rawSecret := stripProxyHeaders(r, userHeader, groupsHeader, secretHeader)
			if rawUser == "" && rawGroups == "" {
				next.ServeHTTP(w, r)
				return
			}
			if !cfg.Enabled || len(nets) == 0 || !isPeerTrusted(r.RemoteAddr, nets) {
				logger.DebugContext(r.Context(), "sso: proxy auth untrusted source", slog.String("reason", "untrusted_source"))
				next.ServeHTTP(w, r)
				return
			}
			if reason := checkSecret(rawSecret); reason != "" {
				logger.WarnContext(r.Context(), "sso: proxy auth secret refused", slog.String("reason", reason))
				onErr(r.Context(), w, r, ErrProxySecret)
				return
			}
			next.ServeHTTP(w, r.WithContext(attachProxyContext(r.Context(), rawUser, rawGroups)))
		})
	}
}

// newSecretCheck returns a check of the presented proxy secret: "" when it matches want, else
// the refusal reason. Both sides are hashed first, so the compare takes the same time whatever
// the presented length; an empty want refuses every request.
func newSecretCheck(want string) func(got string) string {
	wantDigest := sha256.Sum256([]byte(want))
	return func(got string) string {
		if got == "" {
			return "missing_secret"
		}
		gotDigest := sha256.Sum256([]byte(got))
		if want == "" || subtle.ConstantTimeCompare(gotDigest[:], wantDigest[:]) != 1 {
			return "wrong_secret"
		}
		return ""
	}
}

func parseGroupsHeader(raw string) []string {
	var groups []string
	for part := range strings.SplitSeq(raw, "|") {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			groups = append(groups, trimmed)
		}
	}
	return groups
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
