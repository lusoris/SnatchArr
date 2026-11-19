// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package useragent resolves the configured outbound identity and its versioned default.
package useragent

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/fx"

	"github.com/golusoris/golusoris/cache/singleflight"
	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/buildinfo"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

const (
	cacheTTL    = 30 * time.Second
	readTimeout = 5 * time.Second
)

// Default returns the built-in identity for this build.
func Default(info buildinfo.Info) string {
	version := info.Version
	if version == "" {
		version = "dev"
	}
	return buildinfo.Name + "/" + version + " (+https://github.com/lusoris/SnatchArr)"
}

// Effective uses a trimmed override, falling back to the build identity.
func Effective(setting string, info buildinfo.Info) string {
	return effective(setting, Default(info))
}

func effective(setting, fallback string) string {
	if value := strings.TrimSpace(setting); value != "" {
		return value
	}
	return fallback
}

// Reader is the settings read needed by Source; it avoids a settings import cycle.
type Reader interface {
	Get(context.Context) (domain.Settings, error)
}

// Provider supplies an effective identity at the point of use.
type Provider interface{ Current(context.Context) string }

// Source caches one settings read for 30 seconds and coalesces concurrent reloads.
type Source struct {
	mu       sync.Mutex
	reader   Reader
	clk      clock.Clock
	logger   *slog.Logger
	flight   *singleflight.Group[struct{}, string]
	fallback string
	value    string
	expires  time.Time
	gen      uint64
}

// New builds a source, computing the fallback once for the running build.
func New(reader Reader, info buildinfo.Info, clk clock.Clock, logger *slog.Logger) *Source {
	return &Source{
		reader:   reader,
		clk:      clk,
		logger:   logger,
		flight:   singleflight.New[struct{}, string](),
		fallback: Default(info),
	}
}

// Current returns the cached identity, logging read failures and using the default.
func (s *Source) Current(ctx context.Context) string {
	if val, ok := s.cached(); ok {
		return val
	}
	if ctx.Err() != nil {
		return s.fallback
	}
	type result struct {
		value string
		err   error
	}
	resultCh := make(chan result, 1)
	// One waiter per caller; the shared read timeout bounds each waiter goroutine.
	go func() {
		val, _, err := s.flight.Do(ctx, struct{}{}, s.load)
		resultCh <- result{value: val, err: err}
	}()
	select {
	case res := <-resultCh:
		if res.err != nil {
			return s.fallback
		}
		return res.value
	case <-ctx.Done():
		return s.fallback
	}
}

func (s *Source) cached() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.clk.Now().Before(s.expires) {
		return s.value, true
	}
	return "", false
}

func (s *Source) load(ctx context.Context) (string, error) {
	s.mu.Lock()
	if s.clk.Now().Before(s.expires) {
		val := s.value
		s.mu.Unlock()
		return val, nil
	}
	gen := s.gen
	s.mu.Unlock()

	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), readTimeout)
	defer cancel()
	cfg, err := s.reader.Get(readCtx)
	if err != nil {
		wrapped := fmt.Errorf("useragent: read settings: %w", err)
		s.logger.ErrorContext(readCtx, "useragent: settings read failed", slog.Any("error", wrapped))
		return "", wrapped
	}
	resolved := effective(cfg.UserAgent, s.fallback)
	s.mu.Lock()
	if s.gen == gen {
		s.value = resolved
		s.expires = s.clk.Now().Add(cacheTTL)
	}
	s.mu.Unlock()
	return resolved, nil
}

// Invalidate forces the next request to reload after a successful settings write.
func (s *Source) Invalidate() {
	s.mu.Lock()
	s.gen++
	s.expires = time.Time{}
	s.mu.Unlock()
	s.flight.Forget(struct{}{})
}

// Transport applies the current identity without mutating the caller's request.
type Transport struct {
	Base   http.RoundTripper
	Source Provider
}

// RoundTrip resolves the header immediately before dispatch.
func (t Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	copyReq := req.Clone(req.Context())
	copyReq.Header.Set("User-Agent", t.Source.Current(req.Context()))
	resp, err := t.Base.RoundTrip(copyReq)
	if err != nil {
		return nil, fmt.Errorf("useragent: transport: %w", err)
	}
	return resp, nil
}

// HTTPClient preserves the standard HTTP policy while optionally resolving the identity.
func HTTPClient(timeout time.Duration, source Provider) *http.Client {
	client := &http.Client{Timeout: timeout}
	if source != nil {
		client.Transport = Transport{Base: http.DefaultTransport, Source: source}
	}
	return client
}

// Module provides the shared Source and its Provider interface; settings provides its Reader.
var Module = fx.Module("snatcharr.useragent",
	fx.Provide(New, func(s *Source) Provider { return s }),
)
