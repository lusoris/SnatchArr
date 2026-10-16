// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package notify

import (
	"go.uber.org/fx"

	"github.com/lusoris/SnatchArr/api/internal/snatch"
)

// Module provides notification service, webhook store, and background engine.
var Module = fx.Module("snatcharr.notify",
	fx.Provide(
		NewWebhookStore,
		NewService,
		NewEngine,
		fx.Annotate(func(e *Engine) snatch.EventObserver { return e }, fx.ResultTags(snatch.ObserverGroup)),
	),
	fx.Invoke(registerLifecycle),
)

func registerLifecycle(lc fx.Lifecycle, engine *Engine, rec *snatch.Recorder) {
	engine.SetRecorder(rec)
	lc.Append(fx.Hook{
		OnStart: engine.Start,
		OnStop:  engine.Stop,
	})
}
