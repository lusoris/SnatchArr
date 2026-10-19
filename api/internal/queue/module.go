// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package queue

import (
	"time"

	"go.uber.org/fx"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/leader/gate"

	"github.com/lusoris/SnatchArr/api/internal/events"
	"github.com/lusoris/SnatchArr/api/internal/metrics"
)

// SweepInterval is how often every enabled instance queue is evaluated.
const SweepInterval = 10 * time.Minute

// Ticker runs the queue sweep leader loop once per SweepInterval while this replica leads.
type Ticker struct{ *gate.Periodic }

// NewTicker builds the queue sweep leader loop.
func NewTicker(svc *Service, clk clock.Clock, m *metrics.Metrics) *Ticker {
	return &Ticker{gate.NewPeriodic("queue_sweep", SweepInterval, clk, m, svc.SweepAll)}
}

// Module provides the queue store, service and the periodic sweep ticker.
var Module = fx.Module("snatcharr.queue",
	fx.Provide(
		NewStore,
		func(s *Store) StoreInterface { return s },
		NewDBInstanceSource,
		func(d *DBInstanceSource) InstanceSource { return d },
		NewService,
		NewTicker,
		func(b *events.Bus) EventPublisher { return b },
		func(m *metrics.Metrics) MetricsRecorder { return m },
	),
	fx.Provide(gate.Provide[*Ticker]()),
)
