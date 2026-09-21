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

// Module registers WorkerService on the golusoris gRPC server with token auth.
var Module = fx.Module("snatcharr.workergrpc",
	fx.Provide(New, newTokenAuth),
	grpcx.ProvideServerOptionFn(func(a *TokenAuth) grpc.ServerOption {
		return grpc.ChainUnaryInterceptor(a.Unary)
	}),
	grpcx.ProvideServerOptionFn(func(a *TokenAuth) grpc.ServerOption {
		return grpc.ChainStreamInterceptor(a.Stream)
	}),
	fx.Invoke(func(s *grpc.Server, srv *Server) {
		snatcharrv1.RegisterWorkerServiceServer(s, srv)
	}),
)
