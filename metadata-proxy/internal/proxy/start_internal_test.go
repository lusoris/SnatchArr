// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package proxy

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/lusoris/SnatchArr/metadata-proxy/internal/config"
	"github.com/lusoris/SnatchArr/metadata-proxy/internal/metrics"
)

type lockedLogBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedLogBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedLogBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type closeErrListener struct {
	net.Listener
	closeErr error
}

func (l *closeErrListener) Close() error {
	_ = l.Listener.Close()
	return l.closeErr
}

func startInternalEchoTarget(t *testing.T) int {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen internal echo: %v", err)
	}
	var (
		mu     sync.Mutex
		closed bool
		open   []net.Conn
		wg     sync.WaitGroup
	)
	wg.Go(func() {
		for range 16 {
			c, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			_ = c.SetDeadline(time.Now().Add(5 * time.Second))
			mu.Lock()
			if closed {
				mu.Unlock()
				_ = c.Close()
				return
			}
			open = append(open, c)
			mu.Unlock()
			wg.Go(func() {
				defer c.Close()
				br := bufio.NewReader(c)
				for range 8 {
					line, readErr := br.ReadString('\n')
					if len(line) > 0 {
						_, _ = io.WriteString(c, line)
					}
					if readErr != nil {
						return
					}
				}
			})
		}
	})
	t.Cleanup(func() {
		_ = ln.Close()
		mu.Lock()
		closed = true
		toClose := append([]net.Conn(nil), open...)
		mu.Unlock()
		for _, c := range toClose {
			_ = c.Close()
		}
		wg.Wait()
	})
	return ln.Addr().(*net.TCPAddr).Port
}

func newTwoListenerService(t *testing.T, echoPort int) (*Service, *lockedLogBuf) {
	t.Helper()
	opts := config.DefaultOptions()
	opts.Mode = config.ModeLog
	opts.Listeners = []config.Listener{
		{App: "sonarr", Port: 18081},
		{App: "radarr", Port: 18082},
	}
	opts.AllowHosts = []string{"localhost"}
	opts.Ports = []int{echoPort}
	compiled, err := config.Compile(opts)
	if err != nil {
		t.Fatalf("config.Compile: %v", err)
	}
	rec, err := metrics.New(compiled, prometheus.NewRegistry())
	if err != nil {
		t.Fatalf("metrics.New: %v", err)
	}
	logs := &lockedLogBuf{}
	logger := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	svc, err := New(
		compiled,
		rec,
		clockwork.NewRealClock(),
		logger,
		WithBindHost("127.0.0.1"),
		WithEphemeralPorts(),
		WithDialControl(nil),
	)
	if err != nil {
		t.Fatalf("proxy.New: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = svc.Stop(stopCtx)
	})
	return svc, logs
}

func TestStartRollbackBeforeServe(t *testing.T) {
	t.Parallel()

	echoPort := startInternalEchoTarget(t)
	svc, _ := newTwoListenerService(t, echoPort)

	errBindSecond := errors.New("injected second listener bind failure")
	secondEntered := make(chan struct{})
	releaseSecond := make(chan struct{})
	var releaseOnce sync.Once
	unblockSecond := func() { releaseOnce.Do(func() { close(releaseSecond) }) }
	defer unblockSecond()

	var bindCalls atomic.Int32
	var firstBoundAddr atomic.Value
	svc.listen = func(ctx context.Context, network, addr string) (net.Listener, error) {
		if bindCalls.Add(1) == 1 {
			ln, listenErr := (&net.ListenConfig{}).Listen(ctx, network, addr)
			if listenErr == nil {
				firstBoundAddr.Store(ln.Addr().String())
			}
			return ln, listenErr
		}
		close(secondEntered)
		select {
		case <-releaseSecond:
			return nil, errBindSecond
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	startCtx, startCancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer startCancel()

	startErrCh := make(chan error, 1)
	go func() {
		startErrCh <- svc.Start(startCtx)
	}()

	select {
	case <-secondEntered:
	case earlyErr := <-startErrCh:
		t.Fatalf("Start returned before second listener bind blocked: %v", earlyErr)
	case <-startCtx.Done():
		t.Fatalf("timed out waiting for second listener bind: %v", startCtx.Err())
	}

	addr0, ok := firstBoundAddr.Load().(string)
	if !ok || addr0 == "" {
		t.Fatal("first listener address was not recorded")
	}

	clientConn, dialErr := (&net.Dialer{}).DialContext(startCtx, "tcp", addr0)
	if dialErr != nil {
		t.Fatalf("dial listener 0 (%s): %v", addr0, dialErr)
	}
	t.Cleanup(func() { _ = clientConn.Close() })

	target := fmt.Sprintf("localhost:%d", echoPort)
	rawReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	if deadlineErr := clientConn.SetWriteDeadline(time.Now().Add(2 * time.Second)); deadlineErr != nil {
		t.Fatalf("SetWriteDeadline: %v", deadlineErr)
	}
	if _, writeErr := io.WriteString(clientConn, rawReq); writeErr != nil {
		t.Fatalf("write CONNECT to listener 0: %v", writeErr)
	}

	if deadlineErr := clientConn.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); deadlineErr != nil {
		t.Fatalf("SetReadDeadline 300ms: %v", deadlineErr)
	}
	buf := make([]byte, 256)
	n, readErr := clientConn.Read(buf)
	if n > 0 {
		t.Fatalf("listener 0 answered CONNECT before listener 1 bound: %q", string(buf[:n]))
	}
	if !errors.Is(readErr, os.ErrDeadlineExceeded) {
		t.Fatalf("expected 300ms read deadline timeout on unserved listener 0, got: %v", readErr)
	}

	unblockSecond()

	var startErr error
	select {
	case startErr = <-startErrCh:
	case <-startCtx.Done():
		t.Fatalf("timed out waiting for Start to return bind error: %v", startCtx.Err())
	}
	if !errors.Is(startErr, errBindSecond) {
		t.Fatalf("Start error = %v, want errors.Is(_, %v)", startErr, errBindSecond)
	}

	if deadlineErr := clientConn.SetReadDeadline(time.Now().Add(2 * time.Second)); deadlineErr != nil {
		t.Fatalf("SetReadDeadline post-rollback: %v", deadlineErr)
	}
	n, postErr := clientConn.Read(buf)
	if postErr == nil || n > 0 {
		t.Fatalf("expected EOF or reset on client conn after rollback, got n=%d data=%q err=%v", n, string(buf[:n]), postErr)
	}
	if errors.Is(postErr, os.ErrDeadlineExceeded) {
		t.Fatalf("client conn timed out instead of reading EOF or reset after rollback: %v", postErr)
	}

	for _, app := range []string{"sonarr", "radarr"} {
		if gotAddr, bound := svc.Addr(app); bound || gotAddr != "" {
			t.Fatalf("Addr(%s) after failed Start = (%q, %v), want (\"\", false)", app, gotAddr, bound)
		}
	}
	if svc.ready.Load() {
		t.Fatal("svc.ready is true after failed Start, want false")
	}
	if readyErr := svc.ReadinessCheck().Fn(startCtx); !errors.Is(readyErr, ErrListenersNotBound) {
		t.Fatalf("ReadinessCheck after failed Start = %v, want %v", readyErr, ErrListenersNotBound)
	}
}

func TestStartRollbackLogsCloseErrorAtWarn(t *testing.T) {
	t.Parallel()

	echoPort := startInternalEchoTarget(t)
	svc, logs := newTwoListenerService(t, echoPort)

	errCloseFirst := errors.New("injected listener close failure")
	errBindSecond := errors.New("injected second bind failure")
	var bindCalls atomic.Int32
	svc.listen = func(ctx context.Context, network, addr string) (net.Listener, error) {
		if bindCalls.Add(1) == 1 {
			ln, listenErr := (&net.ListenConfig{}).Listen(ctx, network, addr)
			if listenErr != nil {
				return nil, listenErr
			}
			return &closeErrListener{Listener: ln, closeErr: errCloseFirst}, nil
		}
		return nil, errBindSecond
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	startErr := svc.Start(ctx)
	if !errors.Is(startErr, errBindSecond) {
		t.Fatalf("Start error = %v, want errors.Is(_, %v)", startErr, errBindSecond)
	}
	if errors.Is(startErr, errCloseFirst) {
		t.Fatalf("Start returned close error instead of bind error: %v", startErr)
	}
	if gotAddr, bound := svc.Addr("sonarr"); bound || gotAddr != "" {
		t.Fatalf("Addr(sonarr) = (%q, %v), want (\"\", false)", gotAddr, bound)
	}
	if svc.ready.Load() {
		t.Fatal("svc.ready is true after failed Start, want false")
	}
	logOut := logs.String()
	if !strings.Contains(logOut, `"level":"WARN"`) || !strings.Contains(logOut, errCloseFirst.Error()) {
		t.Fatalf("expected WARN log containing close error, got:\n%s", logOut)
	}
}

func TestStartBothListenersServeConnect(t *testing.T) {
	t.Parallel()

	echoPort := startInternalEchoTarget(t)
	svc, _ := newTwoListenerService(t, echoPort)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if startErr := svc.Start(ctx); startErr != nil {
		t.Fatalf("svc.Start: %v", startErr)
	}
	if readyErr := svc.ReadinessCheck().Fn(ctx); readyErr != nil {
		t.Fatalf("ReadinessCheck after Start: %v", readyErr)
	}

	target := fmt.Sprintf("localhost:%d", echoPort)
	rawReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)

	for _, app := range []string{"sonarr", "radarr"} {
		proxyAddr, ok := svc.Addr(app)
		if !ok {
			t.Fatalf("missing bound address for %s", app)
		}
		assertConnectRoundTrip(t, ctx, app, proxyAddr, rawReq)
	}
}

func assertConnectRoundTrip(t *testing.T, ctx context.Context, app, proxyAddr, rawReq string) {
	t.Helper()
	conn, dialErr := (&net.Dialer{}).DialContext(ctx, "tcp", proxyAddr)
	if dialErr != nil {
		t.Fatalf("dial %s (%s): %v", app, proxyAddr, dialErr)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, writeErr := io.WriteString(conn, rawReq); writeErr != nil {
		t.Fatalf("write CONNECT on %s: %v", app, writeErr)
	}
	br := bufio.NewReader(conn)
	resp, readErr := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if readErr != nil {
		t.Fatalf("read CONNECT response on %s: %v", app, readErr)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s CONNECT status = %d, want 200", app, resp.StatusCode)
	}
	msg := "hello-" + app + "\n"
	if _, writeErr := io.WriteString(conn, msg); writeErr != nil {
		t.Fatalf("write payload on %s: %v", app, writeErr)
	}
	got, echoErr := br.ReadString('\n')
	if echoErr != nil {
		t.Fatalf("read echo on %s: %v", app, echoErr)
	}
	if got != msg {
		t.Fatalf("%s echoed %q, want %q", app, got, msg)
	}
}
