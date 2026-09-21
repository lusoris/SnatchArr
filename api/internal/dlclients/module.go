// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients

import (
	"go.uber.org/fx"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/snatch"
)

// Module provides the service, the goenvoy observer and the planner gate.
var Module = fx.Module("snatcharr.dlclients",
	fx.Provide(
		New,
		func(clk clock.Clock) Snapshotter { return NewGoenvoy(clk) },
		fx.Annotate(NewPlannerGate, fx.As(new(snatch.Gate)), fx.ResultTags(snatch.GateGroup)),
	),
)
