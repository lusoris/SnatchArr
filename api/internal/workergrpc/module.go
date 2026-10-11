// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package workergrpc

import (
	"log/slog"

	"go.uber.org/fx"
	"google.golang.org/grpc"

	grpcx "github.com/golusoris/golusoris/grpc"

	"github.com/lusoris/SnatchArr/api/internal/config"
	snatcharrv1 "github.com/lusoris/SnatchArr/api/internal/gen/snatcharr/v1"
)

func newTokenAuth(cfg config.Options, logger *slog.Logger) *TokenAuth {
	return NewTokenAuth(cfg.Worker.Token, logger)
}

// unaryAuth chains the token check. It also takes the service, which it does not use: fx stops
// components in reverse construction order, and this dependency has fx build the service (and
// with it the stores and the database pool) before the gRPC server. The gRPC server therefore
// stops first, and no call is served on a closed pool (#146).
func unaryAuth(a *TokenAuth, _ *Server) grpc.ServerOption {
	return grpc.ChainUnaryInterceptor(a.Unary)
}

// register puts WorkerService on the gRPC server. Its stop hook is appended after the gRPC
// server's own, so it runs before the graceful stop and ends the waiting leases first.
func register(lc fx.Lifecycle, s *grpc.Server, srv *Server) {
	snatcharrv1.RegisterWorkerServiceServer(s, srv)
	lc.Append(fx.StopHook(srv.Shutdown))
}

// Module registers WorkerService on the golusoris gRPC server with token auth.
var Module = fx.Module("snatcharr.workergrpc",
	fx.Provide(New, newTokenAuth),
	grpcx.ProvideServerOptionFn(unaryAuth),
	grpcx.ProvideServerOptionFn(func(a *TokenAuth) grpc.ServerOption {
		return grpc.ChainStreamInterceptor(a.Stream)
	}),
	fx.Invoke(register),
)
