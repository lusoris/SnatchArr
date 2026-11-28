// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"bytes"
	"context"
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

		var controllerCheckErr error
		rawStream := h.serveLibraryProcessingRulesWith(2, fakeBatchStreamer(sampleStoredRules(5)))
		wrappedRules := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify httpsnoop.Wrap in Recover + Logger preserves ResponseController SetWriteDeadline & Flush.
			rc := http.NewResponseController(w)
			if err := rc.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
				controllerCheckErr = err
			} else if err := rc.SetWriteDeadline(time.Time{}); err != nil {
				controllerCheckErr = err
			}
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
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			t.Fatalf("read response body: %v", readErr)
		}
		if controllerCheckErr != nil {
			t.Fatalf("production ResponseWriter failed SetWriteDeadline through middleware: %v", controllerCheckErr)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", resp.StatusCode, string(body))
		}
		if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
		}
		list, decErr := decodeRuleListJSON(body)
		if decErr != nil || len(list) != 5 {
			t.Fatalf("decoded len = %d err = %v; body: %s", len(list), decErr, string(body))
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
		if doErr == nil {
			defer func() { _ = resp.Body.Close() }()
			body, readErr := io.ReadAll(resp.Body)
			if readErr == nil {
				t.Fatalf("expected truncated chunked stream read error after ErrAbortHandler, got clean body: %s", string(body))
			}
			if strings.Contains(string(body), "about:blank") {
				t.Fatalf("Recover middleware wrote problem+json after committed headers: %s", string(body))
			}
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
