// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package useragent

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/buildinfo"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

type reader struct {
	mu    sync.Mutex
	value string
	calls int
	err   error
}

func (r *reader) Get(ctx context.Context) (domain.Settings, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if err := ctx.Err(); err != nil {
		return domain.Settings{}, err
	}
	return domain.Settings{UserAgent: r.value}, r.err
}

func (r *reader) set(value string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.value = value
	r.err = err
}

func (r *reader) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

type blockingReader struct {
	calls   atomic.Int32
	entered chan struct{}
	release chan struct{}
	first   string
	later   string
}

func (r *blockingReader) Get(ctx context.Context) (domain.Settings, error) {
	n := r.calls.Add(1)
	if n == 1 {
		r.entered <- struct{}{}
		select {
		case <-r.release:
		case <-ctx.Done():
			return domain.Settings{}, ctx.Err()
		}
		return domain.Settings{UserAgent: r.first}, nil
	}
	return domain.Settings{UserAgent: r.later}, nil
}

func TestDefault(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ version, want string }{
		{"2.3.4", "SnatchArr/2.3.4 (+https://github.com/lusoris/SnatchArr)"},
		{"", "SnatchArr/dev (+https://github.com/lusoris/SnatchArr)"},
	} {
		t.Run(tc.version, func(t *testing.T) {
			t.Parallel()
			if got := Default(buildinfo.Info{Version: tc.version}); got != tc.want {
				t.Errorf("Default = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEffective(t *testing.T) {
	t.Parallel()
	info := buildinfo.Info{Version: "2.3.4"}
	for _, tc := range []struct{ name, value, want string }{
		{"trim", " custom/1 ", "custom/1"},
		{"empty", "", Default(info)},
		{"whitespace", "  ", Default(info)},
		{"boundary", strings.Repeat("a", 200), strings.Repeat("a", 200)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Effective(tc.value, info); got != tc.want {
				t.Errorf("Effective = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSourceCache(t *testing.T) {
	t.Parallel()
	clk := clock.NewFake()
	r := &reader{value: "first"}
	s := New(r, buildinfo.Info{Version: "2"}, clk, slog.New(slog.DiscardHandler))
	for _, tc := range []struct {
		name, value string
		advance     time.Duration
		invalidate  bool
		want        string
		calls       int
	}{
		{"first", "first", 0, false, "first", 1},
		{"cached", "second", 29 * time.Second, false, "first", 1},
		{"expired", "second", time.Second, false, "second", 2},
		{"invalidated", "third", 0, true, "third", 3},
	} {
		t.Log(tc.name)
		r.set(tc.value, nil)
		clk.Advance(tc.advance)
		if tc.invalidate {
			s.Invalidate()
		}
		if got := s.Current(t.Context()); got != tc.want || r.count() != tc.calls {
			t.Errorf("Current = %q, reads=%d; want %q, %d", got, r.count(), tc.want, tc.calls)
		}
	}
}

func TestSourceCancelledCallerDoesNotPoisonCache(t *testing.T) {
	t.Parallel()
	info := buildinfo.Info{Version: "2"}
	for _, tc := range []struct {
		name   string
		badCtx func(t *testing.T, now time.Time) (context.Context, context.CancelFunc)
	}{
		{
			name: "cancelled caller ctx",
			badCtx: func(t *testing.T, _ time.Time) (context.Context, context.CancelFunc) {
				t.Helper()
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				return ctx, cancel
			},
		},
		{
			name: "expired caller deadline",
			badCtx: func(t *testing.T, now time.Time) (context.Context, context.CancelFunc) {
				t.Helper()
				return context.WithDeadline(t.Context(), now.Add(-5*time.Second))
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			clk := clock.NewFake()
			r := &reader{value: "custom/override"}
			s := New(r, info, clk, slog.New(slog.DiscardHandler))

			deadCtx, cancel := tc.badCtx(t, clk.Now())
			defer cancel()
			if got := s.Current(deadCtx); got != Default(info) {
				t.Fatalf("Current(deadCtx) = %q, want fallback %q", got, Default(info))
			}
			if got := s.Current(t.Context()); got != "custom/override" {
				t.Fatalf("Current(liveCtx) = %q, want %q", got, "custom/override")
			}
			if r.count() != 2 {
				t.Errorf("reader calls = %d, want 2", r.count())
			}
		})
	}
}

func TestSourceReaderError(t *testing.T) {
	t.Parallel()
	info := buildinfo.Info{Version: "2"}
	var logs bytes.Buffer
	r := &reader{value: "custom/override", err: errors.New("read unavailable")}
	s := New(r, info, clock.NewFake(), slog.New(slog.NewTextHandler(&logs, nil)))
	if got := s.Current(t.Context()); got != Default(info) {
		t.Errorf("first Current = %q, want %q", got, Default(info))
	}
	if !strings.Contains(logs.String(), "useragent: read settings: read unavailable") {
		t.Errorf("missing wrapped error: %s", logs.String())
	}
	if got := s.Current(t.Context()); got != Default(info) {
		t.Errorf("second Current on error = %q, want %q", got, Default(info))
	}
	if r.count() != 2 {
		t.Errorf("reader calls after second error = %d, want 2", r.count())
	}
	r.set("custom/override", nil)
	if got := s.Current(t.Context()); got != "custom/override" {
		t.Errorf("Current after recovery = %q, want %q", got, "custom/override")
	}
}

func TestSourceConcurrentSingleflight(t *testing.T) {
	t.Parallel()
	const callers = 16
	r := &blockingReader{
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
		first:   "coalesced/override",
		later:   "unexpected-second-read",
	}
	s := New(r, buildinfo.Info{Version: "2"}, clock.NewFake(), slog.New(slog.DiscardHandler))

	var ready, done sync.WaitGroup
	ready.Add(callers)
	done.Add(callers)
	start := make(chan struct{})
	results := make([]string, callers)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for i := range callers {
		go func(idx int) {
			defer done.Done()
			ready.Done()
			<-start
			results[idx] = s.Current(ctx)
		}(i)
	}

	ready.Wait()
	close(start)
	select {
	case <-r.entered:
	case <-ctx.Done():
		close(r.release)
		done.Wait()
		t.Fatalf("timed out waiting for reader entry: %v", ctx.Err())
	}
	close(r.release)
	done.Wait()

	if calls := r.calls.Load(); calls != 1 {
		t.Errorf("reader calls = %d, want 1", calls)
	}
	for i, got := range results {
		if got != "coalesced/override" {
			t.Errorf("caller %d got %q, want %q", i, got, "coalesced/override")
		}
	}
}

func TestSourceInvalidateDuringInFlightRead(t *testing.T) {
	t.Parallel()
	r := &blockingReader{
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
		first:   "stale/override",
		later:   "fresh/override",
	}
	s := New(r, buildinfo.Info{Version: "2"}, clock.NewFake(), slog.New(slog.DiscardHandler))

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	var inFlightGot string
	go func() {
		defer wg.Done()
		inFlightGot = s.Current(ctx)
	}()

	select {
	case <-r.entered:
	case <-ctx.Done():
		close(r.release)
		wg.Wait()
		t.Fatalf("timed out waiting for in-flight read: %v", ctx.Err())
	}

	s.Invalidate()
	close(r.release)
	wg.Wait()

	if inFlightGot != "stale/override" {
		t.Errorf("in-flight Current = %q, want %q", inFlightGot, "stale/override")
	}
	if got := s.Current(ctx); got != "fresh/override" {
		t.Errorf("post-invalidate Current = %q, want %q", got, "fresh/override")
	}
	if calls := r.calls.Load(); calls != 2 {
		t.Errorf("reader calls = %d, want 2", calls)
	}
}
