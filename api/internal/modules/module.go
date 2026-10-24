// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package modules

import (
	"context"
	"time"

	"go.uber.org/fx"
)

const bootTimeout = 5 * time.Second

func registerLifecycle(lc fx.Lifecycle, svc *Service) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			bCtx, cancel := context.WithTimeout(ctx, bootTimeout)
			defer cancel()
			return svc.Ensure(bCtx)
		},
	})
}

// Module provides the Registry, Service, and boot-time module seeding.
var Module = fx.Module(
	"snatcharr.modules",
	fx.Provide(
		DefaultRegistry,
		New,
	),
	fx.Invoke(registerLifecycle),
)
