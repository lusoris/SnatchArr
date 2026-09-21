// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package seerr

import (
	"context"
	"log/slog"
	"time"

	"go.uber.org/fx"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/leaderx"
	"github.com/lusoris/SnatchArr/api/internal/seerrclient"
)

// SyncInterval is how often every enabled link is refreshed.
const SyncInterval = 5 * time.Minute

// maxSyncTicks bounds the loop (HISS-02): 2^28 five-minute ticks is about 2500 years.
const maxSyncTicks = 1 << 28

// Ticker refreshes the request cache on a fixed interval.
type Ticker struct {
	svc    *Service
	clk    clock.Clock
	logger *slog.Logger
	cancel context.CancelFunc
	done   chan struct{}
}

// NewTicker builds the ticker.
func NewTicker(svc *Service, clk clock.Clock, logger *slog.Logger) *Ticker {
	return &Ticker{svc: svc, clk: clk, logger: logger}
}

// Start launches the loop.
func (t *Ticker) Start(parent context.Context) error {
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	t.cancel = cancel
	t.done = make(chan struct{})
	go t.loop(ctx)
	return nil
}

// Stop ends the loop and waits for the in-flight sync.
func (t *Ticker) Stop(ctx context.Context) error {
	if t.cancel != nil {
		t.cancel()
	}
	select {
	case <-t.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (t *Ticker) loop(ctx context.Context) {
	defer close(t.done)
	timer := t.clk.NewTicker(SyncInterval)
	defer timer.Stop()
	t.tick(ctx)
	for range maxSyncTicks {
		select {
		case <-ctx.Done():
			return
		case <-timer.Chan():
			t.tick(ctx)
		}
	}
}

func (t *Ticker) tick(ctx context.Context) {
	tctx, cancel := context.WithTimeout(ctx, SyncInterval)
	defer cancel()
	t.svc.SyncAll(tctx)
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
	// The sync runs on the leader only (internal/leaderx).
	fx.Provide(leaderx.Provide[*Ticker]()),
)
