// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package app wires golusoris core/HTTP modules with the metadata-proxy CONNECT service.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/fx"

	"github.com/golusoris/golusoris/bootstrap"
	"github.com/golusoris/golusoris/core/clock"
	gconfig "github.com/golusoris/golusoris/core/config"
	glog "github.com/golusoris/golusoris/core/log"
	"github.com/golusoris/golusoris/httpx/middleware"
	"github.com/golusoris/golusoris/k8s/health"
	"github.com/golusoris/golusoris/k8s/metrics/prom"
	"github.com/golusoris/golusoris/observability/statuspage"

	"github.com/lusoris/SnatchArr/metadata-proxy/internal/config"
	"github.com/lusoris/SnatchArr/metadata-proxy/internal/metrics"
	"github.com/lusoris/SnatchArr/metadata-proxy/internal/proxy"
)

// BuildInfo carries build-time version metadata injected via -ldflags.
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

// Module composes the lean golusoris foundation (config, log, clock, HTTP, probes, metrics)
// without crypto or database dependencies, plus the CONNECT proxy service.
var Module = fx.Options(
	gconfig.Module,
	fx.Decorate(config.DecorateCoreOptionsFX),
	glog.Module,
	clock.Module,
	bootstrap.HTTP,
	fx.Provide(statuspage.NewRegistry, health.NewStartupGate),
	health.Module,
	config.Module,
	metrics.Module,
	proxy.Module,
	fx.Invoke(registerAdmin),
)

type adminParams struct {
	fx.In

	Lifecycle fx.Lifecycle
	Router    chi.Router
	Server    *http.Server
	Registry  *statuspage.Registry
	Startup   *health.StartupGate
	Proxy     *proxy.Service
	Logger    *slog.Logger
	Build     BuildInfo `optional:"true"`
}

func registerAdmin(p adminParams) error {
	p.Registry.Register(statuspage.Check{
		Name: "process",
		Tags: []string{health.TagLiveness},
		Fn:   func(context.Context) error { return nil },
	})
	p.Registry.Register(p.Proxy.ReadinessCheck())
	p.Registry.Register(p.Startup.Check("startup"))

	p.Router.Use(middleware.Recover(p.Logger))
	health.Mount(p.Router, p.Registry)
	if err := prom.Mount(p.Router, p.Registry); err != nil {
		return fmt.Errorf("app: mount metrics: %w", err)
	}

	p.Lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			p.Startup.MarkComplete()
			p.Logger.InfoContext(ctx, "metadata-proxy: started",
				slog.String("version", p.Build.Version),
				slog.String("commit", p.Build.Commit),
				slog.String("date", p.Build.Date),
			)
			return nil
		},
	})
	return nil
}
