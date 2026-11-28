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
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/httpx/middleware"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/libraryprocessing"
	"github.com/lusoris/SnatchArr/api/internal/sso"
)

func buildProductionMiddlewareStack(t *testing.T, logBuf *bytes.Buffer, rulesHandler http.HandlerFunc) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	chk := &deadlineObservingChecker{enabled: true}
	h := &Handlers{
		libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, chk),
		logger:            logger,
	}
	srv, err := oas.NewServer(h, testNoopSecurity{},
		oas.WithPathPrefix(PathPrefix),
		oas.WithErrorHandler(ErrorHandler(logger)),
	)
	if err != nil {
		t.Fatalf("oas.NewServer: %v", err)
	}
	r := chi.NewRouter()
	r.Use(
		middleware.RequestID,
		sso.ProxyHeaderMiddleware(config.ProxyAuthOptions{}, logger, ErrorHandler(logger)),
		middleware.TrustProxy(middleware.TrustProxyOptions{}),
		middleware.Recover(logger),
		middleware.Logger(logger, clock.NewFake()),
		middleware.SecureHeaders(middleware.SecureHeadersDefaults()),
	)
	r.With(withHTTP, csrfUnlessBearer(CrossOrigin(logger))).Handle(PathPrefix+"/*", dispatchStreamingRoutes(srv, rulesHandler))
	return r
}

func newProductionRulesHandlers(logBuf *bytes.Buffer) *Handlers {
	return &Handlers{
		libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, &deadlineObservingChecker{enabled: true}),
		logger:            slog.New(slog.NewTextHandler(logBuf, nil)),
	}
}

func newProductionRulesRequest(t *testing.T, logBuf *bytes.Buffer, rulesHandler http.HandlerFunc) *http.Request {
	t.Helper()
	ts := httptest.NewServer(buildProductionMiddlewareStack(t, logBuf, rulesHandler))
	t.Cleanup(ts.Close)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, ts.URL+PathPrefix+"/library-processing/rules", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	req.Header.Set("Authorization", "Bearer test-token")
	return req
}

type productionFlushHandshake struct {
	controllerCheck chan error
	firstBatchRead  chan struct{}
	releaseOnce     sync.Once
}

func newProductionFlushHandshake(h *Handlers) (*productionFlushHandshake, http.HandlerFunc) {
	hs := &productionFlushHandshake{
		controllerCheck: make(chan error, 1),
		firstBatchRead:  make(chan struct{}),
	}
	streamBatches := fakeBatchStreamer(sampleStoredRules(5))
	rawStream := h.serveLibraryProcessingRulesWith(2, func(ctx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error {
		batches := 0
		return streamBatches(ctx, batchSize, func(batch []libraryprocessing.StoredRule) error {
			batches++
			if batches == 3 {
				// The second callback flushes the first batch. Wait here so a
				// flush at handler return cannot satisfy the client assertion.
				if err := hs.waitForFirstBatchRead(ctx); err != nil {
					return err
				}
			}
			return onBatch(batch)
		})
	})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		deadlineErr := rc.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if deadlineErr == nil {
			deadlineErr = rc.SetWriteDeadline(time.Time{})
		}
		hs.controllerCheck <- deadlineErr
		rawStream(w, r)
	})
	return hs, handler
}

func (hs *productionFlushHandshake) waitForFirstBatchRead(ctx context.Context) error {
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-hs.firstBatchRead:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return errors.New("client did not read the first batch before the stream continued")
	}
}

func (hs *productionFlushHandshake) releaseThirdBatch() {
	hs.releaseOnce.Do(func() { close(hs.firstBatchRead) })
}

func (hs *productionFlushHandshake) assertDeadlineSupported(t *testing.T) {
	t.Helper()
	select {
	case err := <-hs.controllerCheck:
		if err != nil {
			t.Fatalf("production ResponseWriter failed SetWriteDeadline through middleware: %v", err)
		}
	default:
		t.Fatal("rules handler did not publish the deadline probe result")
	}
}

func decodeFirstRuleBatch(t *testing.T, src io.Reader, body *bytes.Buffer) {
	t.Helper()
	// Retain bytes read ahead by the decoder for the final full-array check.
	dec := json.NewDecoder(io.TeeReader(src, body))
	if tok, tokErr := dec.Token(); tokErr != nil || tok != json.Delim('[') {
		t.Fatalf("read opening array before stream completion: token=%v err=%v", tok, tokErr)
	}
	for i := range 2 {
		var rule oas.LibraryProcessingRule
		if itemErr := dec.Decode(&rule); itemErr != nil {
			t.Fatalf("read first batch item %d before stream completion: %v", i, itemErr)
		}
	}
}

func testProductionMiddlewareFlushAndDeadline(t *testing.T) {
	t.Helper()
	var logBuf bytes.Buffer
	handshake, wrappedRules := newProductionFlushHandshake(newProductionRulesHandlers(&logBuf))
	req := newProductionRulesRequest(t, &logBuf, wrappedRules)
	t.Cleanup(handshake.releaseThirdBatch)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, doErr := client.Do(req)
	if doErr != nil {
		t.Fatalf("GET /rules: %v", doErr)
	}
	defer func() { _ = resp.Body.Close() }()
	handshake.assertDeadlineSupported(t)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}

	var body bytes.Buffer
	decodeFirstRuleBatch(t, resp.Body, &body)
	handshake.releaseThirdBatch()
	if _, copyErr := io.Copy(&body, resp.Body); copyErr != nil {
		t.Fatalf("read remaining response body: %v", copyErr)
	}
	list, decErr := decodeRuleListJSON(body.Bytes())
	if decErr != nil || len(list) != 5 {
		t.Fatalf("decoded len = %d err = %v; body: %s", len(list), decErr, body.String())
	}
}

func buildMidStreamAbortHandler(h *Handlers) http.HandlerFunc {
	boom := errors.New("second batch query failed")
	return h.serveLibraryProcessingRulesWith(2, func(_ context.Context, _ int, onBatch func([]libraryprocessing.StoredRule) error) error {
		rules := sampleStoredRules(4)
		if err := onBatch(rules[:2]); err != nil {
			return err
		}
		// Second onBatch flushes rules[:2] (committing 200 OK headers), then we fail inside tx.
		if err := onBatch(rules[2:]); err != nil {
			return err
		}
		return boom
	})
}

func testProductionMiddlewareMidStreamAbort(t *testing.T) {
	t.Helper()
	var logBuf bytes.Buffer
	req := newProductionRulesRequest(t, &logBuf, buildMidStreamAbortHandler(newProductionRulesHandlers(&logBuf)))

	client := &http.Client{Timeout: 5 * time.Second}
	resp, doErr := client.Do(req)
	if doErr != nil {
		t.Fatalf("GET /rules did not receive response headers: %v", doErr)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 before stream abort", resp.StatusCode)
	}
	body, readErr := io.ReadAll(resp.Body)
	if !errors.Is(readErr, io.ErrUnexpectedEOF) {
		t.Fatalf("read error = %v, want unexpected EOF after ErrAbortHandler; body: %s", readErr, body)
	}
	if strings.Contains(string(body), "about:blank") {
		t.Fatalf("Recover middleware wrote problem+json after committed headers: %s", body)
	}
	// Adding only the closing bracket must recover the flushed first batch.
	partial := string(body)
	list, decErr := decodeRuleListJSON(append(body, ']'))
	if decErr != nil || len(list) != 2 {
		t.Fatalf("partial response decoded len = %d err = %v, want two flushed rules; body: %s", len(list), decErr, partial)
	}
}

func testProductionMiddlewareUnsupportedWriter(t *testing.T) {
	t.Helper()
	var logBuf bytes.Buffer
	h := newProductionRulesHandlers(&logBuf)
	router := buildProductionMiddlewareStack(t, &logBuf, h.serveLibraryProcessingRulesWith(2, fakeBatchStreamer(sampleStoredRules(3))))

	rec := httptest.NewRecorder()
	plain := struct{ http.ResponseWriter }{rec}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, PathPrefix+"/library-processing/rules", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	router.ServeHTTP(plain, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	list, err := decodeRuleListJSON(rec.Body.Bytes())
	if err != nil || len(list) != 3 {
		t.Fatalf("decoded len = %d err = %v; body: %s", len(list), err, rec.Body.String())
	}
	if strings.Contains(logBuf.String(), "httpx: flush response") {
		t.Fatalf("middleware logged unexpected flush error for unsupported writer:\n%s", logBuf.String())
	}
}

func TestServeLibraryProcessingRulesProductionMiddlewareAndNativeHTTP(t *testing.T) {
	t.Parallel()

	t.Run("real net/http server writer preserves deadline and flush support through middleware stack", func(t *testing.T) {
		t.Parallel()
		testProductionMiddlewareFlushAndDeadline(t)
	})

	t.Run("mid-stream abort re-panics ErrAbortHandler through Recover and closes native HTTP stream", func(t *testing.T) {
		t.Parallel()
		testProductionMiddlewareMidStreamAbort(t)
	})

	t.Run("unsupported writer wrapped by Recover and Logger degrades cleanly without false flush errors", func(t *testing.T) {
		t.Parallel()
		testProductionMiddlewareUnsupportedWriter(t)
	})
}
