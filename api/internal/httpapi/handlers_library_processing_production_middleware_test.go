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

func TestServeLibraryProcessingRulesProductionMiddlewareAndNativeHTTP(t *testing.T) {
	t.Parallel()

	t.Run("real net/http server writer preserves deadline and flush support through middleware stack", func(t *testing.T) {
		t.Parallel()
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logBuf, nil))
		chk := &deadlineObservingChecker{enabled: true}
		h := &Handlers{
			libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, chk),
			logger:            logger,
		}

		controllerCheck := make(chan error, 1)
		firstBatchRead := make(chan struct{})
		streamBatches := fakeBatchStreamer(sampleStoredRules(5))
		rawStream := h.serveLibraryProcessingRulesWith(2, func(ctx context.Context, batchSize int, onBatch func([]libraryprocessing.StoredRule) error) error {
			batches := 0
			return streamBatches(ctx, batchSize, func(batch []libraryprocessing.StoredRule) error {
				batches++
				if batches == 3 {
					// The second callback flushes the first batch. Wait here so a
					// flush at handler return cannot satisfy the client assertion.
					timer := time.NewTimer(2 * time.Second)
					defer timer.Stop()
					select {
					case <-firstBatchRead:
					case <-ctx.Done():
						return ctx.Err()
					case <-timer.C:
						return errors.New("client did not read the first batch before the stream continued")
					}
				}
				return onBatch(batch)
			})
		})
		wrappedRules := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rc := http.NewResponseController(w)
			err := rc.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err == nil {
				err = rc.SetWriteDeadline(time.Time{})
			}
			controllerCheck <- err
			rawStream(w, r)
		})

		router := buildProductionMiddlewareStack(t, &logBuf, wrappedRules)
		ts := httptest.NewServer(router)
		t.Cleanup(ts.Close)

		client := &http.Client{Timeout: 5 * time.Second}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, ts.URL+PathPrefix+"/library-processing/rules", nil)
		if err != nil {
			t.Fatalf("NewRequestWithContext: %v", err)
		}
		req.Header.Set("Authorization", "Bearer test-token")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET /rules: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		select {
		case err := <-controllerCheck:
			if err != nil {
				t.Fatalf("production ResponseWriter failed SetWriteDeadline through middleware: %v", err)
			}
		default:
			t.Fatal("rules handler did not publish the deadline probe result")
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
		}

		var body bytes.Buffer
		// Retain bytes read ahead by the decoder for the final full-array check.
		dec := json.NewDecoder(io.TeeReader(resp.Body, &body))
		if tok, err := dec.Token(); err != nil || tok != json.Delim('[') {
			t.Fatalf("read opening array before stream completion: token=%v err=%v", tok, err)
		}
		for i := range 2 {
			var rule oas.LibraryProcessingRule
			if err := dec.Decode(&rule); err != nil {
				t.Fatalf("read first batch item %d before stream completion: %v", i, err)
			}
		}
		close(firstBatchRead)
		if _, err := io.Copy(&body, resp.Body); err != nil {
			t.Fatalf("read remaining response body: %v", err)
		}
		list, decErr := decodeRuleListJSON(body.Bytes())
		if decErr != nil || len(list) != 5 {
			t.Fatalf("decoded len = %d err = %v; body: %s", len(list), decErr, body.String())
		}
	})

	t.Run("mid-stream abort re-panics ErrAbortHandler through Recover and closes native HTTP stream", func(t *testing.T) {
		t.Parallel()
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logBuf, nil))
		chk := &deadlineObservingChecker{enabled: true}
		h := &Handlers{
			libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, chk),
			logger:            logger,
		}

		boom := errors.New("second batch query failed")
		failingStream := h.serveLibraryProcessingRulesWith(2, func(_ context.Context, _ int, onBatch func([]libraryprocessing.StoredRule) error) error {
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

		router := buildProductionMiddlewareStack(t, &logBuf, failingStream)
		ts := httptest.NewServer(router)
		t.Cleanup(ts.Close)

		client := &http.Client{Timeout: 5 * time.Second}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, ts.URL+PathPrefix+"/library-processing/rules", nil)
		if err != nil {
			t.Fatalf("NewRequestWithContext: %v", err)
		}
		req.Header.Set("Authorization", "Bearer test-token")

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
	})

	t.Run("unsupported writer wrapped by Recover and Logger degrades cleanly without false flush errors", func(t *testing.T) {
		t.Parallel()
		var logBuf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&logBuf, nil))
		chk := &deadlineObservingChecker{enabled: true}
		h := &Handlers{
			libraryProcessing: libraryprocessing.NewServiceWithChecker(nil, nil, nil, chk),
			logger:            logger,
		}
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
	})
}
