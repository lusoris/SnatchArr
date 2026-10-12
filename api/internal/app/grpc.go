// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package app

import (
	"go.uber.org/fx"

	gconfig "github.com/golusoris/golusoris/core/config"
	grpcx "github.com/golusoris/golusoris/grpc"

	"github.com/lusoris/SnatchArr/api/internal/workergrpc"
)

// grpcConfig makes the gRPC server's keys settable and safe for the worker's long RPCs. Both
// are root decorators: the golusoris config and grpc modules consume the decorated values.
var grpcConfig = fx.Options(
	fx.Decorate(withGRPCCompoundKeys),
	fx.Decorate(workergrpc.LeaseKeepalive),
)

// withGRPCCompoundKeys lets APP_GRPC_* reach the grpc.* keys whose names hold underscores
// (APP_GRPC_KEEPALIVE_MAX_CONNECTION_AGE_GRACE -> grpc.keepalive.max_connection_age_grace).
// Without it the loader splits every underscore and the setting never arrives.
func withGRPCCompoundKeys(o gconfig.Options) gconfig.Options {
	o.CompoundKeys = append(o.CompoundKeys, grpcx.CompoundKeys()...)
	return o
}
