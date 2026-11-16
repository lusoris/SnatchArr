// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package proxy_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/metadata-proxy/internal/config"
	"github.com/lusoris/SnatchArr/metadata-proxy/internal/proxy"
)

func isNetTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

type idleTimeoutCase struct {
	name            string
	idleTimeout     time.Duration
	maxTunnel       time.Duration
	keepaliveRounds int
	keepaliveTick   time.Duration
	expectIdleClose bool
}

type oneWayIdleCase struct {
	name      string
	direction string
	stops     bool
}

const (
	idleDirectionDown = "upstream to client"
	idleDirectionUp   = "client to upstream"
)

func TestTunnelIdleTimeout(t *testing.T) {
	t.Parallel()

	echoPort, cleanup := startEchoListener(t)
	t.Cleanup(cleanup)

	cases := []idleTimeoutCase{
		{
			name:            "positive: active round-trip within idle_timeout succeeds",
			idleTimeout:     300 * time.Millisecond,
			maxTunnel:       10 * time.Second,
			keepaliveRounds: 1,
		},
		{
			name:            "negative: idle tunnel with no traffic closes well before max_tunnel",
			idleTimeout:     200 * time.Millisecond,
			maxTunnel:       10 * time.Second,
			expectIdleClose: true,
		},
		{
			name:            "boundary: traffic every half idle period keeps tunnel open past one idle period",
			idleTimeout:     300 * time.Millisecond,
			maxTunnel:       10 * time.Second,
			keepaliveRounds: 3,
			keepaliveTick:   150 * time.Millisecond,
			expectIdleClose: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runIdleTimeoutCase(t, echoPort, tc)
		})
	}
}

func TestTunnelOneWayIdleTimeout(t *testing.T) {
	t.Parallel()

	cases := []oneWayIdleCase{
		{
			name:      "positive: upstream stream survives silent client writes",
			direction: idleDirectionDown,
		},
		{
			name:      "positive: client upload survives silent upstream writes",
			direction: idleDirectionUp,
		},
		{
			name:      "boundary: stopped upstream stream closes after shared idle period",
			direction: idleDirectionDown,
			stops:     true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.direction == idleDirectionUp {
				runOneWayUpload(t)
				return
			}
			runOneWayDownload(t, tc.stops)
		})
	}
}

func runOneWayDownload(t *testing.T, expectIdleClose bool) {
	t.Helper()
	const (
		idleTimeout = 300 * time.Millisecond
		chunkCount  = 8
	)
	chunk := []byte("stream-chunk\n")
	port, lastWrite, cleanup := startStreamingListener(t, chunk, chunkCount, idleTimeout/3)
	t.Cleanup(cleanup)
	conn, br := dialIdleTestTunnel(t, port, idleTimeout)

	want := bytes.Repeat(chunk, chunkCount)
	got := make([]byte, len(want))
	if _, err := io.ReadFull(br, got); err != nil {
		t.Fatalf("read one-way stream: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("one-way stream = %q, want %q", got, want)
	}
	if !expectIdleClose {
		return
	}
	assertTunnelClosesAfterIdle(t, conn, br, idleTimeout, <-lastWrite)
}

func runOneWayUpload(t *testing.T) {
	t.Helper()
	const (
		idleTimeout = 300 * time.Millisecond
		chunkCount  = 8
	)
	chunk := []byte("upload-chunk\n")
	want := bytes.Repeat(chunk, chunkCount)
	received := make(chan []byte, 1)
	port, cleanup := startSingleConnListener(t, func(conn net.Conn) {
		got := make([]byte, len(want))
		_, _ = io.ReadFull(conn, got)
		received <- got
	})
	t.Cleanup(cleanup)
	conn, _ := dialIdleTestTunnel(t, port, idleTimeout)

	ticker := time.NewTicker(idleTimeout / 3)
	defer ticker.Stop()
	for range chunkCount {
		<-ticker.C
		if _, err := conn.Write(chunk); err != nil {
			t.Fatalf("write one-way upload: %v", err)
		}
	}
	select {
	case got := <-received:
		if !bytes.Equal(got, want) {
			t.Fatalf("one-way upload = %q, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("upstream did not receive one-way upload")
	}
}

func dialIdleTestTunnel(t *testing.T, upstreamPort int, idleTimeout time.Duration) (net.Conn, *bufio.Reader) {
	t.Helper()
	opts := config.DefaultOptions()
	opts.Mode = config.ModeLog
	opts.Listeners = []config.Listener{{App: "sonarr", Port: 18081}}
	opts.AllowHosts = []string{"localhost"}
	opts.Ports = []int{upstreamPort}
	opts.IdleTimeout = idleTimeout
	opts.MaxTunnel = 10 * time.Second
	h := startHarness(t, opts, proxy.WithDialControl(proxy.AllowAllDialControl))

	target := fmt.Sprintf("localhost:%d", upstreamPort)
	rawReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	resp, conn, br := dialRawProxy(t, h.addr, rawReq)
	t.Cleanup(func() {
		resp.Body.Close()
		conn.Close()
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT status = %d, want 200", resp.StatusCode)
	}
	return conn, br
}

func assertTunnelClosesAfterIdle(t *testing.T, conn net.Conn, br *bufio.Reader, idleTimeout time.Duration, lastWrite time.Time) {
	t.Helper()
	idleStart := time.Now()
	if err := conn.SetReadDeadline(idleStart.Add(2 * idleTimeout)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	_, readErr := br.ReadByte()
	closedAt := time.Now()
	elapsed := closedAt.Sub(idleStart)
	if readErr == nil {
		t.Fatal("expected stopped stream tunnel to close")
	}
	if isNetTimeout(readErr) {
		t.Fatalf("stopped stream stayed open for %v: %v", elapsed, readErr)
	}
	if closedAt.Sub(lastWrite) < idleTimeout-50*time.Millisecond {
		t.Fatalf("stopped stream closed %v after last upstream write, want at least idle_timeout %v", closedAt.Sub(lastWrite), idleTimeout)
	}
}

func startStreamingListener(t *testing.T, chunk []byte, count int, interval time.Duration) (int, <-chan time.Time, func()) {
	t.Helper()
	lastWrite := make(chan time.Time, 1)
	port, cleanup := startSingleConnListener(t, func(conn net.Conn) {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for i := range count {
			<-ticker.C
			if i == count-1 {
				lastWrite <- time.Now()
			}
			if _, err := conn.Write(chunk); err != nil {
				return
			}
		}
		_, _ = io.Copy(io.Discard, conn)
	})
	return port, lastWrite, cleanup
}

func startSingleConnListener(t *testing.T, handle func(net.Conn)) (int, func()) {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tunnel test upstream: %v", err)
	}
	tcpAddr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		_ = ln.Close()
		t.Fatalf("unexpected listener addr %T", ln.Addr())
	}
	var wg sync.WaitGroup
	wg.Go(func() {
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		handle(conn)
	})
	return tcpAddr.Port, func() {
		_ = ln.Close()
		wg.Wait()
	}
}

func runIdleTimeoutCase(t *testing.T, echoPort int, tc idleTimeoutCase) {
	t.Helper()
	opts := config.DefaultOptions()
	opts.Mode = config.ModeLog
	opts.Listeners = []config.Listener{{App: "sonarr", Port: 18081}}
	opts.AllowHosts = []string{"localhost"}
	opts.Ports = []int{echoPort}
	opts.IdleTimeout = tc.idleTimeout
	opts.MaxTunnel = tc.maxTunnel
	h := startHarness(t, opts, proxy.WithDialControl(proxy.AllowAllDialControl))

	target := fmt.Sprintf("localhost:%d", echoPort)
	rawReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	resp, conn, br := dialRawProxy(t, h.addr, rawReq)
	t.Cleanup(func() {
		resp.Body.Close()
		conn.Close()
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT status = %d, want 200", resp.StatusCode)
	}

	establishedAt := time.Now()
	if tc.keepaliveRounds > 0 {
		runKeepaliveEchoes(t, conn, br, tc.keepaliveRounds, tc.keepaliveTick)
		if tc.keepaliveTick > 0 && time.Since(establishedAt) < tc.idleTimeout {
			t.Fatalf("keepalive elapsed %v < idle_timeout %v", time.Since(establishedAt), tc.idleTimeout)
		}
	}
	if !tc.expectIdleClose {
		if err := conn.Close(); err != nil {
			t.Fatalf("conn.Close: %v", err)
		}
		waitForTunnelCount(t, h.reg, 1)
		return
	}
	assertTunnelClosedBeforeMax(t, conn, br, tc.idleTimeout, tc.maxTunnel)
}

func assertTunnelClosedBeforeMax(t *testing.T, conn net.Conn, br *bufio.Reader, idleTimeout, maxTunnel time.Duration) {
	t.Helper()
	idleStart := time.Now()
	const idleCloseBound = 2 * time.Second
	if err := conn.SetReadDeadline(idleStart.Add(idleCloseBound)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}
	_, readErr := br.ReadByte()
	elapsed := time.Since(idleStart)
	if readErr == nil {
		t.Fatal("expected idle tunnel to close, ReadByte returned nil error")
	}
	if isNetTimeout(readErr) {
		t.Fatalf("tunnel stayed open past %v bound (idle_timeout=%v not applied): %v", idleCloseBound, idleTimeout, readErr)
	}
	if elapsed >= maxTunnel/2 {
		t.Fatalf("tunnel closed after %v, want well before max_tunnel %v", elapsed, maxTunnel)
	}
}

func runKeepaliveEchoes(t *testing.T, conn net.Conn, br *bufio.Reader, rounds int, tick time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	var ticker *time.Ticker
	if tick > 0 {
		ticker = time.NewTicker(tick)
		defer ticker.Stop()
	}

	for i := range rounds {
		if ticker != nil {
			select {
			case <-ctx.Done():
				t.Fatalf("keepalive round %d timed out", i+1)
			case <-ticker.C:
			}
		}
		if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatalf("SetDeadline round %d: %v", i+1, err)
		}
		msg := fmt.Sprintf("keepalive-%d\n", i+1)
		if _, err := io.WriteString(conn, msg); err != nil {
			t.Fatalf("write round %d: %v", i+1, err)
		}
		got, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read round %d: %v", i+1, err)
		}
		if got != msg {
			t.Fatalf("round %d echoed %q, want %q", i+1, got, msg)
		}
	}
}

type establishedClient struct {
	conn net.Conn
	br   *bufio.Reader
}

type stopTunnelCase struct {
	name            string
	tunnelCount     int
	repeatStop      bool
	checkPostRefuse bool
}

func TestStopClosesActiveTunnels(t *testing.T) {
	t.Parallel()

	echoPort, cleanup := startEchoListener(t)
	t.Cleanup(cleanup)

	cases := []stopTunnelCase{
		{
			name:        "positive: Stop closes established idle tunnel before context deadline",
			tunnelCount: 1,
		},
		{
			name:            "negative: new CONNECT dial refused after Stop closes listener",
			tunnelCount:     1,
			checkPostRefuse: true,
		},
		{
			name:        "boundary: repeated Stop is idempotent and closes multiple idle tunnels",
			tunnelCount: 2,
			repeatStop:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runStopTunnelCase(t, echoPort, tc)
		})
	}
}

func runStopTunnelCase(t *testing.T, echoPort int, tc stopTunnelCase) {
	t.Helper()
	opts := config.DefaultOptions()
	opts.Mode = config.ModeLog
	opts.Listeners = []config.Listener{{App: "sonarr", Port: 18081}}
	opts.AllowHosts = []string{"localhost"}
	opts.Ports = []int{echoPort}
	opts.IdleTimeout = 30 * time.Second
	opts.MaxTunnel = 60 * time.Second
	opts.MaxTunnels = 4
	h := startHarness(t, opts, proxy.WithDialControl(proxy.AllowAllDialControl))

	clients := openTestTunnels(t, h.addr, echoPort, tc.tunnelCount)

	stopCtx, stopCancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer stopCancel()
	if err := h.svc.Stop(stopCtx); err != nil {
		t.Fatalf("svc.Stop returned error: %v", err)
	}
	if err := stopCtx.Err(); err != nil {
		t.Fatalf("svc.Stop did not return before context deadline: %v", err)
	}

	assertClientsClosedAfterStop(t, clients)
	if tc.repeatStop {
		if err := h.svc.Stop(stopCtx); err != nil {
			t.Fatalf("second svc.Stop returned error: %v", err)
		}
	}
	if tc.checkPostRefuse {
		assertListenerRefusesDial(t, h.addr)
	}
}

func openTestTunnels(t *testing.T, proxyAddr string, echoPort, count int) []establishedClient {
	t.Helper()
	target := fmt.Sprintf("localhost:%d", echoPort)
	rawReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	clients := make([]establishedClient, 0, count)
	for i := range count {
		resp, conn, br := dialRawProxy(t, proxyAddr, rawReq)
		t.Cleanup(func() {
			resp.Body.Close()
			conn.Close()
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("tunnel %d status = %d, want 200", i+1, resp.StatusCode)
		}
		clients = append(clients, establishedClient{conn: conn, br: br})
	}
	return clients
}

func assertClientsClosedAfterStop(t *testing.T, clients []establishedClient) {
	t.Helper()
	for i, cl := range clients {
		if err := cl.conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatalf("tunnel %d SetReadDeadline: %v", i+1, err)
		}
		_, readErr := cl.br.ReadByte()
		if readErr == nil {
			t.Fatalf("tunnel %d stayed open after Stop", i+1)
		}
		if isNetTimeout(readErr) {
			t.Fatalf("tunnel %d did not close within bounded wait after Stop: %v", i+1, readErr)
		}
	}
}

func assertListenerRefusesDial(t *testing.T, proxyAddr string) {
	t.Helper()
	dialCtx, dialCancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer dialCancel()
	postConn, dialErr := (&net.Dialer{}).DialContext(dialCtx, "tcp", proxyAddr)
	if dialErr == nil {
		postConn.Close()
		t.Fatal("expected dial after Stop to fail, got nil error")
	}
}
