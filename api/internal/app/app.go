// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package app composes the golusoris modules and SnatchArr's own into one fx graph.
package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	"github.com/golusoris/golusoris"
	"github.com/golusoris/golusoris/core/clock"
	dbmigrate "github.com/golusoris/golusoris/db/migrate"
	"github.com/golusoris/golusoris/httpx/csrf"
	"github.com/golusoris/golusoris/httpx/middleware"
	"github.com/golusoris/golusoris/k8s/health"
	"github.com/golusoris/golusoris/k8s/metrics/prom"
	"github.com/golusoris/golusoris/observability/statuspage"
	"github.com/golusoris/golusoris/otel"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/auth"
	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/events"
	"github.com/lusoris/SnatchArr/api/internal/httpapi"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/policies"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/webui"
)

// Module is the complete SnatchArr control plane. Order matters for fx.Invoke: the base
// middleware and probes mount first, then the OpenAPI server, then the SPA catch-all.
var Module = fx.Options(
	golusoris.Core,
	golusoris.DB,
	golusoris.HTTP,
	otel.Module,
	csrf.Module,
	fx.Decorate(embedMigrations),
	config.Module,
	store.Module,
	arrclient.Module,
	instances.Module,
	policies.Module,
	auth.Module,
	events.Module,
	fx.Provide(statuspage.NewRegistry, health.NewStartupGate),
	fx.Invoke(registerHealthChecks),
	fx.Invoke(mountBase),
	httpapi.Module,
	fx.Invoke(mountWebUI),
)

// embedMigrations points golusoris db/migrate at the embedded SQL and runs it on start.
func embedMigrations(o dbmigrate.Options) (dbmigrate.Options, error) {
	fsys, err := store.MigrationsFS()
	if err != nil {
		return dbmigrate.Options{}, fmt.Errorf("app: %w", err)
	}
	o.Auto = true
	return o.WithFS(fsys), nil
}

func registerHealthChecks(reg *statuspage.Registry, pool *pgxpool.Pool, gate *health.StartupGate, lc fx.Lifecycle) {
	reg.Register(statuspage.Check{
		Name: "process", Tags: []string{health.TagLiveness},
		Fn: func(context.Context) error { return nil },
	})
	reg.Register(statuspage.Check{
		Name: "postgres", Tags: []string{health.TagReadiness},
		Fn: func(ctx context.Context) error {
			if err := pool.Ping(ctx); err != nil {
				return fmt.Errorf("postgres ping: %w", err)
			}
			return nil
		},
	})
	reg.Register(gate.Check("startup"))
	lc.Append(fx.Hook{OnStart: func(context.Context) error {
		gate.MarkComplete()
		return nil
	}})
}

type httpDeps struct {
	fx.In
	Router chi.Router
	Logger *slog.Logger
	Clock  clock.Clock
	Reg    *statuspage.Registry
	Events *events.Bus
	Cfg    config.Options
}

func mountBase(d httpDeps) error {
	d.Router.Use(
		middleware.RequestID,
		middleware.Recover(d.Logger),
		middleware.Logger(d.Logger, d.Clock),
		middleware.SecureHeaders(middleware.SecureHeadersDefaults()),
	)
	health.Mount(d.Router, d.Reg)
	if err := prom.Mount(d.Router, d.Reg); err != nil {
		return fmt.Errorf("app: mount metrics: %w", err)
	}
	d.Router.Handle("/api/v1/events/stream", d.Events.Hub().Handler())
	return nil
}

func mountWebUI(d httpDeps) error {
	if d.Cfg.Web.Dev {
		d.Logger.Info("app: embedded web UI disabled (snatcharr.web.dev=true)")
		return nil
	}
	fsys, err := webui.FS()
	if err != nil {
		return fmt.Errorf("app: web ui: %w", err)
	}
	webui.Mount(d.Router, fsys)
	return nil
}
