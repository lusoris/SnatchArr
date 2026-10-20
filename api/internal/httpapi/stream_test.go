// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/realtime/sse"
)

func TestKeepAlive(t *testing.T) {
	t.Parallel()

	t.Run("positive prelude bytes arrive before any event", func(t *testing.T) {
		t.Parallel()
		runPreludePositive(t)
	})

	t.Run("positive heartbeat after fake clock advance", func(t *testing.T) {
		t.Parallel()
		runHeartbeatPositive(t)
	})

	t.Run("positive event between heartbeats intact", func(t *testing.T) {
		t.Parallel()
		runInterleavePositive(t)
	})

	t.Run("negative non-SSE response 401 problem+json passes byte-identical", func(t *testing.T) {
		t.Parallel()
		runNonSSENegative(t)
	})

	t.Run("boundary client disconnect stops goroutine", func(t *testing.T) {
		t.Parallel()
		runDisconnectBoundary(t)
	})

	t.Run("boundary heartbeat write error stops loop", func(t *testing.T) {
		t.Parallel()
		runWriteErrorBoundary(t)
	})

	t.Run("negative nil clock is a wiring error", func(t *testing.T) {
		t.Parallel()
		h, err := KeepAlive(http.NotFoundHandler(), nil, StreamKeepAliveInterval)
		if !errors.Is(err, errNoStreamClock) || h != nil {
			t.Fatalf("KeepAlive(nil clock) = %v, %v; want nil, errNoStreamClock", h, err)
		}
	})
}

func mustKeepAlive(t *testing.T, h http.Handler, clk clock.Clock) http.Handler {
	t.Helper()
	kh, err := KeepAlive(h, clk, StreamKeepAliveInterval)
	if err != nil {
		t.Fatalf("KeepAlive: %v", err)
	}
	return kh
}

func runPreludePositive(t *testing.T) {
	t.Helper()
	fakeClk := clock.NewFake()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", sseContentType)
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		<-r.Context().Done()
	})
	srv := httptest.NewServer(mustKeepAlive(t, h, fakeClk))
	t.Cleanup(srv.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, sseContentType) {
		t.Fatalf("Content-Type = %q, want prefix %q", got, sseContentType)
	}

	buf := make([]byte, len(streamPrelude))
	if _, err := io.ReadFull(resp.Body, buf); err != nil {
		t.Fatalf("ReadFull prelude: %v", err)
	}
	if string(buf) != streamPrelude {
		t.Fatalf("got prelude %q, want %q", string(buf), streamPrelude)
	}
}

func runHeartbeatPositive(t *testing.T) {
	t.Helper()
	fakeClk := clock.NewFake()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", sseContentType)
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		<-r.Context().Done()
	})
	srv := httptest.NewServer(mustKeepAlive(t, h, fakeClk))
	t.Cleanup(srv.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	buf := make([]byte, len(streamPrelude))
	if _, err := io.ReadFull(resp.Body, buf); err != nil {
		t.Fatalf("ReadFull prelude: %v", err)
	}

	if err := fakeClk.BlockUntilContext(t.Context(), 1); err != nil {
		t.Fatalf("BlockUntilContext: %v", err)
	}
	fakeClk.Advance(StreamKeepAliveInterval)

	hbBuf := make([]byte, len(streamHeartbeat))
	if _, err := io.ReadFull(resp.Body, hbBuf); err != nil {
		t.Fatalf("ReadFull heartbeat 1: %v", err)
	}
	if string(hbBuf) != streamHeartbeat {
		t.Fatalf("got heartbeat %q, want %q", string(hbBuf), streamHeartbeat)
	}

	fakeClk.Advance(StreamKeepAliveInterval)
	if _, err := io.ReadFull(resp.Body, hbBuf); err != nil {
		t.Fatalf("ReadFull heartbeat 2: %v", err)
	}
	if string(hbBuf) != streamHeartbeat {
		t.Fatalf("got heartbeat 2 %q, want %q", string(hbBuf), streamHeartbeat)
	}
}

func runInterleavePositive(t *testing.T) {
	t.Helper()
	fakeClk := clock.NewFake()
	eventCh := make(chan string)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", sseContentType)
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		for r.Context().Err() == nil {
			select {
			case <-r.Context().Done():
				return
			case ev := <-eventCh:
				if _, err := io.WriteString(w, ev); err != nil {
					return
				}
				if fl, ok := w.(http.Flusher); ok {
					fl.Flush()
				}
			}
		}
	})
	srv := httptest.NewServer(mustKeepAlive(t, h, fakeClk))
	t.Cleanup(srv.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	prelude := make([]byte, len(streamPrelude))
	if _, err := io.ReadFull(resp.Body, prelude); err != nil {
		t.Fatalf("ReadFull prelude: %v", err)
	}

	if err := fakeClk.BlockUntilContext(t.Context(), 1); err != nil {
		t.Fatalf("BlockUntilContext: %v", err)
	}
	fakeClk.Advance(StreamKeepAliveInterval)
	hb1 := make([]byte, len(streamHeartbeat))
	if _, err := io.ReadFull(resp.Body, hb1); err != nil {
		t.Fatalf("ReadFull hb1: %v", err)
	}

	const eventPayload = "event: run_queued\ndata: {\"ok\":true}\n\n"
	eventCh <- eventPayload

	evBuf := make([]byte, len(eventPayload))
	if _, err := io.ReadFull(resp.Body, evBuf); err != nil {
		t.Fatalf("ReadFull event: %v", err)
	}
	if string(evBuf) != eventPayload {
		t.Fatalf("event payload = %q, want %q", string(evBuf), eventPayload)
	}

	fakeClk.Advance(StreamKeepAliveInterval)
	hb2 := make([]byte, len(streamHeartbeat))
	if _, err := io.ReadFull(resp.Body, hb2); err != nil {
		t.Fatalf("ReadFull hb2: %v", err)
	}
	if string(hb2) != streamHeartbeat {
		t.Fatalf("got hb2 %q, want %q", string(hb2), streamHeartbeat)
	}
}

func runNonSSENegative(t *testing.T) {
	t.Helper()
	fakeClk := clock.NewFake()
	const problemJSON = `{"type":"about:blank","title":"Unauthorized","status":401}`
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, problemJSON)
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
	})
	srv := httptest.NewServer(mustKeepAlive(t, h, fakeClk))
	t.Cleanup(srv.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(body) != problemJSON {
		t.Fatalf("body = %q, want %q", string(body), problemJSON)
	}
}

func runDisconnectBoundary(t *testing.T) {
	t.Helper()

	fakeClk := clock.NewFake()
	var serverWG sync.WaitGroup
	serverWG.Add(1)

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", sseContentType)
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		<-r.Context().Done()
	})
	wrapped := mustKeepAlive(t, h, fakeClk)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer serverWG.Done()
		wrapped.ServeHTTP(w, r)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(t.Context())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}

	buf := make([]byte, len(streamPrelude))
	if _, err := io.ReadFull(resp.Body, buf); err != nil {
		t.Fatalf("ReadFull prelude: %v", err)
	}

	cancel()
	_ = resp.Body.Close()

	done := make(chan struct{})
	go func() {
		serverWG.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("heartbeat goroutine or handler leaked on client disconnect")
	}

	srv.CloseClientConnections()
}

func runWriteErrorBoundary(t *testing.T) {
	t.Helper()
	fakeClk := clock.NewFake()
	errSink := errors.New("simulated network failure")
	fw := &failingWriter{
		header:      make(http.Header),
		failAfter:   1, // Allow prelude write, then fail heartbeat
		errToReturn: errSink,
	}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", sseContentType)
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		<-r.Context().Done()
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}

	srvDone := make(chan struct{})
	kh := mustKeepAlive(t, h, fakeClk)
	go func() {
		defer close(srvDone)
		kh.ServeHTTP(fw, req)
	}()

	if err := fakeClk.BlockUntilContext(t.Context(), 1); err != nil {
		t.Fatalf("BlockUntilContext: %v", err)
	}
	fakeClk.Advance(StreamKeepAliveInterval)

	// Since heartbeat write fails, heartbeat loop terminates immediately.
	// Cancel req context to let next.ServeHTTP exit and verify clean shutdown.
	cancel()
	select {
	case <-srvDone:
	case <-time.After(3 * time.Second):
		t.Fatal("ServeHTTP did not return after heartbeat write error")
	}
}

type failingWriter struct {
	header      http.Header
	writes      int
	failAfter   int
	errToReturn error
}

func (f *failingWriter) Header() http.Header { return f.header }

func (f *failingWriter) Write(p []byte) (int, error) {
	f.writes++
	if f.writes > f.failAfter {
		return 0, f.errToReturn
	}
	return len(p), nil
}

func (f *failingWriter) WriteHeader(_ int) {}

func (f *failingWriter) Flush() {}

func TestHeaderHoldingHop(t *testing.T) {
	t.Parallel()

	t.Run("positive client gets headers within 2s through KeepAlive", func(t *testing.T) {
		t.Parallel()
		runHopKeepAlivePositive(t)
	})

	t.Run("boundary planted bare hub handler gets no headers in 2s", func(t *testing.T) {
		t.Parallel()
		runHopBareHubPlantedBoundary(t)
	})
}

func runHopKeepAlivePositive(t *testing.T) {
	t.Helper()
	fakeClk := clock.NewFake()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", sseContentType)
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		<-r.Context().Done()
	})
	upstream := httptest.NewServer(mustKeepAlive(t, h, fakeClk))
	t.Cleanup(upstream.Close)

	proxy := newHeaderHoldingProxy(t, upstream.URL)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxy.URL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("client.Do through proxy failed: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, sseContentType) {
		t.Fatalf("Content-Type = %q, want prefix %q", got, sseContentType)
	}
}

// Planted defect proof: bare SSE hub handler sends response headers and flushes,
// but produces zero body bytes until an event is published. A header-holding hop
// (such as Node http-proxy without flushHeaders) buffers headers waiting for the first
// body byte, so the downstream client receives no response headers within 2s.
func runHopBareHubPlantedBoundary(t *testing.T) {
	t.Helper()
	hub := sse.NewHub(nil)
	upstream := httptest.NewServer(hub.Handler())
	t.Cleanup(upstream.Close)

	proxy := newHeaderHoldingProxy(t, upstream.URL)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, proxy.URL, nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatalf("bare hub handler through proxy unexpectedly received headers within 2s (status %d)", resp.StatusCode)
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "deadline exceeded") && !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected timeout/deadline exceeded, got: %v", err)
	}
}

type headerHoldingHandler struct {
	upstreamURL string
}

func newHeaderHoldingProxy(t *testing.T, upstreamURL string) *httptest.Server {
	t.Helper()
	proxy := &headerHoldingHandler{upstreamURL: upstreamURL}
	srv := httptest.NewServer(proxy)
	t.Cleanup(srv.Close)
	return srv
}

func (h *headerHoldingHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	outReq, err := http.NewRequestWithContext(req.Context(), req.Method, h.upstreamURL+req.URL.Path, req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for k, vv := range req.Header {
		for _, v := range vv {
			outReq.Header.Add(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(outReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	buf := make([]byte, 1024)
	n, readErr := resp.Body.Read(buf)
	if n > 0 {
		for k, vv := range resp.Header {
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(buf[:n])
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		_, _ = io.Copy(w, resp.Body)
	}
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return
	}
}
