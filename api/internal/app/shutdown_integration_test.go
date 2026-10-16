// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package app

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	gconfig "github.com/golusoris/golusoris/core/config"
	grpcx "github.com/golusoris/golusoris/grpc"

	"github.com/lusoris/SnatchArr/api/internal/buildinfo"
	snatcharrv1 "github.com/lusoris/SnatchArr/api/internal/gen/snatcharr/v1"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

// Bounds (HISS-02): the whole test, and the stop on its own. The stop bound sits well below
// fx's 15 s stop timeout and the 30 s long-poll, so a stop that waits for the lease fails it.
const (
	shutdownTestTimeout = 2 * time.Minute
	stopTimeout         = 5 * time.Second
	workerToken         = "shutdown-test-token"
	// Starts on a fresh gRPC port before a taken one fails the test.
	bindAttempts = 5
	// A server given this address binds a free loopback port itself: the kernel picks and holds
	// it in one step, so no other process can take it in between (#296).
	anyLoopback = "127.0.0.1:0"
	// Package paths of the golusoris constructors whose stop hooks the test orders.
	grpcHooks = "github.com/golusoris/golusoris/grpc."
	poolHooks = "github.com/golusoris/golusoris/db/pgx."
)

// TestMain shares one Postgres container across the package's integration tests.
func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

// stopOrder records the constructors whose stop hooks ran, in order.
type stopOrder struct {
	mu      sync.Mutex
	callers []string
}

func (o *stopOrder) LogEvent(event fxevent.Event) {
	if e, ok := event.(*fxevent.OnStopExecuting); ok {
		o.mu.Lock()
		defer o.mu.Unlock()
		o.callers = append(o.callers, e.CallerName)
	}
}

// index is the position of the first stop hook registered from pkg, -1 when none ran.
func (o *stopOrder) index(pkg string) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.IndexFunc(o.callers, func(caller string) bool { return strings.HasPrefix(caller, pkg) })
}

// freeAddr is a loopback address nothing listened on a moment ago. Another process can bind it
// before the server does (#296), so a server reached on it starts through startOnFreePort.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return addr
}

// startOnFreePort starts the app newApp builds for a gRPC address from pick and returns the
// address it serves on. The gRPC server reports no bound port, so the address is chosen before
// the bind; a start that finds it taken tries another, at most bindAttempts times.
func startOnFreePort(ctx context.Context, t *testing.T, pick func(*testing.T) string, newApp func(grpcAddr string) *fx.App) (*fx.App, string) {
	t.Helper()
	var err error
	for range bindAttempts {
		addr := pick(t)
		app := newApp(addr)
		if err = app.Start(ctx); err == nil {
			return app, addr
		}
		if !errors.Is(err, syscall.EADDRINUSE) {
			break
		}
		t.Logf("start on %s: %v; trying another port", addr, err)
	}
	t.Fatalf("start: %v", err)
	return nil, ""
}

// controlPlaneConfig is the configuration of one control plane on the test's own database,
// read from a file only: the prefix matches no variable.
func controlPlaneConfig(t *testing.T, grpcAddr string) *gconfig.Config {
	t.Helper()
	cc := storetest.New(t).Pool().Config().ConnConfig
	dsn := url.URL{
		Scheme: "postgres", User: url.UserPassword(cc.User, cc.Password),
		Host: net.JoinHostPort(cc.Host, strconv.Itoa(int(cc.Port))), Path: "/" + cc.Database, RawQuery: "sslmode=disable",
	}
	raw, err := json.Marshal(map[string]any{
		"snatcharr": map[string]any{"profile": "dev", "worker": map[string]any{"token": workerToken}},
		"db":        map[string]any{"dsn": dsn.String()},
		"http":      map[string]any{"addr": anyLoopback},
		"grpc":      map[string]any{"listen": grpcAddr},
	})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "config.json")
	if err = os.WriteFile(file, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := gconfig.New(gconfig.Options{Files: []string{file}, EnvPrefix: "SNATCHARR_SHUTDOWN_TEST_UNSET_"})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg
}

// workerClient is a WorkerService client for addr and a context that carries the token.
func workerClient(ctx context.Context, t *testing.T, addr string) (snatcharrv1.WorkerServiceClient, context.Context) {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc client: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := conn.Close(); closeErr != nil {
			t.Errorf("close grpc client: %v", closeErr)
		}
	})
	return snatcharrv1.NewWorkerServiceClient(conn), metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+workerToken)
}

// The control plane as cmd/snatcharr assembles it stops while a worker long-polls for a run:
// the worker reads Unavailable at once, and gRPC stops serving before the database pool
// closes, so no call can meet a closed pool (#146).
func TestStopWithAWaitingLease(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), shutdownTestTimeout)
	defer cancel()
	var order *stopOrder
	// Closed once the server has handed a long-poll to the handler.
	waiting, once := make(chan struct{}), sync.Once{}
	seen := func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if lease, ok := req.(*snatcharrv1.LeaseRunRequest); ok && lease.GetWaitSeconds() > 0 {
			once.Do(func() { close(waiting) })
		}
		return handler(ctx, req)
	}
	app, grpcAddr := startOnFreePort(ctx, t, freeAddr, func(grpcAddr string) *fx.App {
		cfg := controlPlaneConfig(t, grpcAddr)
		// A failed start stops what it started; only the last attempt's stops count.
		rec := &stopOrder{}
		order = rec
		return fx.New(
			fx.Supply(buildinfo.Info{Version: "test"}), Module,
			fx.Decorate(func(*gconfig.Config) *gconfig.Config { return cfg }),
			grpcx.ProvideServerOption(grpc.ChainUnaryInterceptor(seen)),
			fx.WithLogger(func() fxevent.Logger { return rec }),
		)
	})
	client, authed := workerClient(ctx, t, grpcAddr)
	if resp, err := client.LeaseRun(authed, &snatcharrv1.LeaseRunRequest{WorkerId: "worker"}, grpc.WaitForReady(true)); err != nil || resp.GetRun() != nil {
		t.Fatalf("serving, empty queue: LeaseRun() = %v, %v; want an empty answer", resp, err)
	}
	answer := make(chan error, 1)
	go func() {
		_, err := client.LeaseRun(authed, &snatcharrv1.LeaseRunRequest{WorkerId: "worker", WaitSeconds: 30})
		answer <- err
	}()
	select {
	case <-waiting:
	case <-ctx.Done():
		t.Fatalf("the long-poll never reached the server: %v", ctx.Err())
	}

	stopCtx, stopCancel := context.WithTimeout(ctx, stopTimeout)
	defer stopCancel()
	if err := app.Stop(stopCtx); err != nil {
		t.Fatalf("stop within %s: %v", stopTimeout, err)
	}
	select {
	case err := <-answer:
		if st := status.Convert(err); st.Code() != codes.Unavailable || !strings.Contains(st.Message(), "shutting down") {
			t.Errorf("the waiting LeaseRun ended with %v, want Unavailable from the stopping control plane", err)
		}
	case <-ctx.Done():
		t.Fatalf("the waiting LeaseRun never ended: %v", ctx.Err())
	}
	grpcStop, poolStop := order.index(grpcHooks), order.index(poolHooks)
	if grpcStop < 0 || poolStop < 0 || grpcStop > poolStop {
		t.Errorf("stop order %v: want the gRPC server (index %d) stopped before the database pool (index %d), both present",
			order.callers, grpcStop, poolStop)
	}
}

// A gRPC port another process binds between the pick and the server's own bind costs one
// attempt, not the test (#296): the first pick is a port the test holds open.
func TestStartOnFreePortTriesAnotherPort(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), shutdownTestTimeout)
	defer cancel()
	taken, err := (&net.ListenConfig{}).Listen(ctx, "tcp", anyLoopback)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := taken.Close(); closeErr != nil {
			t.Errorf("close listener: %v", closeErr)
		}
	})
	var picked []string
	pick := func(t *testing.T) string {
		t.Helper()
		addr := freeAddr(t)
		if len(picked) == 0 {
			addr = taken.Addr().String()
		}
		picked = append(picked, addr)
		return addr
	}
	app, addr := startOnFreePort(ctx, t, pick, func(grpcAddr string) *fx.App {
		cfg := controlPlaneConfig(t, grpcAddr)
		return fx.New(
			fx.Supply(buildinfo.Info{Version: "test"}), Module,
			fx.Decorate(func(*gconfig.Config) *gconfig.Config { return cfg }),
			fx.NopLogger,
		)
	})
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.WithoutCancel(t.Context()), stopTimeout)
		defer stopCancel()
		if stopErr := app.Stop(stopCtx); stopErr != nil {
			t.Errorf("stop: %v", stopErr)
		}
	})
	// Other processes may take later picks too; the server must run on the last one.
	if len(picked) < 2 || addr != picked[len(picked)-1] {
		t.Errorf("picked %v, serving on %s; want the taken %s skipped and the last pick served", picked, addr, taken.Addr())
	}
}
