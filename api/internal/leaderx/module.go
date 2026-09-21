// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package leaderx

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/config"
	"github.com/golusoris/golusoris/leader"
	pgleader "github.com/golusoris/golusoris/leader/pg"
)

// DefaultName is the advisory-lock name when leader.name (APP_LEADER_NAME) is empty.
const DefaultName = "snatcharr"

// Group is the fx value group loops register in.
const Group = "leader_loops"

// loadOptions reads golusoris' leader/pg options from the "leader" config key
// (APP_LEADER_ENABLED, APP_LEADER_NAME, APP_LEADER_IDENTITY, APP_LEADER_PG_RETRY).
func loadOptions(cfg *config.Config) (pgleader.Options, error) {
	opts := pgleader.DefaultOptions()
	if err := cfg.Unmarshal("leader", &opts); err != nil {
		return pgleader.Options{}, fmt.Errorf("leaderx: load options: %w", err)
	}
	if opts.Name == "" {
		opts.Name = DefaultName
	}
	return opts, nil
}

// Params collects the registered loops and the elector's dependencies.
type Params struct {
	fx.In

	Loops  []Loop `group:"leader_loops"`
	Pool   *pgxpool.Pool
	Clock  clock.Clock
	Logger *slog.Logger
	Opts   pgleader.Options
}

func newGate(p Params) *Gate { return New(p.Clock, p.Logger, p.Loops...) }

func electorFor(p Params) Elect {
	if !p.Opts.Enabled {
		return nil
	}
	p.Logger.Info("leader: election enabled",
		slog.String("name", p.Opts.Name), slog.String("identity", p.Opts.Identity))
	return func(ctx context.Context, cb leader.Callbacks) error {
		if err := pgleader.Run(ctx, p.Pool, p.Opts, p.Clock, cb); err != nil {
			return fmt.Errorf("leaderx: %w", err)
		}
		return nil
	}
}

// run ties the gate to the fx lifecycle: it runs for the whole process and stops with it.
func run(lc fx.Lifecycle, g *Gate, p Params) {
	elect := electorFor(p)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go func() {
				defer close(done)
				if err := g.Run(ctx, elect, p.Opts.PG.Retry); err != nil {
					p.Logger.Error("leader: gate stopped", slog.String("error", err.Error()))
				}
			}()
			return nil
		},
		OnStop: func(stopCtx context.Context) error {
			cancel()
			select {
			case <-done:
				return nil
			case <-stopCtx.Done():
				return fmt.Errorf("leaderx: stop: %w", stopCtx.Err())
			}
		},
	})
}

// Module provides the gate and runs it for the process lifetime.
var Module = fx.Module("snatcharr.leader",
	fx.Provide(loadOptions, newGate),
	fx.Invoke(run),
)

// Provide registers an already-provided *T as a leader-gated loop:
//
//	fx.Provide(NewTicker, leaderx.Provide[*Ticker]())
func Provide[T Loop]() any {
	return fx.Annotate(asLoop[T], fx.ResultTags(`group:"leader_loops"`))
}

func asLoop[T Loop](l T) Loop { return l }
