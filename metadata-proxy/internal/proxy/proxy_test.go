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
		_ = svc.Stop(stopCtx)
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
	h := startHarness(t, opts, proxy.WithDialControl(nil))

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
	h := startHarness(t, opts, proxy.WithDialControl(nil))

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
	h := startHarness(t, opts, proxy.WithDialControl(nil))

	target := fmt.Sprintf("localhost:%d", echoPort)
	rawReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)

	openConns := make([]net.Conn, 0, capTunnels)
	for i := range capTunnels {
		resp, conn, _ := dialRawProxy(t, h.addr, rawReq)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			_ = conn.Close()
			t.Fatalf("tunnel %d status = %d, want 200", i+1, resp.StatusCode)
		}
		openConns = append(openConns, conn)
	}
	t.Cleanup(func() {
		for _, c := range openConns {
			_ = c.Close()
		}
	})

	if got := sendConnectStatus(t, h.addr, target); got != http.StatusServiceUnavailable {
		t.Fatalf("N+1 tunnel status = %d, want 503", got)
	}

	_ = openConns[0].Close()

	const (
		maxAdmitAttempts = 100
		admitRetryTick   = 20 * time.Millisecond
	)
	admitTicker := time.NewTicker(admitRetryTick)
	defer admitTicker.Stop()

	admitted := false
	lastStatus := 0
	for attempt := range maxAdmitAttempts {
		lastStatus = sendConnectStatus(t, h.addr, target)
		if lastStatus == http.StatusOK {
			admitted = true
			break
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
	if !admitted {
		t.Fatalf("exhausted %d attempts waiting for slot release; last status = %d", maxAdmitAttempts, lastStatus)
	}

	waitForTunnelCount(t, h.reg, 2) // 1 refused_limit + 1 ok series
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
	assertReadyStatus(http.StatusOK)

	if err := svc.Stop(ctx); err != nil {
		t.Fatalf("svc.Stop: %v", err)
	}
	assertReadyStatus(http.StatusServiceUnavailable)
}

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

func runIdleTimeoutCase(t *testing.T, echoPort int, tc idleTimeoutCase) {
	t.Helper()
	opts := config.DefaultOptions()
	opts.Mode = config.ModeLog
	opts.Listeners = []config.Listener{{App: "sonarr", Port: 18081}}
	opts.AllowHosts = []string{"localhost"}
	opts.Ports = []int{echoPort}
	opts.IdleTimeout = tc.idleTimeout
	opts.MaxTunnel = tc.maxTunnel
	h := startHarness(t, opts, proxy.WithDialControl(nil))

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
	h := startHarness(t, opts, proxy.WithDialControl(nil))

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
