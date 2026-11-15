// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package proxy runs the per-app CONNECT forward-proxy listeners.
package proxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/fx"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/drain"
	"github.com/golusoris/golusoris/k8s/health"
	"github.com/golusoris/golusoris/observability/metricdef"
	"github.com/golusoris/golusoris/observability/statuspage"

	"github.com/lusoris/SnatchArr/metadata-proxy/internal/config"
	"github.com/lusoris/SnatchArr/metadata-proxy/internal/metrics"
)

const (
	listenerReadHeaderTimeout = 10 * time.Second
	listenerReadTimeout       = 30 * time.Second
	listenerWriteTimeout      = 30 * time.Second
	listenerIdleTimeout       = 60 * time.Second
	listenerMaxHeaderBytes    = 1 << 16
)

// ErrListenersNotBound reports that one or more CONNECT listeners are not bound.
var ErrListenersNotBound = errors.New("proxy: CONNECT listeners not bound")

// Option customizes a Service for testing or host binding.
type Option func(*Service)

// WithBindHost sets the IP/interface address listeners bind to (empty means all interfaces).
func WithBindHost(host string) Option {
	return func(s *Service) {
		s.bindHost = host
	}
}

// WithEphemeralPorts binds listeners on port 0 while keeping configured app labels (for parallel tests).
func WithEphemeralPorts() Option {
	return func(s *Service) {
		s.ephemeralPorts = true
	}
}

// WithDialControl overrides the outbound net.Dialer.Control hook (for loopback tests).
func WithDialControl(ctrl DialControl) Option {
	return func(s *Service) {
		s.dialControl = ctrl
		s.customDialCtrl = true
	}
}

type appListener struct {
	app  string
	port int
	sem  chan struct{}
	srv  *http.Server
	mu   sync.RWMutex
	ln   net.Listener
	addr string
}

// Service owns the configured CONNECT listeners and their readiness state.
type Service struct {
	compiled       *config.Compiled
	metrics        *metrics.Recorder
	clock          clock.Clock
	logger         *slog.Logger
	bindHost       string
	ephemeralPorts bool
	dialControl    DialControl
	customDialCtrl bool
	listen         func(ctx context.Context, network, addr string) (net.Listener, error)
	listeners      []*appListener
	ready          atomic.Bool
	stopOnce       sync.Once
	stopCh         chan struct{}
	tunnelWG       sync.WaitGroup
}

// New constructs a Service from compiled config, metrics recorder, clock, and logger.
func New(
	compiled *config.Compiled,
	rec *metrics.Recorder,
	clk clock.Clock,
	logger *slog.Logger,
	opts ...Option,
) (*Service, error) {
	if compiled == nil {
		return nil, errors.New("proxy: compiled config is nil")
	}
	if rec == nil {
		return nil, errors.New("proxy: metrics recorder is nil")
	}
	if clk == nil {
		return nil, errors.New("proxy: clock is nil")
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	s := &Service{
		compiled: compiled,
		metrics:  rec,
		clock:    clk,
		logger:   logger,
		listen:   (&net.ListenConfig{}).Listen,
		stopCh:   make(chan struct{}),
	}
	for _, opt := range opts {
		opt(s)
	}
	if !s.customDialCtrl {
		s.dialControl = buildGuardianControl(compiled)
	}
	s.listeners = s.buildListeners()
	return s, nil
}

func (s *Service) buildListeners() []*appListener {
	cfgListeners := s.compiled.Options.Listeners
	maxTunnels := s.compiled.Options.MaxTunnels
	out := make([]*appListener, 0, len(cfgListeners))
	for _, item := range cfgListeners {
		al := &appListener{
			app:  item.App,
			port: item.Port,
			sem:  make(chan struct{}, maxTunnels),
		}
		al.srv = &http.Server{
			Handler:           http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { s.handleRequest(al, w, r) }),
			ReadHeaderTimeout: listenerReadHeaderTimeout,
			ReadTimeout:       listenerReadTimeout,
			WriteTimeout:      listenerWriteTimeout,
			IdleTimeout:       listenerIdleTimeout,
			MaxHeaderBytes:    listenerMaxHeaderBytes,
		}
		out = append(out, al)
	}
	return out
}

// ReadinessCheck returns a statuspage.Check that passes once every CONNECT listener is bound.
func (s *Service) ReadinessCheck() statuspage.Check {
	return statuspage.Check{
		Name: "listeners",
		Tags: []string{health.TagReadiness},
		Fn: func(context.Context) error {
			if !s.ready.Load() {
				return ErrListenersNotBound
			}
			return nil
		},
	}
}

// Addr returns the bound host:port for app once Start has succeeded.
func (s *Service) Addr(app string) (string, bool) {
	for _, al := range s.listeners {
		if al.app == app {
			al.mu.RLock()
			addr := al.addr
			al.mu.RUnlock()
			return addr, addr != ""
		}
	}
	return "", false
}

// Start binds every configured CONNECT listener and marks readiness up.
func (s *Service) Start(ctx context.Context) error {
	for i, al := range s.listeners {
		if err := s.bindOne(ctx, al); err != nil {
			s.rollbackBound(ctx, i)
			return err
		}
	}
	serveCtx := context.WithoutCancel(ctx)
	for _, al := range s.listeners {
		s.startServe(serveCtx, al)
	}
	s.ready.Store(true)
	return nil
}

func (s *Service) bindOne(ctx context.Context, al *appListener) error {
	bindPort := al.port
	if s.ephemeralPorts {
		bindPort = 0
	}
	listenAddr := net.JoinHostPort(s.bindHost, strconv.Itoa(bindPort))
	listenFn := s.listen
	if listenFn == nil {
		listenFn = (&net.ListenConfig{}).Listen
	}
	tcpLn, err := listenFn(ctx, "tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("proxy: listen %s (%s): %w", al.app, listenAddr, err)
	}
	bound := tcpLn.Addr().String()
	al.mu.Lock()
	al.ln = tcpLn
	al.addr = bound
	al.mu.Unlock()

	s.logger.InfoContext(ctx, "metadata-proxy: CONNECT listener bound",
		slog.String("app", al.app),
		slog.String("addr", bound),
		slog.String("mode", string(s.compiled.Options.Mode)),
	)
	return nil
}

func (s *Service) startServe(serveCtx context.Context, al *appListener) {
	al.mu.RLock()
	tcpLn := al.ln
	al.mu.RUnlock()
	if tcpLn == nil {
		return
	}
	go s.serveLoop(serveCtx, al, tcpLn)
}

func (s *Service) rollbackBound(ctx context.Context, count int) {
	for i := range count {
		al := s.listeners[i]
		al.mu.Lock()
		ln := al.ln
		al.ln = nil
		al.addr = ""
		al.mu.Unlock()
		if ln == nil {
			continue
		}
		if err := ln.Close(); err != nil {
			s.logger.WarnContext(ctx, "metadata-proxy: listener rollback close error",
				slog.String("app", al.app),
				slog.Any("err", err),
			)
		}
	}
}

func (s *Service) serveLoop(ctx context.Context, al *appListener, tcpLn net.Listener) {
	if err := al.srv.Serve(tcpLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.logger.ErrorContext(ctx, "metadata-proxy: listener serve error",
			slog.String("app", al.app),
			slog.Any("err", err),
		)
	}
}

// Stop marks readiness down, closes active tunnels, and shuts down every CONNECT listener.
func (s *Service) Stop(ctx context.Context) error {
	s.ready.Store(false)
	s.stopOnce.Do(func() { close(s.stopCh) })
	var errs []error
	for _, al := range s.listeners {
		if err := al.srv.Shutdown(ctx); err != nil && !errors.Is(err, net.ErrClosed) {
			errs = append(errs, fmt.Errorf("proxy: shutdown %s: %w", al.app, err))
		}
	}
	done := make(chan struct{})
	go func() {
		s.tunnelWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		errs = append(errs, fmt.Errorf("proxy: drain tunnels: %w", ctx.Err()))
	}
	return errors.Join(errs...)
}

func (s *Service) logMetricError(ctx context.Context, app, result string, err error) {
	if err == nil {
		return
	}
	s.logger.WarnContext(ctx, "metadata-proxy: record metrics failed",
		slog.String("app", app),
		slog.String("result", result),
		slog.Any("err", err),
	)
}

type parsedTarget struct {
	host       string
	metricHost string
	port       uint16
	addr       string
}

func (s *Service) handleRequest(al *appListener, w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	start := s.clock.Now()
	mode := s.compiled.Options.Mode

	if r.Method != http.MethodConnect {
		w.Header().Set("Allow", http.MethodConnect)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		dur := s.clock.Since(start)
		s.logMetricError(ctx, al.app, metrics.ResultRefusedMethod, s.metrics.RecordRefusal(ctx, al.app, metricdef.OtherValue, metrics.ResultRefusedMethod, mode, dur))
		s.logger.WarnContext(ctx, "metadata-proxy: non-CONNECT request refused",
			slog.String("app", al.app),
			slog.String("method", r.Method),
			slog.String("mode", string(mode)),
			slog.String("result", metrics.ResultRefusedMethod),
			slog.Duration("duration", dur),
		)
		return
	}

	target, ok := s.parseTarget(r)
	if !ok {
		s.refuseMalformedTarget(ctx, al.app, w, start)
		return
	}
	if status, result := s.checkPolicy(ctx, al.app, target); status != 0 {
		s.refuseConnect(ctx, al.app, target, status, result, w, start)
		return
	}
	s.runTunnel(ctx, al, target, w, start)
}

func (s *Service) parseTarget(r *http.Request) (parsedTarget, bool) {
	authority := r.Host
	if authority == "" {
		authority = r.URL.Host
	}
	rawHost, portStr, err := net.SplitHostPort(strings.TrimSpace(authority))
	if err != nil {
		return parsedTarget{}, false
	}
	host := config.NormalizeHost(rawHost)
	if host == "" {
		return parsedTarget{}, false
	}
	portNum, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil || portNum == 0 || portNum > math.MaxUint16 {
		return parsedTarget{}, false
	}
	metricHost := metricdef.OtherValue
	if matched, allowed := s.compiled.AllowMatcher.Match(host); allowed {
		metricHost = matched
	}
	port := uint16(portNum)
	return parsedTarget{
		host:       host,
		metricHost: metricHost,
		port:       port,
		addr:       net.JoinHostPort(host, strconv.FormatUint(portNum, 10)),
	}, true
}

func (s *Service) checkPolicy(ctx context.Context, app string, t parsedTarget) (int, string) {
	if s.compiled.RefuseMatcher.Matches(t.host) {
		return http.StatusForbidden, metrics.ResultRefusedHost
	}
	if _, ok := s.compiled.AllowedPorts[t.port]; !ok {
		return http.StatusForbidden, metrics.ResultRefusedPort
	}
	if _, allowed := s.compiled.AllowMatcher.Match(t.host); !allowed {
		if s.compiled.Options.Mode == config.ModeEnforce {
			return http.StatusForbidden, metrics.ResultRefusedHost
		}
		s.logger.WarnContext(ctx, "metadata-proxy: host outside allow_hosts",
			slog.String("app", app),
			slog.String("host", t.host),
			slog.Int("port", int(t.port)),
			slog.String("mode", string(s.compiled.Options.Mode)),
		)
	}
	return 0, ""
}

func (s *Service) refuseMalformedTarget(ctx context.Context, app string, w http.ResponseWriter, start time.Time) {
	mode := s.compiled.Options.Mode
	http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
	dur := s.clock.Since(start)
	s.logMetricError(ctx, app, metrics.ResultRefusedTarget, s.metrics.RecordRefusal(ctx, app, metricdef.OtherValue, metrics.ResultRefusedTarget, mode, dur))
	s.logger.WarnContext(ctx, "metadata-proxy: malformed CONNECT target",
		slog.String("app", app),
		slog.String("mode", string(mode)),
		slog.String("result", metrics.ResultRefusedTarget),
		slog.Duration("duration", dur),
	)
}

func (s *Service) refuseConnect(
	ctx context.Context,
	app string,
	t parsedTarget,
	status int,
	result string,
	w http.ResponseWriter,
	start time.Time,
) {
	mode := s.compiled.Options.Mode
	http.Error(w, http.StatusText(status), status)
	dur := s.clock.Since(start)
	s.logMetricError(ctx, app, result, s.metrics.RecordRefusal(ctx, app, t.metricHost, result, mode, dur))
	s.logOutcome(ctx, slog.LevelWarn, app, t, result, 0, 0, dur)
}

type tunnelOutcome struct {
	status      int
	result      string
	up          int64
	down        int64
	established bool
}

func (s *Service) runTunnel(
	ctx context.Context,
	al *appListener,
	t parsedTarget,
	w http.ResponseWriter,
	start time.Time,
) {
	select {
	case al.sem <- struct{}{}:
	default:
		s.refuseConnect(ctx, al.app, t, http.StatusServiceUnavailable, metrics.ResultRefusedLimit, w, start)
		return
	}
	s.tunnelWG.Add(1)
	defer s.tunnelWG.Done()

	out := s.runSlot(ctx, al, t, w)
	s.finishTunnel(ctx, al.app, t, out, w, start)
}

func (s *Service) runSlot(
	ctx context.Context,
	al *appListener,
	t parsedTarget,
	w http.ResponseWriter,
) tunnelOutcome {
	defer func() { <-al.sem }()
	upstream, status, result, err := s.dialUpstream(ctx, t.addr)
	if err != nil {
		return tunnelOutcome{status: status, result: result}
	}
	up, down, established := s.bridgeTunnel(w, upstream)
	if !established {
		return tunnelOutcome{result: metrics.ResultDialError}
	}
	return tunnelOutcome{result: metrics.ResultOK, up: up, down: down, established: true}
}

func (s *Service) finishTunnel(
	ctx context.Context,
	app string,
	t parsedTarget,
	out tunnelOutcome,
	w http.ResponseWriter,
	start time.Time,
) {
	if out.status == http.StatusForbidden {
		s.refuseConnect(ctx, app, t, out.status, out.result, w, start)
		return
	}
	mode := s.compiled.Options.Mode
	if out.status != 0 {
		http.Error(w, http.StatusText(out.status), out.status)
		dur := s.clock.Since(start)
		s.logMetricError(ctx, app, out.result, s.metrics.RecordDialFailure(ctx, app, t.metricHost, out.result, mode, dur))
		s.logOutcome(ctx, slog.LevelWarn, app, t, out.result, 0, 0, dur)
		return
	}
	dur := s.clock.Since(start)
	if !out.established {
		s.logMetricError(ctx, app, out.result, s.metrics.RecordDialFailure(ctx, app, t.metricHost, out.result, mode, dur))
		s.logOutcome(ctx, slog.LevelWarn, app, t, out.result, 0, 0, dur)
		return
	}
	s.logMetricError(ctx, app, metrics.ResultOK, s.metrics.RecordTunnel(ctx, app, t.metricHost, mode, out.up, out.down, dur))
	s.logOutcome(ctx, slog.LevelInfo, app, t, metrics.ResultOK, out.up, out.down, dur)
}

func (s *Service) logOutcome(
	ctx context.Context,
	level slog.Level,
	app string,
	t parsedTarget,
	result string,
	bytesUp, bytesDown int64,
	dur time.Duration,
) {
	s.logger.Log(ctx, level, "metadata-proxy: tunnel finished",
		slog.String("app", app),
		slog.String("host", t.host),
		slog.Int("port", int(t.port)),
		slog.String("mode", string(s.compiled.Options.Mode)),
		slog.String("result", result),
		slog.Int64("bytes_up", bytesUp),
		slog.Int64("bytes_down", bytesDown),
		slog.Duration("duration", dur),
	)
}

type fxParams struct {
	fx.In

	Lifecycle fx.Lifecycle
	Compiled  *config.Compiled
	Recorder  *metrics.Recorder
	Clock     clock.Clock
	Logger    *slog.Logger
	Drain     drain.Gate `optional:"true"`
	Opts      []Option   `optional:"true"`
}

// Module constructs *Service and binds its lifecycle hooks in the fx graph.
var Module = fx.Module(
	"metadata_proxy.proxy",
	fx.Provide(func(p fxParams) (*Service, error) {
		svc, err := New(p.Compiled, p.Recorder, p.Clock, p.Logger, p.Opts...)
		if err != nil {
			return nil, err
		}
		p.Lifecycle.Append(drain.Wrap(p.Drain, fx.Hook{
			OnStart: svc.Start,
			OnStop:  svc.Stop,
		}))
		return svc, nil
	}),
)
