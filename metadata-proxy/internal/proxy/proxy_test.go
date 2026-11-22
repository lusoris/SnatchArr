// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package proxy_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/golusoris/golusoris/k8s/health"
	"github.com/golusoris/golusoris/observability/statuspage"

	"github.com/lusoris/SnatchArr/metadata-proxy/internal/config"
	"github.com/lusoris/SnatchArr/metadata-proxy/internal/metrics"
	"github.com/lusoris/SnatchArr/metadata-proxy/internal/proxy"
)

const (
	maxTestConns     = 1024
	echoConnDeadline = 5 * time.Second
	metricPollWait   = 5 * time.Second
	metricPollTick   = 5 * time.Millisecond
	metricPollChecks = int(metricPollWait / metricPollTick)
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type proxyHarness struct {
	svc  *proxy.Service
	reg  *prometheus.Registry
	logs *syncBuffer
	addr string
}

func startHarness(t *testing.T, opts config.Options, proxyOpts ...proxy.Option) *proxyHarness {
	t.Helper()
	compiled, err := config.Compile(opts)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	promReg := prometheus.NewRegistry()
	rec, err := metrics.New(compiled, promReg)
	if err != nil {
		t.Fatalf("metrics.New: %v", err)
	}
	logs := &syncBuffer{}
	logger := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	allOpts := append([]proxy.Option{proxy.WithBindHost("127.0.0.1"), proxy.WithEphemeralPorts()}, proxyOpts...)
	svc, err := proxy.New(compiled, rec, clockwork.NewRealClock(), logger, allOpts...)
	if err != nil {
		t.Fatalf("proxy.New: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("svc.Start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		if err := svc.Stop(stopCtx); err != nil {
			t.Errorf("svc.Stop cleanup: %v", err)
		}
	})

	addr, ok := svc.Addr(opts.Listeners[0].App)
	if !ok {
		t.Fatalf("missing bound addr for %s", opts.Listeners[0].App)
	}
	return &proxyHarness{svc: svc, reg: promReg, logs: logs, addr: addr}
}

func dialRawProxy(t *testing.T, proxyAddr, rawReq string) (*http.Response, net.Conn, *bufio.Reader) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		t.Fatalf("dial proxy %s: %v", proxyAddr, err)
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, writeErr := io.WriteString(conn, rawReq); writeErr != nil {
		_ = conn.Close()
		t.Fatalf("write raw request: %v", writeErr)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, &http.Request{Method: http.MethodConnect})
	if err != nil {
		_ = conn.Close()
		t.Fatalf("read proxy response: %v", err)
	}
	return resp, conn, br
}

func sendConnectStatus(t *testing.T, proxyAddr, target string) int {
	t.Helper()
	req := fmt.Sprintf(
		"CONNECT %s HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: Basic c2VjcmV0\r\n\r\n",
		target, target,
	)
	resp, conn, _ := dialRawProxy(t, proxyAddr, req)
	defer resp.Body.Close()
	defer conn.Close()
	return resp.StatusCode
}

func TestPlainHTTPMethodNotAllowed(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		method    string
		targetURI string
		secret    string
	}{
		{
			name:      "negative: GET with absolute URI and indexer apikey query",
			method:    http.MethodGet,
			targetURI: "http://indexer.secret.example/api?t=search&apikey=SECRET_INDEXER_KEY_42",
			secret:    "SECRET_INDEXER_KEY_42",
		},
		{
			name:      "negative: POST with absolute URI and grab token",
			method:    http.MethodPost,
			targetURI: "http://dlclient.secret.example/jsonrpc?token=SECRET_DL_TOKEN_99",
			secret:    "SECRET_DL_TOKEN_99",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			opts := config.DefaultOptions()
			opts.Listeners = []config.Listener{{App: "sonarr", Port: 18081}}
			h := startHarness(t, opts)

			rawReq := fmt.Sprintf(
				"%s %s HTTP/1.1\r\nHost: indexer.secret.example\r\nProxy-Authorization: Basic dXNlcjpwYXNz\r\nContent-Length: 0\r\n\r\n",
				tc.method, tc.targetURI,
			)
			resp, conn, _ := dialRawProxy(t, h.addr, rawReq)
			_ = resp.Body.Close()
			_ = conn.Close()

			if resp.StatusCode != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
			}
			if gotAllow := resp.Header.Get("Allow"); gotAllow != http.MethodConnect {
				t.Fatalf("Allow header = %q, want %q", gotAllow, http.MethodConnect)
			}

			logOut := h.logs.String()
			for _, forbidden := range []string{tc.secret, tc.targetURI, "indexer.secret.example", "dlclient.secret.example", "apikey", "dXNlcjpwYXNz"} {
				if strings.Contains(logOut, forbidden) {
					t.Fatalf("405 log leaked %q in log output:\n%s", forbidden, logOut)
				}
			}
			if !strings.Contains(logOut, `"method":"`+tc.method+`"`) || !strings.Contains(logOut, `"app":"sonarr"`) {
				t.Fatalf("expected app and method in 405 log, got:\n%s", logOut)
			}
		})
	}
}

func serveEcho(ln net.Listener) func() {
	var (
		mu     sync.Mutex
		closed bool
		conns  = make(map[net.Conn]struct{})
		wg     sync.WaitGroup
		once   sync.Once
	)
	wg.Go(func() {
		for range maxTestConns {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			_ = conn.SetDeadline(time.Now().Add(echoConnDeadline))
			mu.Lock()
			if closed {
				mu.Unlock()
				_ = conn.Close()
				return
			}
			conns[conn] = struct{}{}
			mu.Unlock()
			wg.Go(func() {
				defer func() {
					mu.Lock()
					delete(conns, conn)
					mu.Unlock()
					_ = conn.Close()
				}()
				_, _ = io.Copy(conn, conn)
			})
		}
	})
	return func() {
		once.Do(func() {
			_ = ln.Close()
			mu.Lock()
			closed = true
			active := make([]net.Conn, 0, len(conns))
			for c := range conns {
				active = append(active, c)
			}
			mu.Unlock()
			for _, c := range active {
				_ = c.Close()
			}
			wg.Wait()
		})
	}
}

func startEchoListener(t *testing.T) (int, func()) {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen echo: %v", err)
	}
	tcpAddr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		_ = ln.Close()
		t.Fatalf("unexpected listener addr %T", ln.Addr())
	}
	return tcpAddr.Port, serveEcho(ln)
}

func TestPolicyRefusals(t *testing.T) {
	t.Parallel()

	echoPort, cleanup := startEchoListener(t)
	t.Cleanup(cleanup)
	echoPortStr := strconv.Itoa(echoPort)

	cases := []struct {
		name       string
		mode       config.Mode
		allowHosts []string
		refuse     []string
		ports      []int
		target     string
		wantStatus int
		wantResult string
	}{
		{
			name:       "negative: refuse_hosts exact match refused in log mode before dial",
			mode:       config.ModeLog,
			allowHosts: []string{"localhost"},
			refuse:     []string{"localhost"},
			ports:      []int{echoPort},
			target:     "localhost:" + echoPortStr,
			wantStatus: http.StatusForbidden,
			wantResult: metrics.ResultRefusedHost,
		},
		{
			name:       "negative: refuse_hosts leading-dot suffix match refused before dial",
			mode:       config.ModeLog,
			allowHosts: []string{".tracker.example"},
			refuse:     []string{".tracker.example"},
			ports:      []int{echoPort},
			target:     "api.tracker.example:" + echoPortStr,
			wantStatus: http.StatusForbidden,
			wantResult: metrics.ResultRefusedHost,
		},
		{
			name:       "negative: port outside configured ports refused",
			mode:       config.ModeLog,
			allowHosts: []string{"localhost"},
			ports:      []int{443},
			target:     "localhost:" + echoPortStr,
			wantStatus: http.StatusForbidden,
			wantResult: metrics.ResultRefusedPort,
		},
		{
			name:       "negative: enforce mode refuses host outside allow_hosts",
			mode:       config.ModeEnforce,
			allowHosts: []string{"skyhook.sonarr.tv"},
			ports:      []int{echoPort},
			target:     "localhost:" + echoPortStr,
			wantStatus: http.StatusForbidden,
			wantResult: metrics.ResultRefusedHost,
		},
		{
			name:       "boundary: malformed CONNECT target without port",
			mode:       config.ModeLog,
			allowHosts: []string{"skyhook.sonarr.tv"},
			ports:      []int{443},
			target:     "skyhook.sonarr.tv",
			wantStatus: http.StatusBadRequest,
			wantResult: metrics.ResultRefusedTarget,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var dialCalls atomic.Int32
			countingCtrl := func(string, string, syscall.RawConn) error {
				dialCalls.Add(1)
				return nil
			}

			opts := config.DefaultOptions()
			opts.Mode = tc.mode
			opts.Listeners = []config.Listener{{App: "sonarr", Port: 18081}}
			opts.AllowHosts = tc.allowHosts
			opts.RefuseHosts = tc.refuse
			opts.Ports = tc.ports
			h := startHarness(t, opts, proxy.WithDialControl(countingCtrl))

			gotStatus := sendConnectStatus(t, h.addr, tc.target)
			if gotStatus != tc.wantStatus {
				t.Fatalf("CONNECT %s status = %d, want %d", tc.target, gotStatus, tc.wantStatus)
			}
			if gotDials := dialCalls.Load(); gotDials != 0 {
				t.Fatalf("expected 0 dial attempts on policy refusal, got %d", gotDials)
			}
			if !strings.Contains(h.logs.String(), `"result":"`+tc.wantResult+`"`) {
				t.Fatalf("expected result %q in logs, got:\n%s", tc.wantResult, h.logs.String())
			}
		})
	}
}

func TestDefaultGuardRefusals(t *testing.T) {
	t.Parallel()

	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", ":0")
	if err != nil {
		t.Fatalf("listen dual-stack: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for range maxTestConns {
			c, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			_ = c.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	portStr := strconv.Itoa(port)

	opts := config.DefaultOptions()
	opts.Mode = config.ModeLog
	opts.Listeners = []config.Listener{{App: "sonarr", Port: 18081}}
	opts.Ports = []int{443, port}
	opts.DeniedPrefixes = []string{"10.96.0.0/12", "100.128.0.0/16", "2c0f:fff0::/32"}
	opts.DialTimeout = 50 * time.Millisecond
	h := startHarness(t, opts)

	targets := []struct {
		name   string
		target string
	}{
		{"negative: IPv4 loopback 127.0.0.1", "127.0.0.1:" + portStr},
		{"negative: IPv6 loopback ::1", "[::1]:" + portStr},
		{"negative: link-local metadata 169.254.169.254", "169.254.169.254:443"},
		{"negative: cluster Service IP 10.96.0.1", "10.96.0.1:443"},
		{"negative: extra IPv4 denied_prefixes 100.128.0.7", "100.128.0.7:443"},
		{"negative: extra IPv6 denied_prefixes 2c0f:fff0::7", "[2c0f:fff0::7]:443"},
		{"negative: hostname resolving to loopback (localhost)", "localhost:" + portStr},
	}

	for _, tc := range targets {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := sendConnectStatus(t, h.addr, tc.target)
			if got != http.StatusForbidden {
				t.Fatalf("default guard CONNECT %s status = %d, want %d", tc.target, got, http.StatusForbidden)
			}
		})
	}
}

func TestLogModeOutsideAllowHostsTunnelsAndWarns(t *testing.T) {
	t.Parallel()

	echoPort, cleanup := startEchoListener(t)
	t.Cleanup(cleanup)

	opts := config.DefaultOptions()
	opts.Mode = config.ModeLog
	opts.Listeners = []config.Listener{{App: "sonarr", Port: 18081}}
	opts.AllowHosts = []string{"skyhook.sonarr.tv"}
	opts.Ports = []int{echoPort}
	h := startHarness(t, opts, proxy.WithDialControl(proxy.AllowAllDialControl))

	target := fmt.Sprintf("localhost:%d", echoPort)
	rawReq := fmt.Sprintf(
		"CONNECT %s HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: Basic c2VjcmV0\r\n\r\n",
		target, target,
	)
	resp, conn, br := dialRawProxy(t, h.addr, rawReq)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_ = conn.Close()
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	msg := "ping-over-log-mode\n"
	if _, err := io.WriteString(conn, msg); err != nil {
		_ = conn.Close()
		t.Fatalf("write tunnel: %v", err)
	}
	gotLine, err := br.ReadString('\n')
	if err != nil {
		_ = conn.Close()
		t.Fatalf("read tunnel echo: %v", err)
	}
	_ = conn.Close()
	if gotLine != msg {
		t.Fatalf("echoed %q, want %q", gotLine, msg)
	}

	waitForTunnelCount(t, h.reg, 1)

	logOut := h.logs.String()
	if !strings.Contains(logOut, `"level":"WARN"`) ||
		!strings.Contains(logOut, `"msg":"metadata-proxy: host outside allow_hosts"`) ||
		!strings.Contains(logOut, `"app":"sonarr"`) ||
		!strings.Contains(logOut, `"host":"localhost"`) {
		t.Fatalf("expected WARN line for unlisted host with app and host, got:\n%s", logOut)
	}
	if strings.Contains(logOut, "c2VjcmV0") {
		t.Fatalf("Proxy-Authorization leaked into logs:\n%s", logOut)
	}
}

func startTLSEchoServer(t *testing.T) (int, *x509.CertPool, func()) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ecdsa key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)

	tlsCfg := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: priv}},
	}
	rawLn, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tls echo: %v", err)
	}
	tlsLn := tls.NewListener(rawLn, tlsCfg)
	port := rawLn.Addr().(*net.TCPAddr).Port
	return port, pool, serveEcho(tlsLn)
}

func TestTLSEchoTunnelAndMetrics(t *testing.T) {
	t.Parallel()

	tlsPort, rootPool, cleanup := startTLSEchoServer(t)
	t.Cleanup(cleanup)

	opts := config.DefaultOptions()
	opts.Mode = config.ModeEnforce
	opts.Listeners = []config.Listener{{App: "sonarr", Port: 18081}}
	opts.AllowHosts = []string{"localhost"}
	opts.Ports = []int{tlsPort}
	h := startHarness(t, opts, proxy.WithDialControl(proxy.AllowAllDialControl))

	target := fmt.Sprintf("localhost:%d", tlsPort)
	rawReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	resp, rawConn, _ := dialRawProxy(t, h.addr, rawReq)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_ = rawConn.Close()
		t.Fatalf("CONNECT status = %d, want 200", resp.StatusCode)
	}

	tlsClient := tls.Client(rawConn, &tls.Config{
		MinVersion: tls.VersionTLS13,
		ServerName: "localhost",
		RootCAs:    rootPool,
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := tlsClient.HandshakeContext(ctx); err != nil {
		_ = tlsClient.Close()
		t.Fatalf("TLS handshake over CONNECT tunnel: %v", err)
	}

	payload := "snatcharr-metadata-proxy-tls-roundtrip\n"
	if _, err := io.WriteString(tlsClient, payload); err != nil {
		_ = tlsClient.Close()
		t.Fatalf("write TLS payload: %v", err)
	}
	reply := make([]byte, len(payload))
	if _, err := io.ReadFull(tlsClient, reply); err != nil {
		_ = tlsClient.Close()
		t.Fatalf("read TLS echo: %v", err)
	}
	if string(reply) != payload {
		_ = tlsClient.Close()
		t.Fatalf("TLS echo = %q, want %q", string(reply), payload)
	}
	_ = tlsClient.Close()

	waitForTunnelCount(t, h.reg, 1)

	expectedTunnels := `# HELP metadata_proxy_tunnels_total Total CONNECT requests handled by the metadata proxy, partitioned by outcome.
# TYPE metadata_proxy_tunnels_total counter
metadata_proxy_tunnels_total{app="sonarr",host="localhost",mode="enforce",result="ok"} 1
`
	if err := testutil.GatherAndCompare(h.reg, strings.NewReader(expectedTunnels), metrics.MetricTunnelsTotal); err != nil {
		t.Fatalf("GatherAndCompare(%s): %v", metrics.MetricTunnelsTotal, err)
	}

	upBytes, downBytes := readByteCounters(t, h.reg)
	if upBytes < float64(len(payload)) || downBytes < float64(len(payload)) {
		t.Fatalf("byte counters up=%v down=%v, want both >= %d", upBytes, downBytes, len(payload))
	}
	expectedBytes := fmt.Sprintf(`# HELP metadata_proxy_tunnel_bytes_total Total bytes copied through established CONNECT tunnels.
# TYPE metadata_proxy_tunnel_bytes_total counter
metadata_proxy_tunnel_bytes_total{app="sonarr",direction="down",host="localhost"} %g
metadata_proxy_tunnel_bytes_total{app="sonarr",direction="up",host="localhost"} %g
`, downBytes, upBytes)
	if err := testutil.GatherAndCompare(h.reg, strings.NewReader(expectedBytes), metrics.MetricTunnelBytesTotal); err != nil {
		t.Fatalf("GatherAndCompare(%s): %v", metrics.MetricTunnelBytesTotal, err)
	}
}

func readByteCounters(t *testing.T, reg *prometheus.Registry) (float64, float64) {
	t.Helper()
	fams, err := reg.Gather()
	if err != nil {
		t.Fatalf("reg.Gather: %v", err)
	}
	var up, down float64
	for _, fam := range fams {
		if fam.GetName() != metrics.MetricTunnelBytesTotal {
			continue
		}
		for _, m := range fam.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "direction" && lp.GetValue() == metrics.DirectionUp {
					up = m.GetCounter().GetValue()
				}
				if lp.GetName() == "direction" && lp.GetValue() == metrics.DirectionDown {
					down = m.GetCounter().GetValue()
				}
			}
		}
	}
	return up, down
}

func waitForTunnelCount(t *testing.T, reg *prometheus.Registry, want int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), metricPollWait)
	defer cancel()

	ticker := time.NewTicker(metricPollTick)
	defer ticker.Stop()
	for range metricPollChecks {
		if testutil.CollectAndCount(reg, metrics.MetricTunnelsTotal) >= want {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %d tunnel metric series", want)
		case <-ticker.C:
		}
	}
	t.Fatalf("exhausted %d checks waiting for %d tunnel metric series", metricPollChecks, want)
}

func TestMaxTunnelsBoundary(t *testing.T) {
	t.Parallel()

	echoPort, cleanup := startEchoListener(t)
	t.Cleanup(cleanup)

	const capTunnels = 2
	opts := config.DefaultOptions()
	opts.Mode = config.ModeLog
	opts.Listeners = []config.Listener{{App: "sonarr", Port: 18081}}
	opts.AllowHosts = []string{"localhost"}
	opts.Ports = []int{echoPort}
	opts.MaxTunnels = capTunnels
	h := startHarness(t, opts, proxy.WithDialControl(proxy.AllowAllDialControl))

	target := fmt.Sprintf("localhost:%d", echoPort)
	rawReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)

	openConns := openBoundaryTunnels(t, h.addr, rawReq, capTunnels)

	if got := sendConnectStatus(t, h.addr, target); got != http.StatusServiceUnavailable {
		t.Fatalf("N+1 tunnel status = %d, want 503", got)
	}

	_ = openConns[0].Close()
	waitForSlotRelease(t, h.addr, target)

	waitForTunnelCount(t, h.reg, 2) // 1 refused_limit + 1 ok series
}

func openBoundaryTunnels(t *testing.T, proxyAddr, rawReq string, count int) []net.Conn {
	t.Helper()
	conns, err := dialBoundaryTunnels(t.Context(), proxyAddr, rawReq, count, t.Cleanup)
	if err != nil {
		t.Fatal(err)
	}
	return conns
}

func dialBoundaryTunnels(
	ctx context.Context,
	proxyAddr, rawReq string,
	count int,
	registerCleanup func(func()),
) ([]net.Conn, error) {
	openConns := make([]net.Conn, 0, count)
	registerCleanup(func() {
		for _, c := range openConns {
			_ = c.Close()
		}
	})
	for i := range count {
		conn, status, err := dialOneBoundaryTunnel(ctx, proxyAddr, rawReq)
		if err != nil {
			return openConns, fmt.Errorf("tunnel %d: %w", i+1, err)
		}
		if status != http.StatusOK {
			_ = conn.Close()
			return openConns, fmt.Errorf("tunnel %d status = %d, want 200", i+1, status)
		}
		openConns = append(openConns, conn)
	}
	return openConns, nil
}

func dialOneBoundaryTunnel(ctx context.Context, proxyAddr, rawReq string) (net.Conn, int, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", proxyAddr)
	if err != nil {
		return nil, 0, fmt.Errorf("dial proxy %s: %w", proxyAddr, err)
	}
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err = io.WriteString(conn, rawReq); err != nil {
		_ = conn.Close()
		return nil, 0, fmt.Errorf("write CONNECT: %w", err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodConnect})
	if err != nil {
		_ = conn.Close()
		return nil, 0, fmt.Errorf("read CONNECT response: %w", err)
	}
	_ = resp.Body.Close()
	return conn, resp.StatusCode, nil
}

func waitForSlotRelease(t *testing.T, proxyAddr, target string) {
	t.Helper()
	const (
		maxAdmitAttempts = 100
		admitRetryTick   = 20 * time.Millisecond
	)
	admitTicker := time.NewTicker(admitRetryTick)
	defer admitTicker.Stop()

	lastStatus := 0
	for attempt := range maxAdmitAttempts {
		lastStatus = sendConnectStatus(t, proxyAddr, target)
		if lastStatus == http.StatusOK {
			return
		}
		if lastStatus != http.StatusServiceUnavailable {
			t.Fatalf("attempt %d: tunnel status after slot release = %d, want 200 or 503", attempt+1, lastStatus)
		}
		if attempt+1 < maxAdmitAttempts {
			select {
			case <-t.Context().Done():
				t.Fatalf("context done at attempt %d (last status = %d): %v", attempt+1, lastStatus, t.Context().Err())
			case <-admitTicker.C:
			}
		}
	}
	t.Fatalf("exhausted %d attempts waiting for slot release; last status = %d", maxAdmitAttempts, lastStatus)
}

func TestReadinessBeforeAndAfterBind(t *testing.T) {
	t.Parallel()

	opts := config.DefaultOptions()
	opts.Listeners = []config.Listener{{App: "sonarr", Port: 18081}, {App: "radarr", Port: 18082}}
	compiled, err := config.Compile(opts)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	clk := clockwork.NewRealClock()
	rec, err := metrics.New(compiled, prometheus.NewRegistry())
	if err != nil {
		t.Fatalf("metrics.New: %v", err)
	}
	svc, err := proxy.New(compiled, rec, clk, nil, proxy.WithBindHost("127.0.0.1"), proxy.WithEphemeralPorts())
	if err != nil {
		t.Fatalf("proxy.New: %v", err)
	}

	reg := statuspage.NewRegistry(clk)
	reg.Register(svc.ReadinessCheck())
	handler := health.ReadyzHandler(reg)

	assertReadyStatus := func(want int) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/readyz", nil)
		recW := httptest.NewRecorder()
		handler.ServeHTTP(recW, req)
		if recW.Code != want {
			t.Fatalf("/readyz status = %d, want %d (body=%q)", recW.Code, want, recW.Body.String())
		}
	}

	assertReadyStatus(http.StatusServiceUnavailable)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("svc.Start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = svc.Stop(stopCtx)
	})
	assertReadyStatus(http.StatusOK)

	if err := svc.Stop(ctx); err != nil {
		t.Fatalf("svc.Stop: %v", err)
	}
	assertReadyStatus(http.StatusServiceUnavailable)
}

type badCardinalityRegisterer struct {
	prometheus.Registerer
}

func (b badCardinalityRegisterer) Register(c prometheus.Collector) error {
	if _, ok := c.(*prometheus.CounterVec); ok {
		return prometheus.AlreadyRegisteredError{
			ExistingCollector: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "wrong_labels_total", Help: "test"}, nil),
			NewCollector:      c,
		}
	}
	return b.Registerer.Register(c)
}

func TestMetricErrorLoggedAtWarn(t *testing.T) {
	t.Parallel()

	opts := config.DefaultOptions()
	opts.Listeners = []config.Listener{{App: "sonarr", Port: 18081}}
	compiled, err := config.Compile(opts)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	rec, err := metrics.New(compiled, badCardinalityRegisterer{Registerer: prometheus.NewRegistry()})
	if err != nil {
		t.Fatalf("metrics.New: %v", err)
	}
	logs := &syncBuffer{}
	logger := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	svc, err := proxy.New(compiled, rec, clockwork.NewRealClock(), logger, proxy.WithBindHost("127.0.0.1"), proxy.WithEphemeralPorts())
	if err != nil {
		t.Fatalf("proxy.New: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("svc.Start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		if stopErr := svc.Stop(stopCtx); stopErr != nil {
			t.Errorf("svc.Stop: %v", stopErr)
		}
	})

	addr, ok := svc.Addr("sonarr")
	if !ok {
		t.Fatal("missing bound addr for sonarr")
	}
	gotStatus := sendConnectStatus(t, addr, "malformed-without-port")
	if gotStatus != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", gotStatus)
	}

	logOut := logs.String()
	if !strings.Contains(logOut, `"level":"WARN"`) ||
		!strings.Contains(logOut, `"msg":"metadata-proxy: record metrics failed"`) ||
		!strings.Contains(logOut, `"result":"refused_target"`) {
		t.Fatalf("expected WARN log with result label on metric failure, got:\n%s", logOut)
	}
}

func TestBoundaryTunnelCleanupOnPartialFailure(t *testing.T) {
	t.Parallel()

	echoPort, cleanup := startEchoListener(t)
	t.Cleanup(cleanup)

	cases := []struct {
		name       string
		maxTunnels int
		requestCnt int
		wantErr    bool
	}{
		{name: "positive: full batch within cap registers cleanup", maxTunnels: 2, requestCnt: 2},
		{name: "negative: second tunnel hitting 503 still cleans up first client socket", maxTunnels: 1, requestCnt: 2, wantErr: true},
		{name: "boundary: zero tunnels requested is a safe no-op", maxTunnels: 1, requestCnt: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runBoundaryCleanupCase(t, echoPort, tc.maxTunnels, tc.requestCnt, tc.wantErr)
		})
	}
}

func runBoundaryCleanupCase(t *testing.T, echoPort, maxTunnels, requestCnt int, wantErr bool) {
	t.Helper()
	opts := config.DefaultOptions()
	opts.Mode = config.ModeLog
	opts.Listeners = []config.Listener{{App: "sonarr", Port: 18081}}
	opts.AllowHosts = []string{"localhost"}
	opts.Ports = []int{echoPort}
	opts.MaxTunnels = maxTunnels
	h := startHarness(t, opts, proxy.WithDialControl(proxy.AllowAllDialControl))

	target := fmt.Sprintf("localhost:%d", echoPort)
	rawReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)

	var cleanups []func()
	recordCleanup := func(fn func()) { cleanups = append(cleanups, fn) }
	t.Cleanup(func() {
		for _, fn := range cleanups {
			fn()
		}
	})

	opened, err := dialBoundaryTunnels(t.Context(), h.addr, rawReq, requestCnt, recordCleanup)
	t.Cleanup(func() {
		for _, c := range opened {
			_ = c.Close()
		}
	})
	if wantErr && err == nil {
		t.Fatal("expected partial-batch error when exceeding MaxTunnels, got nil")
	}
	if !wantErr && err != nil {
		t.Fatalf("unexpected batch error: %v", err)
	}
	for _, fn := range cleanups {
		fn()
	}
	for i, c := range opened {
		if _, writeErr := c.Write([]byte("x")); !errors.Is(writeErr, net.ErrClosed) {
			t.Fatalf("client socket %d remained open after registered cleanups ran: writeErr=%v, want net.ErrClosed", i+1, writeErr)
		}
	}
	waitForSlotRelease(t, h.addr, target)
}

func TestConnectPreservesRootDotInDialAddr(t *testing.T) {
	t.Parallel()

	echoPort, cleanup := startEchoListener(t)
	t.Cleanup(cleanup)
	portStr := strconv.Itoa(echoPort)

	cases := []struct {
		name           string
		target         string
		wantStatus     int
		wantDialTarget string
		wantHostLog    string
	}{
		{
			name:           "positive: rooted FQDN keeps trailing dot in dial address and normalizes policy/metric host",
			target:         "SKYHOOK.SONARR.TV.:" + portStr,
			wantStatus:     http.StatusOK,
			wantDialTarget: "skyhook.sonarr.tv.:" + portStr,
			wantHostLog:    "skyhook.sonarr.tv",
		},
		{
			name:           "boundary: unrooted FQDN dials without trailing dot",
			target:         "skyhook.sonarr.tv:" + portStr,
			wantStatus:     http.StatusOK,
			wantDialTarget: "skyhook.sonarr.tv:" + portStr,
			wantHostLog:    "skyhook.sonarr.tv",
		},
		{
			name:       "negative: rooted refused host rejected before dial",
			target:     "indexer.secret.example.:" + portStr,
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runRootDotDialCase(t, echoPort, tc.target, tc.wantStatus, tc.wantDialTarget, tc.wantHostLog)
		})
	}
}

func runRootDotDialCase(t *testing.T, echoPort int, target string, wantStatus int, wantDial, wantHost string) {
	t.Helper()
	var dialed atomic.Value
	var dialCount atomic.Int32
	echoAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(echoPort))

	dialHook := func(ctx context.Context, targetAddr string) (net.Conn, int, string, error) {
		dialCount.Add(1)
		dialed.Store(targetAddr)
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", echoAddr)
		if err != nil {
			return nil, http.StatusBadGateway, metrics.ResultDialError, err
		}
		return conn, http.StatusOK, metrics.ResultOK, nil
	}

	opts := config.DefaultOptions()
	opts.Mode = config.ModeEnforce
	opts.Listeners = []config.Listener{{App: "sonarr", Port: 18081}}
	opts.AllowHosts = []string{"skyhook.sonarr.tv"}
	opts.RefuseHosts = []string{"indexer.secret.example"}
	opts.Ports = []int{echoPort}
	h := startHarness(t, opts, proxy.WithDialForTest(dialHook))

	rawReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	resp, conn, br := dialRawProxy(t, h.addr, rawReq)
	t.Cleanup(func() {
		_ = resp.Body.Close()
		_ = conn.Close()
	})
	if resp.StatusCode != wantStatus {
		t.Fatalf("CONNECT %s status = %d, want %d", target, resp.StatusCode, wantStatus)
	}
	if wantDial == "" {
		if got := dialCount.Load(); got != 0 {
			t.Fatalf("dial count = %d, want 0", got)
		}
		return
	}
	assertRootDotTunnelOutcome(t, h, conn, br, &dialed, wantDial, wantHost)
}

func assertRootDotTunnelOutcome(
	t *testing.T,
	h *proxyHarness,
	conn net.Conn,
	br *bufio.Reader,
	dialed *atomic.Value,
	wantDial, wantHost string,
) {
	t.Helper()
	if _, err := io.WriteString(conn, "ping\n"); err != nil {
		t.Fatalf("write tunnel: %v", err)
	}
	line, err := br.ReadString('\n')
	if err != nil || line != "ping\n" {
		t.Fatalf("echo = %q (err=%v), want %q", line, err, "ping\n")
	}
	_ = conn.Close()

	if gotDial, _ := dialed.Load().(string); gotDial != wantDial {
		t.Fatalf("dialed targetAddr = %q, want %q", gotDial, wantDial)
	}
	waitForTunnelCount(t, h.reg, 1)
	wantSeries := fmt.Sprintf(`# HELP metadata_proxy_tunnels_total Total CONNECT requests handled by the metadata proxy, partitioned by outcome.
# TYPE metadata_proxy_tunnels_total counter
metadata_proxy_tunnels_total{app="sonarr",host=%q,mode="enforce",result="ok"} 1
`, wantHost)
	if err := testutil.GatherAndCompare(h.reg, strings.NewReader(wantSeries), metrics.MetricTunnelsTotal); err != nil {
		t.Fatalf("GatherAndCompare: %v", err)
	}
}

func TestStopCancelsInFlightUpstreamDial(t *testing.T) {
	t.Parallel()

	echoPort, cleanup := startEchoListener(t)
	t.Cleanup(cleanup)

	cases := []struct {
		name string
		mode string
	}{
		{name: "negative: Stop cancels stalled upstream dial before dial_timeout", mode: "stop"},
		{name: "boundary: client disconnect cancels in-flight upstream dial", mode: "disconnect"},
		{name: "positive: normal dial succeeds and Stop closes hijacked tunnel", mode: "normal"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			switch tc.mode {
			case "stop":
				runStopDuringStalledDial(t, echoPort)
			case "disconnect":
				runClientDisconnectDuringStalledDial(t, echoPort)
			default:
				runNormalDialThenStop(t, echoPort)
			}
		})
	}
}

func runStopDuringStalledDial(t *testing.T, port int) {
	t.Helper()
	dialEntered := make(chan struct{})
	var enterOnce sync.Once
	var svcRef atomic.Pointer[proxy.Service]

	stalledDial := func(ctx context.Context, targetAddr string) (net.Conn, int, string, error) {
		enterOnce.Do(func() { close(dialEntered) })
		<-ctx.Done()
		return proxy.DialUpstreamForTest(svcRef.Load(), ctx, targetAddr)
	}

	opts := config.DefaultOptions()
	opts.Mode = config.ModeLog
	opts.Listeners = []config.Listener{{App: "sonarr", Port: 18081}}
	opts.AllowHosts = []string{"localhost"}
	opts.Ports = []int{port}
	opts.DialTimeout = 30 * time.Second
	h := startHarness(t, opts, proxy.WithDialControl(proxy.AllowAllDialControl), proxy.WithDialForTest(stalledDial))
	svcRef.Store(h.svc)

	target := fmt.Sprintf("localhost:%d", port)
	statusCh := make(chan int, 1)
	go func() {
		statusCh <- sendConnectStatus(t, h.addr, target)
	}()

	select {
	case <-dialEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for upstream dial to start")
	}

	stopCtx, stopCancel := context.WithTimeout(t.Context(), time.Second)
	defer stopCancel()
	if err := h.svc.Stop(stopCtx); err != nil {
		t.Fatalf("svc.Stop during stalled dial failed: %v", err)
	}
	if status := <-statusCh; status != http.StatusBadGateway {
		t.Fatalf("canceled dial status = %d, want 502", status)
	}
}

func runClientDisconnectDuringStalledDial(t *testing.T, port int) {
	t.Helper()
	dialEntered := make(chan struct{})
	dialCanceled := make(chan error, 1)
	var enterOnce sync.Once

	stalledDial := func(ctx context.Context, _ string) (net.Conn, int, string, error) {
		enterOnce.Do(func() { close(dialEntered) })
		<-ctx.Done()
		dialCanceled <- ctx.Err()
		return nil, http.StatusBadGateway, metrics.ResultDialError, ctx.Err()
	}

	opts := config.DefaultOptions()
	opts.Listeners = []config.Listener{{App: "sonarr", Port: 18081}}
	opts.AllowHosts = []string{"localhost"}
	opts.Ports = []int{port}
	opts.DialTimeout = 30 * time.Second
	h := startHarness(t, opts, proxy.WithDialForTest(stalledDial))

	conn, err := (&net.Dialer{}).DialContext(t.Context(), "tcp", h.addr)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	target := fmt.Sprintf("localhost:%d", port)
	_, _ = fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)

	select {
	case <-dialEntered:
	case <-time.After(2 * time.Second):
		_ = conn.Close()
		t.Fatal("timed out waiting for upstream dial to start")
	}
	_ = conn.Close()

	select {
	case err := <-dialCanceled:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("dial ctx err = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("client disconnect did not cancel dial context")
	}
}

func runNormalDialThenStop(t *testing.T, echoPort int) {
	t.Helper()
	opts := config.DefaultOptions()
	opts.Listeners = []config.Listener{{App: "sonarr", Port: 18081}}
	opts.AllowHosts = []string{"localhost"}
	opts.Ports = []int{echoPort}
	h := startHarness(t, opts, proxy.WithDialControl(proxy.AllowAllDialControl))

	target := fmt.Sprintf("localhost:%d", echoPort)
	rawReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)
	resp, conn, _ := dialRawProxy(t, h.addr, rawReq)
	t.Cleanup(func() {
		_ = resp.Body.Close()
		_ = conn.Close()
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	stopCtx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := h.svc.Stop(stopCtx); err != nil {
		t.Fatalf("svc.Stop: %v", err)
	}
}

func TestAddrLifecycleValidity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		mode string
	}{
		{name: "negative: Addr stays false while second listener bind is pending and after failed Start", mode: "partial_fail"},
		{name: "positive: Addr publishes ephemeral bound addresses after Start and clears them on Stop", mode: "start_stop"},
		{name: "boundary: unknown app always returns empty false", mode: "unknown_app"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			switch tc.mode {
			case "partial_fail":
				runAddrPartialBindFailure(t)
			case "start_stop":
				runAddrStartAndStopLifecycle(t)
			default:
				runAddrUnknownApp(t)
			}
		})
	}
}

func newAddrTestService(t *testing.T, extraOpts ...proxy.Option) *proxy.Service {
	t.Helper()
	opts := config.DefaultOptions()
	opts.Listeners = []config.Listener{
		{App: "sonarr", Port: 18081},
		{App: "radarr", Port: 18082},
	}
	compiled, err := config.Compile(opts)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	rec, err := metrics.New(compiled, prometheus.NewRegistry())
	if err != nil {
		t.Fatalf("metrics.New: %v", err)
	}
	baseOpts := append([]proxy.Option{proxy.WithBindHost("127.0.0.1"), proxy.WithEphemeralPorts()}, extraOpts...)
	svc, err := proxy.New(compiled, rec, clockwork.NewRealClock(), nil, baseOpts...)
	if err != nil {
		t.Fatalf("proxy.New: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = svc.Stop(stopCtx)
	})
	return svc
}

func runAddrPartialBindFailure(t *testing.T) {
	t.Helper()
	secondPending := make(chan struct{})
	releaseSecond := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(releaseSecond) })

	errSecond := errors.New("second listener bind failed")
	var calls atomic.Int32
	listenHook := func(ctx context.Context, network, addr string) (net.Listener, error) {
		if calls.Add(1) == 1 {
			return (&net.ListenConfig{}).Listen(ctx, network, addr)
		}
		close(secondPending)
		<-releaseSecond
		return nil, errSecond
	}

	svc := newAddrTestService(t, proxy.WithListenForTest(listenHook))
	if addr, ok := svc.Addr("sonarr"); ok || addr != "" {
		t.Fatalf("before Start Addr(sonarr) = (%q, %v), want (\"\", false)", addr, ok)
	}

	startErrCh := make(chan error, 1)
	go func() { startErrCh <- svc.Start(t.Context()) }()

	select {
	case <-secondPending:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second bind")
	}
	if addr, ok := svc.Addr("sonarr"); ok || addr != "" {
		t.Fatalf("while second bind pending Addr(sonarr) = (%q, %v), want (\"\", false)", addr, ok)
	}

	releaseOnce.Do(func() { close(releaseSecond) })
	if err := <-startErrCh; !errors.Is(err, errSecond) {
		t.Fatalf("Start err = %v, want %v", err, errSecond)
	}
	if addr, ok := svc.Addr("sonarr"); ok || addr != "" {
		t.Fatalf("after failed Start Addr(sonarr) = (%q, %v), want (\"\", false)", addr, ok)
	}
}

func runAddrStartAndStopLifecycle(t *testing.T) {
	t.Helper()
	svc := newAddrTestService(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	if err := svc.Start(ctx); err != nil {
		t.Fatalf("svc.Start: %v", err)
	}
	for _, app := range []string{"sonarr", "radarr"} {
		addr, ok := svc.Addr(app)
		if !ok || !strings.HasPrefix(addr, "127.0.0.1:") || strings.HasSuffix(addr, ":0") {
			t.Fatalf("after Start Addr(%s) = (%q, %v), want bound ephemeral 127.0.0.1:port", app, addr, ok)
		}
	}
	if err := svc.Stop(ctx); err != nil {
		t.Fatalf("svc.Stop: %v", err)
	}
	for _, app := range []string{"sonarr", "radarr"} {
		if addr, ok := svc.Addr(app); ok || addr != "" {
			t.Fatalf("after Stop Addr(%s) = (%q, %v), want (\"\", false)", app, addr, ok)
		}
	}
}

func runAddrUnknownApp(t *testing.T) {
	t.Helper()
	svc := newAddrTestService(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("svc.Start: %v", err)
	}
	if addr, ok := svc.Addr("lidarr"); ok || addr != "" {
		t.Fatalf("Addr(lidarr) = (%q, %v), want (\"\", false)", addr, ok)
	}
}

func TestConnectRequiresAuthorityForm(t *testing.T) {
	t.Parallel()

	echoPort, cleanup := startEchoListener(t)
	t.Cleanup(cleanup)
	portStr := strconv.Itoa(echoPort)

	cases := []struct {
		name       string
		target     string
		hostHdr    string
		wantStatus int
		wantResult string
	}{
		{
			name:       "positive: authority-form CONNECT tunnels",
			target:     "localhost:" + portStr,
			hostHdr:    "localhost:" + portStr,
			wantStatus: http.StatusOK,
			wantResult: metrics.ResultOK,
		},
		{
			name:       "boundary: authority-form uses request-target over mismatched Host header",
			target:     "localhost:" + portStr,
			hostHdr:    "refused.example:" + portStr,
			wantStatus: http.StatusOK,
			wantResult: metrics.ResultOK,
		},
		{
			name:       "negative: net/rpc path-form CONNECT with allowed Host header rejected as 400",
			target:     "/_goRPC_",
			hostHdr:    "localhost:" + portStr,
			wantStatus: http.StatusBadRequest,
			wantResult: metrics.ResultRefusedTarget,
		},
		{
			name:       "negative: path-form CONNECT with refused Host header rejected as 400 before policy",
			target:     "/_goRPC_",
			hostHdr:    "refused.example:" + portStr,
			wantStatus: http.StatusBadRequest,
			wantResult: metrics.ResultRefusedTarget,
		},
		{
			name:       "negative: network-path //host:port CONNECT rejected as 400",
			target:     "//localhost:" + portStr,
			hostHdr:    "localhost:" + portStr,
			wantStatus: http.StatusBadRequest,
			wantResult: metrics.ResultRefusedTarget,
		},
		{
			name:       "negative: authority with path and query rejected as 400 without leaking query",
			target:     "localhost:" + portStr + "/rpc?apikey=SECRET_CONNECT_QUERY_88",
			hostHdr:    "localhost:" + portStr,
			wantStatus: http.StatusBadRequest,
			wantResult: metrics.ResultRefusedTarget,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runAuthorityFormCase(t, echoPort, tc.target, tc.hostHdr, tc.wantStatus, tc.wantResult)
		})
	}
}

func runAuthorityFormCase(t *testing.T, echoPort int, target, hostHdr string, wantStatus int, wantResult string) {
	t.Helper()
	var dialCalls atomic.Int32
	ctrl := func(string, string, syscall.RawConn) error {
		dialCalls.Add(1)
		return nil
	}

	opts := config.DefaultOptions()
	opts.Mode = config.ModeEnforce
	opts.Listeners = []config.Listener{{App: "sonarr", Port: 18081}}
	opts.AllowHosts = []string{"localhost"}
	opts.RefuseHosts = []string{"refused.example"}
	opts.Ports = []int{echoPort}
	h := startHarness(t, opts, proxy.WithDialControl(ctrl))

	rawReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, hostHdr)
	resp, conn, _ := dialRawProxy(t, h.addr, rawReq)
	_ = resp.Body.Close()
	_ = conn.Close()

	if resp.StatusCode != wantStatus {
		t.Fatalf("CONNECT %q (Host: %q) status = %d, want %d", target, hostHdr, resp.StatusCode, wantStatus)
	}
	if wantStatus != http.StatusOK && dialCalls.Load() != 0 {
		t.Fatalf("expected 0 dials on rejected CONNECT %q, got %d", target, dialCalls.Load())
	}
	waitForTunnelCount(t, h.reg, 1)
	logOut := waitForLogSubstring(t, h.logs, `"result":"`+wantResult+`"`)
	if strings.Contains(logOut, "SECRET_CONNECT_QUERY_88") {
		t.Fatalf("logs leaked query secret:\n%s", logOut)
	}
}

func TestConnectTrailingDotNormalization(t *testing.T) {
	t.Parallel()

	echoPort, cleanup := startEchoListener(t)
	t.Cleanup(cleanup)
	portStr := strconv.Itoa(echoPort)

	cases := []struct {
		name       string
		target     string
		wantStatus int
		wantResult string
	}{
		{
			name:       "positive: single trailing dot on allowed host accepted",
			target:     "localhost.:" + portStr,
			wantStatus: http.StatusOK,
			wantResult: metrics.ResultOK,
		},
		{
			name:       "negative: double trailing dot on allowed host rejected with 400 refused_target",
			target:     "localhost..:" + portStr,
			wantStatus: http.StatusBadRequest,
			wantResult: metrics.ResultRefusedTarget,
		},
		{
			name:       "negative: double trailing dot on refused host rejected with 400 refused_target instead of 403",
			target:     "refused.example..:" + portStr,
			wantStatus: http.StatusBadRequest,
			wantResult: metrics.ResultRefusedTarget,
		},
		{
			name:       "boundary: bare single dot host rejected with 400 refused_target",
			target:     ".:" + portStr,
			wantStatus: http.StatusBadRequest,
			wantResult: metrics.ResultRefusedTarget,
		},
		{
			name:       "boundary: bare double dot host rejected with 400 refused_target",
			target:     "..:" + portStr,
			wantStatus: http.StatusBadRequest,
			wantResult: metrics.ResultRefusedTarget,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runTrailingDotCase(t, echoPort, tc.target, tc.wantStatus, tc.wantResult)
		})
	}
}

func runTrailingDotCase(t *testing.T, echoPort int, target string, wantStatus int, wantResult string) {
	t.Helper()
	var dialCalls atomic.Int32
	echoAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(echoPort))
	dialHook := func(ctx context.Context, _ string) (net.Conn, int, string, error) {
		dialCalls.Add(1)
		conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", echoAddr)
		if err != nil {
			return nil, http.StatusBadGateway, metrics.ResultDialError, err
		}
		return conn, http.StatusOK, metrics.ResultOK, nil
	}

	opts := config.DefaultOptions()
	opts.Mode = config.ModeEnforce
	opts.Listeners = []config.Listener{{App: "sonarr", Port: 18081}}
	opts.AllowHosts = []string{"localhost"}
	opts.RefuseHosts = []string{"refused.example"}
	opts.Ports = []int{echoPort}
	h := startHarness(t, opts, proxy.WithDialForTest(dialHook))

	got := sendConnectStatus(t, h.addr, target)
	if got != wantStatus {
		t.Fatalf("CONNECT %q status = %d, want %d", target, got, wantStatus)
	}
	if wantStatus != http.StatusOK && dialCalls.Load() != 0 {
		t.Fatalf("CONNECT %q dialed %d times, want 0", target, dialCalls.Load())
	}
	waitForTunnelCount(t, h.reg, 1)
	_ = waitForLogSubstring(t, h.logs, `"result":"`+wantResult+`"`)
}

func waitForLogSubstring(t *testing.T, logs *syncBuffer, want string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), metricPollWait)
	defer cancel()

	ticker := time.NewTicker(metricPollTick)
	defer ticker.Stop()
	for range metricPollChecks {
		if out := logs.String(); strings.Contains(out, want) {
			return out
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for log %q in:\n%s", want, logs.String())
		case <-ticker.C:
		}
	}
	t.Fatalf("exhausted %d checks waiting for log %q in:\n%s", metricPollChecks, want, logs.String())
	return ""
}
