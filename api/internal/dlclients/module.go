// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients

import (
	"go.uber.org/fx"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/leader/gate"

	"github.com/lusoris/SnatchArr/api/internal/buildinfo"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
	"github.com/lusoris/SnatchArr/api/internal/useragent"
)

// Module provides the service, the goenvoy observer and the planner gate.
var Module = fx.Module("snatcharr.dlclients",
	fx.Provide(
		New,
		newReconcilerWithSource,
		func(clk clock.Clock, info buildinfo.Info, ua *useragent.Source) Snapshotter {
			return NewGoenvoyWithSource(clk, info, ua)
		},
		fx.Annotate(NewPlannerGate, fx.As(new(snatch.Gate)), fx.ResultTags(snatch.GateGroup)),
	),
	fx.Provide(gate.Provide[*Reconciler]()),
)
