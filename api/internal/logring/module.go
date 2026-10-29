// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package logring

import (
	"log/slog"

	"go.uber.org/fx"

	corelog "github.com/golusoris/golusoris/core/log"

	"github.com/lusoris/SnatchArr/api/internal/config"
)

// middlewareOrder places the ring inside the otel trace-ID middleware (order 0), so
// stored records carry trace_id.
const middlewareOrder = -10

// Middleware tees every record of the process logger into r.
func Middleware(r *Ring) corelog.HandlerMiddleware {
	return corelog.HandlerMiddleware{
		Name:  "snatcharr.logring",
		Order: middlewareOrder,
		Wrap:  func(next slog.Handler) slog.Handler { return r.Wrap(next, SourceAPI) },
	}
}

// Module provides the ring, sized by snatcharr.logs.ring_size, and hooks it into the
// golusoris logger.
var Module = fx.Module("snatcharr.logring",
	fx.Provide(func(o config.BaseOptions) *Ring { return New(o.Logs.RingSize) }),
	fx.Provide(corelog.AsMiddleware(Middleware)),
)
