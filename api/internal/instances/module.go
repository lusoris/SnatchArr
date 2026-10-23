// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package instances

import (
	"time"

	"go.uber.org/fx"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/leader/gate"

	"github.com/lusoris/SnatchArr/api/internal/metrics"
)

// CheckInterval is how often every enabled instance is checked.
const CheckInterval = 5 * time.Minute

// Ticker checks instance health once per CheckInterval while this replica leads.
type Ticker struct{ *gate.Periodic }

// NewTicker builds the instance check leader loop; m counts the passes that could not list the instances.
func NewTicker(svc *Service, clk clock.Clock, m *metrics.Metrics) *Ticker {
	return &Ticker{gate.NewPeriodic("instances: check", CheckInterval, clk, m, svc.CheckAll)}
}

// Module provides the Service, its check ticker, and registers it in the leader_loops fx group.
var Module = fx.Module("snatcharr.instances",
	fx.Provide(
		New,
		NewTicker,
	),
	fx.Provide(gate.Provide[*Ticker]()),
)
