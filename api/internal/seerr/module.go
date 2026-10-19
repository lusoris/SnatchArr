// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package seerr

import (
	"time"

	"go.uber.org/fx"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/leader/gate"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/metrics"
	"github.com/lusoris/SnatchArr/api/internal/seerrclient"
)

// SyncInterval is how often every enabled link is refreshed.
const SyncInterval = 5 * time.Minute

// Ticker refreshes the request cache once per SyncInterval while this replica leads.
type Ticker struct{ *gate.Periodic }

// NewTicker builds the sync's leader loop; m counts the passes that could not list the links.
func NewTicker(svc *Service, clk clock.Clock, m *metrics.Metrics) *Ticker {
	return &Ticker{gate.NewPeriodic("seerr: sync", SyncInterval, clk, m, svc.SyncAll)}
}

// Module provides the Seerr service, its HTTP client and the sync ticker.
var Module = fx.Module("snatcharr.seerr",
	fx.Provide(
		New,
		NewTicker,
		func() seerrclient.Client {
			o := arrclient.DefaultOptions()
			return seerrclient.New(o.Timeout, o.UserAgent)
		},
		func(p *arrclient.GoenvoyProber) arrclient.Resolver { return p },
	),
	// The sync runs on the leader only (leader/gate).
	fx.Provide(gate.Provide[*Ticker]()),
)
