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
	if n, want := s.total.Load(), int64(3); n != want {
		t.Fatalf("requests = %d, want %d (one lookup)", n, want)
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

func TestWaitingLookupHonoursDeadline(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name           string
		callerDeadline bool
		lookup         time.Duration
	}{
		{name: "caller deadline", callerDeadline: true},
		{name: "provider lookup bound", lookup: time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newStub(t)
			s.started = make(chan struct{}, 1)
			s.canceled = make(chan struct{}, 1)
			release := make(chan struct{})
			s.release = release
			t.Cleanup(func() { close(release) })
			p := s.provider(t, plantedKey, plantedPIN, nil)
			if tc.lookup > 0 {
				p.lookup = tc.lookup
			}
			lookupCtx := t.Context()
			cancel := func() {}
			if tc.callerDeadline {
				lookupCtx, cancel = context.WithTimeout(t.Context(), time.Second)
			}
			defer cancel()
			result := make(chan error, 1)
			go func() { _, err := p.TitlesFor(lookupCtx, sonarrRef(), "de"); result <- err }()

			outer, cancelOuter := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancelOuter()
			select {
			case <-s.started:
			case <-outer.Done():
				t.Fatalf("upstream request did not start: %v", outer.Err())
			}
			select {
			case err := <-result:
				if !errors.Is(err, ErrUnavailable) {
					t.Fatalf("TitlesFor() error = %v, want %v", err, ErrUnavailable)
				}
				if tc.callerDeadline && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("TitlesFor() error = %v, want context.DeadlineExceeded", err)
				}
			case <-outer.Done():
				t.Fatalf("TitlesFor() did not stop waiting for upstream: %v", outer.Err())
			}
			if !tc.callerDeadline {
				select {
				case <-s.canceled:
				case <-time.After(2 * time.Second):
					t.Fatal("upstream request did not observe provider lookup bound")
				}
			}
		})
	}
}
