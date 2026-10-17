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

	"github.com/golusoris/golusoris/bootstrap"
	"github.com/golusoris/golusoris/core/clock"
	dbmigrate "github.com/golusoris/golusoris/db/migrate"
	dbpgx "github.com/golusoris/golusoris/db/pgx"
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
	"github.com/lusoris/SnatchArr/api/internal/configarr"
	"github.com/lusoris/SnatchArr/api/internal/dlclients"
	"github.com/lusoris/SnatchArr/api/internal/events"
	"github.com/lusoris/SnatchArr/api/internal/httpapi"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/leaderx"
	"github.com/lusoris/SnatchArr/api/internal/logring"
	"github.com/lusoris/SnatchArr/api/internal/metrics"
	"github.com/lusoris/SnatchArr/api/internal/notify"
	"github.com/lusoris/SnatchArr/api/internal/policies"
	"github.com/lusoris/SnatchArr/api/internal/prowlarr"
	"github.com/lusoris/SnatchArr/api/internal/queue"
	"github.com/lusoris/SnatchArr/api/internal/retention"
	"github.com/lusoris/SnatchArr/api/internal/seerr"
	"github.com/lusoris/SnatchArr/api/internal/settings"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
	"github.com/lusoris/SnatchArr/api/internal/sso"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/webui"
	"github.com/lusoris/SnatchArr/api/internal/workergrpc"
)

// golusorisDB mirrors golusoris.DB from github.com/golusoris/golusoris v0.16.5 without importing the umbrella package.
var golusorisDB = fx.Module(
	"golusoris.db",
	dbpgx.Module,
	dbmigrate.Module,
)

// Module is the complete SnatchArr control plane. Order matters for fx.Invoke: the base
// middleware and probes mount first, then the OpenAPI server, then the SPA catch-all.
var Module = fx.Options(
	bootstrap.Core,
	golusorisDB,
	bootstrap.HTTP,
	otel.Module,
	csrf.Module,
	grpcx.Module,
	grpcConfig,
	fx.Decorate(embedMigrations),
	// The dev profile may start without APP_CRYPTO_KEY (crypto.go).
	fx.Decorate(newEncryptor),
	config.Module,
	logring.Module,
	store.Module,
	metrics.Module,
	arrclient.Module,
	instances.Module,
	policies.Module,
	settings.Module,
	auth.Module,
	events.Module,
	snatch.Module,
	dlclients.Module,
	seerr.Module,
	queue.Module,
	prowlarr.Module,
	configarr.Module,
	notify.Module,
	retention.Module,
	fx.Provide(func(s *dlclients.Service) workergrpc.Pacer { return s }),
	workergrpc.Module,
	sso.Module,
	httpapi.Module,
	// After every module that registers a loop: the gate collects the leader_loops group.
	leaderx.Module,
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

// registerHealthChecks registers the probe checks. Readiness covers Postgres and the leader
// gate: a replica whose periodic loops cannot run (gate stopped, a loop failed to start,
// election failing) reports not ready instead of serving as if the planner ran.
func registerHealthChecks(reg *statuspage.Registry, pool *pgxpool.Pool, gate *health.StartupGate,
	loops *leaderx.Gate, lc fx.Lifecycle,
) {
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
	reg.Register(loops.Check("leader"))
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
	Logs   *logring.Ring
	Cfg    config.Options
	SSO    *sso.Service
}

func mountHTTP(d httpDeps, api httpapi.MountParams) error {
	d.Router.Use(
		middleware.RequestID,
		sso.ProxyHeaderMiddleware(d.Cfg.ProxyAuth, d.Logger),
		middleware.TrustProxy(middleware.TrustProxyOptions{TrustedCIDRs: d.Cfg.Proxy.Trusted}),
		middleware.Recover(d.Logger),
		middleware.Logger(d.Logger, d.Clock),
		middleware.SecureHeaders(middleware.SecureHeadersDefaults()),
	)
	health.Mount(d.Router, d.Reg)
	if err := prom.Mount(d.Router, d.Reg); err != nil {
		return fmt.Errorf("app: mount metrics: %w", err)
	}
	httpapi.MountStream(d.Router, api, d.Events.Hub().Handler())
	httpapi.MountLogStream(d.Router, api, d.Logs.StreamHandler())
	sso.Mount(d.Router, httpapi.PathPrefix, d.SSO, httpapi.ErrorHandler(d.Logger))
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
