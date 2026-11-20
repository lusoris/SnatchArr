// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package tvdbprovider

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/rueidis"

	rediscache "github.com/golusoris/golusoris/cache/redis"
)

func TestHTTPCacheStoreTimeout(t *testing.T) {
	t.Parallel()
	opts := httpCacheOptions("example.test", config{l2: "none", shared: true})
	if opts.StoreTimeout != storeTimeout {
		t.Fatalf("StoreTimeout = %v, want %v", opts.StoreTimeout, storeTimeout)
	}
	if storeTimeout >= closeWaitTimeout {
		t.Fatalf("storeTimeout = %v, want below closeWaitTimeout %v", storeTimeout, closeWaitTimeout)
	}
}

func TestRedisClientOptionInstallsLoopbackGuard(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		password  string
		tls       bool
		plaintext bool
		wantGuard bool
	}{
		{name: "credentialed plaintext", password: "planted-password", wantGuard: true},
		{name: "TLS", password: "planted-password", tls: true},
		{name: "plaintext opt-in", password: "planted-password", plaintext: true},
		{name: "no password"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config{
				redis:          rediscache.Options{Addr: "localhost:6379", Password: tc.password, TLS: tc.tls},
				redisPlaintext: tc.plaintext,
			}
			opts, err := redisClientOptionForCache(cfg, slog.New(slog.DiscardHandler))
			if err != nil {
				t.Fatalf("redisClientOptionForCache() error = %v", err)
			}
			if got := opts.DialCtxFn != nil; got != tc.wantGuard {
				t.Fatalf("DialCtxFn installed = %t, want %t", got, tc.wantGuard)
			}
		})
	}
}

func TestLoopbackOnlyDial(t *testing.T) {
	t.Parallel()
	t.Run("remote refused without dialing", testRemoteRedisDialRefused)
	t.Run("loopback accepted", testLoopbackRedisDialAccepted)
	t.Run("TLS configuration refused", testTLSRedisDialRefused)
}

func testRemoteRedisDialRefused(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	started := time.Now()
	conn, err := loopbackOnlyDial(ctx, "192.0.2.1:6379", &net.Dialer{}, nil)
	if conn != nil || !errors.Is(err, errRedisNotLoopback) {
		t.Fatalf("loopbackOnlyDial() = %v, %v, want nil and errRedisNotLoopback", conn, err)
	}
	if elapsed := time.Since(started); elapsed >= 250*time.Millisecond {
		t.Fatalf("remote refusal elapsed = %v, want below 250ms", elapsed)
	}
}

func testLoopbackRedisDialAccepted(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	if tcp, ok := listener.(*net.TCPListener); ok {
		if deadlineErr := tcp.SetDeadline(time.Now().Add(500 * time.Millisecond)); deadlineErr != nil {
			t.Fatalf("SetDeadline() error = %v", deadlineErr)
		}
	}
	accepted := make(chan error, 1)
	go acceptOne(listener, accepted)
	conn, err := loopbackOnlyDial(ctx, listener.Addr().String(), &net.Dialer{}, nil)
	if err != nil {
		t.Fatalf("loopbackOnlyDial() error = %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := <-accepted; err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
}

func acceptOne(listener net.Listener, result chan<- error) {
	conn, err := listener.Accept()
	if err == nil {
		err = conn.Close()
	}
	result <- err
}

func testTLSRedisDialRefused(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	conn, err := loopbackOnlyDial(ctx, "127.0.0.1:6379", &net.Dialer{}, &tls.Config{MinVersion: tls.VersionTLS12})
	if conn != nil || !errors.Is(err, errRedisNotLoopback) {
		t.Fatalf("loopbackOnlyDial() = %v, %v, want nil and errRedisNotLoopback", conn, err)
	}
}

type nonTCPAddr struct{}

func (nonTCPAddr) Network() string { return "test" }
func (nonTCPAddr) String() string  { return "test" }

func TestIsLoopbackPeer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		addr net.Addr
		want bool
	}{
		{name: "IPv4", addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1")}, want: true},
		{name: "IPv6", addr: &net.TCPAddr{IP: net.ParseIP("::1")}, want: true},
		{name: "remote", addr: &net.TCPAddr{IP: net.ParseIP("192.0.2.1")}},
		{name: "nil"},
		{name: "non-TCP", addr: nonTCPAddr{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isLoopbackPeer(tc.addr); got != tc.want {
				t.Fatalf("isLoopbackPeer(%v) = %t, want %t", tc.addr, got, tc.want)
			}
		})
	}
}

type blockingRedisClient struct{ rueidis.Client }

func (blockingRedisClient) Do(ctx context.Context, _ rueidis.Completed) rueidis.RedisResult {
	<-ctx.Done()
	return rueidis.NewErrorResult(ctx.Err())
}

func (blockingRedisClient) DoMulti(ctx context.Context, multi ...rueidis.Completed) []rueidis.RedisResult {
	<-ctx.Done()
	results := make([]rueidis.RedisResult, len(multi))
	for i := range results {
		results[i] = rueidis.NewErrorResult(ctx.Err())
	}
	return results
}

type boundRedisCall func(context.Context, rueidis.Client) error

func TestBoundRedisDoBounds(t *testing.T) {
	t.Parallel()
	testBoundRedisCalls(t, func(ctx context.Context, client rueidis.Client) error {
		return client.Do(ctx, rueidis.Completed{}).Error()
	})
}

func TestBoundRedisDoMultiBounds(t *testing.T) {
	t.Parallel()
	testBoundRedisCalls(t, func(ctx context.Context, client rueidis.Client) error {
		return client.DoMulti(ctx, rueidis.Completed{})[0].Error()
	})
}

func testBoundRedisCalls(t *testing.T, call boundRedisCall) {
	t.Helper()
	t.Run("shutdown", func(t *testing.T) { t.Parallel(); testBoundRedisShutdown(t, call) })
	t.Run("active lookup deadline", func(t *testing.T) { t.Parallel(); testBoundRedisActiveLookup(t, call) })
	t.Run("caller deadline without active lookup", func(t *testing.T) { t.Parallel(); testBoundRedisCaller(t, call, false) })
	t.Run("cleared active lookup", func(t *testing.T) { t.Parallel(); testBoundRedisCaller(t, call, true) })
}

func testBoundRedisShutdown(t *testing.T, call boundRedisCall) {
	t.Helper()
	active := &atomicContextPointer{}
	lifetime, stopLifetime := context.WithCancel(t.Context())
	client := newBoundRedis(blockingRedisClient{}, redisBounds{lifetime: func() context.Context { return lifetime }, active: &active.Pointer})
	caller, stopCaller := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer stopCaller()
	result := make(chan error, 1)
	started := time.Now()
	go func() { result <- call(caller, client) }()
	stopLifetime()
	assertPromptRedisCancellation(t, result, started, context.Canceled)
}

func testBoundRedisActiveLookup(t *testing.T, call boundRedisCall) {
	t.Helper()
	active := &atomicContextPointer{}
	lifetime, stopLifetime := context.WithCancel(t.Context())
	defer stopLifetime()
	lookup, stopLookup := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer stopLookup()
	active.Store(&lookup)
	client := newBoundRedis(blockingRedisClient{}, redisBounds{lifetime: func() context.Context { return lifetime }, active: &active.Pointer})
	caller, stopCaller := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer stopCaller()
	started := time.Now()
	result := make(chan error, 1)
	go func() { result <- call(caller, client) }()
	assertPromptRedisCancellation(t, result, started, context.Canceled)
}

func testBoundRedisCaller(t *testing.T, call boundRedisCall, clearActive bool) {
	t.Helper()
	active := &atomicContextPointer{}
	if clearActive {
		stale, cancel := context.WithCancel(t.Context())
		cancel()
		active.Store(&stale)
		active.Store(nil)
	}
	lifetime, stopLifetime := context.WithCancel(t.Context())
	defer stopLifetime()
	client := newBoundRedis(blockingRedisClient{}, redisBounds{lifetime: func() context.Context { return lifetime }, active: &active.Pointer})
	caller, stopCaller := context.WithTimeout(t.Context(), 40*time.Millisecond)
	defer stopCaller()
	if err := call(caller, client); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Redis call error = %v, want context.DeadlineExceeded", err)
	}
}

func assertPromptRedisCancellation(t *testing.T, result <-chan error, started time.Time, want error) {
	t.Helper()
	err := <-result
	if !errors.Is(err, want) {
		t.Fatalf("Redis call error = %v, want %v", err, want)
	}
	if elapsed := time.Since(started); elapsed >= 250*time.Millisecond {
		t.Fatalf("Redis cancellation elapsed = %v, want below 250ms", elapsed)
	}
}

type atomicContextPointer struct {
	Pointer atomic.Pointer[context.Context]
}

func (p *atomicContextPointer) Store(ctx *context.Context) { p.Pointer.Store(ctx) }
