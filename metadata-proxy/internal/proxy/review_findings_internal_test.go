// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package proxy

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestGeneralOptionsReachHandler(t *testing.T) {
	t.Parallel()
	svc := newDialControlTestService(t, WithBindHost("127.0.0.1"), WithEphemeralPorts())
	startCtx, startCancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(startCancel)
	if err := svc.Start(startCtx); err != nil {
		t.Fatalf("svc.Start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		if err := svc.Stop(stopCtx); err != nil {
			t.Errorf("svc.Stop cleanup: %v", err)
		}
	})

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

func mustProxyAddr(t *testing.T, svc *Service) string {
	t.Helper()
	addr, ok := svc.Addr("sonarr")
	if !ok {
		t.Fatal("sonarr listener is not bound")
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
	startCtx, startCancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer startCancel()
	if err := svc.Start(startCtx); err != nil {
		t.Fatalf("svc.Start: %v", err)
	}
	if err := svc.ReadinessCheck().Fn(startCtx); err == nil {
		t.Fatal("ReadinessCheck after Start hid earlier serve failure")
	}
	if svc.ready.Load() {
		t.Fatal("Start set ready despite the earlier serve failure")
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		if err := svc.Stop(stopCtx); err != nil {
			t.Errorf("svc.Stop cleanup: %v", err)
		}
	})
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
		assertTunnelPeerClosed(t, clientPeer, upstreamPeer, clientDrained)
	}
}

func assertTunnelPeerClosed(t *testing.T, clientPeer, upstreamPeer net.Conn, clientDrained <-chan error) {
	t.Helper()
	if _, err := clientPeer.Read(make([]byte, 1)); err == nil {
		t.Fatal("client peer read after stop = nil error")
	}
	if _, err := upstreamPeer.Read(make([]byte, 1)); err == nil {
		t.Fatal("upstream peer read after stop = nil error")
	}
	select {
	case <-clientDrained:
	case <-time.After(2 * time.Second):
		t.Fatal("client connection did not close after stop")
	}
}
