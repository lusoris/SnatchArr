// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestRulesGETMiddlewareDelayConsumesNativeWriteBudget(t *testing.T) {
	t.Parallel()
	const (
		shortBudget = 500 * time.Millisecond
		hold        = 800 * time.Millisecond
		longBudget  = 5 * time.Second
		watchdog    = 10 * time.Second
	)
	path := PathPrefix + "/library-processing/rules"

	for _, tc := range []struct {
		name       string
		protoMajor int
		budget     time.Duration
	}{
		{"HTTP1/expired", 1, shortBudget},
		{"HTTP1/control", 1, longBudget},
		{"TLS_HTTP2/expired", 2, shortBudget},
		{"TLS_HTTP2/control", 2, longBudget},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h2 := tc.protoMajor == 2
			expired := tc.budget == shortBudget
			ctx, cancel := context.WithTimeout(t.Context(), watchdog)
			defer cancel()

			fixtures := sampleStoredRules(5)
			h := &Handlers{
				libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, &deadlineObservingChecker{enabled: true}),
			}
			stream := h.serveLibraryProcessingRulesWith(2, fakeBatchStreamer(fixtures))
			rulesEntered := make(chan struct{})
			var logs bytes.Buffer
			router := buildProductionMiddlewareStack(t, &logs, func(w http.ResponseWriter, r *http.Request) {
				close(rulesEntered)
				stream(w, r)
			})

			arrived := make(chan *http.Request, 1)
			release := make(chan struct{})
			ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				arrived <- r
				select {
				case <-release:
					// Also enter the router after an HTTP/2 reset. The gate
					// must not turn request cancellation into a synthetic response.
					router.ServeHTTP(w, r)
				case <-ctx.Done():
				}
			}))
			ts.Config.Handler = WithNativeRulesWriteCancellation(ts.Config.Handler)
			ts.Config.WriteTimeout = tc.budget
			socketTimeouts := make(chan error, 1)
			var transport http.RoundTripper
			if h2 {
				ts.EnableHTTP2 = true
				ts.StartTLS()
				// Use the httptest client's trust roots with the native
				// transport; the peer reset is read from its stream error.
				transport = &http.Transport{
					TLSClientConfig:   ts.Client().Transport.(*http.Transport).TLSClientConfig.Clone(),
					ForceAttemptHTTP2: true,
				}
			} else {
				ts.Listener = ruleBudgetListener{Listener: ts.Listener, writeTimeouts: socketTimeouts}
				ts.Start()
				transport = &http.Transport{DisableKeepAlives: true}
			}
			t.Cleanup(ts.Close)
			client := &http.Client{
				Transport: transport,
				CheckRedirect: func(*http.Request, []*http.Request) error {
					return http.ErrUseLastResponse
				},
			}
			t.Cleanup(client.CloseIdleConnections)
			defer func() {
				select {
				case <-release:
				default:
					close(release)
				}
			}()

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+path, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer test-token")
			type outcome struct {
				response       *http.Response
				body           []byte
				doErr, readErr error
				finished       time.Time
			}
			results := make(chan outcome, 1)
			sent := time.Now()
			go func() {
				resp, doErr := client.Do(req)
				got := outcome{response: resp, doErr: doErr}
				if doErr == nil {
					got.body, got.readErr = io.ReadAll(resp.Body)
					_ = resp.Body.Close()
				}
				got.finished = time.Now()
				results <- got
			}()
			awaitResult := func() outcome {
				t.Helper()
				select {
				case got := <-results:
					return got
				case <-ctx.Done():
					t.Fatalf("GET did not finish before watchdog: %v", ctx.Err())
					return outcome{}
				}
			}

			var incoming *http.Request
			select {
			case incoming = <-arrived:
			case got := <-results:
				t.Fatalf("GET finished before reaching delay middleware: response=%v error=%v", got.response, got.doErr)
			case <-ctx.Done():
				t.Fatalf("GET did not reach delay middleware: %v", ctx.Err())
			}
			if incoming.Method != http.MethodGet || incoming.URL.Path != path || incoming.ProtoMajor != tc.protoMajor {
				t.Fatalf("middleware saw %s %s over %s; want GET %s over HTTP/%d", incoming.Method, incoming.URL.Path, incoming.Proto, path, tc.protoMajor)
			}
			if h2 && (incoming.TLS == nil || incoming.TLS.NegotiatedProtocol != "h2") {
				t.Fatal("HTTP/2 request did not negotiate h2 over TLS")
			}
			if !h2 && (incoming.ProtoMinor != 1 || incoming.TLS != nil) {
				t.Fatalf("want cleartext HTTP/1.1; got %s, TLS=%v", incoming.Proto, incoming.TLS != nil)
			}
			if srv, _ := incoming.Context().Value(http.ServerContextKey).(*http.Server); srv != ts.Config || srv.WriteTimeout != tc.budget {
				t.Fatalf("request did not inherit native server with WriteTimeout=%v", tc.budget)
			}

			// Measure the delay from actual middleware entry, not from dial or
			// TLS setup. Both controls spend the same time in this phase.
			timer := time.NewTimer(hold)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
				t.Fatalf("watchdog expired in middleware phase: %v", ctx.Err())
			}
			assertRulesStillGated := func() {
				t.Helper()
				select {
				case <-rulesEntered:
					t.Fatal("rules handler ran while the middleware gate was closed")
				default:
				}
			}
			assertRulesStillGated()

			if expired && h2 {
				// HTTP/2 can reset an idle stream without any application
				// write. Require the reset before letting the handler run.
				got := awaitResult()
				var reset http2.StreamError
				if got.response != nil || !errors.As(got.doErr, &reset) || reset.Code != http2.ErrCodeInternal || reset.StreamID == 0 {
					t.Fatalf("want HTTP/2 INTERNAL_ERROR stream reset before headers; response=%v error=%v", got.response, got.doErr)
				}
				if got.finished.Sub(sent) < tc.budget || ctx.Err() != nil {
					t.Fatalf("reset did not match the native budget: elapsed=%v client context=%v", got.finished.Sub(sent), ctx.Err())
				}
				select {
				case <-incoming.Context().Done():
				case <-ctx.Done():
					t.Fatal("peer reset did not terminate the server request context")
				}
				assertRulesStillGated()
				close(release)
				return
			}

			// HTTP/1.1 does not report an expired write deadline until a
			// write is attempted. The controls must likewise still be live.
			select {
			case got := <-results:
				t.Fatalf("GET finished before middleware release: response=%v error=%v", got.response, got.doErr)
			default:
			}
			if reqErr := incoming.Context().Err(); reqErr != nil {
				t.Fatalf("request already canceled before middleware release: %v", reqErr)
			}
			close(release)
			got := awaitResult()
			select {
			case <-rulesEntered:
			default:
				t.Fatal("GET never reached the production rules handler")
			}

			if expired {
				if got.response != nil || !errors.Is(got.doErr, io.EOF) || ctx.Err() != nil {
					t.Fatalf("want HTTP/1.1 EOF before headers from the expired native budget; response=%v error=%v client context=%v body=%q", got.response, got.doErr, ctx.Err(), got.body)
				}
				select {
				case <-socketTimeouts:
				default:
					t.Fatal("HTTP/1.1 EOF was not accompanied by a native socket write timeout")
				}
				return
			}

			if got.doErr != nil || got.readErr != nil || got.response == nil {
				t.Fatalf("positive control GET failed: request=%v read=%v", got.doErr, got.readErr)
			}
			if got.response.ProtoMajor != tc.protoMajor || got.response.StatusCode != http.StatusOK {
				t.Fatalf("positive control got %s status %d; want HTTP/%d status 200; body=%q", got.response.Proto, got.response.StatusCode, tc.protoMajor, got.body)
			}
			if h2 && (got.response.TLS == nil || got.response.TLS.NegotiatedProtocol != "h2") {
				t.Fatal("HTTP/2 positive control did not negotiate h2 over TLS")
			}
			list, err := decodeRuleListJSON(got.body)
			if err != nil || !json.Valid(got.body) || len(list) != len(fixtures) {
				t.Fatalf("positive control did not return the complete rules array: len=%d want=%d err=%v body=%q", len(list), len(fixtures), err, got.body)
			}
		})
	}
}
