// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golusoris/golusoris/core/clock"
)

// StreamKeepAliveInterval is the default cadence at which heartbeat comments are
// written to keep proxies and firewalls from dropping the SSE stream.
const StreamKeepAliveInterval = 15 * time.Second

const (
	streamPrelude   = "retry: 5000\n: connected\n\n"
	streamHeartbeat = ": keepalive\n\n"
	sseContentType  = "text/event-stream"
)

// KeepAlive wraps an SSE handler to emit an immediate stream prelude on first
// flush and periodic heartbeat comments thereafter, preventing proxies that
// buffer response headers until the first body byte from stalling the client.
// The clock is the injected golusoris clock; a nil clock is a wiring error.
func KeepAlive(next http.Handler, clk clock.Clock, every time.Duration, logger *slog.Logger) (http.Handler, error) {
	if clk == nil {
		return nil, errNoStreamClock
	}
	if every <= 0 {
		every = StreamKeepAliveInterval
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &keepAliveHandler{
		next:   next,
		clk:    clk,
		every:  every,
		logger: logger,
	}, nil
}

var errNoStreamClock = errors.New("httpapi: stream keep-alive needs a clock")

type keepAliveHandler struct {
	next   http.Handler
	clk    clock.Clock
	every  time.Duration
	logger *slog.Logger
}

func (k *keepAliveHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// The server's WriteTimeout would cut a long-lived stream; clear it for
	// this response only. A writer chain that cannot clear it is logged, since
	// the stream would then end at the server's WriteTimeout.
	if err := http.NewResponseController(w).SetWriteDeadline(time.Time{}); err != nil {
		k.logger.WarnContext(req.Context(), "httpapi: clear stream write deadline", slog.String("error", err.Error()))
	}
	kw := &keepAliveWriter{
		ResponseWriter: w,
	}
	ctx := req.Context()
	ticker := k.clk.NewTicker(k.every)
	defer ticker.Stop()

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		k.heartbeatLoop(ctx, done, ticker.Chan(), kw)
	})

	k.next.ServeHTTP(kw, req)
	close(done)
	wg.Wait()
}

// heartbeatLoop runs until the request ends, the handler returns or a write fails.
func (k *keepAliveHandler) heartbeatLoop(ctx context.Context, done <-chan struct{}, ticks <-chan time.Time, kw *keepAliveWriter) {
	for awaitBeat(ctx, done, ticks, kw) {
	}
}

// awaitBeat waits for one tick and reports whether the loop goes on.
func awaitBeat(ctx context.Context, done <-chan struct{}, ticks <-chan time.Time, kw *keepAliveWriter) bool {
	select {
	case <-ctx.Done():
		return false
	case <-done:
		return false
	case <-ticks:
		return kw.heartbeat()
	}
}

type keepAliveWriter struct {
	http.ResponseWriter
	mu          sync.Mutex
	preludeDone bool
	broken      bool
}

// Unwrap returns the underlying ResponseWriter for http.ResponseController.
func (w *keepAliveWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// Write ensures thread-safety with concurrent heartbeat writes.
func (w *keepAliveWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.ResponseWriter.Write(p)
	if err != nil {
		return n, fmt.Errorf("stream: write: %w", err)
	}
	return n, nil
}

// WriteHeader synchronizes header status writes with the mutex.
func (w *keepAliveWriter) WriteHeader(statusCode int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.ResponseWriter.WriteHeader(statusCode)
}

// Flush writes the prelude if this is an SSE response on first flush, then delegates to the underlying Flusher.
func (w *keepAliveWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.preludeDone {
		if strings.HasPrefix(w.Header().Get("Content-Type"), sseContentType) {
			if _, err := io.WriteString(w.ResponseWriter, streamPrelude); err == nil {
				w.preludeDone = true
			}
		}
	}
	if err := flushResponseWriter(w.ResponseWriter); err != nil {
		w.broken = true
	}
}

// heartbeat writes an SSE keepalive comment if the prelude has been sent.
// It returns false on write error to terminate the heartbeat loop.
func (w *keepAliveWriter) heartbeat() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.broken {
		return false
	}
	if !w.preludeDone {
		return true
	}
	if _, err := io.WriteString(w.ResponseWriter, streamHeartbeat); err != nil {
		return false
	}
	return flushResponseWriter(w.ResponseWriter) == nil
}

func flushResponseWriter(w http.ResponseWriter) error {
	if fl, ok := w.(http.Flusher); ok {
		fl.Flush()
		return nil
	}
	if err := http.NewResponseController(w).Flush(); err != nil {
		return fmt.Errorf("stream: flush: %w", err)
	}
	return nil
}
