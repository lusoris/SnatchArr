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
	"net"
	"net/http"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/lusoris/SnatchArr/metadata-proxy/internal/config"
	"github.com/lusoris/SnatchArr/metadata-proxy/internal/metrics"
)

func startServiceForTest(tb testing.TB, svc *Service) {
	tb.Helper()
	startCtx, startCancel := context.WithTimeout(tb.Context(), 5*time.Second)
	tb.Cleanup(startCancel)
	if err := svc.Start(startCtx); err != nil {
		tb.Fatalf("svc.Start: %v", err)
	}
	tb.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		if err := svc.Stop(stopCtx); err != nil {
			tb.Errorf("svc.Stop cleanup: %v", err)
		}
	})
}

func TestGeneralOptionsReachHandler(t *testing.T) {
	t.Parallel()
	svc := newDialControlTestService(t, WithBindHost("127.0.0.1"), WithEphemeralPorts())
	startServiceForTest(t, svc)

	cases := []struct {
		name       string
		request    string
		method     string
		wantStatus int
		wantAllow  string
	}{
		{name: "general options", request: "OPTIONS * HTTP/1.1\r\nHost: x\r\n\r\n", method: http.MethodOptions, wantStatus: http.StatusMethodNotAllowed, wantAllow: http.MethodConnect},
		{name: "path options", request: "OPTIONS /x HTTP/1.1\r\nHost: x\r\n\r\n", method: http.MethodOptions, wantStatus: http.StatusMethodNotAllowed, wantAllow: http.MethodConnect},
		{name: "connect reaches handler", request: "CONNECT x HTTP/1.1\r\nHost: x\r\n\r\n", method: http.MethodConnect, wantStatus: http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			conn, dialErr := (&net.Dialer{}).DialContext(ctx, "tcp", mustProxyAddr(t, svc))
			if dialErr != nil {
				t.Fatalf("dial proxy: %v", dialErr)
			}
			defer conn.Close()
			if deadlineErr := conn.SetDeadline(svc.clock.Now().Add(5 * time.Second)); deadlineErr != nil {
				t.Fatalf("SetDeadline: %v", deadlineErr)
			}
			if _, writeErr := io.WriteString(conn, tc.request); writeErr != nil {
				t.Fatalf("write request: %v", writeErr)
			}
			resp, responseErr := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: tc.method})
			if responseErr != nil {
				t.Fatalf("http.ReadResponse: %v", responseErr)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if got := resp.Header.Get("Allow"); got != tc.wantAllow {
				t.Fatalf("Allow = %q, want %q", got, tc.wantAllow)
			}
		})
	}
}

func mustProxyAddr(tb testing.TB, svc *Service) string {
	tb.Helper()
	addr, ok := svc.Addr("sonarr")
	if !ok {
		tb.Fatal("sonarr listener is not bound")
	}
	return addr
}

type failingAcceptListener struct{ err error }

func (l failingAcceptListener) Accept() (net.Conn, error) { return nil, l.err }
func (failingAcceptListener) Close() error                { return nil }
func (failingAcceptListener) Addr() net.Addr              { return &net.TCPAddr{} }

func TestServeFailurePermanentlyFailsReadiness(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name            string
		serveErr        error
		wantReady       bool
		wantServeFailed bool
	}{
		{name: "permanent accept failure", serveErr: errors.New("accept failed"), wantServeFailed: true},
		{name: "server closed", serveErr: http.ErrServerClosed, wantReady: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc := newDialControlTestService(t)
			al := svc.listeners[0]
			svc.ready.Store(true)
			if errors.Is(tc.serveErr, http.ErrServerClosed) {
				if err := al.srv.Close(); err != nil {
					t.Fatalf("server Close: %v", err)
				}
				svc.ready.Store(true)
			}
			svc.serveLoop(context.Background(), al, failingAcceptListener{err: tc.serveErr})
			if got := svc.serveFailed.Load(); got != tc.wantServeFailed {
				t.Fatalf("serveFailed = %v, want %v", got, tc.wantServeFailed)
			}
			readyErr := svc.ReadinessCheck().Fn(context.Background())
			if tc.wantReady && readyErr != nil {
				t.Fatalf("ReadinessCheck after ErrServerClosed: %v", readyErr)
			}
			if !tc.wantReady && readyErr == nil {
				t.Fatal("ReadinessCheck after permanent Serve failure = nil")
			}
		})
	}
}

func TestStartKeepsEarlierServeFailure(t *testing.T) {
	t.Parallel()
	svc := newDialControlTestService(t, WithBindHost("127.0.0.1"), WithEphemeralPorts())
	svc.serveFailed.Store(true)
	startServiceForTest(t, svc)
	if err := svc.ReadinessCheck().Fn(t.Context()); err == nil {
		t.Fatal("ReadinessCheck after Start hid earlier serve failure")
	}
	if svc.ready.Load() {
		t.Fatal("Start set ready despite the earlier serve failure")
	}
}

type cleanupRecorderTB struct {
	testing.TB
	cleanups []func()
}

func (c *cleanupRecorderTB) Cleanup(fn func()) {
	c.cleanups = append(c.cleanups, fn)
}

func (c *cleanupRecorderTB) runAll() {
	for _, fn := range slices.Backward(c.cleanups) {
		fn()
	}
}

func isInternalConnRefused(err error) bool {
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	return runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(10061))
}

func awaitListenerServing(t *testing.T, addr string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, dialErr := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if dialErr != nil {
		t.Fatalf("dial bound listener: %v", dialErr)
	}
	defer conn.Close()
	if deadlineErr := conn.SetDeadline(time.Now().Add(5 * time.Second)); deadlineErr != nil {
		t.Fatalf("SetDeadline: %v", deadlineErr)
	}
	if _, writeErr := io.WriteString(conn, "OPTIONS * HTTP/1.1\r\nHost: x\r\n\r\n"); writeErr != nil {
		t.Fatalf("write probe request: %v", writeErr)
	}
	resp, readErr := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodOptions})
	if readErr != nil {
		t.Fatalf("read probe response: %v", readErr)
	}
	_ = resp.Body.Close()
}

func TestStartServiceForTestRegistersCleanupBeforeAssertions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		useHelper     bool
		repeatCleanup bool
		wantOpen      bool
	}{
		{name: "positive: helper registers Stop before post-Start assertions", useHelper: true},
		{name: "negative: skipping Stop registration leaves listener open on early abort", wantOpen: true},
		{name: "boundary: repeated cleanup run stays idempotent", useHelper: true, repeatCleanup: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runStartCleanupCase(t, tc.useHelper, tc.repeatCleanup, tc.wantOpen)
		})
	}
}

func runStartCleanupCase(t *testing.T, useHelper, repeatCleanup, wantOpen bool) {
	t.Helper()
	svc := newDialControlTestService(t, WithBindHost("127.0.0.1"), WithEphemeralPorts())
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = svc.Stop(stopCtx)
	})
	rec := &cleanupRecorderTB{TB: t}
	if useHelper {
		startServiceForTest(rec, svc)
	} else {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		rec.Cleanup(cancel)
		if err := svc.Start(ctx); err != nil {
			t.Fatalf("svc.Start: %v", err)
		}
	}
	addr := mustProxyAddr(t, svc)
	awaitListenerServing(t, addr)
	rec.runAll()
	if repeatCleanup {
		rec.runAll()
	}
	dialCtx, dialCancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer dialCancel()
	conn, dialErr := (&net.Dialer{}).DialContext(dialCtx, "tcp", addr)
	if dialErr == nil {
		_ = conn.Close()
	}
	if wantOpen && dialErr != nil {
		t.Fatalf("expected uncleaned listener to accept dial, got: %v", dialErr)
	}
	if !wantOpen && !isInternalConnRefused(dialErr) {
		t.Fatalf("expected ECONNREFUSED after recorded cleanup, got: %v", dialErr)
	}
}

type hijackResponseWriter struct{ conn net.Conn }

func (h hijackResponseWriter) Header() http.Header         { return make(http.Header) }
func (h hijackResponseWriter) Write(p []byte) (int, error) { return len(p), nil }
func (h hijackResponseWriter) WriteHeader(int)             {}
func (h hijackResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	reader := bufio.NewReader(bytes.NewBufferString("buffered"))
	if _, err := reader.Peek(len("buffered")); err != nil {
		return nil, nil, err
	}
	clientBuffer := bufio.NewReadWriter(reader, bufio.NewWriter(io.Discard))
	return h.conn, clientBuffer, nil
}

type signaledWriteConn struct {
	net.Conn
	started chan struct{}
	once    sync.Once
}

func (c *signaledWriteConn) Write(p []byte) (int, error) {
	c.once.Do(func() { close(c.started) })
	return c.Conn.Write(p)
}

func TestBridgeTunnelShutdownDuringBufferedWrite(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		stop bool
	}{
		{name: "stop interrupts blocked buffered write", stop: true},
		{name: "buffered bytes pass when upstream reads"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runBridgeBufferedWriteCase(t, tc.stop)
		})
	}
}

func runBridgeBufferedWriteCase(t *testing.T, stop bool) {
	t.Helper()
	svc := newDialControlTestService(t)
	clientConn, clientPeer := net.Pipe()
	upstreamConn, upstreamPeer := net.Pipe()
	defer clientPeer.Close()
	defer upstreamPeer.Close()
	if err := clientPeer.SetDeadline(svc.clock.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("client SetDeadline: %v", err)
	}
	if err := upstreamPeer.SetDeadline(svc.clock.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("upstream SetDeadline: %v", err)
	}
	clientDrained := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, clientPeer); clientDrained <- err }()

	started := make(chan struct{})
	upstream := &signaledWriteConn{Conn: upstreamConn, started: started}
	result := make(chan struct{}, 1)
	go func() {
		_, _, _ = svc.bridgeTunnel(hijackResponseWriter{conn: clientConn}, upstream)
		close(result)
	}()
	if stop {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("buffered write did not start")
		}
		svc.stopOnce.Do(func() { close(svc.stopCh) })
	} else {
		got := make([]byte, len("buffered"))
		if _, err := io.ReadFull(upstreamPeer, got); err != nil {
			t.Fatalf("read buffered upstream bytes: %v", err)
		}
		if string(got) != "buffered" {
			t.Fatalf("upstream bytes = %q, want buffered", got)
		}
		_ = clientPeer.Close()
	}
	select {
	case <-result:
	case <-time.After(2 * time.Second):
		t.Fatal("bridgeTunnel did not return within 2 seconds")
	}
	if stop {
		assertTunnelPeerClosed(t, upstreamPeer, clientDrained)
	}
}

func checkTunnelPeersClosed(upstreamPeer net.Conn, clientDrained <-chan error, wait time.Duration) error {
	select {
	case drainErr := <-clientDrained:
		if drainErr != nil {
			return fmt.Errorf("client drain error = %w, want nil (clean EOF)", drainErr)
		}
	case <-time.After(wait):
		return errors.New("client connection did not close within wait bound")
	}
	n, err := upstreamPeer.Read(make([]byte, 1))
	if err == nil || n > 0 {
		return fmt.Errorf("upstream peer read = (%d, %w), want (0, io.EOF)", n, err)
	}
	if !errors.Is(err, io.EOF) {
		return fmt.Errorf("upstream peer read error = %w, want io.EOF", err)
	}
	return nil
}

func assertTunnelPeerClosed(t *testing.T, upstreamPeer net.Conn, clientDrained <-chan error) {
	t.Helper()
	if err := checkTunnelPeersClosed(upstreamPeer, clientDrained, 2*time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestCheckTunnelPeersClosed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name              string
		closeClient       bool
		closeUpstream     bool
		writeUpstreamByte bool
		wantErr           bool
	}{
		{name: "positive: clean EOF on both client and upstream pipes", closeClient: true, closeUpstream: true},
		{name: "negative: unclosed client hitting read deadline fails", closeClient: false, closeUpstream: true, wantErr: true},
		{name: "negative: unclosed upstream hitting read deadline fails", closeClient: true, closeUpstream: false, wantErr: true},
		{name: "boundary: readable byte on upstream fails closure check", closeClient: true, writeUpstreamByte: true, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := runCheckTunnelPeersCase(t, tc.closeClient, tc.closeUpstream, tc.writeUpstreamByte)
			if tc.wantErr && err == nil {
				t.Fatal("expected checkTunnelPeersClosed to fail, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("checkTunnelPeersClosed: %v", err)
			}
		})
	}
}

func runCheckTunnelPeersCase(t *testing.T, closeClient, closeUpstream, writeUpstreamByte bool) error {
	t.Helper()
	clientConn, clientPeer := net.Pipe()
	upstreamConn, upstreamPeer := net.Pipe()
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = clientPeer.Close()
		_ = upstreamConn.Close()
		_ = upstreamPeer.Close()
	})
	now := time.Now()
	_ = clientPeer.SetDeadline(now.Add(20 * time.Millisecond))
	_ = upstreamPeer.SetDeadline(now.Add(20 * time.Millisecond))
	clientDrained := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, clientPeer)
		clientDrained <- err
	}()
	if closeClient {
		_ = clientConn.Close()
	}
	if writeUpstreamByte {
		_ = upstreamPeer.SetDeadline(now.Add(2 * time.Second))
		go func() { _, _ = upstreamConn.Write([]byte("x")) }()
	} else if closeUpstream {
		_ = upstreamConn.Close()
	}
	return checkTunnelPeersClosed(upstreamPeer, clientDrained, 500*time.Millisecond)
}

func TestTunnelActivityPreservesMonotonicDeadline(t *testing.T) {
	t.Parallel()
	monoNow := time.Now()
	farFuture := time.Date(2300, time.January, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name           string
		base           time.Time
		idleTimeout    time.Duration
		tunnelDeadline time.Time
		want           time.Time
		wantMono       bool
	}{
		{
			name:           "positive: monotonic clock reading preserved on idle deadline",
			base:           monoNow,
			idleTimeout:    2 * time.Minute,
			tunnelDeadline: monoNow.Add(30 * time.Minute),
			want:           monoNow.Add(2 * time.Minute),
			wantMono:       true,
		},
		{
			name:           "negative: earlier tunnel deadline caps idle deadline and keeps monotonic reading",
			base:           monoNow,
			idleTimeout:    10 * time.Minute,
			tunnelDeadline: monoNow.Add(30 * time.Second),
			want:           monoNow.Add(30 * time.Second),
			wantMono:       true,
		},
		{
			name:           "boundary: timestamp beyond int64 UnixNano range avoids overflow",
			base:           farFuture,
			idleTimeout:    2 * time.Minute,
			tunnelDeadline: farFuture.Add(30 * time.Minute),
			want:           farFuture.Add(2 * time.Minute),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			act := newTunnelActivity(tc.base)
			got := act.deadline(tc.idleTimeout, tc.tunnelDeadline)
			if !got.Equal(tc.want) {
				t.Fatalf("deadline = %v, want %v", got, tc.want)
			}
			if hasMono := strings.Contains(got.String(), " m="); hasMono != tc.wantMono {
				t.Fatalf("deadline %q monotonic=%v, want %v", got.String(), hasMono, tc.wantMono)
			}
		})
	}
}

func TestTunnelActivityRejectsOlderTimestamp(t *testing.T) {
	t.Parallel()
	base := time.Now()
	idle := time.Minute
	maxDeadline := base.Add(time.Hour)

	cases := []struct {
		name    string
		samples []time.Duration
		wantAdd time.Duration
	}{
		{
			name:    "positive: newer sample advances idle deadline",
			samples: []time.Duration{100 * time.Millisecond, 250 * time.Millisecond},
			wantAdd: 250 * time.Millisecond,
		},
		{
			name:    "negative: older sample after pause does not overwrite newer sample",
			samples: []time.Duration{250 * time.Millisecond, 50 * time.Millisecond},
			wantAdd: 250 * time.Millisecond,
		},
		{
			name:    "boundary: equal sample preserves current deadline",
			samples: []time.Duration{200 * time.Millisecond, 200 * time.Millisecond},
			wantAdd: 200 * time.Millisecond,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			act := newTunnelActivity(base)
			for _, off := range tc.samples {
				act.touch(base.Add(off))
			}
			want := base.Add(tc.wantAdd).Add(idle)
			if got := act.deadline(idle, maxDeadline); !got.Equal(want) {
				t.Fatalf("deadline = %v, want %v", got, want)
			}
		})
	}
}

type fakeClockWriteConn struct {
	net.Conn
	clk     *clockwork.FakeClock
	advance time.Duration
	wrote   int
	err     error
}

func (c *fakeClockWriteConn) SetWriteDeadline(time.Time) error { return nil }

func (c *fakeClockWriteConn) Write(p []byte) (int, error) {
	c.clk.Advance(c.advance)
	if c.err != nil {
		return c.wrote, c.err
	}
	return len(p), nil
}

func TestForwardPumpBytesRefreshesActivityOnWrite(t *testing.T) {
	t.Parallel()
	const idle = 300 * time.Millisecond

	cases := []struct {
		name        string
		advance     time.Duration
		maxLifetime time.Duration
		wrote       int
		writeErr    error
		wantWrote   int
		wantOK      bool
		wantOffset  time.Duration
	}{
		{
			name:        "positive: slow write refreshes activity to leave full idle window",
			advance:     250 * time.Millisecond,
			maxLifetime: 10 * time.Second,
			wantWrote:   len("payload"),
			wantOK:      true,
			wantOffset:  550 * time.Millisecond,
		},
		{
			name:        "negative: zero-byte write timeout does not refresh activity",
			advance:     300 * time.Millisecond,
			maxLifetime: 10 * time.Second,
			wrote:       0,
			writeErr:    os.ErrDeadlineExceeded,
			wantWrote:   0,
			wantOK:      false,
			wantOffset:  300 * time.Millisecond,
		},
		{
			name:        "boundary: refreshed activity stays capped at absolute tunnel deadline",
			advance:     250 * time.Millisecond,
			maxLifetime: 400 * time.Millisecond,
			wantWrote:   len("payload"),
			wantOK:      true,
			wantOffset:  400 * time.Millisecond,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			clk := clockwork.NewFakeClock()
			t0 := clk.Now()
			act := newTunnelActivity(t0)
			dst := &fakeClockWriteConn{clk: clk, advance: tc.advance, wrote: tc.wrote, err: tc.writeErr}
			gotWrote, gotOK := forwardPumpBytes(dst, []byte("payload"), idle, t0.Add(tc.maxLifetime), clk, act)
			if gotWrote != tc.wantWrote || gotOK != tc.wantOK {
				t.Fatalf("forwardPumpBytes = (%d, %v), want (%d, %v)", gotWrote, gotOK, tc.wantWrote, tc.wantOK)
			}
			wantDeadline := t0.Add(tc.wantOffset)
			if got := act.deadline(idle, t0.Add(tc.maxLifetime)); !got.Equal(wantDeadline) {
				t.Fatalf("activity deadline = %v, want %v", got, wantDeadline)
			}
		})
	}
}

type stalledDirectionSpec struct {
	name          string
	partialRead   int
	uploadChunks  int
	maxTunnel     time.Duration
	drainDown     bool
	wantCompleted bool
}

func TestCopyBothWaysStalledWriteWithActivePeer(t *testing.T) {
	t.Parallel()
	cases := []stalledDirectionSpec{
		{
			name:          "positive: stalled downstream write survives while client uploads and delivers unwritten tail",
			partialRead:   4,
			uploadChunks:  6,
			maxTunnel:     3 * time.Second,
			drainDown:     true,
			wantCompleted: true,
		},
		{
			name:          "negative: stalled downstream write with silent upload closes after idle timeout",
			partialRead:   0,
			uploadChunks:  0,
			maxTunnel:     3 * time.Second,
			drainDown:     false,
			wantCompleted: false,
		},
		{
			name:          "boundary: absolute tunnel deadline ends stalled write despite ongoing uploads",
			partialRead:   4,
			uploadChunks:  20,
			maxTunnel:     220 * time.Millisecond,
			drainDown:     false,
			wantCompleted: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runStalledDirectionCase(t, tc)
		})
	}
}

func newIdleTuneTestService(t *testing.T, idleTimeout, maxTunnel time.Duration) *Service {
	t.Helper()
	opts := config.DefaultOptions()
	opts.Listeners = []config.Listener{{App: "sonarr", Port: 18081}}
	opts.IdleTimeout = idleTimeout
	opts.MaxTunnel = maxTunnel
	compiled, err := config.Compile(opts)
	if err != nil {
		t.Fatalf("config.Compile: %v", err)
	}
	rec, err := metrics.New(compiled, prometheus.NewRegistry())
	if err != nil {
		t.Fatalf("metrics.New: %v", err)
	}
	svc, err := New(compiled, rec, clockwork.NewRealClock(), nil)
	if err != nil {
		t.Fatalf("proxy.New: %v", err)
	}
	return svc
}

func runStalledDirectionCase(t *testing.T, tc stalledDirectionSpec) {
	t.Helper()
	const (
		idleTimeout = 80 * time.Millisecond
		downMsg     = "hello-world!"
	)
	svc := newIdleTuneTestService(t, idleTimeout, tc.maxTunnel)
	clientConn, clientPeer := net.Pipe()
	upstreamConn, upstreamPeer := net.Pipe()
	t.Cleanup(func() {
		_ = clientPeer.Close()
		_ = upstreamPeer.Close()
	})

	done := make(chan struct{})
	maxTimerStart := make(chan struct{})
	go watchTunnel(done, maxTimerStart, svc.clock, tc.maxTunnel, svc.stopCh, svc.logger, clientConn, upstreamConn)
	copyDone := make(chan [2]int64, 1)
	go func() {
		defer close(done)
		up, down := svc.copyBothWays(clientConn, upstreamConn, 0, svc.clock.Now().Add(tc.maxTunnel), maxTimerStart)
		copyDone <- [2]int64{up, down}
	}()

	go func() {
		_, _ = upstreamPeer.Write([]byte(downMsg))
		_, _ = io.Copy(io.Discard, upstreamPeer)
	}()

	gotDown := driveClientUploadsAndRead(t, clientPeer, tc, downMsg)
	select {
	case counts := <-copyDone:
		if tc.wantCompleted && (gotDown != downMsg || counts[1] != int64(len(downMsg))) {
			t.Fatalf("downstream = %q (bytes=%d), want %q", gotDown, counts[1], downMsg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("copyBothWays did not finish within 2s bound")
	}
}

func driveClientUploadsAndRead(t *testing.T, clientPeer net.Conn, tc stalledDirectionSpec, downMsg string) string {
	t.Helper()
	var out bytes.Buffer
	if tc.partialRead > 0 {
		_ = clientPeer.SetReadDeadline(time.Now().Add(time.Second))
		head := make([]byte, tc.partialRead)
		if _, err := io.ReadFull(clientPeer, head); err != nil {
			t.Fatalf("read downstream head: %v", err)
		}
		out.Write(head)
	}
	for i := range tc.uploadChunks {
		time.Sleep(35 * time.Millisecond)
		_ = clientPeer.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))
		if _, err := clientPeer.Write([]byte("up\n")); err != nil {
			if tc.wantCompleted {
				t.Fatalf("upload chunk %d failed while opposite write stalled: %v", i+1, err)
			}
			return out.String()
		}
	}
	if tc.drainDown {
		_ = clientPeer.SetReadDeadline(time.Now().Add(time.Second))
		tail := make([]byte, len(downMsg)-tc.partialRead)
		if _, err := io.ReadFull(clientPeer, tail); err != nil {
			t.Fatalf("read downstream tail after upload: %v", err)
		}
		out.Write(tail)
		_ = clientPeer.Close()
	}
	return out.String()
}
