// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package leaderx

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/golusoris/golusoris/core/clock"
)

// maxTicks bounds a periodic loop (HISS-02): 2^30 ticks at one a second is about 34 years.
const maxTicks = 1 << 30

// Periodic is the Loop every timer-driven leader job uses: it calls tick at once and then
// once per interval, each call under a deadline of one interval, until Stop. The gate may
// start it again after a stop, as leadership comes and goes.
type Periodic struct {
	name     string
	interval time.Duration
	clk      clock.Clock
	tick     func(ctx context.Context)

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// NewPeriodic builds a loop. name prefixes its errors; tick owns its own error handling.
func NewPeriodic(name string, interval time.Duration, clk clock.Clock, tick func(ctx context.Context)) *Periodic {
	return &Periodic{name: name, interval: interval, clk: clk, tick: tick}
}

// Start launches the loop; the first tick runs at once. It refuses a second start while the
// loop runs.
func (p *Periodic) Start(parent context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancel != nil {
		return fmt.Errorf("%s: loop is already running", p.name)
	}
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	p.cancel, p.done = cancel, make(chan struct{})
	go p.loop(ctx, p.done)
	return nil
}

// Stop ends the loop and waits for the tick in flight, at most until ctx ends. Stopping a
// loop that does not run is a no-op.
func (p *Periodic) Stop(ctx context.Context) error {
	p.mu.Lock()
	cancel, done := p.cancel, p.done
	p.cancel, p.done = nil, nil
	p.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("%s: stop: %w", p.name, ctx.Err())
	}
}

// Running reports whether the loop is started.
func (p *Periodic) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cancel != nil
}

func (p *Periodic) loop(ctx context.Context, done chan<- struct{}) {
	defer close(done)
	timer := p.clk.NewTicker(p.interval)
	defer timer.Stop()
	p.once(ctx)
	for range maxTicks {
		select {
		case <-ctx.Done():
			return
		case <-timer.Chan():
			p.once(ctx)
		}
	}
}

func (p *Periodic) once(ctx context.Context) {
	tctx, cancel := context.WithTimeout(ctx, p.interval)
	defer cancel()
	p.tick(tctx)
}
