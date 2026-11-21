// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package tvdbprovider

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tvdb "github.com/golusoris/goenvoy/metadata/video/tvdb"
	"github.com/jonboulle/clockwork"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/rueidis"

	"github.com/golusoris/golusoris/cache/memory"
	rediscache "github.com/golusoris/golusoris/cache/redis"
	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/httpx/ratelimit"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/foreigntitle"
	"github.com/lusoris/SnatchArr/api/internal/metrics/metricstest"
)

const (
	plantedKey = "plantedplantedplanted"
	plantedPIN = "planted-pin-4c2d08"
)

// stub is a TVDB API double. Every route counts its hits.
type stub struct {
	t                      *testing.T
	srv                    *httptest.Server
	tlsConfig              *tls.Config
	total                  atomic.Int64
	logins                 atomic.Int64
	loginRC                int
	dataRC                 int // when non-zero every data route answers with this status
	series                 map[int]string
	noTr                   map[int]bool     // series ids whose deu translation answers 404
	aliases                map[int][]string // series id -> deu aliases
	movies                 map[string]int   // remote id -> movie id
	movieAli               map[int][]string
	delay                  time.Duration
	age                    int64
	agent                  atomic.Value  // last User-Agent header seen
	started                chan struct{} // signals a held series request
	release                <-chan struct{}
	canceled               chan struct{} // signals cancellation of the held series request
	otherStarted           chan struct{}
	otherRelease           <-chan struct{}
	otherCanceled          chan time.Time
	pathMu                 sync.Mutex
	pathHits               map[string]int
	requestTimes           [16]time.Time
	requestCount           int
	loginHandler           func(http.ResponseWriter, *http.Request) bool
	redirect               func(http.ResponseWriter, *http.Request) bool
	numericTranslationOnce atomic.Bool
	emptySeriesOnce        atomic.Bool
	staleWhileRevalidate   bool
	statusHeader           string
}

func newStub(t *testing.T) *stub {
	t.Helper()
	return newStubWithServer(t, httptest.NewServer)
}

func newTLSStub(t *testing.T) *stub {
	t.Helper()
	return newStubWithServer(t, httptest.NewTLSServer)
}

func newStubWithServer(t *testing.T, newServer func(http.Handler) *httptest.Server) *stub {
	t.Helper()
	s := &stub{
		t: t, loginRC: http.StatusOK,
		series:   map[int]string{81189: "Breaking Bad DE"},
		aliases:  map[int][]string{81189: {"Breaking Bad Alias"}},
		movies:   map[string]int{"tt0111161": 439},
		movieAli: map[int][]string{439: {"Die Verurteilten Alias"}},
	}
	s.srv = newServer(http.HandlerFunc(s.serve))
	if transport, ok := s.srv.Client().Transport.(*http.Transport); ok {
		s.tlsConfig = transport.TLSClientConfig
	}
	t.Cleanup(s.srv.Close)
	return s
}

func (s *stub) serve(w http.ResponseWriter, r *http.Request) {
	s.pathMu.Lock()
	if s.pathHits == nil {
		s.pathHits = make(map[string]int)
	}
	s.pathHits[r.URL.Path]++
	if s.requestCount < len(s.requestTimes) {
		s.requestTimes[s.requestCount] = time.Now()
		s.requestCount++
	}
	// Record the arrival before publishing its count to concurrent readers.
	s.total.Add(1)
	s.pathMu.Unlock()
	s.agent.Store(r.Header.Get("User-Agent"))
	if s.redirect != nil && s.redirect(w, r) {
		return
	}
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	if r.URL.Path == "/login" {
		s.login(w, r)
		return
	}
	if s.age > 0 {
		w.Header().Set("Age", strconv.FormatInt(s.age, 10))
	}
	if s.staleWhileRevalidate {
		w.Header().Set("Cache-Control", "stale-while-revalidate=3600")
	}
	if s.statusHeader != "" {
		w.Header().Set(cacheStatusHeader, s.statusHeader)
	}
	if r.Header.Get("Authorization") != "Bearer stub-token" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if s.dataRC != 0 {
		w.WriteHeader(s.dataRC)
		_, _ = w.Write([]byte(`{"message":"` + plantedKey + `"}`))
		return
	}
	if s.release != nil && r.URL.Path == "/series/81189" {
		s.started <- struct{}{}
		select {
		case <-s.release:
		case <-r.Context().Done():
			if s.canceled != nil {
				s.canceled <- struct{}{}
			}
			return
		}
	}
	if s.otherRelease != nil && r.URL.Path == "/series/81200" {
		s.otherStarted <- struct{}{}
		select {
		case <-s.otherRelease:
		case <-r.Context().Done():
			if s.otherCanceled != nil {
				s.otherCanceled <- time.Now()
			}
			return
		}
	}
	s.data(w, r.URL.Path)
}

func (s *stub) requestArrival(index int) (time.Time, bool) {
	s.pathMu.Lock()
	defer s.pathMu.Unlock()
	if index < 0 || index >= s.requestCount {
		return time.Time{}, false
	}
	return s.requestTimes[index], true
}

func (s *stub) login(w http.ResponseWriter, r *http.Request) {
	s.logins.Add(1)
	if s.loginHandler != nil && s.loginHandler(w, r) {
		return
	}
	var body struct{ APIKey, PIN string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if s.loginRC != http.StatusOK {
		w.WriteHeader(s.loginRC)
		_, _ = w.Write([]byte(`{"message":"login failed"}`))
		return
	}
	if body.APIKey != plantedKey || (body.PIN != "" && body.PIN != plantedPIN) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Invalid credentials ` + plantedKey + `"}`))
		return
	}
	_, _ = w.Write([]byte(`{"status":"success","data":{"token":"stub-token"}}`))
}

func (s *stub) data(w http.ResponseWriter, path string) {
	var id int
	switch {
	case strings.HasPrefix(path, "/search/remoteid/"):
		mid, ok := s.movies[strings.TrimPrefix(path, "/search/remoteid/")]
		if !ok {
			fmt.Fprint(w, `{"status":"success","data":[]}`)
			return
		}
		fmt.Fprintf(w, `{"status":"success","data":[{"movie":{"id":%d,"name":"x"}}]}`, mid)
	case strings.HasSuffix(path, "/translations/deu") && strings.HasPrefix(path, "/series/"):
		_, _ = fmt.Sscanf(path, "/series/%d/translations/deu", &id)
		name, ok := s.series[id]
		if !ok || s.noTr[id] {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if s.numericTranslationOnce.Swap(false) {
			fmt.Fprint(w, `{"status":"success","data":{"name":123}}`)
			return
		}
		fmt.Fprintf(w, `{"status":"success","data":{"name":%q,"language":"deu","aliases":[]}}`, name)
	case strings.HasSuffix(path, "/translations/eng") && strings.HasPrefix(path, "/series/"):
		fmt.Fprint(w, `{"status":"success","data":{"name":"English","language":"eng","aliases":[]}}`)
	case strings.HasPrefix(path, "/series/"):
		_, _ = fmt.Sscanf(path, "/series/%d", &id)
		if _, ok := s.series[id]; !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if s.emptySeriesOnce.Swap(false) {
			return
		}
		fmt.Fprintf(w, `{"status":"success","data":{"id":%d,"name":"en","aliases":%s}}`, id, aliasJSON(s.aliases[id]))
	case strings.HasSuffix(path, "/translations/deu") && strings.HasPrefix(path, "/movies/"):
		_, _ = fmt.Sscanf(path, "/movies/%d/translations/deu", &id)
		fmt.Fprint(w, `{"status":"success","data":{"name":"Die Verurteilten","language":"deu"}}`)
	case strings.HasPrefix(path, "/movies/"):
		_, _ = fmt.Sscanf(path, "/movies/%d", &id)
		fmt.Fprintf(w, `{"status":"success","data":{"id":%d,"name":"en","aliases":%s}}`, id, aliasJSON(s.movieAli[id]))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func aliasJSON(names []string) string {
	items := make([]string, 0, len(names)+1)
	for _, n := range names {
		items = append(items, fmt.Sprintf(`{"language":"deu","name":%q}`, n))
	}
	items = append(items, `{"language":"fra","name":"Ignore Moi"}`)
	return "[" + strings.Join(items, ",") + "]"
}

func (s *stub) provider(t *testing.T, key, pin string, logs *bytes.Buffer) *Provider {
	t.Helper()
	return s.providerWith(t, key, pin, logs, testPool(t))
}

func (s *stub) providerWith(t *testing.T, key, pin string, logs *bytes.Buffer, l1 *memory.Cache, extra ...Option) *Provider {
	t.Helper()
	logger := slog.New(slog.DiscardHandler)
	if logs != nil {
		logger = slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	opts := []Option{WithBaseURL(s.srv.URL), WithRetryWait(time.Millisecond), WithRate("")}
	if s.tlsConfig != nil {
		opts = append(opts, withTLSConfig(s.tlsConfig))
	}
	for _, option := range extra {
		if option != nil {
			opts = append(opts, option)
		}
	}
	p, err := New(key, pin, l1, clock.NewFake(), logger, opts...)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	closeInCleanup(t, p)
	return p
}

func closeInCleanup(t *testing.T, p *Provider) {
	t.Helper()
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
}

// testPool stands in for the process-wide L1 pool the fx graph provides.
func testPool(t *testing.T) *memory.Cache {
	t.Helper()
	l1, err := memory.NewForTest(1024, 0)
	if err != nil {
		t.Fatalf("memory.NewForTest() error = %v", err)
	}
	return l1
}

func sonarrRef() Ref { return Ref{Kind: domain.KindSonarr, TVDBID: 81189} }

func titlesOf(list []foreigntitle.LanguageTitle) []string {
	out := make([]string, 0, len(list))
	for _, l := range list {
		out = append(out, l.Title)
	}
	return out
}

func TestTitlesForSeries(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	p := s.provider(t, plantedKey, plantedPIN, nil)
	got, err := p.TitlesFor(t.Context(), sonarrRef(), "de")
	if err != nil {
		t.Fatalf("TitlesFor() error = %v", err)
	}
	want := []foreigntitle.LanguageTitle{
		{Language: "de", Title: "Breaking Bad DE", Source: Source},
		{Language: "de", Title: "Breaking Bad Alias", Source: Source},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("TitlesFor() = %+v, want %+v (fra alias must be dropped)", got, want)
	}
}

func TestTitlesForMovieRemoteID(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	p := s.provider(t, plantedKey, "", nil)
	both := []string{"Die Verurteilten", "Die Verurteilten Alias"}
	cases := []struct {
		name string
		ref  Ref
		want []string
	}{
		{"imdb id", Ref{Kind: domain.KindRadarr, IMDBID: "tt0111161"}, both},
		{"tmdb id falls back after unknown imdb", Ref{Kind: domain.KindRadarr, IMDBID: "tt9999999", TMDBID: 5}, both},
	}
	s.movies["5"] = 439
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := p.TitlesFor(t.Context(), tc.ref, "de")
			if err != nil {
				t.Fatalf("TitlesFor() error = %v", err)
			}
			if g := strings.Join(titlesOf(got), "|"); g != strings.Join(tc.want, "|") {
				t.Fatalf("TitlesFor() = %q, want %q", g, strings.Join(tc.want, "|"))
			}
		})
	}
}

func TestDisabledMakesNoRequest(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	for _, p := range []*Provider{nil, s.provider(t, "", plantedPIN, nil)} {
		got, err := p.TitlesFor(t.Context(), sonarrRef(), "de")
		if err != nil || got != nil {
			t.Fatalf("TitlesFor() = %v, %v, want nil, nil", got, err)
		}
		if p.Enabled() || p.Failed() {
			t.Fatal("disabled provider reports enabled or failed")
		}
	}
	if n := s.total.Load(); n != 0 {
		t.Fatalf("requests = %d, want 0", n)
	}
}

func TestOtherKindsMakeNoRequest(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	p := s.provider(t, plantedKey, "", nil)
	for _, ref := range []Ref{
		{Kind: domain.KindLidarr, TVDBID: 1},
		{Kind: domain.KindReadarr, TVDBID: 1},
		{Kind: domain.KindWhisparrV2, TVDBID: 1},
		{Kind: domain.KindStash, TVDBID: 1},
		{Kind: domain.KindSonarr},
		{Kind: domain.KindRadarr},
	} {
		if got, err := p.TitlesFor(t.Context(), ref, "de"); err != nil || len(got) != 0 {
			t.Fatalf("TitlesFor(%+v) = %v, %v, want empty", ref, got, err)
		}
	}
	if n := s.total.Load(); n != 0 {
		t.Fatalf("requests = %d, want 0", n)
	}
}

func TestWrongCredentialsLatch(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, key, pin string }{
		{"wrong key", "refusedrefusedrefused", "refused-pin-93aa10"},
		{"right key, wrong PIN", plantedKey, "refused-pin-93aa10"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newStub(t)
			var logs bytes.Buffer
			p := s.provider(t, tc.key, tc.pin, &logs)
			_, err := p.TitlesFor(t.Context(), sonarrRef(), "de")
			if !errors.Is(err, ErrUnauthorized) {
				t.Fatalf("TitlesFor() error = %v, want ErrUnauthorized", err)
			}
			if !p.Failed() {
				t.Fatal("Failed() = false after a refused login")
			}
			first := s.total.Load()
			_, err = p.TitlesFor(t.Context(), sonarrRef(), "de")
			if !errors.Is(err, ErrUnauthorized) {
				t.Fatalf("second TitlesFor() error = %v, want ErrUnauthorized", err)
			}
			if n := s.total.Load(); n != first {
				t.Fatalf("second call made %d requests, want 0", n-first)
			}
			assertNoSecret(t, err.Error(), logs.String(), fmt.Sprintf("%+v", err))
		})
	}
}

func TestRateLimitedBoundedAttempts(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	s.dataRC = http.StatusTooManyRequests
	var logs bytes.Buffer
	p := s.provider(t, plantedKey, plantedPIN, &logs)
	_, err := p.TitlesFor(t.Context(), sonarrRef(), "de")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("TitlesFor() error = %v, want ErrRateLimited", err)
	}
	// 1 login + (1 try + retryMax retries) on the first data route.
	if n, want := s.total.Load(), int64(1+1+retryMax); n != want {
		t.Fatalf("requests = %d, want %d", n, want)
	}
	assertNoSecret(t, err.Error(), logs.String())
}

func TestOutboundRateLimit(t *testing.T) {
	t.Parallel()
	const (
		// The limiter window starts at the first request, so the two-request burst stays within it.
		burstCeiling = 900 * time.Millisecond
		// The limiter window starts at the first request, so the third request waits for the next window.
		windowFloor = 900 * time.Millisecond
	)
	for _, tc := range []struct {
		name         string
		deadline     time.Duration
		wantErr      error
		wantRequests int64
		wantPrompt   bool
	}{
		{"burst waits", 0, nil, 3, false},
		{"caller deadline does not stop outbound wait", 50 * time.Millisecond, nil, 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newStub(t)
			p, err := New(
				plantedKey,
				plantedPIN,
				testPool(t),
				clockwork.NewRealClock(),
				slog.New(slog.DiscardHandler),
				WithBaseURL(s.srv.URL),
				WithRetryWait(time.Millisecond),
				WithRate("2-S"),
			)
			if err != nil {
				t.Fatal(err)
			}
			closeInCleanup(t, p)
			ctx := t.Context()
			if tc.deadline > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tc.deadline)
				defer cancel()
			}
			started := time.Now()
			_, err = p.TitlesFor(ctx, sonarrRef(), "de")
			switch {
			case tc.wantPrompt:
				if !errors.Is(err, ErrUnavailable) || !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("TitlesFor() error = %v, want ErrUnavailable and context.DeadlineExceeded", err)
				}
				callerElapsed := time.Since(started)
				if callerElapsed >= 400*time.Millisecond {
					t.Fatalf("caller return elapsed = %v, want less than 400ms", callerElapsed)
				}
				const maxPolls = 100
				pollStarted := time.Now()
				for range maxPolls {
					if s.total.Load() >= tc.wantRequests {
						break
					}
					time.Sleep(50 * time.Millisecond)
				}
				pollElapsed := time.Since(pollStarted)
				if got := s.total.Load(); got != tc.wantRequests {
					t.Fatalf("requests = %d, want %d", got, tc.wantRequests)
				}
				t.Logf("caller return = %v, request-count poll = %v", callerElapsed, pollElapsed)
			default:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("TitlesFor() error = %v, want %v", err, tc.wantErr)
				}
				if got := s.total.Load(); got != tc.wantRequests {
					t.Fatalf("requests = %d, want %d", got, tc.wantRequests)
				}
			}
			firstArrival, ok := s.requestArrival(0)
			if !ok {
				t.Fatal("request arrival 1 was not recorded")
			}
			secondArrival, ok := s.requestArrival(1)
			if !ok {
				t.Fatal("request arrival 2 was not recorded")
			}
			thirdArrival, ok := s.requestArrival(2)
			if !ok {
				t.Fatal("request arrival 3 was not recorded")
			}
			if burstGap := secondArrival.Sub(firstArrival); burstGap >= burstCeiling {
				t.Fatalf("request 2 - request 1 = %v, want less than %v", burstGap, burstCeiling)
			}
			if windowGap := thirdArrival.Sub(firstArrival); windowGap < windowFloor {
				t.Fatalf("request 3 - request 1 = %v, want at least %v", windowGap, windowFloor)
			}
		})
	}
}

func TestNotFoundResponseIsCached(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	p := s.provider(t, plantedKey, "", nil)
	ref := Ref{Kind: domain.KindSonarr, TVDBID: 424242}
	for range 3 {
		got, err := p.TitlesFor(t.Context(), ref, "de")
		if err != nil || len(got) != 0 {
			t.Fatalf("TitlesFor() = %v, %v, want empty, nil", got, err)
		}
	}
	if n, want := s.total.Load(), int64(2); n != want { // login + series 404
		t.Fatalf("requests = %d, want %d (cached 404)", n, want)
	}
	unknown := Ref{Kind: domain.KindRadarr, IMDBID: "tt0000000"}
	if got, err := p.TitlesFor(t.Context(), unknown, "de"); err != nil || len(got) != 0 {
		t.Fatalf("unknown remote id = %v, %v, want empty, nil", got, err)
	}
}

func TestCacheHitMakesNoRequest(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	p := s.provider(t, plantedKey, "", nil)
	if _, err := p.TitlesFor(t.Context(), sonarrRef(), "de"); err != nil {
		t.Fatal(err)
	}
	before := s.total.Load()
	got, err := p.TitlesFor(t.Context(), sonarrRef(), "de")
	if err != nil || len(got) != 2 {
		t.Fatalf("cached = %v, %v", got, err)
	}
	if n := s.total.Load(); n != before {
		t.Fatalf("cache hit made %d requests", n-before)
	}
}

func TestCacheTTLUsesOriginAgeBoundary(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		age      int64
		wantData int64
	}{
		{"positive before expiry", int64((PositiveTTL - time.Hour) / time.Second), 2},
		{"boundary at expiry", int64(PositiveTTL / time.Second), 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newStub(t)
			s.age = tc.age
			p := s.provider(t, plantedKey, "", nil)
			for range 2 {
				if _, err := p.TitlesFor(t.Context(), sonarrRef(), "de"); err != nil {
					t.Fatalf("TitlesFor() error = %v", err)
				}
			}
			if got := s.total.Load() - s.logins.Load(); got != tc.wantData {
				t.Fatalf("data requests = %d, want %d", got, tc.wantData)
			}
		})
	}
}

func TestUndecodableTranslationIsNotCached(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	s.numericTranslationOnce.Store(true)
	p := s.provider(t, plantedKey, "", nil)
	if _, err := p.TitlesFor(t.Context(), sonarrRef(), "de"); err == nil {
		t.Fatal("first translation unexpectedly succeeded")
	}
	got, err := p.TitlesFor(t.Context(), sonarrRef(), "de")
	if err != nil {
		t.Fatalf("second TitlesFor() error = %v", err)
	}
	if titles := titlesOf(got); len(titles) != 2 || titles[0] != "Breaking Bad DE" {
		t.Fatalf("second TitlesFor() = %v, want corrected translation", titles)
	}
	s.pathMu.Lock()
	translationHits := s.pathHits["/series/81189/translations/deu"]
	s.pathMu.Unlock()
	if translationHits != 2 {
		t.Fatalf("translation requests = %d, want 2", translationHits)
	}
}

func TestEmptySeriesBodyIsNotCached(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	s.emptySeriesOnce.Store(true)
	p := s.provider(t, plantedKey, "", nil)
	_, _ = p.TitlesFor(t.Context(), sonarrRef(), "de")

	got, err := p.TitlesFor(t.Context(), sonarrRef(), "de")
	if err != nil {
		t.Fatalf("second TitlesFor() error = %v", err)
	}
	if titles := titlesOf(got); !slices.Contains(titles, "Breaking Bad Alias") {
		t.Fatalf("second TitlesFor() = %v, want Breaking Bad Alias", titles)
	}
	s.pathMu.Lock()
	seriesHits := s.pathHits["/series/81189"]
	s.pathMu.Unlock()
	if seriesHits != 2 {
		t.Fatalf("series requests = %d, want 2", seriesHits)
	}
}

func TestOriginStaleWhileRevalidateIsIgnored(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	s.age = int64(PositiveTTL / time.Second)
	s.staleWhileRevalidate = true
	p := s.provider(t, plantedKey, "", nil)
	for range 2 {
		resp, err := p.http.Do(authorizedRequest(t, s, "/series/81189"))
		if err != nil {
			t.Fatalf("Do() error = %v", err)
		}
		if status := resp.Header.Get(cacheStatusHeader); status == "STALE" {
			t.Fatalf("cache status = STALE, want synchronous re-fetch")
		}
		if err := resp.Body.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}
	s.pathMu.Lock()
	hits := s.pathHits["/series/81189"]
	s.pathMu.Unlock()
	if hits != 2 {
		t.Fatalf("series requests = %d, want 2", hits)
	}
}

func TestCredentialedResponsesNeedSharedHost(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		new   func(t *testing.T, p *Provider, s *stub) *http.Request
		extra Option
	}{
		{
			name: "login POST",
			new: func(t *testing.T, _ *Provider, s *stub) *http.Request {
				t.Helper()
				body := strings.NewReader(`{"apikey":"` + plantedKey + `","pin":"` + plantedPIN + `"}`)
				req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, s.srv.URL+"/login", body)
				if err != nil {
					t.Fatal(err)
				}
				return req
			},
		},
		{
			name:  "bearer GET without shared host",
			extra: withSharedHost(false),
			new: func(t *testing.T, _ *Provider, s *stub) *http.Request {
				t.Helper()
				req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, s.srv.URL+"/series/81189", nil)
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Authorization", "Bearer stub-token")
				return req
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newStub(t)
			p := s.providerWith(t, plantedKey, plantedPIN, nil, testPool(t), tc.extra)
			for range 2 {
				resp, err := p.http.Do(tc.new(t, p, s))
				if err != nil {
					t.Fatalf("Do() error = %v", err)
				}
				if err := resp.Body.Close(); err != nil {
					t.Fatalf("Close() error = %v", err)
				}
			}
			if got := s.total.Load(); got != 2 {
				t.Fatalf("upstream requests = %d, want 2", got)
			}
		})
	}
}

func TestCacheKeysContainNoCredential(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	l1 := testPool(t)
	p := s.providerWith(t, plantedKey, plantedPIN, nil, l1)
	if _, err := p.TitlesFor(t.Context(), sonarrRef(), "de"); err != nil {
		t.Fatal(err)
	}
	if count := len(slices.Collect(l1.Keys())); count == 0 {
		t.Fatalf("cache key count = %d, want at least one", count)
	}
	for key := range l1.Keys() {
		assertNoSecret(t, fmt.Sprint(key))
	}
}

func TestRedisPlaintextRequiresOptIn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, addr string
		password   string
		tls, optIn bool
		wantError  bool
		wantWarn   bool
	}{
		{"remote credential", "cache.example:6379", "planted-password", false, false, true, false},
		{"remote opted in", "cache.example:6379", "planted-password", false, true, false, true},
		{"remote TLS", "cache.example:6379", "planted-password", true, false, false, false},
		{"remote no password", "cache.example:6379", "", false, false, false, false},
		{"localhost", "localhost:6379", "planted-password", false, false, false, false},
		{"127 range", "127.0.0.2:6379", "planted-password", false, false, false, false},
		{"IPv6 loopback", "[::1]:6379", "planted-password", false, false, false, false},
		{"mixed list", "127.0.0.1:6379,cache.example:6379", "planted-password", false, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			p, err := New(plantedKey, "", testPool(t), clock.NewFake(), logger,
				WithL2("redis", rediscache.Options{Addr: tc.addr, Password: tc.password, TLS: tc.tls}),
				WithRedisPlaintext(tc.optIn), withRedisClient(&closeOnlyRedisClient{}))
			if tc.wantError {
				if err == nil || !strings.Contains(err.Error(), redisPlaintextKey) {
					t.Fatalf("New() error = %v, want %s", err, redisPlaintextKey)
				}
			} else {
				if err != nil {
					t.Fatalf("New() error = %v", err)
				}
				closeInCleanup(t, p)
			}
			if got := strings.Count(logs.String(), "level=WARN"); (got == 1) != tc.wantWarn || got > 1 {
				t.Fatalf("Warn records = %d, want one = %t", got, tc.wantWarn)
			}
			if tc.wantWarn && !strings.Contains(logs.String(), redisPlaintextKey) {
				t.Fatal("Warn missing opt-in key")
			}
			if strings.Contains(logs.String(), tc.addr) || (tc.password != "" && strings.Contains(logs.String(), tc.password)) {
				t.Fatal("Warn exposed Redis address or password")
			}
		})
	}
}

func TestRedisAddressLimit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, addr string
		wantError  bool
	}{
		{"sixteen", strings.Repeat("a:1,", maxRedisAddresses-1) + "a:1", false},
		{"seventeen", strings.Repeat("a:1,", maxRedisAddresses) + "a:1", true},
		{"empty entry", "a:1,,b:2", true},
		{"empty address", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := redisClientOption(rediscache.Options{Addr: tc.addr})
			if (err != nil) != tc.wantError {
				t.Fatalf("redisClientOption() error = %v, want error = %t", err, tc.wantError)
			}
			if tc.name == "seventeen" && !strings.Contains(err.Error(), "16") {
				t.Fatalf("address cap error = %v, want 16", err)
			}
		})
	}
}

func TestRedisClientOptionDisablesClientCache(t *testing.T) {
	t.Parallel()
	got, err := redisClientOption(rediscache.Options{Addr: "localhost:6379"})
	if err != nil {
		t.Fatalf("redisClientOption() error = %v", err)
	}
	if !got.DisableCache {
		t.Fatal("DisableCache = false, want true for the plain L2 client")
	}
}

func TestCallerCancelKeepsSharedLookup(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	s.started = make(chan struct{}, 2)
	release := make(chan struct{})
	s.release = release
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	p := s.provider(t, plantedKey, "", nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := p.TitlesFor(ctx, sonarrRef(), "de"); result <- err }()
	deadline, stop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stop()
	select {
	case <-s.started:
	case <-deadline.Done():
		t.Fatal("series request did not start")
	}
	cancel()
	resultErr := error(nil)
	select {
	case resultErr = <-result:
	case <-deadline.Done():
		t.Fatal("first lookup did not finish")
	}
	if !errors.Is(resultErr, ErrUnavailable) || !errors.Is(resultErr, context.Canceled) {
		t.Fatalf("first lookup error = %v, want ErrUnavailable and context.Canceled", resultErr)
	}
	close(release)
	waitDetached(t, p, 0, deadline.Done(), "detached lookup did not finish")
	got, err := p.TitlesFor(deadline, sonarrRef(), "de")
	if err != nil || len(got) == 0 {
		t.Fatalf("second lookup = %v, %v, want titles", got, err)
	}
	s.pathMu.Lock()
	series, translation := s.pathHits["/series/81189"], s.pathHits["/series/81189/translations/deu"]
	s.pathMu.Unlock()
	if series != 1 || translation != 1 {
		t.Fatalf("upstream series/translation requests = %d/%d, want 1/1", series, translation)
	}
}

func TestWarmHitDuringColdLookup(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	s.series[81200] = "Other Series DE"
	s.started = make(chan struct{}, 1)
	release := make(chan struct{})
	s.release = release
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	p := s.provider(t, plantedKey, "", nil)
	ref := Ref{Kind: domain.KindSonarr, TVDBID: 81200}
	got, err := p.TitlesFor(t.Context(), ref, "de")
	if err != nil || len(got) == 0 || titlesOf(got)[0] != "Other Series DE" {
		t.Fatalf("prime TitlesFor() = %v, %v", got, err)
	}
	cold := make(chan error, 1)
	go func() { _, coldErr := p.TitlesFor(t.Context(), sonarrRef(), "de"); cold <- coldErr }()
	select {
	case <-s.started:
	case <-time.After(5 * time.Second):
		t.Fatal("cold series request did not start")
	}
	before := s.total.Load()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	got, err = p.TitlesFor(ctx, ref, "de")
	if err != nil || len(got) == 0 {
		t.Fatalf("warm TitlesFor() = %v, %v", got, err)
	}
	if after := s.total.Load(); after != before {
		t.Fatalf("warm cache hit made %d requests", after-before)
	}
	close(release)
	select {
	case err := <-cold:
		if err != nil {
			t.Fatalf("cold TitlesFor() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cold lookup did not finish")
	}
}

func TestConcurrentColdLookups(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	s.series[81200] = "Other Series DE"
	p := s.provider(t, plantedKey, "", nil)
	if _, err := p.TitlesFor(t.Context(), Ref{Kind: domain.KindSonarr, TVDBID: 424242}, "de"); err != nil {
		t.Fatalf("token prime TitlesFor() error = %v", err)
	}
	cases := []struct {
		ref  Ref
		lang string
	}{
		{Ref{Kind: domain.KindSonarr, TVDBID: 81189}, "de"},
		{Ref{Kind: domain.KindSonarr, TVDBID: 81200}, "de"},
		{Ref{Kind: domain.KindRadarr, IMDBID: "tt0111161"}, "de"},
		{Ref{Kind: domain.KindSonarr, TVDBID: 81189}, "en"},
	}
	results := make(chan error, len(cases))
	var wg sync.WaitGroup
	for _, tc := range cases {
		wg.Go(func() {
			_, err := p.TitlesFor(t.Context(), tc.ref, tc.lang)
			results <- err
		})
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Errorf("concurrent TitlesFor() error = %v", err)
		}
	}
}

func TestDetachedLookupsBounded(t *testing.T) {
	t.Parallel()
	t.Run("cap-1", func(t *testing.T) {
		t.Parallel()
		s := newStub(t)
		s.started = make(chan struct{}, 1)
		s.canceled = make(chan struct{}, 1)
		release := make(chan struct{})
		s.release = release
		p := s.provider(t, plantedKey, "", nil)
		for range cap(p.detached) - 1 {
			p.detached <- struct{}{}
		}
		t.Cleanup(func() {
			for range cap(p.detached) - 1 {
				<-p.detached
			}
		})
		t.Cleanup(func() {
			select {
			case <-release:
			default:
				close(release)
			}
		})
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		result := make(chan error, 1)
		go func() { _, err := p.TitlesFor(ctx, sonarrRef(), "de"); result <- err }()
		select {
		case <-s.started:
			cancel()
		case <-time.After(5 * time.Second):
			t.Fatal("detached series request did not start")
		}
		select {
		case err := <-result:
			if !errors.Is(err, ErrUnavailable) || !errors.Is(err, context.Canceled) {
				t.Fatalf("detached caller error = %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("detached caller did not return")
		}
		select {
		case <-s.canceled:
			t.Fatal("detached lookup observed caller cancellation")
		case <-time.After(300 * time.Millisecond):
		}
		close(release)
		waitDetached(t, p, cap(p.detached)-1, time.After(2*time.Second), "detached lookup did not release its token")
		before := s.total.Load()
		got, err := p.TitlesFor(t.Context(), sonarrRef(), "de")
		if err != nil || len(got) == 0 {
			t.Fatalf("cached detached lookup = %v, %v", got, err)
		}
		if after := s.total.Load(); after != before {
			t.Fatalf("cache fill made %d requests", after-before)
		}
	})
	t.Run("cap", func(t *testing.T) {
		t.Parallel()
		s := newStub(t)
		s.started = make(chan struct{}, 1)
		s.canceled = make(chan struct{}, 1)
		release := make(chan struct{})
		s.release = release
		p := s.provider(t, plantedKey, "", nil)
		for range cap(p.detached) {
			p.detached <- struct{}{}
		}
		t.Cleanup(func() {
			for range cap(p.detached) {
				<-p.detached
			}
		})
		t.Cleanup(func() {
			select {
			case <-release:
			default:
				close(release)
			}
		})
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		result := make(chan error, 1)
		go func() { _, err := p.TitlesFor(ctx, sonarrRef(), "de"); result <- err }()
		select {
		case <-s.started:
			cancel()
		case <-time.After(5 * time.Second):
			t.Fatal("attached series request did not start")
		}
		select {
		case err := <-result:
			if !errors.Is(err, ErrUnavailable) || !errors.Is(err, context.Canceled) {
				t.Fatalf("attached caller error = %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("attached caller did not return")
		}
		select {
		case <-s.canceled:
		case <-time.After(2 * time.Second):
			t.Fatal("attached lookup did not observe caller cancellation")
		}
	})
}

func TestLookupSingleDeadline(t *testing.T) {
	t.Parallel()
	const deadlineTolerance = 100 * time.Millisecond
	s := newStub(t)
	s.series[81200] = "Other Series DE"
	s.otherStarted = make(chan struct{}, 1)
	s.otherCanceled = make(chan time.Time, 1)
	otherRelease := make(chan struct{})
	s.otherRelease = otherRelease
	t.Cleanup(func() { close(otherRelease) })
	p := s.provider(t, plantedKey, "", nil)
	p.lookup = 900 * time.Millisecond
	deadline, stop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stop()
	fetchDeadline := make(chan time.Time, 1)
	p.fetchStart = func(ctx context.Context) {
		value, _ := ctx.Deadline()
		fetchDeadline <- value
	}
	result := make(chan error, 1)
	go func() {
		_, err := p.TitlesFor(t.Context(), Ref{Kind: domain.KindSonarr, TVDBID: 81200}, "de")
		result <- err
	}()
	var startedDeadline time.Time
	select {
	case startedDeadline = <-fetchDeadline:
	case <-deadline.Done():
		t.Fatal("fetch did not start")
	}
	select {
	case <-s.otherStarted:
	case <-deadline.Done():
		t.Fatal("series request did not start")
	}
	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("lookup error = %v, want context.DeadlineExceeded", err)
		}
	case <-deadline.Done():
		t.Fatal("lookup did not finish")
	}
	var canceledAt time.Time
	select {
	case canceledAt = <-s.otherCanceled:
	case <-deadline.Done():
		t.Fatal("series request did not observe its lookup deadline")
	}
	lowerBound := startedDeadline.Add(-deadlineTolerance)
	upperBound := startedDeadline.Add(p.lookup / 2)
	if canceledAt.Before(lowerBound) || !canceledAt.Before(upperBound) {
		t.Fatalf("upstream cancellation = %v, want [%v, %v)", canceledAt, lowerBound, upperBound)
	}
}

func TestCloseClosesOwnedIdleConnections(t *testing.T) {
	t.Parallel()
	const closeObservationTimeout = 2 * time.Second
	closed := make(chan struct{}, 4)
	s := newConnStateStub(t, func(_ net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			select {
			case closed <- struct{}{}:
			default:
			}
		}
	})
	p := s.provider(t, plantedKey, "", nil)
	if _, err := p.TitlesFor(t.Context(), sonarrRef(), "de"); err != nil {
		t.Fatalf("TitlesFor() error = %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	deadline, cancel := context.WithTimeout(t.Context(), closeObservationTimeout)
	defer cancel()
	select {
	case <-closed:
	case <-deadline.Done():
		t.Fatal("owned idle connection did not close")
	}
}

func newConnStateStub(t *testing.T, connState func(net.Conn, http.ConnState)) *stub {
	t.Helper()
	s := &stub{
		t: t, loginRC: http.StatusOK,
		series:   map[int]string{81189: "Breaking Bad DE"},
		aliases:  map[int][]string{81189: {"Breaking Bad Alias"}},
		movies:   map[string]int{"tt0111161": 439},
		movieAli: map[int][]string{439: {"Die Verurteilten Alias"}},
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(s.serve))
	srv.Config.ConnState = connState
	srv.Start()
	s.srv = srv
	t.Cleanup(srv.Close)
	return s
}

func TestCloseStopsDetachedLookup(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	s.started = make(chan struct{}, 1)
	s.canceled = make(chan struct{}, 1)
	release := make(chan struct{})
	s.release = release
	t.Cleanup(func() { close(release) })
	p := s.provider(t, plantedKey, "", nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	first := make(chan error, 1)
	go func() { _, err := p.TitlesFor(ctx, sonarrRef(), "de"); first <- err }()
	startDeadline, stopStart := context.WithTimeout(t.Context(), 5*time.Second)
	defer stopStart()
	select {
	case <-s.started:
	case <-startDeadline.Done():
		t.Fatal("series request did not start")
	}
	deadline, stop := context.WithTimeout(t.Context(), 2*time.Second)
	defer stop()
	cancel()
	select {
	case err := <-first:
		if !errors.Is(err, ErrUnavailable) || !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled lookup error = %v", err)
		}
	case <-deadline.Done():
		t.Fatal("cancelled caller did not return")
	}
	start := time.Now()
	if err := p.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed >= 2*time.Second {
		t.Fatalf("Close() elapsed = %v, want less than 2s", elapsed)
	}
	select {
	case <-s.canceled:
	case <-deadline.Done():
		t.Fatal("held series request did not observe cancellation")
	}
	before := s.total.Load()
	if _, err := p.TitlesFor(t.Context(), sonarrRef(), "de"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("lookup after Close error = %v, want ErrUnavailable", err)
	}
	if got := s.total.Load(); got != before {
		t.Fatalf("requests after Close = %d, want %d", got, before)
	}
}

func TestCloseWaitsForDetachedLookups(t *testing.T) {
	t.Parallel()
	t.Run("held lookups", testCloseWaitsForHeldLookups)
	t.Run("close wait bound", testCloseWaitBound)
}

func testCloseWaitsForHeldLookups(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	s.series[81200] = "Other Series DE"
	s.started = make(chan struct{}, 1)
	s.canceled = make(chan struct{}, 1)
	s.otherStarted = make(chan struct{}, 1)
	s.otherCanceled = make(chan time.Time, 1)
	release, otherRelease := make(chan struct{}), make(chan struct{})
	s.release, s.otherRelease = release, otherRelease
	p := s.provider(t, plantedKey, "", nil)
	t.Cleanup(func() { close(release); close(otherRelease) })
	firstCtx, cancelFirst := context.WithCancel(t.Context())
	secondCtx, cancelSecond := context.WithCancel(t.Context())
	first, second := make(chan error, 1), make(chan error, 1)
	go func() { _, err := p.TitlesFor(firstCtx, sonarrRef(), "de"); first <- err }()
	go func() {
		_, err := p.TitlesFor(secondCtx, Ref{Kind: domain.KindSonarr, TVDBID: 81200}, "de")
		second <- err
	}()
	for _, started := range []<-chan struct{}{s.started, s.otherStarted} {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("held upstream request did not start")
		}
	}
	cancelFirst()
	cancelSecond()
	for _, result := range []<-chan error{first, second} {
		select {
		case err := <-result:
			if !errors.Is(err, ErrUnavailable) || !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled caller error = %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("canceled caller did not return")
		}
	}
	select {
	case <-s.canceled:
		t.Fatal("first detached lookup observed caller cancellation")
	case <-s.otherCanceled:
		t.Fatal("second detached lookup observed caller cancellation")
	case <-time.After(300 * time.Millisecond):
	}
	start := time.Now()
	if err := p.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed >= 2*time.Second {
		t.Fatalf("Close() elapsed = %v, want less than 2s", elapsed)
	}
	select {
	case <-s.canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("first held upstream request did not observe Close cancellation")
	}
	select {
	case <-s.otherCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("second held upstream request did not observe Close cancellation")
	}
}

func testCloseWaitBound(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	p, err := New(plantedKey, "", testPool(t), clock.NewFake(), slog.New(slog.DiscardHandler), WithBaseURL(s.srv.URL), WithRate(""))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	p.detached <- struct{}{}
	start := time.Now()
	err = p.Close()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close() error = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed < closeWaitTimeout-100*time.Millisecond || elapsed >= 3*time.Second {
		t.Fatalf("Close() elapsed = %v, want close wait bound", elapsed)
	}
}

const maxDetachedPollAttempts = 5000

func waitDetached[T any](t *testing.T, p *Provider, want int, deadline <-chan T, message string) {
	t.Helper()
	for attempt := range maxDetachedPollAttempts {
		if len(p.detached) == want {
			return
		}
		if attempt+1 == maxDetachedPollAttempts {
			break
		}
		select {
		case <-deadline:
			t.Fatal(message)
		default:
			time.Sleep(time.Millisecond)
		}
	}
	t.Fatal(message)
}

func TestLoginGateRespectsLookupDeadline(t *testing.T) {
	t.Parallel()
	const (
		shortLookup = 60 * time.Millisecond
		waitBound   = 600 * time.Millisecond
	)
	s := newStub(t)
	s.series[81200] = "Other Series DE"
	loginStarted := make(chan struct{}, 1)
	loginRelease := make(chan struct{})
	closeRelease := sync.OnceFunc(func() { close(loginRelease) })
	t.Cleanup(closeRelease)
	s.loginHandler = func(_ http.ResponseWriter, r *http.Request) bool {
		select {
		case loginStarted <- struct{}{}:
		default:
		}
		select {
		case <-loginRelease:
		case <-r.Context().Done():
		}
		return false
	}
	p := s.provider(t, plantedKey, plantedPIN, nil)
	first := make(chan error, 1)
	go func() {
		_, err := p.TitlesFor(t.Context(), sonarrRef(), "de")
		first <- err
	}()
	backstop, stopBackstop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stopBackstop()
	select {
	case <-loginStarted:
	case <-backstop.Done():
		t.Fatal("first login did not start")
	}
	callerCtx := t.Context()
	if _, hasDeadline := callerCtx.Deadline(); hasDeadline {
		t.Fatal("caller context unexpectedly carries a deadline")
	}
	p.lookup = shortLookup
	second := make(chan error, 1)
	started := time.Now()
	go func() {
		_, err := p.TitlesFor(callerCtx, Ref{Kind: domain.KindSonarr, TVDBID: 81200}, "de")
		second <- err
	}()
	select {
	case err := <-second:
		if !errors.Is(err, ErrUnavailable) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("second TitlesFor() error = %v, want ErrUnavailable and context.DeadlineExceeded", err)
		}
		if elapsed := time.Since(started); elapsed >= waitBound {
			t.Fatalf("second TitlesFor() elapsed = %v, want less than %v", elapsed, waitBound)
		}
	case <-time.After(waitBound):
		closeRelease()
		t.Fatalf("second TitlesFor() did not stop on lookup deadline within %v", waitBound)
	}
	p.lookup = lookupTimeout
	closeRelease()
	select {
	case err := <-first:
		if err != nil {
			t.Fatalf("first TitlesFor() error = %v", err)
		}
	case <-backstop.Done():
		t.Fatal("first lookup did not finish after login release")
	}
	got, err := p.TitlesFor(t.Context(), Ref{Kind: domain.KindSonarr, TVDBID: 81200}, "de")
	if err != nil || len(got) == 0 {
		t.Fatalf("third TitlesFor() = %v, %v, want titles", got, err)
	}
	if logins := s.logins.Load(); logins != 1 {
		t.Fatalf("logins = %d, want 1", logins)
	}
}

func TestLoginGateClassifiesBeforeRelease(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(fmt.Sprintf("refused_%d", status), func(t *testing.T) {
			t.Parallel()
			s := newStub(t)
			loginStarted := make(chan struct{}, 2)
			loginRelease := make(chan struct{})
			closeRelease := sync.OnceFunc(func() { close(loginRelease) })
			t.Cleanup(closeRelease)
			s.loginHandler = func(w http.ResponseWriter, r *http.Request) bool {
				select {
				case loginStarted <- struct{}{}:
				default:
				}
				select {
				case <-loginRelease:
				case <-r.Context().Done():
					return true
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"message":"login failed"}`))
				return true
			}
			p := s.provider(t, plantedKey, plantedPIN, nil)
			gateWaiting := make(chan struct{}, 1)
			p.loginWait = func() {
				select {
				case gateWaiting <- struct{}{}:
				default:
				}
			}
			first, second := make(chan error, 1), make(chan error, 1)
			go func() { _, err := p.TitlesFor(t.Context(), sonarrRef(), "de"); first <- err }()
			backstop, stopBackstop := context.WithTimeout(t.Context(), 5*time.Second)
			defer stopBackstop()
			select {
			case <-loginStarted:
			case <-backstop.Done():
				t.Fatal("first login did not start")
			}
			go func() { _, err := p.TitlesFor(t.Context(), sonarrRef(), "de"); second <- err }()
			select {
			case <-gateWaiting:
			case <-backstop.Done():
				t.Fatal("second lookup did not wait at the login gate")
			}
			closeRelease()
			for i, ch := range []<-chan error{first, second} {
				select {
				case err := <-ch:
					if !errors.Is(err, ErrUnauthorized) {
						t.Fatalf("lookup %d error = %v, want ErrUnauthorized", i+1, err)
					}
				case <-backstop.Done():
					t.Fatalf("lookup %d did not finish", i+1)
				}
			}
			if !p.Failed() {
				t.Fatal("Failed() = false, want true")
			}
			if logins := s.logins.Load(); logins != 1 {
				t.Fatalf("logins = %d, want 1", logins)
			}
		})
	}
	t.Run("transient_500_then_success", func(t *testing.T) {
		t.Parallel()
		s := newStub(t)
		var failOnce atomic.Bool
		failOnce.Store(true)
		s.loginHandler = func(w http.ResponseWriter, _ *http.Request) bool {
			if failOnce.Swap(false) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"message":"temporary failure"}`))
				return true
			}
			return false
		}
		p := s.provider(t, plantedKey, plantedPIN, nil)
		if _, err := p.TitlesFor(t.Context(), sonarrRef(), "de"); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("first TitlesFor() error = %v, want ErrUnavailable", err)
		}
		if p.Failed() {
			t.Fatal("Failed() = true after transient 500 login")
		}
		got, err := p.TitlesFor(t.Context(), sonarrRef(), "de")
		if err != nil || len(got) == 0 {
			t.Fatalf("second TitlesFor() = %v, %v, want titles", got, err)
		}
		if p.Failed() {
			t.Fatal("Failed() = true after recovered login")
		}
		if logins := s.logins.Load(); logins != 2 {
			t.Fatalf("logins = %d, want 2", logins)
		}
	})
}

func TestCallMapsErrors(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	p := s.provider(t, plantedKey, plantedPIN, nil)
	for _, tc := range []struct {
		name        string
		expired     bool
		fnErr       error
		wantErr     error
		wantWrapped error
		forbidErr   error
	}{
		{
			name: "positive_nil",
		},
		{
			name:      "positive_rate_limited",
			fnErr:     fmt.Errorf("httpx/ratelimit: api4.thetvdb.com: %w", ratelimit.ErrLimited),
			wantErr:   ErrRateLimited,
			forbidErr: ErrUnavailable,
		},
		{
			name:        "boundary_expired_context",
			expired:     true,
			fnErr:       fmt.Errorf("dial: %w", context.DeadlineExceeded),
			wantErr:     ErrUnavailable,
			wantWrapped: context.DeadlineExceeded,
		},
		{
			name:      "boundary_live_context_inner_deadline",
			fnErr:     fmt.Errorf("inner: %w", context.DeadlineExceeded),
			wantErr:   ErrUnavailable,
			forbidErr: context.DeadlineExceeded,
		},
		{
			name:      "negative_plain_error",
			fnErr:     errors.New("transport boom " + plantedKey),
			wantErr:   ErrUnavailable,
			forbidErr: ErrRateLimited,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			if tc.expired {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer cancel()
			}
			err := p.call(ctx, func(context.Context) error { return tc.fnErr })
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("call() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("call() error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantWrapped != nil && !errors.Is(err, tc.wantWrapped) {
				t.Fatalf("call() error = %v, want wrapped %v", err, tc.wantWrapped)
			}
			if tc.forbidErr != nil && errors.Is(err, tc.forbidErr) {
				t.Fatalf("call() error = %v, must not match %v", err, tc.forbidErr)
			}
			assertNoSecret(t, err.Error())
		})
	}
}

var errCacheOpenProbe = errors.New("cache open probe")

func isCacheOpen(p *Provider) bool {
	probe := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errCacheOpenProbe
	})
	req := &http.Request{
		Method: http.MethodPost,
		URL:    &url.URL{Scheme: "https", Host: "api4.thetvdb.com", Path: "/login"},
		Header: make(http.Header),
	}
	resp, err := p.cache.Wrap(probe).RoundTrip(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	return errors.Is(err, errCacheOpenProbe)
}

func TestCloseWaitsForAttachedLookup(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	s.series[81200] = "Other Series DE"
	s.started = make(chan struct{}, maxDetached)
	releaseDetached := make(chan struct{})
	closeDetached := sync.OnceFunc(func() { close(releaseDetached) })
	s.release = releaseDetached
	p := s.provider(t, plantedKey, "", nil)
	t.Cleanup(closeDetached)
	if _, err := p.TitlesFor(t.Context(), Ref{Kind: domain.KindSonarr, TVDBID: 424242}, "de"); err != nil {
		t.Fatalf("prime TitlesFor() error = %v", err)
	}
	attachedStarted := make(chan struct{}, 1)
	attachedCanceled := make(chan struct{}, 1)
	releaseAttached := make(chan struct{})
	closeAttached := sync.OnceFunc(func() { close(releaseAttached) })
	t.Cleanup(closeAttached)
	var cacheOpenDuringWorker atomic.Bool
	origTransport := p.http.Transport
	p.http.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/series/81200" {
			attachedStarted <- struct{}{}
			<-req.Context().Done()
			attachedCanceled <- struct{}{}
			<-releaseAttached
			cacheOpenDuringWorker.Store(isCacheOpen(p))
		}
		return origTransport.RoundTrip(req)
	})
	detachedEntered := make(chan struct{}, maxDetached)
	p.fetchStart = func(context.Context) {
		select {
		case detachedEntered <- struct{}{}:
		default:
		}
	}
	backstop, stopBackstop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stopBackstop()
	detachedResults := make(chan error, maxDetached)
	cancels := make([]context.CancelFunc, 0, maxDetached)
	for range maxDetached {
		dctx, cancel := context.WithCancel(t.Context())
		cancels = append(cancels, cancel)
		go func() {
			_, err := p.TitlesFor(dctx, sonarrRef(), "de")
			detachedResults <- err
		}()
	}
	for range maxDetached {
		select {
		case <-detachedEntered:
		case <-backstop.Done():
			t.Fatal("detached lookup did not enter fetch")
		}
	}
	select {
	case <-s.started:
	case <-backstop.Done():
		t.Fatal("detached upstream request did not start")
	}
	for _, cancel := range cancels {
		cancel()
	}
	for range maxDetached {
		select {
		case err := <-detachedResults:
			if !errors.Is(err, ErrUnavailable) || !errors.Is(err, context.Canceled) {
				t.Fatalf("detached caller error = %v, want ErrUnavailable and context.Canceled", err)
			}
		case <-backstop.Done():
			t.Fatal("detached caller did not return")
		}
	}
	if got := len(p.detached); got != maxDetached {
		t.Fatalf("held detached slots = %d, want %d", got, maxDetached)
	}
	attachedDone := make(chan error, 1)
	go func() {
		_, err := p.TitlesFor(t.Context(), Ref{Kind: domain.KindSonarr, TVDBID: 81200}, "de")
		attachedDone <- err
	}()
	select {
	case <-attachedStarted:
	case <-backstop.Done():
		t.Fatal("attached lookup did not start")
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- p.Close() }()
	select {
	case <-attachedCanceled:
	case <-backstop.Done():
		t.Fatal("attached lookup did not observe Close cancellation")
	}
	select {
	case err := <-closeDone:
		t.Fatalf("Close() returned (%v) before attached lookup finished", err)
	case <-time.After(100 * time.Millisecond):
	}
	if !isCacheOpen(p) {
		t.Fatal("cache closed before attached lookup finished")
	}
	closeAttached()
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	case <-backstop.Done():
		t.Fatal("Close() did not return after attached lookup finished")
	}
	select {
	case err := <-attachedDone:
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("attached TitlesFor() error = %v, want ErrUnavailable", err)
		}
	case <-backstop.Done():
		t.Fatal("attached caller did not return")
	}
	if !cacheOpenDuringWorker.Load() {
		t.Fatal("cache was closed while attached worker was still running")
	}
	if isCacheOpen(p) {
		t.Fatal("cache remained open after Close() returned")
	}
}

func TestCloseStopsAdmittedLookupBeforeFetch(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	p := s.provider(t, plantedKey, "", nil)
	admitted := make(chan struct{})
	allowFetch := make(chan struct{})
	closeAllow := sync.OnceFunc(func() { close(allowFetch) })
	t.Cleanup(closeAllow)
	var admitOnce sync.Once
	p.admitted = func() {
		admitOnce.Do(func() { close(admitted) })
		<-allowFetch
	}
	var fetchCalled atomic.Bool
	p.fetchStart = func(context.Context) {
		fetchCalled.Store(true)
	}
	lookupDone := make(chan error, 1)
	go func() {
		_, err := p.TitlesFor(t.Context(), sonarrRef(), "de")
		lookupDone <- err
	}()
	backstop, stopBackstop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stopBackstop()
	select {
	case <-admitted:
	case <-backstop.Done():
		t.Fatal("lookup was not admitted")
	}
	closeDone := make(chan error, 1)
	go func() { closeDone <- p.Close() }()
	select {
	case <-p.lifetimeDone:
	case <-backstop.Done():
		t.Fatal("Close() did not cancel lifetime")
	}
	closeAllow()
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	case <-backstop.Done():
		t.Fatal("Close() did not finish")
	}
	select {
	case err := <-lookupDone:
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("TitlesFor() error = %v, want ErrUnavailable", err)
		}
	case <-backstop.Done():
		t.Fatal("TitlesFor() did not return")
	}
	if fetchCalled.Load() {
		t.Fatal("admitted lookup called fetchStart after Close started")
	}
	if got := s.total.Load(); got != 0 {
		t.Fatalf("upstream requests = %d, want 0", got)
	}
}

func TestCacheMetrics(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	m, reg := metricstest.New(t)
	p := s.providerWith(t, plantedKey, plantedPIN, nil, testPool(t), WithMetrics(m))
	for range 2 {
		if _, err := p.TitlesFor(t.Context(), sonarrRef(), "de"); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		status string
		want   float64
	}{{"MISS", 2}, {"HIT", 2}} {
		got, ok := metricstest.ValueLabels(t, reg, "snatcharr_metadata_cache_requests_total", map[string]string{
			"provider": Source,
			"status":   tc.status,
		})
		if !ok || got != tc.want {
			t.Fatalf("cache metric %s = %v (present %t), want %v", tc.status, got, ok, tc.want)
		}
	}
}

func TestCacheMetricsNormalizesUncachedStatuses(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	m, reg := metricstest.New(t)
	p := s.providerWith(t, plantedKey, plantedPIN, nil, testPool(t), WithMetrics(m))
	s.dataRC = http.StatusTooManyRequests
	assertPassThroughCountedAsNone(t, p, s, reg, "bare 429")
	// golusoris returns a 429 unchanged; originGuard removes any origin cache status header so pass-throughs count as NONE.
	s.statusHeader = "planted"
	assertPassThroughCountedAsNone(t, p, s, reg, "planted status")
	if got, ok := cacheRequestsLabel(t, reg, "planted"); ok {
		t.Fatalf("cache metric status planted = %v, want no series", got)
	}
	beforeHit, hadHit := cacheRequestsLabel(t, reg, "HIT")
	s.statusHeader = "HIT"
	assertPassThroughCountedAsNone(t, p, s, reg, "origin HIT on 429")
	if afterHit, hasHit := cacheRequestsLabel(t, reg, "HIT"); hasHit != hadHit || afterHit != beforeHit {
		t.Fatalf("cache metric HIT = (%v, %t), want (%v, %t)", afterHit, hasHit, beforeHit, hadHit)
	}
}

func assertPassThroughCountedAsNone(t *testing.T, p *Provider, s *stub, reg prometheus.Gatherer, label string) {
	t.Helper()
	beforeTotal := s.total.Load()
	beforeNone, _ := cacheRequestsLabel(t, reg, cacheStatusNone)
	doPassThrough(t, p, s)
	attempts := s.total.Load() - beforeTotal
	if attempts < 1 {
		t.Fatalf("%s: upstream attempts = %d, want at least 1", label, attempts)
	}
	afterNone, _ := cacheRequestsLabel(t, reg, cacheStatusNone)
	if got, want := afterNone-beforeNone, float64(attempts); got != want {
		t.Fatalf("%s: cache metric NONE delta = %v, want %v", label, got, want)
	}
}

func doPassThrough(t *testing.T, p *Provider, s *stub) {
	t.Helper()
	resp, err := p.http.Do(authorizedRequest(t, s, "/series/81189"))
	if err != nil && resp == nil {
		t.Fatalf("429 Do() error = %v", err)
	}
	if resp != nil {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Fatalf("Close(429) error = %v", closeErr)
		}
	}
}

func cacheRequestsLabel(t *testing.T, reg prometheus.Gatherer, status string) (float64, bool) {
	t.Helper()
	return metricstest.ValueLabels(t, reg, "snatcharr_metadata_cache_requests_total", map[string]string{
		"provider": Source,
		"status":   status,
	})
}

func authorizedRequest(t *testing.T, s *stub, path string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, s.srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer stub-token")
	return req
}

type closeOnlyRedisClient struct {
	rueidis.Client
	closed atomic.Bool
}

func (c *closeOnlyRedisClient) Close() { c.closed.Store(true) }

func TestRedisL2UsesInjectedClientAndClosesIt(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	client := &closeOnlyRedisClient{}
	p := s.providerWith(
		t,
		plantedKey,
		plantedPIN,
		nil,
		testPool(t),
		WithL2("redis", rediscache.Options{Addr: "dragonfly.test:6379"}),
		withRedisClient(client),
	)
	if _, ok := p.redis.(*boundRedis); !ok {
		t.Fatalf("Redis L2 client type = %T, want *boundRedis", p.redis)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !client.closed.Load() {
		t.Fatal("Close() did not close the injected Redis client")
	}
}

func TestAliasCapAndEmpty(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	many := make([]string, 0, 300)
	for i := range 300 {
		many = append(many, fmt.Sprintf("Alias %d", i))
	}
	s.aliases[81189] = many
	p := s.provider(t, plantedKey, "", nil)
	got, err := p.TitlesFor(t.Context(), sonarrRef(), "de")
	if err != nil || len(got) != maxAliases {
		t.Fatalf("len = %d, err = %v, want %d", len(got), err, maxAliases)
	}
	s.aliases[81190], s.series[81190] = nil, ""
	got, err = p.TitlesFor(t.Context(), Ref{Kind: domain.KindSonarr, TVDBID: 81190}, "de")
	if err != nil || len(got) != 0 {
		t.Fatalf("empty translation and aliases = %v, %v, want empty", got, err)
	}
}

func TestFirstMovieID(t *testing.T) {
	t.Parallel()
	results := func(count int) []tvdb.SearchByRemoteIDResult {
		out := make([]tvdb.SearchByRemoteIDResult, 0, count)
		for range count {
			out = append(out, tvdb.SearchByRemoteIDResult{})
		}
		return out
	}
	tests := []struct {
		name    string
		results []tvdb.SearchByRemoteIDResult
		wantID  int
		wantOK  bool
	}{
		{
			name:    "movie after output cap",
			results: append(results(maxAliases+50), tvdb.SearchByRemoteIDResult{Movie: &tvdb.MovieBase{ID: 42}}),
			wantID:  42,
			wantOK:  true,
		},
		{
			name:    "no movie",
			results: results(maxAliases + 50),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotID, gotOK := firstMovieID(tt.results)
			if gotID != tt.wantID || gotOK != tt.wantOK {
				t.Fatalf("firstMovieID() = %d, %t, want %d, %t", gotID, gotOK, tt.wantID, tt.wantOK)
			}
		})
	}
}

type collectAliasScanCase struct {
	name           string
	translation    *tvdb.Translation
	aliases        []tvdb.Alias
	wantLen        int
	wantTitle      string
	wantTitleIndex int
}

var collectAliasScanCases = []collectAliasScanCase{
	{
		name:      "matching alias after output cap",
		aliases:   append(aliasScanAliases("eng", maxAliases+50), tvdb.Alias{Language: "deu", Name: "German title"}),
		wantLen:   1,
		wantTitle: "German title",
	},
	{
		name:    "accepted titles remain capped",
		aliases: aliasScanAliases("deu", maxAliases+50),
		wantLen: maxAliases,
	},
	{
		name: "empty aliases",
	},
	{
		name:    "no matching aliases",
		aliases: aliasScanAliases("eng", maxAliases+50),
	},
	{
		name: "translation duplicate aliases do not consume output cap",
		translation: &tvdb.Translation{
			Name:    "Titel",
			Aliases: append(aliasScanDuplicateAliases("  tItEl  ", maxAliases+50), "Anderer Titel"),
		},
		wantLen:        2,
		wantTitle:      "Anderer Titel",
		wantTitleIndex: 1,
	},
	{
		name: "translation aliases remain capped",
		translation: &tvdb.Translation{
			Aliases: aliasScanTranslationAliases("Titel", maxAliases+50),
		},
		wantLen: maxAliases,
	},
	{
		name: "translation name duplicates add no titles",
		translation: &tvdb.Translation{
			Name:    "Titel",
			Aliases: []string{"Titel", " titel ", "TITEL"},
		},
		wantLen: 1,
	},
}

func aliasScanAliases(language string, count int) []tvdb.Alias {
	out := make([]tvdb.Alias, 0, count)
	for i := range count {
		out = append(out, tvdb.Alias{Language: language, Name: fmt.Sprintf("Alias %d", i)})
	}
	return out
}

func aliasScanTranslationAliases(name string, count int) []string {
	out := make([]string, 0, count)
	for i := range count {
		out = append(out, fmt.Sprintf("%s %d", name, i))
	}
	return out
}

func aliasScanDuplicateAliases(title string, count int) []string {
	out := make([]string, 0, count)
	for range count {
		out = append(out, title)
	}
	return out
}

func TestCollectAliasScan(t *testing.T) {
	t.Parallel()
	for _, tt := range collectAliasScanCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := collect("de", "deu", tt.translation, tt.aliases)
			if len(got) != tt.wantLen {
				t.Fatalf("len(collect()) = %d, want %d", len(got), tt.wantLen)
			}
			if tt.wantTitle != "" && got[tt.wantTitleIndex].Title != tt.wantTitle {
				t.Fatalf("collect()[%d].Title = %q, want %q", tt.wantTitleIndex, got[tt.wantTitleIndex].Title, tt.wantTitle)
			}
		})
	}
}

// TestMissingTranslationKeepsAliases covers a series that exists but has no translation in
// the language: TVDB answers 404 for the translation, and the aliases still come back.
func TestMissingTranslationKeepsAliases(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	s.noTr = map[int]bool{81189: true}
	p := s.provider(t, plantedKey, "", nil)
	got, err := p.TitlesFor(t.Context(), sonarrRef(), "de")
	if err != nil {
		t.Fatalf("TitlesFor() error = %v, want nil for a missing translation", err)
	}
	want := foreigntitle.LanguageTitle{Language: "de", Title: "Breaking Bad Alias", Source: Source}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("TitlesFor() = %+v, want only %+v", got, want)
	}
	got, err = p.TitlesFor(t.Context(), sonarrRef(), "de")
	if err != nil || len(got) != 1 || got[0] != want {
		t.Fatalf("cached TitlesFor() = %+v, %v, want only %+v", got, err, want)
	}
	if n, want := s.total.Load(), int64(3); n != want { // login, series, translation 404
		t.Fatalf("requests = %d, want %d (translation 404 cached)", n, want)
	}
}

func TestLanguageCodeCoversEveryRule(t *testing.T) {
	t.Parallel()
	rules, err := foreigntitle.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(rules) == 0 {
		t.Fatal("no embedded rules")
	}
	want := map[string]string{"de": "deu", "fr": "fra"}
	for code := range rules {
		got, err := LanguageCode(code)
		if err != nil || len(got) != 3 {
			t.Fatalf("LanguageCode(%q) = %q, %v", code, got, err)
		}
		if w, ok := want[code]; ok && got != w {
			t.Fatalf("LanguageCode(%q) = %q, want %q", code, got, w)
		}
	}
	for _, bad := range []string{"", "!!", "zzzzzz"} {
		if _, err := LanguageCode(bad); !errors.Is(err, ErrLanguage) {
			t.Fatalf("LanguageCode(%q) error = %v, want ErrLanguage", bad, err)
		}
	}
}

func TestOversizeBodyIsUnavailable(t *testing.T) {
	t.Parallel()
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			_, _ = w.Write([]byte(`{"data":{"token":"stub-token"}}`))
			return
		}
		_, _ = w.Write(bytes.Repeat([]byte("a"), maxBodyBytes+10))
	}))
	t.Cleanup(big.Close)
	p, err := New(plantedKey, "", testPool(t), clock.NewFake(), slog.New(slog.DiscardHandler), WithBaseURL(big.URL), WithRate(""))
	if err != nil {
		t.Fatal(err)
	}
	closeInCleanup(t, p)
	if _, err := p.TitlesFor(t.Context(), sonarrRef(), "de"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
}

func TestServerErrorKeepsSecretsOut(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	s.dataRC = http.StatusInternalServerError
	var logs bytes.Buffer
	p := s.provider(t, plantedKey, plantedPIN, &logs)
	_, err := p.TitlesFor(t.Context(), sonarrRef(), "de")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
	assertNoSecret(t, err.Error(), logs.String())
}

// assertNoSecret fails when any text holds a planted credential or the stub token. The
// text values come first; the trailing secrets are extra values the call sent.
func assertNoSecret(t *testing.T, texts ...string) {
	t.Helper()
	secrets := []string{plantedKey, plantedPIN, "stub-token", "refusedrefusedrefused", "refused-pin-93aa10"}
	for _, text := range texts {
		for _, secret := range secrets {
			if strings.Contains(text, secret) {
				t.Fatalf("secret leaked into output: %q", text)
			}
		}
	}
}

func TestNewWithoutPool(t *testing.T) {
	t.Parallel()
	if _, err := New(plantedKey, "", nil, clock.NewFake(), nil); !errors.Is(err, errNoCache) {
		t.Fatalf("enabled without pool: error = %v, want errNoCache", err)
	}
	p, err := New("", "", nil, clock.NewFake(), nil)
	if err != nil || p.Enabled() {
		t.Fatalf("disabled without pool: enabled = %v, error = %v; want false, nil", p.Enabled(), err)
	}
}

func TestUserAgentHeader(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, ua string }{
		{"configured", "SnatchArr/test (+probe)"},
		{"unset keeps the client default", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newStub(t)
			p, err := New(plantedKey, "", testPool(t), clock.NewFake(), nil, WithBaseURL(s.srv.URL), WithRate(""), WithUserAgent(tc.ua))
			if err != nil {
				t.Fatal(err)
			}
			closeInCleanup(t, p)
			if _, err := p.TitlesFor(t.Context(), sonarrRef(), "de"); err != nil {
				t.Fatal(err)
			}
			got, _ := s.agent.Load().(string)
			if tc.ua != "" && got != tc.ua {
				t.Fatalf("User-Agent = %q, want %q", got, tc.ua)
			}
			if got == "" {
				t.Fatal("User-Agent empty")
			}
		})
	}
}
