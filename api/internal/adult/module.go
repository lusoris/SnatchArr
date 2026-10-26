// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult

import (
	"go.uber.org/fx"

	"github.com/golusoris/golusoris/leader/gate"
)

// Module provides the adult service, tickers, boot check, and registers tickers in leader_loops.
var Module = fx.Module("snatcharr.adult",
	fx.Provide(
		NewService,
		NewTicker,
		NewScanTicker,
	),
	fx.Invoke(registerBootCheck),
	// Sync and scan run on the elected leader replica only.
	fx.Provide(
		gate.Provide[*Ticker](),
		gate.Provide[*ScanTicker](),
	),
)
