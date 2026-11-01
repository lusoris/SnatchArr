// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients

import (
	"time"

	"github.com/golusoris/golusoris/core/clock"
)

// MaxConcurrentObserve exports the concurrency bound for tests.
const MaxConcurrentObserve = maxConcurrentObserve

// DLClass exports dlClass for tests in dlclients_test.
type DLClass = dlClass

const (
	DLClassNone   = dlClassNone
	DLClassActive = dlClassActive
	DLClassQueued = dlClassQueued
)

var (
	ClassifyQbit         = classifyQbit
	ClassifyTransmission = classifyTransmission
	ClassifyDeluge       = classifyDeluge
	ClassifyRTorrent     = classifyRTorrent
	ClassifySAB          = classifySAB
	ClassifySABStatus    = classifySABStatus
	ClassifyNZBGet       = classifyNZBGet
	TransmissionClient   = transmissionClient
)

// NewGoenvoyWithTimeout builds the adapter set with a shorter observation bound, so a test can
// prove the bound without waiting ObserveTimeout per client.
func NewGoenvoyWithTimeout(clk clock.Clock, timeout time.Duration) *Goenvoy {
	g := NewGoenvoy(clk)
	g.timeout = timeout
	return g
}
