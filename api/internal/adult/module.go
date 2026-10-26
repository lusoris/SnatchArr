// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult

import (
	"go.uber.org/fx"

	"github.com/golusoris/golusoris/leader/gate"
)

// Module provides the adult service, its ticker, and registers the ticker in the leader_loops fx group.
var Module = fx.Module("snatcharr.adult",
	fx.Provide(
		NewService,
		NewTicker,
	),
	// Sync runs on the elected leader replica only.
	fx.Provide(gate.Provide[*Ticker]()),
)
