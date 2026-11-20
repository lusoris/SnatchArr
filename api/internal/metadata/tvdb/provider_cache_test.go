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
	"syscall"
	"testing"
	"time"

	"github.com/redis/rueidis"

	rediscache "github.com/golusoris/golusoris/cache/redis"
)

// redisTestBackstop only makes a broken bound fail instead of hanging the test;
// it is never the boundary under test.
const redisTestBackstop = 10 * time.Second

var errUnexpectedRedisDial = errors.New("unexpected Redis dial attempt")

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
	ctx, cancel := context.WithTimeout(t.Context(), redisTestBackstop)
	defer cancel()
	assertRedisDialRefusedBeforeAttempt(t, ctx, "192.0.2.1:6379", nil)
}

func testLoopbackRedisDialAccepted(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), redisTestBackstop)
	defer cancel()
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	if tcp, ok := listener.(*net.TCPListener); ok {
		if deadlineErr := tcp.SetDeadline(time.Now().Add(redisTestBackstop)); deadlineErr != nil {
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
	ctx, cancel := context.WithTimeout(t.Context(), redisTestBackstop)
	defer cancel()
	assertRedisDialRefusedBeforeAttempt(t, ctx, "127.0.0.1:6379", &tls.Config{MinVersion: tls.VersionTLS12})
}

func assertRedisDialRefusedBeforeAttempt(t *testing.T, ctx context.Context, dst string, tlsConfig *tls.Config) {
	t.Helper()
	attempted := &atomic.Bool{}
	dialer := &net.Dialer{ControlContext: func(context.Context, string, string, syscall.RawConn) error {
		attempted.Store(true)
		return errUnexpectedRedisDial
	}}
	conn, err := loopbackOnlyDial(ctx, dst, dialer, tlsConfig)
	if conn != nil {
		t.Fatalf("loopbackOnlyDial() connection = %v, want nil", conn)
	}
	if !errors.Is(err, errRedisNotLoopback) {
		t.Fatalf("loopbackOnlyDial() error = %v, want errRedisNotLoopback", err)
	}
	if attempted.Load() {
		t.Fatal("loopbackOnlyDial() attempted a network dial, want refusal before dialing")
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

type blockingRedisClient struct {
	rueidis.Client
	held chan<- struct{}
}

func (c blockingRedisClient) Do(ctx context.Context, _ rueidis.Completed) rueidis.RedisResult {
	return rueidis.NewErrorResult(c.wait(ctx))
}

func (c blockingRedisClient) DoMulti(ctx context.Context, multi ...rueidis.Completed) []rueidis.RedisResult {
	err := c.wait(ctx)
	results := make([]rueidis.RedisResult, len(multi))
	for i := range results {
		results[i] = rueidis.NewErrorResult(err)
	}
	return results
}

func (c blockingRedisClient) wait(ctx context.Context) error {
	if c.held != nil {
		select {
		case c.held <- struct{}{}:
		default:
		}
	}
	<-ctx.Done()
	return ctx.Err()
}

type redisDeadlineContext struct {
	context.Context
	done    chan struct{}
	expired atomic.Bool
}

func newRedisDeadlineContext(parent context.Context) *redisDeadlineContext {
	return &redisDeadlineContext{Context: parent, done: make(chan struct{})}
}

func (c *redisDeadlineContext) Done() <-chan struct{} { return c.done }

func (c *redisDeadlineContext) Err() error {
	if c.expired.Load() {
		return context.DeadlineExceeded
	}
	return nil
}

func (c *redisDeadlineContext) expire() {
	if c.expired.CompareAndSwap(false, true) {
		close(c.done)
	}
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
	held := make(chan struct{}, 1)
	client := newBoundRedis(blockingRedisClient{held: held}, redisBounds{lifetime: func() context.Context { return lifetime }, active: &active.Pointer})
	caller, stopCaller := context.WithCancel(t.Context())
	defer stopCaller()
	result := make(chan error, 1)
	go func() { result <- call(caller, client) }()
	waitForRedisHeld(t, held)
	stopLifetime()
	assertRedisCancellation(t, result, context.Canceled)
}

func testBoundRedisActiveLookup(t *testing.T, call boundRedisCall) {
	t.Helper()
	active := &atomicContextPointer{}
	lifetime, stopLifetime := context.WithCancel(t.Context())
	defer stopLifetime()
	lookup, stopLookup := context.WithCancel(t.Context())
	defer stopLookup()
	active.Store(&lookup)
	held := make(chan struct{}, 1)
	client := newBoundRedis(blockingRedisClient{held: held}, redisBounds{lifetime: func() context.Context { return lifetime }, active: &active.Pointer})
	caller, stopCaller := context.WithCancel(t.Context())
	defer stopCaller()
	result := make(chan error, 1)
	go func() { result <- call(caller, client) }()
	waitForRedisHeld(t, held)
	stopLookup()
	assertRedisCancellation(t, result, context.Canceled)
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
	held := make(chan struct{}, 1)
	client := newBoundRedis(blockingRedisClient{held: held}, redisBounds{lifetime: func() context.Context { return lifetime }, active: &active.Pointer})
	caller := newRedisDeadlineContext(t.Context())
	result := make(chan error, 1)
	go func() { result <- call(caller, client) }()
	waitForRedisHeld(t, held)
	caller.expire()
	if err := waitForRedisResult(t, result); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Redis call error = %v, want context.DeadlineExceeded", err)
	}
}

func waitForRedisHeld(t *testing.T, held <-chan struct{}) {
	t.Helper()
	timer := time.NewTimer(redisTestBackstop)
	defer timer.Stop()
	select {
	case <-held:
	case <-timer.C:
		t.Fatal("blocking Redis client did not hold the call before the backstop")
	}
}

func waitForRedisResult(t *testing.T, result <-chan error) error {
	t.Helper()
	timer := time.NewTimer(redisTestBackstop)
	defer timer.Stop()
	select {
	case err := <-result:
		return err
	case <-timer.C:
		t.Fatal("bound Redis call did not return before the backstop")
		return nil
	}
}

func assertRedisCancellation(t *testing.T, result <-chan error, want error) {
	t.Helper()
	if err := waitForRedisResult(t, result); !errors.Is(err, want) {
		t.Fatalf("Redis call error = %v, want %v", err, want)
	}
}

type atomicContextPointer struct {
	Pointer atomic.Pointer[context.Context]
}

func (p *atomicContextPointer) Store(ctx *context.Context) { p.Pointer.Store(ctx) }
