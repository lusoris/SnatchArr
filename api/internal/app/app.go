// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package app composes the golusoris modules and SnatchArr's own into one fx graph.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	"github.com/golusoris/golusoris"
	"github.com/golusoris/golusoris/core/clock"
	dbmigrate "github.com/golusoris/golusoris/db/migrate"
	grpcx "github.com/golusoris/golusoris/grpc"
	"github.com/golusoris/golusoris/httpx/csrf"
	"github.com/golusoris/golusoris/httpx/middleware"
	"github.com/golusoris/golusoris/k8s/health"
	"github.com/golusoris/golusoris/k8s/metrics/prom"
	"github.com/golusoris/golusoris/observability/statuspage"
	"github.com/golusoris/golusoris/otel"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/auth"
	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/dlclients"
	"github.com/lusoris/SnatchArr/api/internal/events"
	"github.com/lusoris/SnatchArr/api/internal/httpapi"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/policies"
	"github.com/lusoris/SnatchArr/api/internal/settings"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/webui"
	"github.com/lusoris/SnatchArr/api/internal/workergrpc"
)

// Module is the complete SnatchArr control plane. Order matters for fx.Invoke: the base
// middleware and probes mount first, then the OpenAPI server, then the SPA catch-all.
var Module = fx.Options(
	golusoris.Core,
	golusoris.DB,
	golusoris.HTTP,
	otel.Module,
	csrf.Module,
	grpcx.Module,
	fx.Decorate(embedMigrations),
	config.Module,
	store.Module,
	arrclient.Module,
	instances.Module,
	policies.Module,
	settings.Module,
	auth.Module,
	events.Module,
	snatch.Module,
	dlclients.Module,
	fx.Provide(func(s *dlclients.Service) workergrpc.Pacer { return s }),
	workergrpc.Module,
	httpapi.Module,
	fx.Provide(statuspage.NewRegistry, health.NewStartupGate),
	fx.Invoke(registerHealthChecks),
	// fx constructs providers lazily: the HTTP server and the migrator only exist (and run
	// their lifecycle hooks) if something depends on them.
	fx.Invoke(func(*http.Server, *dbmigrate.Migrator) {}),
	// One invoke registers every route: chi requires all Use() calls before the first
	// route, and fx runs child-module invokes before the parent's, so mounting must not be
	// spread across modules.
	fx.Invoke(mountHTTP),
)

// embedMigrations points golusoris db/migrate at the embedded SQL and runs it on start.
func embedMigrations(o dbmigrate.Options) (dbmigrate.Options, error) {
	fsys, err := store.MigrationsFS()
	if err != nil {
		return dbmigrate.Options{}, fmt.Errorf("app: %w", err)
	}
	o.Auto = true
	o.Path = "." // MigrationsFS is already rooted at migrations/postgres
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

func mountHTTP(d httpDeps, api httpapi.MountParams) error {
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
	if err := httpapi.Mount(d.Router, api); err != nil {
		return err
	}
	return mountWebUI(d)
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
