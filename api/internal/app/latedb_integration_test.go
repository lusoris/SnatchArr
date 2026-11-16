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
	proxyConnTimeout  = 20 * time.Second
	maxProxyAccepts   = 64
	maxProxyPolls     = 120
	proxyPollInterval = 250 * time.Millisecond
)

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

// startLateDatabaseProxy makes a reserved loopback address begin forwarding after
// lateDBDelay; cleanup cancels the accept loop and waits for every connection.
func startLateDatabaseProxy(t *testing.T, ctx context.Context, proxyAddr, realAddr string) {
	t.Helper()
	proxyCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- serveLateDatabaseProxy(t, proxyCtx, proxyAddr, realAddr) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("late database proxy: %v", err)
			}
		case <-time.After(proxyConnTimeout + time.Second):
			t.Error("late database proxy did not stop")
		}
	})
}

// serveLateDatabaseProxy waits before binding, then accepts a bounded number of
// connections and waits for all forwarders when its context ends.
func serveLateDatabaseProxy(t *testing.T, ctx context.Context, proxyAddr, realAddr string) error {
	t.Helper()
	timer := time.NewTimer(lateDBDelay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		return nil
	}
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", proxyAddr)
	if err != nil {
		return fmt.Errorf("listen on reserved address: %w", err)
	}
	tcpLn, ok := ln.(*net.TCPListener)
	if !ok {
		return fmt.Errorf("proxy listener %T is not TCP", ln)
	}
	var active sync.WaitGroup
	defer active.Wait()
	defer func() {
		if closeErr := ln.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			t.Errorf("close proxy listener: %v", closeErr)
		}
	}()
	accepted := 0
	for range maxProxyPolls {
		if ctx.Err() != nil {
			return nil
		}
		if err := tcpLn.SetDeadline(clockwork.NewRealClock().Now().Add(proxyPollInterval)); err != nil {
			return fmt.Errorf("deadline on proxy listener: %w", err)
		}
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			var netErr net.Error
			if errors.As(acceptErr, &netErr) && netErr.Timeout() {
				continue
			}
			return fmt.Errorf("accept database connection: %w", acceptErr)
		}
		accepted++
		if accepted > maxProxyAccepts {
			closeProxyConnection(t, conn)
			return fmt.Errorf("accepted %d database connections: bound reached", accepted)
		}
		active.Add(1)
		go func() {
			defer active.Done()
			forwardDatabaseConnection(t, ctx, conn, realAddr)
		}()
	}
	return fmt.Errorf("proxy accept loop reached %d polls", maxProxyPolls)
}

// forwardDatabaseConnection gives each side a deadline and copies PostgreSQL
// traffic in both directions until the app closes or the test cancels.
func forwardDatabaseConnection(t *testing.T, ctx context.Context, client net.Conn, realAddr string) {
	t.Helper()
	defer closeProxyConnection(t, client)
	server, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", realAddr)
	if err != nil {
		t.Errorf("dial real database: %v", err)
		return
	}
	defer closeProxyConnection(t, server)
	deadline := clockwork.NewRealClock().Now().Add(proxyConnTimeout)
	if err := client.SetDeadline(deadline); err != nil {
		t.Errorf("deadline on client connection: %v", err)
		return
	}
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
	proxyAddr := freeAddr(t)
	realAddr := newTestDatabase(t).host
	var startProxy sync.Once
	var migrator *dbmigrate.Migrator
	app, _ := startOnFreePort(ctx, t, freeAddr, func(grpcAddr string) *fx.App {
		cfg := lateDatabaseConfig(t, grpcAddr, proxyAddr)
		startProxy.Do(func() { startLateDatabaseProxy(t, ctx, proxyAddr, realAddr) })
		return fx.New(
			fx.Supply(buildinfo.Info{Version: "test"}), Module,
			fx.Decorate(func(*config.Config) *config.Config { return cfg }),
			fx.Populate(&migrator), fx.NopLogger,
		)
	})
	stopCtx, stopCancel := context.WithTimeout(context.WithoutCancel(t.Context()), stopTimeout)
	defer stopCancel()
	defer func() {
		if err := app.Stop(stopCtx); err != nil {
			t.Errorf("stop late database app: %v", err)
		}
	}()
	version, dirty, err := migrator.Version()
	if err != nil || dirty || version == 0 {
		t.Errorf("migrated schema: version %d, dirty %v, error %v", version, dirty, err)
	}
}
