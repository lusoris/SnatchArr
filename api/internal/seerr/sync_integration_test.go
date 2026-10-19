// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package seerr

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/events"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/metrics/metricstest"
	"github.com/lusoris/SnatchArr/api/internal/seerrclient"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
	"github.com/lusoris/SnatchArr/api/internal/testkit/await"
)

const (
	// clientTimeout bounds every request to the fake Seerr (HISS-02).
	clientTimeout = 5 * time.Second
	// askedBuffer is more request lists than the ticker test fetches, so the fake never blocks.
	askedBuffer = 16
	// stopBound bounds the ticker's stop (HISS-02).
	stopBound = 5 * time.Second
)

// noResolver finds no library entity; the fake Seerr lists no request, so nothing asks it.
type noResolver struct{}

func (noResolver) EntityByExternalID(context.Context, domain.AppKind, string, string, int, int) (int64, bool, error) {
	return 0, false, nil
}

// fakeSeerr is a Seerr that knows no server and lists no request. It counts the fetches of
// the request list, which is the call one sync of a link cannot skip.
type fakeSeerr struct {
	url   string
	lists atomic.Int32
	asked chan struct{}
}

// newFakeSeerr starts the fake. With fails set, the request list answers 500.
func newFakeSeerr(t *testing.T, fails bool) *fakeSeerr {
	t.Helper()
	f := &fakeSeerr{asked: make(chan struct{}, askedBuffer)}
	none := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/settings/sonarr", none)
	mux.HandleFunc("GET /api/v1/settings/radarr", none)
	mux.HandleFunc("GET /api/v1/request", func(w http.ResponseWriter, _ *http.Request) {
		f.lists.Add(1)
		select {
		case f.asked <- struct{}{}:
		default:
		}
		if fails {
			http.Error(w, "the test fails this link", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pageInfo":{"pages":1,"results":0},"results":[]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	f.url = srv.URL
	return f
}

type syncFixture struct {
	svc *Service
	st  *store.Store
	clk *clockwork.FakeClock
}

func newSyncFixture(t *testing.T, logger *slog.Logger) syncFixture {
	t.Helper()
	st := storetest.New(t)
	clk := clock.NewFake()
	enc, err := crypto.NewEncryptor(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("encryptor: %v", err)
	}
	ids := id.New()
	inst := instances.New(st, enc, clk, ids, nil, logger)
	rec := snatch.NewRecorder(st, clk, events.New(logger))
	svc := New(st, enc, clk, ids, seerrclient.New(clientTimeout, "SnatchArr-test"), noResolver{}, inst, snatch.NewRuns(st, clk, ids), rec, logger)
	return syncFixture{svc: svc, st: st, clk: clk}
}

// linkState is what one SyncAll leaves behind for a link.
type linkState int

const (
	untouched linkState = iota // never asked, nothing recorded
	synced                     // asked, sync recorded without an error
	failed                     // asked, the error recorded
)

// testLink is one link of a case and the state SyncAll must leave it in.
type testLink struct {
	name     string
	disabled bool
	fails    bool
	want     linkState
}

// create stores the link and starts the fake Seerr it points at.
func (l testLink) create(t *testing.T, f syncFixture) (domain.SeerrLink, *fakeSeerr) {
	t.Helper()
	fake := newFakeSeerr(t, l.fails)
	link, err := f.svc.Create(t.Context(), Input{Name: l.name, BaseURL: fake.url, APIKey: "a-key-for-the-fake", Enabled: !l.disabled})
	if err != nil {
		t.Fatalf("create link %s: %v", l.name, err)
	}
	return link, fake
}

// check compares what the store and the fake Seerr saw with the wanted state.
func (l testLink) check(t *testing.T, f syncFixture, link domain.SeerrLink, fake *fakeSeerr) {
	t.Helper()
	row, err := f.st.Q().GetSeerrLink(t.Context(), link.ID)
	if err != nil {
		t.Fatalf("get link %s: %v", l.name, err)
	}
	got := untouched
	switch {
	case row.LastSyncAt != nil && row.LastError != nil:
		got = failed
	case row.LastSyncAt != nil:
		got = synced
	}
	if got != l.want {
		t.Errorf("link %s: state %d (last sync %v, last error %v), want %d", l.name, got, row.LastSyncAt, row.LastError, l.want)
	}
	if asked := fake.lists.Load() > 0; asked != (l.want != untouched) {
		t.Errorf("link %s: request list fetched %d times, want fetched = %v", l.name, fake.lists.Load(), l.want != untouched)
	}
}

// warnings returns the lines the service logged at warn level.
func warnings(logged string) []string {
	var out []string
	for line := range strings.Lines(logged) {
		if strings.Contains(line, "level=WARN") {
			out = append(out, line)
		}
	}
	return out
}

// assertWarnings checks that the service logged one warning per wanted fragment, in order.
func assertWarnings(t *testing.T, logged string, want []string) {
	t.Helper()
	got := warnings(logged)
	if len(got) != len(want) {
		t.Fatalf("%d warnings logged, want %d:\n%s", len(got), len(want), logged)
	}
	for i, fragment := range want {
		if !strings.Contains(got[i], fragment) {
			t.Errorf("warning %d = %q, want it to contain %q", i, got[i], fragment)
		}
	}
}

type syncAllCase struct {
	name      string
	links     []testLink
	cancelled bool
	wantWarn  []string
}

func (tc syncAllCase) run(t *testing.T) {
	t.Helper()
	var logged bytes.Buffer
	f := newSyncFixture(t, slog.New(slog.NewTextHandler(&logged, nil)))
	links := make([]domain.SeerrLink, len(tc.links))
	fakes := make([]*fakeSeerr, len(tc.links))
	for i, l := range tc.links {
		links[i], fakes[i] = l.create(t, f)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if tc.cancelled {
		cancel()
	}
	err := f.svc.SyncAll(ctx)
	// Only a failed listing fails the pass; a failing link records its own last_error.
	if tc.cancelled != (err != nil) || (err != nil && (!errors.Is(err, context.Canceled) || !strings.HasPrefix(err.Error(), "seerr: list links: "))) {
		t.Errorf("SyncAll() = %v, want an error with the prefix \"seerr: list links: \" only when the store fails", err)
	}
	for i, l := range tc.links {
		l.check(t, f, links[i], fakes[i])
	}
	assertWarnings(t, logged.String(), tc.wantWarn)
}

func TestService_SyncAll(t *testing.T) {
	t.Parallel()
	cases := []syncAllCase{
		// Positive.
		{name: "every enabled link is synced", links: []testLink{{name: "a", want: synced}, {name: "b", want: synced}}},
		// Negative. Links are synced in the order of their names, so the failing one sits
		// between two healthy ones.
		{
			name:     "one link fails, the others are still synced",
			links:    []testLink{{name: "a", want: synced}, {name: "b", fails: true, want: failed}, {name: "c", want: synced}},
			wantWarn: []string{`msg="seerr: sync" link=b `},
		},
		{name: "a disabled link is left alone", links: []testLink{{name: "a", disabled: true, want: untouched}, {name: "b", want: synced}}},
		{
			name:      "the store fails",
			links:     []testLink{{name: "a", want: untouched}},
			cancelled: true,
			wantWarn:  []string{`msg="seerr: list links"`},
		},
		// Boundary.
		{name: "no link"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// Positive: the ticker hands Service.SyncAll to the leader loop, which runs it at once and
// again after one SyncInterval.
func TestNewTicker_SyncsPerInterval(t *testing.T) {
	t.Parallel()
	f := newSyncFixture(t, slog.New(slog.DiscardHandler))
	_, fake := testLink{name: "a"}.create(t, f)
	startTicker(t, f)

	await.Await(t, fake.asked, "the first sync")
	f.clk.Advance(SyncInterval)
	await.Await(t, fake.asked, "the sync after one interval")
}

// startTicker starts the sync's leader loop on fresh metrics and stops it when the test ends.
func startTicker(t *testing.T, f syncFixture) *prometheus.Registry {
	t.Helper()
	m, reg := metricstest.New(t)
	ticker := NewTicker(f.svc, f.clk, m)
	if err := ticker.Start(t.Context()); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), stopBound)
		defer cancel()
		if err := ticker.Stop(ctx); err != nil {
			t.Errorf("Stop() = %v", err)
		}
	})
	return reg
}

// Positive: a pass with a failing link still succeeds as a pass; the link shows its own error.
func TestNewTicker_RecordsTheLastSuccess(t *testing.T) {
	t.Parallel()
	f := newSyncFixture(t, slog.New(slog.DiscardHandler))
	testLink{name: "a", fails: true}.create(t, f)
	reg := startTicker(t, f)
	metricstest.Await(t, reg, "snatcharr_loop_last_success_timestamp_seconds", "loop", "seerr_sync",
		float64(f.clk.Now().UnixNano())/float64(time.Second))
	metricstest.Await(t, reg, "snatcharr_loop_failures_total", "loop", "seerr_sync", 0)
}

// Negative: a pass that cannot list the links counts as a failed tick (gap audit A1-10).
func TestNewTicker_CountsAFailedListing(t *testing.T) {
	t.Parallel()
	f := newSyncFixture(t, slog.New(slog.DiscardHandler))
	if _, err := f.st.Pool().Exec(t.Context(), `DROP TABLE seerr_links CASCADE`); err != nil {
		t.Fatalf("drop seerr_links: %v", err)
	}
	reg := startTicker(t, f)
	metricstest.Await(t, reg, "snatcharr_loop_failures_total", "loop", "seerr_sync", 1)
}
