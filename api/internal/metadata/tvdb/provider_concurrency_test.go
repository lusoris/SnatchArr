// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package tvdbprovider

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestConcurrentCallsShareOneLookup(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	s.delay = 20 * time.Millisecond
	p := s.provider(t, plantedKey, plantedPIN, nil)
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for range 10 {
		wg.Go(func() {
			got, err := p.TitlesFor(context.Background(), sonarrRef(), "de")
			if err == nil && len(got) != 2 {
				err = fmt.Errorf("got %d titles", len(got))
			}
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n, want := s.logins.Load(), int64(1); n != want {
		t.Fatalf("logins = %d, want %d", n, want)
	}
	if n, want := s.total.Load(), int64(3); n != want { // login, series, translation
		t.Fatalf("requests = %d, want %d (one lookup)", n, want)
	}
}

func startSeriesLookup(ctx context.Context, p *Provider) <-chan error {
	done := make(chan error, 1)
	go func() {
		got, err := p.TitlesFor(ctx, sonarrRef(), "de")
		if err == nil && len(got) != 2 {
			err = fmt.Errorf("lookup got %d titles, want 2", len(got))
		}
		done <- err
	}()
	return done
}

func receiveLookup(t *testing.T, ctx context.Context, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		t.Fatal("lookup did not finish within the test bound")
		return ctx.Err()
	}
}

func TestEarlyWaiterLeavesSharedLookupRunning(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	s.started, s.release = make(chan struct{}, 1), release
	p := s.provider(t, plantedKey, plantedPIN, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	early, leave := context.WithCancel(ctx)
	defer leave()
	first := startSeriesLookup(early, p)
	select {
	case <-s.started:
	case <-ctx.Done():
		t.Fatal("first lookup did not reach the stub")
	}
	second := startSeriesLookup(ctx, p)
	leave()
	if err := receiveLookup(t, ctx, first); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("early waiter = %v, want ErrUnavailable", err)
	}
	unblock()
	if err := receiveLookup(t, ctx, second); err != nil {
		t.Fatalf("second waiter = %v, want both titles", err)
	}
	if n := s.total.Load(); n != 3 {
		t.Fatalf("requests = %d, want 3 (one shared lookup)", n)
	}
}

func TestEarlyWaiterLeavesSharedLookupCached(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	s.started, s.release = make(chan struct{}, 1), release
	p := s.provider(t, plantedKey, plantedPIN, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	early, leave := context.WithCancel(ctx)
	defer leave()
	first := startSeriesLookup(early, p)
	select {
	case <-s.started:
	case <-ctx.Done():
		t.Fatal("lookup did not reach the stub")
	}
	leave()
	if err := receiveLookup(t, ctx, first); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("sole waiter = %v, want ErrUnavailable", err)
	}
	unblock()
	// Join the detached flight, or read its cached result, without allowing a new load.
	got, err := p.cached(ctx, "series:81189:deu", func(context.Context) (entry, error) {
		return entry{}, errors.New("detached lookup did not fill the cache")
	})
	if err != nil || len(got.Titles) != 2 {
		t.Fatalf("detached result = %v, %v, want both titles", got, err)
	}
	if err := receiveLookup(t, ctx, startSeriesLookup(ctx, p)); err != nil {
		t.Fatal(err)
	}
	if n := s.total.Load(); n != 3 {
		t.Fatalf("requests after cache hit = %d, want 3", n)
	}
}

func TestEarlyWaiterLeavesSharedRefreshRunning(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	s.started, s.release = make(chan struct{}, 1), release
	p := s.provider(t, plantedKey, plantedPIN, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	const key = "series:81189:deu"
	stale := entry{NotFound: true, At: p.clk.Now().Add(-NegativeTTL)}
	if err := p.cache.Set(ctx, key, stale); err != nil {
		t.Fatal(err)
	}
	early, leave := context.WithCancel(ctx)
	defer leave()
	first := startSeriesLookup(early, p)
	select {
	case <-s.started:
	case <-ctx.Done():
		t.Fatal("refresh did not reach the stub")
	}
	second := make(chan error, 1)
	go func() {
		// Enter the refresh flight even though the first refresher has evicted the sentinel.
		got, err := p.refreshed(ctx, key, func(c context.Context) (entry, error) {
			return p.load(c, sonarrRef(), "deu", "de")
		})
		if err == nil && (got.NotFound || len(got.Titles) != 2) {
			err = fmt.Errorf("refresh got %+v, want both titles", got)
		}
		second <- err
	}()
	leave()
	if err := receiveLookup(t, ctx, first); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("early refresher = %v, want ErrUnavailable", err)
	}
	unblock()
	if err := receiveLookup(t, ctx, second); err != nil {
		t.Fatalf("second refresher = %v, want both titles", err)
	}
	if n := s.total.Load(); n != 3 {
		t.Fatalf("refresh requests = %d, want 3 (one shared refresh)", n)
	}
}

func TestConcurrentDistinctItemsRace(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	for id := 100; id < 108; id++ {
		s.series[id] = fmt.Sprintf("Serie %d", id)
	}
	p := s.provider(t, plantedKey, "", nil)
	var wg sync.WaitGroup
	for id := 100; id < 108; id++ {
		wg.Go(func() {
			if _, err := p.TitlesFor(context.Background(), Ref{Kind: domain.KindSonarr, TVDBID: id}, "de"); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if n := s.logins.Load(); n != 1 {
		t.Fatalf("logins = %d, want 1", n)
	}
}

// TestWaitingLookupHonoursDeadline checks a queued lookup returns ErrUnavailable at the
// caller's deadline or, without one, the provider's bound (HISS-02). It holds the slot
// through the zero-request assertion, then releases it and waits for the detached load.
func TestWaitingLookupHonoursDeadline(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		bound  time.Duration // provider lookup bound
		caller time.Duration // caller deadline; 0 = none
	}{
		{"caller deadline", lookupTimeout, 2 * time.Second},
		{"no caller deadline, provider bound", time.Second, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newStub(t)
			p := s.provider(t, plantedKey, "", nil)
			p.lookup = tc.bound
			ctx := t.Context()
			if tc.caller > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tc.caller)
				defer cancel()
			}
			p.slot <- struct{}{} // another lookup holds the slot
			finished := make(chan struct{})
			_, err := p.cached(ctx, "series:81189:deu", func(c context.Context) (entry, error) {
				defer close(finished)
				return p.load(c, sonarrRef(), "deu", "de")
			})
			if !errors.Is(err, ErrUnavailable) {
				t.Errorf("waiting lookup: error = %v, want ErrUnavailable", err)
			}
			if n := s.total.Load(); n != 0 {
				t.Errorf("waiting lookup sent %d requests, want 0", n)
			}
			<-p.slot
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("detached load did not finish after releasing the slot")
			}
		})
	}
}

func TestWaitingLookupSharedWorkHonoursBound(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	early, leave := context.WithCancel(ctx)
	defer leave()
	work := make(chan context.Context, 1)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	finished := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := waitEntry(early, 20*time.Millisecond, func(shared context.Context) (entry, error) {
			defer close(finished)
			work <- shared
			if _, ok := shared.Deadline(); !ok {
				return entry{}, errors.New("shared lookup has no deadline")
			}
			<-release
			<-shared.Done()
			return entry{}, fmt.Errorf("shared lookup: %w", shared.Err())
		})
		done <- err
	}()
	var shared context.Context
	select {
	case shared = <-work:
	case <-ctx.Done():
		t.Fatal("shared lookup did not start")
	}
	if _, ok := shared.Deadline(); !ok {
		t.Fatal("shared lookup has no deadline")
	}
	leave()
	if err := receiveLookup(t, ctx, done); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("early waiter = %v, want ErrUnavailable", err)
	}
	unblock()
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal("shared lookup did not stop at its bound")
	}
	if !errors.Is(shared.Err(), context.DeadlineExceeded) {
		t.Fatalf("shared context = %v, want DeadlineExceeded", shared.Err())
	}
}

// TestSameKeyWaitingLookupHonoursDeadline keeps the shared load blocked until the
// second caller returns, then releases it and waits for the first caller to finish.
func TestSameKeyWaitingLookupHonoursDeadline(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	release := make(chan struct{})
	s.started, s.release = make(chan struct{}, 1), release
	p := s.provider(t, plantedKey, "", nil)
	p.lookup = 10 * time.Second
	first := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		got, err := p.TitlesFor(t.Context(), sonarrRef(), "de")
		if err == nil && len(got) != 2 {
			err = fmt.Errorf("first lookup got %d titles, want 2", len(got))
		}
		first <- err
	}()
	defer func() {
		close(release)
		if err := <-first; err != nil {
			t.Errorf("first lookup: %v", err)
		}
	}()
	select {
	case <-s.started:
	case <-time.After(p.lookup):
		t.Fatal("first lookup did not reach the stub")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	started := time.Now()
	_, err := p.TitlesFor(ctx, sonarrRef(), "de")
	elapsed := time.Since(started)
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("same-key waiting lookup: error = %v, want ErrUnavailable", err)
	}
	if elapsed >= p.lookup/2 {
		t.Errorf("same-key waiting lookup took %v, want less than %v", elapsed, p.lookup/2)
	}
	select {
	case <-finished:
		t.Error("first lookup finished before the waiting caller returned")
	default:
	}
	if n := s.total.Load(); n != 2 {
		t.Errorf("requests = %d, want 2 (first lookup still blocked before translation)", n)
	}
}
