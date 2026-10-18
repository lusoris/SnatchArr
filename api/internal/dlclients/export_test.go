// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients

import (
	"time"

	"github.com/golusoris/golusoris/core/clock"
)

// NewGoenvoyWithTimeout builds the adapter set with a shorter observation bound, so a test can
// prove the bound without waiting ObserveTimeout per client.
func NewGoenvoyWithTimeout(clk clock.Clock, timeout time.Duration) *Goenvoy {
	g := NewGoenvoy(clk)
	g.timeout = timeout
	return g
}
