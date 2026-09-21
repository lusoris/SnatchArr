// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package workergrpc

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TokenAuth checks the shared worker bearer token on every RPC. An empty token (dev
// profile) disables the check, which is logged once at start-up.
type TokenAuth struct {
	token string
}

// NewTokenAuth builds the interceptor pair.
func NewTokenAuth(token string, logger *slog.Logger) *TokenAuth {
	if token == "" {
		logger.Warn("workergrpc: no worker token configured; WorkerService is unauthenticated (dev only)")
	}
	return &TokenAuth{token: token}
}

func (a *TokenAuth) check(ctx context.Context) error {
	if a.token == "" {
		return nil
	}
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "missing metadata")
	}
	for _, v := range md.Get("authorization") {
		presented := strings.TrimPrefix(v, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(presented), []byte(a.token)) == 1 {
			return nil
		}
	}
	return status.Error(codes.Unauthenticated, "invalid worker token")
}

// Unary is the unary interceptor.
func (a *TokenAuth) Unary(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	if err := a.check(ctx); err != nil {
		return nil, err
	}
	return handler(ctx, req)
}

// Stream is the stream interceptor.
func (a *TokenAuth) Stream(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	if err := a.check(ss.Context()); err != nil {
		return err
	}
	return handler(srv, ss)
}
