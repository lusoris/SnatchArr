// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/libraryprocessing"
)

// The first rule is larger than every buffer between handler and client: the
// client h2 transport advertises 64 KiB stream and connection receive windows
// (http.HTTP2Config; the connection window adds the 65535 byte protocol
// initial window) and both TCP socket buffers are shrunk to 32 KiB. The 1 MiB
// intermediate write that flushes it therefore cannot complete until the client
// reads or the server gives up.
const (
	blockedTransportPayload  = 1 << 20
	blockedTransportH2Window = 64 << 10
	blockedTransportSockBuf  = 32 << 10
	blockedTransportStep     = 5 * time.Second
	blockedTransportQuiet    = 300 * time.Millisecond
	blockedTransportLong     = 30 * time.Second
	blockedTransportShort    = 1500 * time.Millisecond
	blockedTransportDrain    = 10 * time.Second
	blockedTransportWatchdog = 40 * time.Second
)

type blockedTransportMode int

const (
	// Snapshot-only cancellation releases the blocked write; the native total
	// timeout is far away and the client stays passive until the server returns.
	blockedCancelReleases blockedTransportMode = iota
	// Nobody cancels; only the short native WriteTimeout can release the write.
	blockedNativeTimeout
	// Nobody cancels and the client drains: the full array must arrive.
	blockedPositiveControl
	// Snapshot cancellation without the native capability cannot interrupt the
	// blocked write; it only takes effect once the client drains.
	blockedNoNativeStaysBlocked
)

type blockedTransportProbe struct {
	entered     chan struct{}
	writeDone   chan struct{}
	handlerDone chan struct{}
	enteredOnce sync.Once
	doneOnce    sync.Once
	cancelAsked atomic.Bool

	// Written before entered closes.
	snapCancel context.CancelFunc
	// Written before writeDone closes.
	writeErr         error
	writeAfterCancel bool
	writeReturned    time.Time
}

func newBlockedTransportProbe() *blockedTransportProbe {
	return &blockedTransportProbe{
		entered:     make(chan struct{}),
		writeDone:   make(chan struct{}),
		handlerDone: make(chan struct{}),
	}
}

// blockedTransportWriter observes only the oversized intermediate write and
// leaves flush, deadline and unwrap behaviour to the production writer chain.
type blockedTransportWriter struct {
	http.ResponseWriter
	probe *blockedTransportProbe
}

func (w *blockedTransportWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *blockedTransportWriter) Write(p []byte) (int, error) {
	if len(p) < blockedTransportPayload {
		return w.ResponseWriter.Write(p)
	}
	w.probe.enteredOnce.Do(func() { close(w.probe.entered) })
	n, err := w.ResponseWriter.Write(p)
	w.probe.doneOnce.Do(func() {
		w.probe.writeErr = err
		w.probe.writeAfterCancel = w.probe.cancelAsked.Load()
		w.probe.writeReturned = time.Now()
		close(w.probe.writeDone)
	})
	return n, err
}

type blockedTransportListener struct{ net.Listener }

func (l blockedTransportListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	if tc, ok := c.(*net.TCPConn); ok {
		if err := tc.SetWriteBuffer(blockedTransportSockBuf); err != nil {
			_ = c.Close()
			return nil, err
		}
	}
	return c, nil
}

func blockedTransportDial(ctx context.Context, network, addr string) (net.Conn, error) {
	var d net.Dialer
	c, err := d.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	if tc, ok := c.(*net.TCPConn); ok {
		if err := tc.SetReadBuffer(blockedTransportSockBuf); err != nil {
			_ = c.Close()
			return nil, err
		}
	}
	return c, nil
}

// blockedTransportDialTLS shrinks the client read buffer like the cleartext
// dialer and offers only h2, so the native transport negotiates HTTP/2.
func blockedTransportDialTLS(base *tls.Config) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		raw, err := blockedTransportDial(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		cfg := base.Clone()
		cfg.NextProtos = []string{"h2"}
		if host, _, splitErr := net.SplitHostPort(addr); splitErr == nil {
			cfg.ServerName = host
		}
		conn := tls.Client(raw, cfg)
		if err := conn.HandshakeContext(ctx); err != nil {
			_ = raw.Close()
			return nil, err
		}
		return conn, nil
	}
}

type blockedTransportTail struct {
	n    int64
	tail []byte
}

func (w *blockedTransportTail) Write(p []byte) (int, error) {
	w.n += int64(len(p))
	w.tail = append(w.tail, p...)
	if len(w.tail) > 16 {
		w.tail = append(w.tail[:0], w.tail[len(w.tail)-16:]...)
	}
	return len(p), nil
}

var errBlockedTransportDrainTimeout = errors.New("client drain did not finish")

// blockedTransportReply is the response head; the body stays with the client
// goroutine, which closes it on every path.
type blockedTransportReply struct {
	status     int
	proto      string
	protoMajor int
	err        error
}

// blockedTransportClient owns one request: the goroutine sends the head, then
// reads the body only once drain hands it a destination.
type blockedTransportClient struct {
	head   chan blockedTransportReply
	drain  chan io.Writer
	result chan error
}

func startBlockedTransportClient(ctx context.Context, client *http.Client, req *http.Request) *blockedTransportClient {
	c := &blockedTransportClient{
		head:   make(chan blockedTransportReply, 1),
		drain:  make(chan io.Writer, 1),
		result: make(chan error, 1),
	}
	go c.run(ctx, client, req)
	return c
}

func (c *blockedTransportClient) run(ctx context.Context, client *http.Client, req *http.Request) {
	resp, err := client.Do(req)
	if err != nil {
		c.head <- blockedTransportReply{err: err}
		return
	}
	defer func() { _ = resp.Body.Close() }()
	c.head <- blockedTransportReply{status: resp.StatusCode, proto: resp.Proto, protoMajor: resp.ProtoMajor}
	select {
	case dst := <-c.drain:
		_, copyErr := io.Copy(dst, resp.Body)
		c.result <- copyErr
	case <-ctx.Done():
	}
}

func (c *blockedTransportClient) drainTo(t *testing.T, dst io.Writer) (blockedTransportReply, error) {
	t.Helper()
	var rep blockedTransportReply
	select {
	case rep = <-c.head:
	case <-time.After(blockedTransportStep):
		t.Fatal("client received no response headers")
	}
	if rep.err != nil {
		t.Fatalf("client request failed before the body: %v", rep.err)
	}
	c.drain <- dst
	select {
	case err := <-c.result:
		return rep, err
	case <-time.After(blockedTransportDrain):
		t.Fatal(errBlockedTransportDrainTimeout)
		return rep, errBlockedTransportDrainTimeout
	}
}

func blockedTransportFixtures() []libraryprocessing.StoredRule {
	rules := sampleStoredRules(3)
	rules[0].Rule.Track.Languages = []string{strings.Repeat("x", blockedTransportPayload)}
	return rules
}

func blockedTransportWait(t *testing.T, ch <-chan struct{}, failure string) {
	t.Helper()
	timer := time.NewTimer(blockedTransportStep)
	defer timer.Stop()
	select {
	case <-ch:
	case <-timer.C:
		t.Fatal(failure)
	}
}

func blockedTransportStillBlocked(t *testing.T, probe *blockedTransportProbe, d time.Duration) {
	t.Helper()
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-probe.writeDone:
		t.Fatal("blocked write returned while the client had read nothing: payload fits in transport buffers")
	case <-probe.handlerDone:
		t.Fatal("handler returned while its intermediate write should still be blocked")
	case <-timer.C:
	}
}

func startBlockedTransportServer(t *testing.T, h2 bool, writeTimeout time.Duration, handler http.Handler) (*httptest.Server, *http.Client, context.Context) {
	t.Helper()
	ts := httptest.NewUnstartedServer(handler)
	ts.Config.WriteTimeout = writeTimeout
	ts.Listener = blockedTransportListener{Listener: ts.Listener}
	var transport http.RoundTripper
	if h2 {
		ts.EnableHTTP2 = true
		ts.StartTLS()
		transport = &http.Transport{
			DialTLSContext:    blockedTransportDialTLS(ts.Client().Transport.(*http.Transport).TLSClientConfig),
			ForceAttemptHTTP2: true,
			HTTP2: &http.HTTP2Config{
				MaxReceiveBufferPerStream:     blockedTransportH2Window,
				MaxReceiveBufferPerConnection: blockedTransportH2Window,
			},
		}
	} else {
		ts.Start()
		transport = &http.Transport{DisableKeepAlives: true, DialContext: blockedTransportDial}
	}
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	ctx, cancel := context.WithTimeout(t.Context(), blockedTransportWatchdog)
	t.Cleanup(ts.Close)
	// Runs before ts.Close: a failed assertion must never leave the handler
	// blocked, because Close waits for it.
	t.Cleanup(func() {
		cancel()
		ts.CloseClientConnections()
		client.CloseIdleConnections()
	})
	return ts, client, ctx
}

type blockedTransportFixture struct {
	ctx          context.Context
	probe        *blockedTransportProbe
	replies      *blockedTransportClient
	sent         time.Time
	writeTimeout time.Duration
	h2           bool
}

func buildBlockedTransportHandler(t *testing.T, native bool, probe *blockedTransportProbe) http.Handler {
	t.Helper()
	withSnap := func(parent context.Context) (context.Context, context.CancelFunc) {
		snapCtx, cancel := context.WithCancel(parent)
		probe.snapCancel = cancel
		return snapCtx, cancel
	}
	h := &Handlers{
		libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, &deadlineObservingChecker{enabled: true}),
	}
	// Batch size 1: batch 0 (oversized) is pending, batch 1 flushes it as the
	// intermediate write under the snapshot context.
	stream := h.serveLibraryProcessingRulesAt(1, withSnap, fakeBatchStreamer(blockedTransportFixtures()))
	var logs bytes.Buffer
	router := buildProductionMiddlewareStack(t, &logs, func(w http.ResponseWriter, r *http.Request) {
		stream(&blockedTransportWriter{ResponseWriter: w, probe: probe}, r)
	})
	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(probe.handlerDone)
		router.ServeHTTP(w, r)
	})
	if native {
		return WithNativeRulesWriteCancellation(handler)
	}
	return handler
}

func startBlockedTransportFixture(t *testing.T, h2, native bool, mode blockedTransportMode) *blockedTransportFixture {
	t.Helper()
	writeTimeout := blockedTransportLong
	if mode == blockedNativeTimeout {
		writeTimeout = blockedTransportShort
	}
	probe := newBlockedTransportProbe()
	handler := buildBlockedTransportHandler(t, native, probe)
	ts, client, ctx := startBlockedTransportServer(t, h2, writeTimeout, handler)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+PathPrefix+"/library-processing/rules", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer test-token")
	sent := time.Now()
	replies := startBlockedTransportClient(ctx, client, req)

	blockedTransportWait(t, probe.entered, "intermediate write never started")
	if mode != blockedNativeTimeout {
		blockedTransportStillBlocked(t, probe, blockedTransportQuiet)
	}
	return &blockedTransportFixture{
		ctx:          ctx,
		probe:        probe,
		replies:      replies,
		sent:         sent,
		writeTimeout: writeTimeout,
		h2:           h2,
	}
}

// assertCancelReleases waits for the handler to abort via the native callback
// before draining the client stream.
func (f *blockedTransportFixture) assertCancelReleases(t *testing.T) {
	t.Helper()
	f.probe.cancelAsked.Store(true)
	f.probe.snapCancel()
	// The client has not read, canceled or disconnected; only the native
	// callback can free the write.
	blockedTransportWait(t, f.probe.handlerDone,
		"snapshot cancellation did not release the blocked write: native cancel is missing or a no-op")
	assertBlockedTransportAborted(t, f.probe)
	if !f.probe.writeAfterCancel {
		t.Fatal("blocked write returned before snapshot cancellation was requested")
	}
	if elapsed := f.probe.writeReturned.Sub(f.sent); elapsed >= f.writeTimeout/2 {
		t.Fatalf("release took %v, too close to the native %v total timeout", elapsed, f.writeTimeout)
	}
	if f.ctx.Err() != nil {
		t.Fatalf("client context ended before the server returned: %v", f.ctx.Err())
	}
	assertBlockedTransportTruncated(t, f.replies)
}

// assertNativeTimeout waits for the short native WriteTimeout to abort the
// handler before draining the client stream.
func (f *blockedTransportFixture) assertNativeTimeout(t *testing.T) {
	t.Helper()
	blockedTransportWait(t, f.probe.handlerDone, "native WriteTimeout did not release the blocked write")
	assertBlockedTransportAborted(t, f.probe)
	if f.probe.writeAfterCancel {
		t.Fatal("test canceled the snapshot in the native-timeout case")
	}
	if elapsed := f.probe.writeReturned.Sub(f.sent); elapsed < f.writeTimeout {
		t.Fatalf("write released after %v, before the native %v total timeout", elapsed, f.writeTimeout)
	}
	assertBlockedTransportTruncated(t, f.replies)
}

// assertNoNativeStaysBlocked verifies that snapshot cancellation alone leaves
// the write blocked, so the client must drain before the handler can return.
func (f *blockedTransportFixture) assertNoNativeStaysBlocked(t *testing.T) {
	t.Helper()
	f.probe.cancelAsked.Store(true)
	f.probe.snapCancel()
	blockedTransportStillBlocked(t, f.probe, 2*blockedTransportQuiet)
	assertBlockedTransportTruncated(t, f.replies)
	blockedTransportWait(t, f.probe.handlerDone, "handler did not return after the client drained")
	if f.probe.writeErr != nil || !f.probe.writeAfterCancel {
		t.Fatalf("write error = %v, after cancel = %v; want a write that finished only after cancel", f.probe.writeErr, f.probe.writeAfterCancel)
	}
}

// assertPositiveControl drains the full response array before waiting for the
// handler to return cleanly.
func (f *blockedTransportFixture) assertPositiveControl(t *testing.T) {
	t.Helper()
	var body bytes.Buffer
	head, drainErr := f.replies.drainTo(t, &body)
	if drainErr != nil {
		t.Fatalf("positive control body read: %v", drainErr)
	}
	wantProto := 1
	if f.h2 {
		wantProto = 2
	}
	if head.status != http.StatusOK || head.protoMajor != wantProto {
		t.Fatalf("positive control got %s status %d", head.proto, head.status)
	}
	list, decErr := decodeRuleListJSON(body.Bytes())
	if decErr != nil || len(list) != 3 {
		t.Fatalf("positive control decoded len = %d err = %v", len(list), decErr)
	}
	blockedTransportWait(t, f.probe.handlerDone, "handler did not return after full drain")
	if f.probe.writeErr != nil || f.probe.writeAfterCancel {
		t.Fatalf("positive control write error = %v, after cancel = %v", f.probe.writeErr, f.probe.writeAfterCancel)
	}
}

func runBlockedTransportCase(t *testing.T, h2, native bool, mode blockedTransportMode) {
	t.Helper()
	f := startBlockedTransportFixture(t, h2, native, mode)
	switch mode {
	case blockedCancelReleases:
		f.assertCancelReleases(t)
	case blockedNativeTimeout:
		f.assertNativeTimeout(t)
	case blockedNoNativeStaysBlocked:
		f.assertNoNativeStaysBlocked(t)
	case blockedPositiveControl:
		f.assertPositiveControl(t)
	}
}

func assertBlockedTransportAborted(t *testing.T, probe *blockedTransportProbe) {
	t.Helper()
	select {
	case <-probe.writeDone:
	default:
		t.Fatal("handler returned without the intermediate write returning")
	}
	if probe.writeErr == nil {
		t.Fatal("blocked intermediate write returned without an error")
	}
}

// assertBlockedTransportTruncated drains the response and requires an
// incomplete JSON array without a closing bracket.
func assertBlockedTransportTruncated(t *testing.T, replies *blockedTransportClient) {
	t.Helper()
	var tail blockedTransportTail
	_, err := replies.drainTo(t, &tail)
	if err == nil {
		t.Fatalf("client read a complete response (%d bytes) from an aborted stream", tail.n)
	}
	if bytes.HasSuffix(bytes.TrimSpace(tail.tail), []byte("]")) {
		t.Fatalf("aborted stream ended with a closing bracket: %q", tail.tail)
	}
}

// Run one slow-reader fixture at a time to bound memory and socket pressure.
var blockedTransportSlot = make(chan struct{}, 1)

// acquireBlockedTransportSlot is bounded and registers its release as the first
// cleanup, so it runs after the server and client teardown of the case.
func acquireBlockedTransportSlot(t *testing.T) {
	t.Helper()
	timer := time.NewTimer(8 * blockedTransportWatchdog)
	defer timer.Stop()
	select {
	case blockedTransportSlot <- struct{}{}:
		t.Cleanup(func() { <-blockedTransportSlot })
	case <-timer.C:
		t.Fatal("no blocked transport slot became free")
	}
}

func TestServeLibraryProcessingRulesBlockedTransport(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		h2     bool
		native bool
		mode   blockedTransportMode
	}{
		{"HTTP1_slow_reader/snapshot cancel releases blocked write via native callback", false, true, blockedCancelReleases},
		{"HTTP1_slow_reader/native total timeout releases without snapshot cancel", false, true, blockedNativeTimeout},
		{"HTTP1_slow_reader/positive control drains full array", false, true, blockedPositiveControl},
		{"HTTP1_slow_reader/without native capability cancel cannot release", false, false, blockedNoNativeStaysBlocked},
		{"TLS_HTTP2_flow_control/snapshot cancel releases blocked write via native callback", true, true, blockedCancelReleases},
		{"TLS_HTTP2_flow_control/native total timeout releases without snapshot cancel", true, true, blockedNativeTimeout},
		{"TLS_HTTP2_flow_control/positive control drains full array", true, true, blockedPositiveControl},
		{"TLS_HTTP2_flow_control/without native capability cancel cannot release", true, false, blockedNoNativeStaysBlocked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			acquireBlockedTransportSlot(t)
			runBlockedTransportCase(t, tc.h2, tc.native, tc.mode)
		})
	}
}
