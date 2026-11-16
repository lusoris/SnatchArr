// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"go.uber.org/fx"

	"github.com/golusoris/golusoris/core/config"
	dbmigrate "github.com/golusoris/golusoris/db/migrate"

	"github.com/lusoris/SnatchArr/api/internal/buildinfo"
)

const (
	lateDBDelay       = 1500 * time.Millisecond
	lateDBTimeout     = 30 * time.Second
	maxProxyAccepts   = 64
	maxProxyPolls     = 120
	proxyPollInterval = 250 * time.Millisecond
	proxyDeadlineHold = 5 * time.Second
)

// lateDatabaseProxyStats tracks early connection resets separately from database forwards.
type lateDatabaseProxyStats struct {
	rejected  atomic.Int32
	forwarded atomic.Int32
}

// lateDatabaseConfig preserves the shutdown test's full control-plane config while
// pointing both database consumers at the delayed proxy and setting retry explicitly.
func lateDatabaseConfig(t *testing.T, grpcAddr, proxyAddr string) *config.Config {
	t.Helper()
	base := controlPlaneConfig(t, grpcAddr)
	dsn, err := url.Parse(base.Get("db.dsn"))
	if err != nil {
		t.Fatalf("parse test database DSN: %v", err)
	}
	dsn.Host = proxyAddr
	raw, err := json.Marshal(map[string]any{
		"snatcharr": map[string]any{"profile": base.Get("snatcharr.profile"), "worker": map[string]any{"token": workerToken}},
		"db": map[string]any{"dsn": dsn.String(), "retry": map[string]any{
			"attempts": 10, "initial": "100ms", "max": "1s",
		}},
		"http": map[string]any{"addr": base.Get("http.addr")},
		"grpc": map[string]any{"listen": grpcAddr},
	})
	if err != nil {
		t.Fatalf("marshal late database config: %v", err)
	}
	file := filepath.Join(t.TempDir(), "late-database.json")
	if writeErr := os.WriteFile(file, raw, 0o600); writeErr != nil {
		t.Fatalf("write late database config: %v", writeErr)
	}
	cfg, err := config.New(config.Options{Files: []string{file}, DisableEnv: true})
	if err != nil {
		t.Fatalf("load late database config: %v", err)
	}
	return cfg
}

// startLateDatabaseProxy serves from a listener bound before app construction and closes
// active forwards when the test context ends.
func startLateDatabaseProxy(t *testing.T, ctx context.Context, ln *net.TCPListener, realAddr string) *lateDatabaseProxyStats {
	t.Helper()
	proxyCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	stats := &lateDatabaseProxyStats{}
	go func() { done <- serveLateDatabaseProxy(t, proxyCtx, ln, realAddr, stats) }()
	t.Cleanup(func() {
		cancel()
		if err := ln.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Errorf("close late database proxy listener: %v", err)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("late database proxy: %v", err)
			}
		case <-time.After(time.Second):
			t.Error("late database proxy did not stop")
		}
	})
	return stats
}

// serveLateDatabaseProxy resets early connections, forwards later ones after the delay
// window opens at the first rejected connection, and waits for every bounded forwarder
// after closing active connections on context cancellation.
func serveLateDatabaseProxy(t *testing.T, ctx context.Context, ln *net.TCPListener, realAddr string, stats *lateDatabaseProxyStats) error {
	t.Helper()
	var delay <-chan time.Time
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	var active sync.WaitGroup
	defer active.Wait()
	accepted := 0
	delayElapsed := false
	for range maxProxyPolls {
		if ctx.Err() != nil {
			return nil
		}
		if err := ln.SetDeadline(clockwork.NewRealClock().Now().Add(proxyPollInterval)); err != nil {
			return fmt.Errorf("deadline on proxy listener: %w", err)
		}
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			var netErr net.Error
			if errors.As(acceptErr, &netErr) && netErr.Timeout() {
				delayElapsed = lateDatabaseDelayElapsed(delay, delayElapsed)
				continue
			}
			if ctx.Err() != nil && errors.Is(acceptErr, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("accept database connection: %w", acceptErr)
		}
		accepted++
		if accepted > maxProxyAccepts*2 {
			closeProxyConnection(t, conn)
			return fmt.Errorf("accepted %d database connections: bound reached", accepted)
		}
		delayElapsed = lateDatabaseDelayElapsed(delay, delayElapsed)
		ready := lateDatabaseProxyForwards(delayElapsed, stats.rejected.Load())
		if err := handleLateDatabaseConnection(t, ctx, conn, ready, realAddr, stats, &active); err != nil {
			return err
		}
		if timer == nil && stats.rejected.Load() > 0 {
			timer = time.NewTimer(lateDBDelay)
			delay = timer.C
		}
	}
	return fmt.Errorf("proxy accept loop reached %d polls", maxProxyPolls)
}

// lateDatabaseDelayElapsed reports whether the delay window opened at the first
// rejected connection has elapsed, consuming its timer once.
func lateDatabaseDelayElapsed(delay <-chan time.Time, elapsed bool) bool {
	if elapsed {
		return true
	}
	select {
	case <-delay:
		return true
	default:
		return false
	}
}

// lateDatabaseProxyForwards reports whether the delay window opened at the first
// rejected connection and at least one connection was rejected.
func lateDatabaseProxyForwards(delayElapsed bool, rejected int32) bool {
	return delayElapsed && rejected > 0
}

func TestLateDatabaseProxyForwards(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		delayElapsed bool
		rejected     int32
		want         bool
	}{
		{name: "delay pending, none rejected", want: false},
		{name: "delay elapsed, none rejected", delayElapsed: true, want: false},
		{name: "delay pending, one rejected", rejected: 1, want: false},
		{name: "delay elapsed, one rejected", delayElapsed: true, rejected: 1, want: true},
		{name: "delay elapsed, maxProxyAccepts rejected", delayElapsed: true, rejected: maxProxyAccepts, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := lateDatabaseProxyForwards(test.delayElapsed, test.rejected); got != test.want {
				t.Errorf("lateDatabaseProxyForwards(%v, %d) = %v, want %v", test.delayElapsed, test.rejected, got, test.want)
			}
		})
	}
}

func TestLateDatabaseDelayElapsed(t *testing.T) {
	t.Parallel()
	elapsed := make(chan time.Time, 1)
	elapsed <- time.Time{}
	cases := []struct {
		name    string
		delay   <-chan time.Time
		elapsed bool
		want    bool
	}{
		{name: "nil channel, not elapsed", want: false},
		{name: "empty buffered channel, not elapsed", delay: make(chan time.Time, 1), want: false},
		{name: "buffered channel holding one value", delay: elapsed, want: true},
		{name: "nil channel, already elapsed", elapsed: true, want: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := lateDatabaseDelayElapsed(test.delay, test.elapsed); got != test.want {
				t.Errorf("lateDatabaseDelayElapsed(%v, %v) = %v, want %v", test.delay != nil, test.elapsed, got, test.want)
			}
		})
	}
}

// handleLateDatabaseConnection resets an early connection or starts a bounded forwarder.
func handleLateDatabaseConnection(t *testing.T, ctx context.Context, conn net.Conn, ready bool, realAddr string, stats *lateDatabaseProxyStats, active *sync.WaitGroup) error {
	t.Helper()
	deadline, err := proxyConnectionDeadline(ctx)
	if err != nil {
		closeProxyConnection(t, conn)
		return err
	}
	if !ready {
		if stats.rejected.Add(1) > maxProxyAccepts {
			closeProxyConnection(t, conn)
			return fmt.Errorf("rejected more than %d early database connections", maxProxyAccepts)
		}
		return rejectEarlyDatabaseConnection(t, conn, deadline)
	}
	if stats.forwarded.Add(1) > maxProxyAccepts {
		closeProxyConnection(t, conn)
		return fmt.Errorf("forwarded more than %d database connections", maxProxyAccepts)
	}
	active.Go(func() {
		forwardDatabaseConnection(t, ctx, conn, realAddr, deadline)
	})
	return nil
}

// proxyConnectionDeadline extends the test deadline slightly so cleanup can close
// connections before any proxy deadline expires.
func proxyConnectionDeadline(ctx context.Context) (time.Time, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return time.Time{}, errors.New("late database proxy context has no deadline")
	}
	return deadline.Add(proxyDeadlineHold), nil
}

// rejectEarlyDatabaseConnection resets a pre-start database connection immediately.
func rejectEarlyDatabaseConnection(t *testing.T, conn net.Conn, deadline time.Time) error {
	t.Helper()
	defer closeProxyConnection(t, conn)
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("deadline on rejected database connection: %w", err)
	}
	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return fmt.Errorf("rejected database connection %T is not TCP", conn)
	}
	if err := tcpConn.SetLinger(0); err != nil {
		return fmt.Errorf("set reset on rejected database connection: %w", err)
	}
	return nil
}

// forwardDatabaseConnection gives each side a deadline and copies PostgreSQL
// traffic in both directions until the app closes or the test cancels.
func forwardDatabaseConnection(t *testing.T, ctx context.Context, client net.Conn, realAddr string, deadline time.Time) {
	t.Helper()
	defer closeProxyConnection(t, client)
	stopClientClose := context.AfterFunc(ctx, func() { closeProxyConnection(t, client) })
	defer stopClientClose()
	if err := client.SetDeadline(deadline); err != nil {
		t.Errorf("deadline on client connection: %v", err)
		return
	}
	server, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", realAddr)
	if err != nil {
		t.Errorf("dial real database: %v", err)
		return
	}
	defer closeProxyConnection(t, server)
	stopServerClose := context.AfterFunc(ctx, func() { closeProxyConnection(t, server) })
	defer stopServerClose()
	if err := server.SetDeadline(deadline); err != nil {
		t.Errorf("deadline on real database connection: %v", err)
		return
	}
	forward := make(chan error, 1)
	go func() { _, copyErr := io.Copy(server, client); forward <- copyErr }()
	_, reverseErr := io.Copy(client, server)
	closeProxyConnection(t, client)
	closeProxyConnection(t, server)
	if forwardErr := <-forward; forwardErr != nil && ctx.Err() == nil {
		t.Logf("database forward ended: %v", forwardErr)
	}
	if reverseErr != nil && ctx.Err() == nil {
		t.Logf("database reverse ended: %v", reverseErr)
	}
}

// closeProxyConnection reports unexpected close errors without hiding an earlier
// close made to unblock the opposite copy direction.
func closeProxyConnection(t *testing.T, conn net.Conn) {
	t.Helper()
	if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		t.Logf("close database proxy connection: %v", err)
	}
}

// TestLateDatabaseStart proves the pool retry connects before the auto migrator
// opens, even when PostgreSQL begins accepting connections after app creation.
func TestLateDatabaseStart(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), lateDBTimeout)
	defer cancel()
	realAddr := newTestDatabase(t).host
	var startProxy sync.Once
	var migrator *dbmigrate.Migrator
	proxyListener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen for late database proxy: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := proxyListener.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			t.Errorf("close late database proxy listener: %v", closeErr)
		}
	})
	var proxyStats *lateDatabaseProxyStats
	app, _ := startOnFreePort(ctx, t, freeAddr, func(grpcAddr string) *fx.App {
		cfg := lateDatabaseConfig(t, grpcAddr, proxyListener.Addr().String())
		startProxy.Do(func() { proxyStats = startLateDatabaseProxy(t, ctx, proxyListener, realAddr) })
		return fx.New(
			fx.Supply(buildinfo.Info{Version: "test"}), Module,
			fx.Decorate(func(*config.Config) *config.Config { return cfg }),
			fx.Populate(&migrator), fx.NopLogger,
		)
	})
	stopCtx, stopCancel := context.WithTimeout(context.WithoutCancel(t.Context()), stopTimeout)
	defer stopCancel()
	defer func() {
		if stopErr := app.Stop(stopCtx); stopErr != nil {
			t.Errorf("stop late database app: %v", stopErr)
		}
	}()
	version, dirty, err := migrator.Version()
	if err != nil || dirty || version == 0 {
		t.Errorf("migrated schema: version %d, dirty %v, error %v", version, dirty, err)
	}
	if proxyStats == nil || proxyStats.rejected.Load() == 0 {
		t.Error("late database proxy did not reject a connection before forwarding")
	}
}
