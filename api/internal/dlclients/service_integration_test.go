// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients_test

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	arr "github.com/golusoris/goenvoy/arr/v2"
	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/dlclients"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

const maxConcurrentObserve = dlclients.MaxConcurrentObserve

func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

type stubDiscoverer struct{}

func (d *stubDiscoverer) DownloadClients(context.Context, domain.AppKind, string, string) ([]arr.ProviderResource, error) {
	return nil, nil
}

type trackingSnapshotter struct {
	inFlight    atomic.Int32
	maxInFlight atomic.Int32
	entered     chan struct{}
	release     chan struct{}
	clk         clock.Clock
	errMap      map[string]string
}

func newTrackingSnapshotter(clk clock.Clock, capacity int) *trackingSnapshotter {
	return &trackingSnapshotter{
		entered: make(chan struct{}, capacity),
		release: make(chan struct{}),
		clk:     clk,
		errMap:  make(map[string]string),
	}
}

func (s *trackingSnapshotter) Snapshot(ctx context.Context, c domain.DownloadClient, _ string) dlclients.Snapshot {
	curr := s.inFlight.Add(1)
	for range 1000 {
		prevMax := s.maxInFlight.Load()
		if curr <= prevMax || s.maxInFlight.CompareAndSwap(prevMax, curr) {
			break
		}
	}
	defer s.inFlight.Add(-1)

	select {
	case s.entered <- struct{}{}:
	default:
	}

	select {
	case <-s.release:
	case <-ctx.Done():
	}

	snap := dlclients.Snapshot{
		ClientID:  c.ID,
		Name:      c.Name,
		Kind:      c.Kind,
		CheckedAt: s.clk.Now(),
		Reachable: true,
	}
	if errMsg, ok := s.errMap[c.Name]; ok {
		snap.Reachable = false
		snap.Error = errMsg
	}
	return snap
}

type fixture struct {
	svc *dlclients.Service
	st  *store.Store
	clk *clockwork.FakeClock
	enc *crypto.Encryptor
}

func newFixture(t *testing.T, snap dlclients.Snapshotter) fixture {
	t.Helper()
	st := storetest.New(t)
	enc, err := crypto.NewEncryptor(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("newFixture: encryptor: %v", err)
	}
	clk := clock.NewFake()
	logger := slog.New(slog.DiscardHandler)
	svc := dlclients.New(st, enc, clk, id.New(), snap, &stubDiscoverer{}, nil, logger)
	return fixture{svc: svc, st: st, clk: clk, enc: enc}
}

func createClients(t *testing.T, ctx context.Context, svc *dlclients.Service, count int) []domain.DownloadClient {
	t.Helper()
	out := make([]domain.DownloadClient, count)
	for i := range count {
		c, err := svc.Create(ctx, dlclients.Input{
			Kind:      domain.ClientQBittorrent,
			Name:      fmt.Sprintf("client-%02d", i),
			BaseURL:   fmt.Sprintf("http://client-%02d.local:8080", i),
			Username:  "admin",
			Secret:    "adminpass",
			Enabled:   true,
			MaxActive: 10,
		})
		if err != nil {
			t.Fatalf("createClients %d: %v", i, err)
		}
		out[i] = c
	}
	return out
}

func TestService_Status_Concurrency(t *testing.T) {
	t.Parallel()
	const clientCount = 16
	snap := newTrackingSnapshotter(clock.NewFake(), clientCount)
	f := newFixture(t, snap)
	clients := createClients(t, t.Context(), f.svc, clientCount)

	type statusResult struct {
		snaps []dlclients.Snapshot
		err   error
	}
	resCh := make(chan statusResult, 1)
	go func() {
		snaps, err := f.svc.Status(t.Context())
		resCh <- statusResult{snaps: snaps, err: err}
	}()

	timeout := time.After(5 * time.Second)
	reached := false
	for attempts := 0; attempts < 1000 && !reached; attempts++ {
		if snap.inFlight.Load() >= maxConcurrentObserve {
			break
		}
		select {
		case <-snap.entered:
		case <-timeout:
			reached = true
		}
	}

	close(snap.release)
	res := <-resCh
	if res.err != nil {
		t.Fatalf("Status() error = %v", res.err)
	}
	if got := snap.maxInFlight.Load(); got != maxConcurrentObserve {
		t.Fatalf("max in-flight observations = %d, want %d", got, maxConcurrentObserve)
	}
	if len(res.snaps) != clientCount {
		t.Fatalf("len(snaps) = %d, want %d", len(res.snaps), clientCount)
	}
	for i, c := range clients {
		if res.snaps[i].ClientID != c.ID {
			t.Errorf("snap[%d].ClientID = %v, want %v", i, res.snaps[i].ClientID, c.ID)
		}
		if res.snaps[i].Name != c.Name {
			t.Errorf("snap[%d].Name = %q, want %q", i, res.snaps[i].Name, c.Name)
		}
	}
}

func TestService_Verdict_Concurrency(t *testing.T) {
	t.Parallel()
	const clientCount = 16
	snap := newTrackingSnapshotter(clock.NewFake(), clientCount)
	f := newFixture(t, snap)
	clients := createClients(t, t.Context(), f.svc, clientCount)

	type verdictResult struct {
		v   dlclients.Verdict
		err error
	}
	resCh := make(chan verdictResult, 1)
	instID := uuid.New()
	go func() {
		v, err := f.svc.Verdict(t.Context(), instID)
		resCh <- verdictResult{v: v, err: err}
	}()

	timeout := time.After(5 * time.Second)
	reached := false
	for attempts := 0; attempts < 1000 && !reached; attempts++ {
		if snap.inFlight.Load() >= maxConcurrentObserve {
			break
		}
		select {
		case <-snap.entered:
		case <-timeout:
			reached = true
		}
	}

	close(snap.release)
	res := <-resCh
	if res.err != nil {
		t.Fatalf("Verdict() error = %v", res.err)
	}
	if got := snap.maxInFlight.Load(); got != maxConcurrentObserve {
		t.Fatalf("max in-flight observations = %d, want %d", got, maxConcurrentObserve)
	}
	if !res.v.Allowed {
		t.Fatalf("Verdict().Allowed = false, want true; reason=%s", res.v.Reason)
	}
	_ = clients
}

func TestService_Status_Boundaries(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		clientCount int
		wantBound   int32
	}{
		{"empty (0 clients)", 0, 0},
		{"single client", 1, 1},
		{"exactly bound (8 clients)", maxConcurrentObserve, maxConcurrentObserve},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			snap := newTrackingSnapshotter(clock.NewFake(), tc.clientCount)
			close(snap.release) // do not block
			f := newFixture(t, snap)
			clients := createClients(t, t.Context(), f.svc, tc.clientCount)

			snaps, err := f.svc.Status(t.Context())
			if err != nil {
				t.Fatalf("Status() error = %v", err)
			}
			if snaps == nil {
				t.Fatal("Status() returned nil slice, want non-nil")
			}
			if len(snaps) != tc.clientCount {
				t.Fatalf("len(snaps) = %d, want %d", len(snaps), tc.clientCount)
			}
			for i, c := range clients {
				if snaps[i].ClientID != c.ID {
					t.Errorf("snap[%d].ClientID = %v, want %v", i, snaps[i].ClientID, c.ID)
				}
			}
		})
	}
}

func TestService_Status_Negative(t *testing.T) {
	t.Parallel()
	const clientCount = 4
	snap := newTrackingSnapshotter(clock.NewFake(), clientCount)
	snap.errMap["client-01"] = "connection refused: 127.0.0.1:8080"
	close(snap.release)

	f := newFixture(t, snap)
	clients := createClients(t, t.Context(), f.svc, clientCount)

	snaps, err := f.svc.Status(t.Context())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if len(snaps) != clientCount {
		t.Fatalf("len(snaps) = %d, want %d", len(snaps), clientCount)
	}

	for i, c := range clients {
		if snaps[i].ClientID != c.ID {
			t.Errorf("snap[%d].ClientID = %v, want %v", i, snaps[i].ClientID, c.ID)
		}
		if snaps[i].Name != c.Name {
			t.Errorf("snap[%d].Name = %q, want %q", i, snaps[i].Name, c.Name)
		}
	}

	// Verify failed client is reported as unreachable with error, others reachable
	if snaps[1].Reachable {
		t.Errorf("client-01 Reachable = true, want false")
	}
	if snaps[1].Error != "connection refused: 127.0.0.1:8080" {
		t.Errorf("client-01 Error = %q, want %q", snaps[1].Error, "connection refused: 127.0.0.1:8080")
	}
	for _, idx := range []int{0, 2, 3} {
		if !snaps[idx].Reachable {
			t.Errorf("client-%02d Reachable = false, want true (Error: %s)", idx, snaps[idx].Error)
		}
	}
}

func TestService_Status_Race(t *testing.T) {
	t.Parallel()
	const clientCount = 8
	snap := newTrackingSnapshotter(clock.NewFake(), clientCount)
	close(snap.release)

	f := newFixture(t, snap)
	_ = createClients(t, t.Context(), f.svc, clientCount)

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(iter int) {
			defer wg.Done()
			if iter%2 == 0 {
				_, err := f.svc.Status(t.Context())
				if err != nil {
					t.Errorf("concurrent Status error: %v", err)
				}
			} else {
				_, err := f.svc.Verdict(t.Context(), uuid.New())
				if err != nil {
					t.Errorf("concurrent Verdict error: %v", err)
				}
			}
		}(i)
	}
	wg.Wait()
}
