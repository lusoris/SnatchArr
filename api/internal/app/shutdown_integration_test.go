// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package app

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
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

// freeAddr is a loopback address nothing listens on.
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
		"http":      map[string]any{"addr": freeAddr(t)},
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
	grpcAddr := freeAddr(t)
	cfg := controlPlaneConfig(t, grpcAddr)
	order := &stopOrder{}
	// Closed once the server has handed a long-poll to the handler.
	waiting, once := make(chan struct{}), sync.Once{}
	seen := func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if lease, ok := req.(*snatcharrv1.LeaseRunRequest); ok && lease.GetWaitSeconds() > 0 {
			once.Do(func() { close(waiting) })
		}
		return handler(ctx, req)
	}
	app := fx.New(
		fx.Supply(buildinfo.Info{Version: "test"}), Module,
		fx.Decorate(func(*gconfig.Config) *gconfig.Config { return cfg }),
		grpcx.ProvideServerOption(grpc.ChainUnaryInterceptor(seen)),
		fx.WithLogger(func() fxevent.Logger { return order }),
	)
	if err := app.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
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
