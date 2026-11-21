// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package metrics declares and records the ADR-0049 CONNECT forward-proxy metrics.
package metrics

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/fx"

	"github.com/golusoris/golusoris/observability/metricdef"

	"github.com/lusoris/SnatchArr/metadata-proxy/internal/config"
)

// Metric names from the ADR-0049 CONNECT measurement table.
const (
	MetricTunnelsTotal          = "metadata_proxy_tunnels_total"
	MetricTunnelBytesTotal      = "metadata_proxy_tunnel_bytes_total"
	MetricTunnelDurationSeconds = "metadata_proxy_tunnel_duration_seconds"
	MetricRefusedTunnelsTotal   = "metadata_proxy_refused_tunnels_total"
)

// Direction label values for metadata_proxy_tunnel_bytes_total.
const (
	DirectionUp   = "up"   // client to provider
	DirectionDown = "down" // provider to client
)

// Fixed result label values for metadata_proxy_tunnels_total and metadata_proxy_tunnel_duration_seconds.
const (
	ResultOK            = "ok"
	ResultRefusedMethod = "refused_method"
	ResultRefusedTarget = "refused_target"
	ResultRefusedHost   = "refused_host"
	ResultRefusedPort   = "refused_port"
	ResultRefusedIP     = "refused_ip"
	ResultRefusedLimit  = "refused_limit"
	ResultDialTimeout   = "dial_timeout"
	ResultDialError     = "dial_error"
)

// AllResults returns a copy of the fixed result label allow-list.
func AllResults() []string {
	return []string{
		ResultOK,
		ResultRefusedMethod,
		ResultRefusedTarget,
		ResultRefusedHost,
		ResultRefusedPort,
		ResultRefusedIP,
		ResultRefusedLimit,
		ResultDialTimeout,
		ResultDialError,
	}
}

// NewCatalog constructs the validated metricdef.Catalog for the four CONNECT metrics.
func NewCatalog(apps, allowHosts []string) (*metricdef.Catalog, error) {
	appLimit := metricdef.LabelLimit{Allow: nonEmptyAllow(apps)}
	hostLimit := metricdef.LabelLimit{Allow: nonEmptyAllow(allowHosts)}
	resultLimit := metricdef.LabelLimit{Allow: AllResults()}
	modeLimit := metricdef.LabelLimit{Allow: []string{string(config.ModeLog), string(config.ModeEnforce)}}
	dirLimit := metricdef.LabelLimit{Allow: []string{DirectionUp, DirectionDown}}

	cat, err := metricdef.NewCatalog(
		metricdef.Def{
			Name:   MetricTunnelsTotal,
			Help:   "Total CONNECT requests handled by the metadata proxy, partitioned by outcome.",
			Kind:   metricdef.KindCounter,
			Labels: []string{"app", "host", "result", "mode"},
			Limits: map[string]metricdef.LabelLimit{
				"app": appLimit, "host": hostLimit, "result": resultLimit, "mode": modeLimit,
			},
		},
		metricdef.Def{
			Name:   MetricTunnelBytesTotal,
			Help:   "Total bytes copied through established CONNECT tunnels.",
			Unit:   "bytes",
			Kind:   metricdef.KindCounter,
			Labels: []string{"app", "host", "direction"},
			Limits: map[string]metricdef.LabelLimit{
				"app": appLimit, "host": hostLimit, "direction": dirLimit,
			},
		},
		metricdef.Def{
			Name:   MetricTunnelDurationSeconds,
			Help:   "Duration of CONNECT requests and established tunnels in seconds.",
			Unit:   "seconds",
			Kind:   metricdef.KindHistogram,
			Labels: []string{"app", "host", "result"},
			Limits: map[string]metricdef.LabelLimit{
				"app": appLimit, "host": hostLimit, "result": resultLimit,
			},
		},
		metricdef.Def{
			Name:   MetricRefusedTunnelsTotal,
			Help:   "Total forward-proxy requests refused before establishing an upstream tunnel.",
			Kind:   metricdef.KindCounter,
			Labels: []string{"app"},
			Limits: map[string]metricdef.LabelLimit{"app": appLimit},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("metrics: build catalog: %w", err)
	}
	return cat, nil
}

func nonEmptyAllow(values []string) []string {
	if len(values) == 0 {
		return []string{metricdef.OtherValue}
	}
	return slices.Clone(values)
}

// Recorder holds typed metric handles registered from the CONNECT catalog.
type Recorder struct {
	catalog  *metricdef.Catalog
	tunnels  *metricdef.Counter
	bytes    *metricdef.Counter
	duration *metricdef.Histogram
	refused  *metricdef.Counter
}

// New builds the CONNECT catalog for compiled and registers (or reuses) its collectors on reg.
func New(compiled *config.Compiled, reg prometheus.Registerer) (*Recorder, error) {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	cat, err := NewCatalog(compiled.Apps, compiled.AllowMatcher.Patterns())
	if err != nil {
		return nil, err
	}
	handles, err := cat.RegisterOrReuse(reg)
	if err != nil {
		return nil, fmt.Errorf("metrics: register catalog: %w", err)
	}
	return recorderFromHandles(cat, handles)
}

func recorderFromHandles(cat *metricdef.Catalog, h *metricdef.Handles) (*Recorder, error) {
	tunnels, err := h.Counter(MetricTunnelsTotal)
	if err != nil {
		return nil, fmt.Errorf("metrics: %w", err)
	}
	bytesCounter, err := h.Counter(MetricTunnelBytesTotal)
	if err != nil {
		return nil, fmt.Errorf("metrics: %w", err)
	}
	duration, err := h.Histogram(MetricTunnelDurationSeconds)
	if err != nil {
		return nil, fmt.Errorf("metrics: %w", err)
	}
	refused, err := h.Counter(MetricRefusedTunnelsTotal)
	if err != nil {
		return nil, fmt.Errorf("metrics: %w", err)
	}
	return &Recorder{
		catalog:  cat,
		tunnels:  tunnels,
		bytes:    bytesCounter,
		duration: duration,
		refused:  refused,
	}, nil
}

// Catalog returns the underlying metricdef.Catalog.
func (r *Recorder) Catalog() *metricdef.Catalog { return r.catalog }

// RecordRefusal records a refused request (400, 403, 405, 503).
func (r *Recorder) RecordRefusal(ctx context.Context, app, host, result string, mode config.Mode, dur time.Duration) error {
	err := errors.Join(
		r.refused.Inc(ctx, app),
		r.tunnels.Inc(ctx, app, host, result, string(mode)),
		r.duration.Observe(ctx, max(dur.Seconds(), 0), app, host, result),
	)
	if err != nil {
		return fmt.Errorf("metrics: record refusal: %w", err)
	}
	return nil
}

// RecordDialFailure records an upstream dial error or timeout (502, 504).
func (r *Recorder) RecordDialFailure(ctx context.Context, app, host, result string, mode config.Mode, dur time.Duration) error {
	err := errors.Join(
		r.tunnels.Inc(ctx, app, host, result, string(mode)),
		r.duration.Observe(ctx, max(dur.Seconds(), 0), app, host, result),
	)
	if err != nil {
		return fmt.Errorf("metrics: record dial failure: %w", err)
	}
	return nil
}

// RecordTunnel records a completed CONNECT tunnel and its transferred bytes.
func (r *Recorder) RecordTunnel(
	ctx context.Context,
	app, host string,
	mode config.Mode,
	bytesUp, bytesDown int64,
	dur time.Duration,
) error {
	err := errors.Join(
		r.bytes.Add(ctx, float64(max(bytesUp, 0)), app, host, DirectionUp),
		r.bytes.Add(ctx, float64(max(bytesDown, 0)), app, host, DirectionDown),
		r.tunnels.Inc(ctx, app, host, ResultOK, string(mode)),
		r.duration.Observe(ctx, max(dur.Seconds(), 0), app, host, ResultOK),
	)
	if err != nil {
		return fmt.Errorf("metrics: record tunnel: %w", err)
	}
	return nil
}

type fxParams struct {
	fx.In

	Compiled   *config.Compiled
	Registerer prometheus.Registerer `optional:"true"`
}

// Module provides *Recorder to the fx graph.
var Module = fx.Module(
	"metadata_proxy.metrics",
	fx.Provide(func(p fxParams) (*Recorder, error) {
		return New(p.Compiled, p.Registerer)
	}),
)
