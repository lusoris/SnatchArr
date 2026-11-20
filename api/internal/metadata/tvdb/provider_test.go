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
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tvdb "github.com/golusoris/goenvoy/metadata/video/tvdb"
	"github.com/jonboulle/clockwork"
	"github.com/redis/rueidis"

	"github.com/golusoris/golusoris/cache/memory"
	rediscache "github.com/golusoris/golusoris/cache/redis"
	"github.com/golusoris/golusoris/core/clock"

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
	t             *testing.T
	srv           *httptest.Server
	tlsConfig     *tls.Config
	total         atomic.Int64
	logins        atomic.Int64
	loginRC       int
	dataRC        int // when non-zero every data route answers with this status
	series        map[int]string
	noTr          map[int]bool     // series ids whose deu translation answers 404
	aliases       map[int][]string // series id -> deu aliases
	movies        map[string]int   // remote id -> movie id
	movieAli      map[int][]string
	delay         time.Duration
	age           int64
	agent         atomic.Value  // last User-Agent header seen
	started       chan struct{} // signals a held series request
	release       <-chan struct{}
	canceled      chan struct{} // signals cancellation of the held series request
	otherStarted  chan struct{}
	otherRelease  <-chan struct{}
	otherCanceled chan time.Time
	pathMu        sync.Mutex
	pathHits      map[string]int
	requestTimes  [16]time.Time
	requestCount  int
	redirect      func(http.ResponseWriter, *http.Request) bool
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
	s.total.Add(1)
	s.pathMu.Lock()
	if s.pathHits == nil {
		s.pathHits = make(map[string]int)
	}
	s.pathHits[r.URL.Path]++
	if s.requestCount < len(s.requestTimes) {
		s.requestTimes[s.requestCount] = time.Now()
		s.requestCount++
	}
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
	var body struct{ APIKey, PIN string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if s.loginRC != http.StatusOK || body.APIKey != plantedKey || (body.PIN != "" && body.PIN != plantedPIN) {
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
		fmt.Fprintf(w, `{"status":"success","data":{"name":%q,"language":"deu","aliases":[]}}`, name)
	case strings.HasPrefix(path, "/series/"):
		_, _ = fmt.Sscanf(path, "/series/%d", &id)
		if _, ok := s.series[id]; !ok {
			w.WriteHeader(http.StatusNotFound)
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
	opts = append(opts, extra...)
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
		{"positive before expiry", int64(PositiveTTL/time.Second) - 1, 2},
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

func TestCredentialedResponsesNeedSharedHost(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		new  func(t *testing.T, p *Provider, s *stub) *http.Request
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
			name: "bearer GET without shared host",
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
			p := s.providerWith(t, plantedKey, plantedPIN, nil, testPool(t), withSharedHost(false))
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

func TestLookupSingleDeadline(t *testing.T) {
	t.Parallel()
	const (
		// Long enough that a fresh deadline taken after the slot differs on every platform clock.
		distinctDeadlineGap = 20 * time.Millisecond
		deadlineTolerance   = 100 * time.Millisecond
	)
	s := newStub(t)
	s.series[81200] = "Other Series DE"
	s.otherStarted = make(chan struct{}, 1)
	s.otherCanceled = make(chan time.Time, 1)
	otherRelease := make(chan struct{})
	s.otherRelease = otherRelease
	t.Cleanup(func() { close(otherRelease) })
	p := s.provider(t, plantedKey, "", nil)
	p.lookup = 900 * time.Millisecond
	p.slot <- struct{}{}
	slotHeld := true
	t.Cleanup(func() {
		if slotHeld {
			<-p.slot
		}
	})
	deadline, stop := context.WithTimeout(t.Context(), 5*time.Second)
	defer stop()
	slotDeadline := make(chan time.Time, 1)
	p.slotWait = func(ctx context.Context) {
		value, _ := ctx.Deadline()
		slotDeadline <- value
	}
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
	var waitDeadline time.Time
	select {
	case waitDeadline = <-slotDeadline:
	case <-deadline.Done():
		t.Fatal("lookup did not start waiting for the slot")
	}
	time.Sleep(distinctDeadlineGap)
	<-p.slot
	slotHeld = false
	var startedDeadline time.Time
	select {
	case startedDeadline = <-fetchDeadline:
	case <-deadline.Done():
		t.Fatal("fetch did not start")
	}
	if !startedDeadline.Equal(waitDeadline) {
		t.Fatalf("fetch deadline D2 = %v, want slot-wait deadline D1 = %v", startedDeadline, waitDeadline)
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
	lowerBound := waitDeadline.Add(-deadlineTolerance)
	upperBound := waitDeadline.Add(p.lookup / 2)
	if canceledAt.Before(lowerBound) || !canceledAt.Before(upperBound) {
		t.Fatalf("upstream cancellation = %v, want [%v, %v)", canceledAt, lowerBound, upperBound)
	}
}

func TestActiveLookupContextLifecycle(t *testing.T) {
	t.Parallel()
	s := newStub(t)
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
	result := make(chan error, 1)
	go func() { _, err := p.TitlesFor(t.Context(), sonarrRef(), "de"); result <- err }()
	deadline, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	select {
	case <-s.started:
	case <-deadline.Done():
		t.Fatal("series request did not start")
	}
	active := p.active.Load()
	if active == nil {
		t.Fatal("active lookup context = nil while upstream is blocked")
	}
	if _, ok := (*active).Deadline(); !ok {
		t.Fatal("active lookup context has no deadline")
	}
	close(release)
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("TitlesFor() error = %v", err)
		}
	case <-deadline.Done():
		t.Fatal("TitlesFor() did not return")
	}
	// TitlesFor can return before the lookup goroutine's defers run, so wait for the slot release.
	select {
	case p.slot <- struct{}{}:
	case <-deadline.Done():
		t.Fatal("lookup slot was not released")
	}
	defer func() { <-p.slot }()
	if active := p.active.Load(); active != nil {
		t.Fatal("active lookup context was not cleared before the lookup slot was released")
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
