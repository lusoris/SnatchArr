// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/http2"

	"github.com/lusoris/SnatchArr/api/internal/libraryprocessing"
)

// Observe failed native socket writes without setting, clearing, or replacing
// deadlines. HTTP/1 EOF alone cannot distinguish a write timeout from an abort.
type ruleBudgetListener struct {
	net.Listener
	writeTimeouts chan<- error
}

func (l ruleBudgetListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return ruleBudgetConn{Conn: c, writeTimeouts: l.writeTimeouts}, nil
}

type ruleBudgetConn struct {
	net.Conn
	writeTimeouts chan<- error
}

func (c ruleBudgetConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if errors.Is(err, os.ErrDeadlineExceeded) {
		select {
		case c.writeTimeouts <- err:
		default:
		}
	}
	return n, err
}

const (
	nativeBudgetShort    = 500 * time.Millisecond
	nativeBudgetHold     = 800 * time.Millisecond
	nativeBudgetLong     = 5 * time.Second
	nativeBudgetWatchdog = 10 * time.Second
	nativeBudgetPath     = PathPrefix + "/library-processing/rules"
	nativeBudgetFixtures = 5
)

type nativeBudgetOutcome struct {
	response       *http.Response
	body           []byte
	doErr, readErr error
	finished       time.Time
}

type nativeBudgetFixture struct {
	ctx            context.Context
	server         *http.Server
	url            string
	client         *http.Client
	protoMajor     int
	budget         time.Duration
	sent           time.Time
	arrived        chan *http.Request
	release        chan struct{}
	releaseOnce    sync.Once
	rulesEntered   chan struct{}
	socketTimeouts chan error
	results        chan *nativeBudgetOutcome
	queued         *nativeBudgetOutcome
}

func buildNativeBudgetRouter(t *testing.T) (http.Handler, chan struct{}) {
	t.Helper()
	h := &Handlers{
		libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, &deadlineObservingChecker{enabled: true}),
	}
	stream := h.serveLibraryProcessingRulesWith(2, fakeBatchStreamer(sampleStoredRules(nativeBudgetFixtures)))
	rulesEntered := make(chan struct{})
	var logs bytes.Buffer
	router := buildProductionMiddlewareStack(t, &logs, func(w http.ResponseWriter, r *http.Request) {
		close(rulesEntered)
		stream(w, r)
	})
	return router, rulesEntered
}

func startNativeBudgetTransport(ts *httptest.Server, h2 bool, socketTimeouts chan<- error) http.RoundTripper {
	if h2 {
		ts.EnableHTTP2 = true
		ts.StartTLS()
		// Use the httptest client's trust roots with the native
		// transport; the peer reset is read from its stream error.
		return &http.Transport{
			TLSClientConfig:   ts.Client().Transport.(*http.Transport).TLSClientConfig.Clone(),
			ForceAttemptHTTP2: true,
		}
	}
	ts.Listener = ruleBudgetListener{Listener: ts.Listener, writeTimeouts: socketTimeouts}
	ts.Start()
	return &http.Transport{DisableKeepAlives: true}
}

func newNativeBudgetFixture(t *testing.T, protoMajor int, budget time.Duration) *nativeBudgetFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), nativeBudgetWatchdog)
	router, rulesEntered := buildNativeBudgetRouter(t)
	f := &nativeBudgetFixture{
		ctx:            ctx,
		protoMajor:     protoMajor,
		budget:         budget,
		arrived:        make(chan *http.Request, 1),
		release:        make(chan struct{}),
		rulesEntered:   rulesEntered,
		socketTimeouts: make(chan error, 1),
		results:        make(chan *nativeBudgetOutcome, 1),
	}
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.arrived <- r
		select {
		case <-f.release:
			// Also enter the router after an HTTP/2 reset. The gate
			// must not turn request cancellation into a synthetic response.
			router.ServeHTTP(w, r)
		case <-ctx.Done():
		}
	}))
	ts.Config.Handler = WithNativeRulesWriteCancellation(ts.Config.Handler)
	ts.Config.WriteTimeout = budget
	transport := startNativeBudgetTransport(ts, protoMajor == 2, f.socketTimeouts)
	f.server = ts.Config
	f.url = ts.URL
	f.client = &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	t.Cleanup(ts.Close)
	t.Cleanup(f.client.CloseIdleConnections)
	t.Cleanup(cancel)
	t.Cleanup(f.releaseGate)
	return f
}

func (f *nativeBudgetFixture) releaseGate() {
	f.releaseOnce.Do(func() { close(f.release) })
}

func (f *nativeBudgetFixture) startGET(t *testing.T) {
	t.Helper()
	req, err := http.NewRequestWithContext(f.ctx, http.MethodGet, f.url+nativeBudgetPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer test-token")
	f.sent = time.Now()
	go func() {
		resp, doErr := f.client.Do(req)
		got := &nativeBudgetOutcome{response: resp, doErr: doErr}
		if doErr == nil {
			got.body, got.readErr = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
		}
		got.finished = time.Now()
		f.results <- got
	}()
}

func (f *nativeBudgetFixture) pollResult() (*nativeBudgetOutcome, bool) {
	if f.queued != nil {
		got := f.queued
		f.queued = nil
		return got, true
	}
	select {
	case got := <-f.results:
		return got, true
	default:
		return nil, false
	}
}

func (f *nativeBudgetFixture) awaitResult(t *testing.T) *nativeBudgetOutcome {
	t.Helper()
	if f.queued != nil {
		got := f.queued
		f.queued = nil
		return got
	}
	select {
	case got := <-f.results:
		return got
	case <-f.ctx.Done():
		t.Fatalf("GET did not finish before watchdog: %v", f.ctx.Err())
		return nil
	}
}

// Probes pass nil for initialArrival to force the result arm while leaving
// f.arrived available for the follow-up check. Real fixtures pass f.arrived.
func (f *nativeBudgetFixture) selectArrival(initialArrival <-chan *http.Request, allowQueuedReset bool) (*http.Request, string) {
	select {
	case incoming := <-initialArrival:
		return incoming, ""
	case got := <-f.results:
		select {
		case incoming := <-f.arrived:
			if !allowQueuedReset {
				return nil, fmt.Sprintf("GET finished before middleware release: response=%v error=%v", got.response, got.doErr)
			}
			f.queued = got
			return incoming, ""
		default:
			return nil, fmt.Sprintf("GET finished before reaching delay middleware: response=%v error=%v", got.response, got.doErr)
		}
	case <-f.ctx.Done():
		return nil, fmt.Sprintf("GET did not reach delay middleware: %v", f.ctx.Err())
	}
}

func (f *nativeBudgetFixture) assertRulesStillGated(t *testing.T) {
	t.Helper()
	select {
	case <-f.rulesEntered:
		t.Fatal("rules handler ran while the middleware gate was closed")
	default:
	}
}

func (f *nativeBudgetFixture) assertIncomingProtocol(t *testing.T, incoming *http.Request) {
	t.Helper()
	if incoming.Method != http.MethodGet || incoming.URL.Path != nativeBudgetPath || incoming.ProtoMajor != f.protoMajor {
		t.Fatalf("middleware saw %s %s over %s; want GET %s over HTTP/%d", incoming.Method, incoming.URL.Path, incoming.Proto, nativeBudgetPath, f.protoMajor)
	}
	if f.protoMajor == 2 {
		if incoming.TLS == nil || incoming.TLS.NegotiatedProtocol != "h2" {
			t.Fatal("HTTP/2 request did not negotiate h2 over TLS")
		}
		return
	}
	if incoming.ProtoMinor != 1 || incoming.TLS != nil {
		t.Fatalf("want cleartext HTTP/1.1; got %s, TLS=%v", incoming.Proto, incoming.TLS != nil)
	}
}

func (f *nativeBudgetFixture) awaitArrivalAndHold(t *testing.T, allowQueuedReset bool) *http.Request {
	t.Helper()
	incoming, failure := f.selectArrival(f.arrived, allowQueuedReset)
	if failure != "" {
		t.Fatal(failure)
	}
	f.assertIncomingProtocol(t, incoming)
	if srv, _ := incoming.Context().Value(http.ServerContextKey).(*http.Server); srv != f.server || srv.WriteTimeout != f.budget {
		t.Fatalf("request did not inherit native server with WriteTimeout=%v", f.budget)
	}
	// Measure the delay from actual middleware entry, not from dial or
	// TLS setup. Both controls spend the same time in this phase.
	timer := time.NewTimer(nativeBudgetHold)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-f.ctx.Done():
		t.Fatalf("watchdog expired in middleware phase: %v", f.ctx.Err())
	}
	f.assertRulesStillGated(t)
	return incoming
}

func (f *nativeBudgetFixture) checkHTTP2BudgetReset(got *nativeBudgetOutcome) string {
	var reset http2.StreamError
	if got.response != nil || !errors.As(got.doErr, &reset) || reset.Code != http2.ErrCodeInternal || reset.StreamID == 0 {
		return fmt.Sprintf("want HTTP/2 INTERNAL_ERROR stream reset before headers; response=%v error=%v", got.response, got.doErr)
	}
	if got.finished.Sub(f.sent) < f.budget || f.ctx.Err() != nil {
		return fmt.Sprintf("reset did not match the native budget: elapsed=%v client context=%v", got.finished.Sub(f.sent), f.ctx.Err())
	}
	return ""
}

// assertHTTP2BudgetReset verifies that HTTP/2 resets an idle stream before
// releasing the gate, cancels the server request context, and never enters the handler.
func (f *nativeBudgetFixture) assertHTTP2BudgetReset(t *testing.T, incoming *http.Request) {
	t.Helper()
	got := f.awaitResult(t)
	if failure := f.checkHTTP2BudgetReset(got); failure != "" {
		t.Fatal(failure)
	}
	select {
	case <-incoming.Context().Done():
	case <-f.ctx.Done():
		t.Fatal("peer reset did not terminate the server request context")
	}
	f.assertRulesStillGated(t)
	f.releaseGate()
}

func (f *nativeBudgetFixture) checkBeforeRelease(incoming *http.Request) string {
	if got, ok := f.pollResult(); ok {
		return fmt.Sprintf("GET finished before middleware release: response=%v error=%v", got.response, got.doErr)
	}
	if reqErr := incoming.Context().Err(); reqErr != nil {
		return fmt.Sprintf("request already canceled before middleware release: %v", reqErr)
	}
	return ""
}

func (f *nativeBudgetFixture) releaseAndAwait(t *testing.T, incoming *http.Request) *nativeBudgetOutcome {
	t.Helper()
	// HTTP/1.1 does not report an expired write deadline until a
	// write is attempted. The controls must likewise still be live.
	if failure := f.checkBeforeRelease(incoming); failure != "" {
		t.Fatal(failure)
	}
	f.releaseGate()
	got := f.awaitResult(t)
	select {
	case <-f.rulesEntered:
	default:
		t.Fatal("GET never reached the production rules handler")
	}
	return got
}

// assertHTTP1BudgetTimeout releases the gate and verifies that the expired
// HTTP/1.1 write deadline produces a native socket write timeout and client EOF.
func (f *nativeBudgetFixture) assertHTTP1BudgetTimeout(t *testing.T, incoming *http.Request) {
	t.Helper()
	got := f.releaseAndAwait(t, incoming)
	if got.response != nil || !errors.Is(got.doErr, io.EOF) || f.ctx.Err() != nil {
		t.Fatalf("want HTTP/1.1 EOF before headers from the expired native budget; response=%v error=%v client context=%v body=%q", got.response, got.doErr, f.ctx.Err(), got.body)
	}
	select {
	case <-f.socketTimeouts:
	default:
		t.Fatal("HTTP/1.1 EOF was not accompanied by a native socket write timeout")
	}
}

func assertPositiveControlBody(t *testing.T, body []byte) {
	t.Helper()
	list, err := decodeRuleListJSON(body)
	if err != nil || !json.Valid(body) || len(list) != nativeBudgetFixtures {
		t.Fatalf("positive control did not return the complete rules array: len=%d want=%d err=%v body=%q", len(list), nativeBudgetFixtures, err, body)
	}
}

// assertBudgetControlResponse releases the gate and verifies a complete 200 OK
// rules array over the negotiated protocol.
func (f *nativeBudgetFixture) assertBudgetControlResponse(t *testing.T, incoming *http.Request) {
	t.Helper()
	got := f.releaseAndAwait(t, incoming)
	if got.doErr != nil || got.readErr != nil || got.response == nil {
		t.Fatalf("positive control GET failed: request=%v read=%v", got.doErr, got.readErr)
	}
	if got.response.ProtoMajor != f.protoMajor || got.response.StatusCode != http.StatusOK {
		t.Fatalf("positive control got %s status %d; want HTTP/%d status 200; body=%q", got.response.Proto, got.response.StatusCode, f.protoMajor, got.body)
	}
	if f.protoMajor == 2 && (got.response.TLS == nil || got.response.TLS.NegotiatedProtocol != "h2") {
		t.Fatal("HTTP/2 positive control did not negotiate h2 over TLS")
	}
	assertPositiveControlBody(t, got.body)
}

func TestRulesGETMiddlewareDelayConsumesNativeWriteBudget(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		protoMajor int
		budget     time.Duration
	}{
		{"HTTP1/expired", 1, nativeBudgetShort},
		{"HTTP1/control", 1, nativeBudgetLong},
		{"TLS_HTTP2/expired", 2, nativeBudgetShort},
		{"TLS_HTTP2/control", 2, nativeBudgetLong},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h2 := tc.protoMajor == 2
			expired := tc.budget == nativeBudgetShort
			f := newNativeBudgetFixture(t, tc.protoMajor, tc.budget)
			f.startGET(t)
			incoming := f.awaitArrivalAndHold(t, h2 && expired)
			switch {
			case expired && h2:
				f.assertHTTP2BudgetReset(t, incoming)
			case expired:
				f.assertHTTP1BudgetTimeout(t, incoming)
			default:
				f.assertBudgetControlResponse(t, incoming)
			}
		})
	}
}

func newQueuedArrivalProbe(t *testing.T) *nativeBudgetFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	t.Cleanup(cancel)
	return &nativeBudgetFixture{
		ctx:     ctx,
		budget:  nativeBudgetShort,
		sent:    time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC),
		arrived: make(chan *http.Request, 1),
		results: make(chan *nativeBudgetOutcome, 1),
	}
}

func testQueuedHTTP2ArrivalRetainsReset(t *testing.T) {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, nativeBudgetPath, nil)
	f := newQueuedArrivalProbe(t)
	want := &nativeBudgetOutcome{
		doErr:    http2.StreamError{StreamID: 1, Code: http2.ErrCodeInternal},
		finished: f.sent.Add(nativeBudgetShort),
	}
	f.arrived <- req
	f.results <- want
	incoming, failure := f.selectArrival(nil, true)
	if failure != "" || incoming != req {
		t.Fatalf("queued HTTP/2 arrival failed: incoming=%v failure=%q", incoming, failure)
	}
	if f.queued != want {
		t.Fatalf("queued HTTP/2 outcome = %p; want %p", f.queued, want)
	}
	got := f.awaitResult(t)
	if got != want {
		t.Fatalf("awaited HTTP/2 outcome = %p; want %p", got, want)
	}
	if resetFailure := f.checkHTTP2BudgetReset(got); resetFailure != "" {
		t.Fatalf("retained HTTP/2 reset failed normal check: %s", resetFailure)
	}
}

func testQueuedArrivalNegativeCases(t *testing.T) {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, nativeBudgetPath, nil)
	f := newQueuedArrivalProbe(t)
	f.arrived <- req
	f.results <- &nativeBudgetOutcome{doErr: io.EOF, finished: f.sent.Add(nativeBudgetShort)}
	if incoming, failure := f.selectArrival(nil, false); incoming != nil || !strings.Contains(failure, "GET finished before middleware release") {
		t.Fatalf("HTTP/1 queued completion was not rejected strictly: incoming=%v failure=%q", incoming, failure)
	}
	noArrival := newQueuedArrivalProbe(t)
	noArrival.results <- &nativeBudgetOutcome{
		doErr:    http2.StreamError{StreamID: 1, Code: http2.ErrCodeInternal},
		finished: noArrival.sent.Add(nativeBudgetShort),
	}
	if incoming, failure := noArrival.selectArrival(nil, true); incoming != nil || !strings.Contains(failure, "GET finished before reaching delay middleware") {
		t.Fatalf("missing arrival did not fail: incoming=%v failure=%q", incoming, failure)
	}
	wrongOutcome := newQueuedArrivalProbe(t)
	wrong := &nativeBudgetOutcome{doErr: io.EOF, finished: wrongOutcome.sent.Add(nativeBudgetShort)}
	wrongOutcome.arrived <- req
	wrongOutcome.results <- wrong
	if incoming, failure := wrongOutcome.selectArrival(nil, true); incoming != req || failure != "" {
		t.Fatalf("unexpected arrival result: incoming=%v failure=%q", incoming, failure)
	}
	if wrongOutcome.queued != wrong {
		t.Fatalf("queued non-reset outcome = %p; want %p", wrongOutcome.queued, wrong)
	}
	if resetFailure := wrongOutcome.checkHTTP2BudgetReset(wrongOutcome.awaitResult(t)); resetFailure == "" {
		t.Fatal("non-reset outcome passed HTTP/2 reset check")
	}
}

func TestNativeWriteBudgetQueuedArrivalSelect(t *testing.T) {
	t.Parallel()
	t.Run("queued HTTP2 arrival retains concurrent reset for normal assertion", func(t *testing.T) {
		t.Parallel()
		testQueuedHTTP2ArrivalRetainsReset(t)
	})
	t.Run("planted negative cases reject HTTP1 early finish missing arrival and non-reset", func(t *testing.T) {
		t.Parallel()
		testQueuedArrivalNegativeCases(t)
	})
}
