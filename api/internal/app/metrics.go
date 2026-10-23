// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/fx"

	"github.com/golusoris/golusoris/k8s/metrics/prom"
)

const (
	metricsReadHeaderTimeout = 5 * time.Second
	metricsReadTimeout       = 10 * time.Second
	metricsWriteTimeout      = 10 * time.Second
	metricsIdleTimeout       = 30 * time.Second
)

// mountMetrics serves /metrics on the main router, or, when snatcharr.metrics.addr is set, on a
// listener of its own so a NetworkPolicy can admit the scraper without opening the API port.
func mountMetrics(d httpDeps) error {
	if d.Cfg.Metrics.Addr == "" {
		if err := prom.Mount(d.Router, d.Reg); err != nil {
			return fmt.Errorf("mount metrics on main router: %w", err)
		}
		return nil
	}
	return startMetricsServer(d)
}

// startMetricsServer binds the metrics listener on fx start and shuts it down on fx stop.
func startMetricsServer(d httpDeps) error {
	r := chi.NewRouter()
	if err := prom.Mount(r, d.Reg); err != nil {
		return fmt.Errorf("mount metrics router: %w", err)
	}
	srv := &http.Server{
		Handler:           r,
		ReadHeaderTimeout: metricsReadHeaderTimeout,
		ReadTimeout:       metricsReadTimeout,
		WriteTimeout:      metricsWriteTimeout,
		IdleTimeout:       metricsIdleTimeout,
	}
	d.LC.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", d.Cfg.Metrics.Addr)
			if err != nil {
				return fmt.Errorf("metrics server listen %s: %w", d.Cfg.Metrics.Addr, err)
			}
			serveCtx := context.WithoutCancel(ctx)
			go func() {
				if serveErr := srv.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
					d.Logger.ErrorContext(serveCtx, "app: metrics server serve error", slog.Any("err", serveErr))
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			if err := srv.Shutdown(ctx); err != nil {
				return fmt.Errorf("metrics server shutdown: %w", err)
			}
			return nil
		},
	})
	return nil
}
