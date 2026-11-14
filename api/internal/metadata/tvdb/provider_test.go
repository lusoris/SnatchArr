// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package tvdbprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/golusoris/golusoris/cache/memory"
	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/foreigntitle"
)

const (
	plantedKey = "plantedplantedplanted"
	plantedPIN = "planted-pin-4c2d08"
)

// stub is a TVDB API double. Every route counts its hits.
type stub struct {
	t        *testing.T
	srv      *httptest.Server
	total    atomic.Int64
	logins   atomic.Int64
	loginRC  int
	dataRC   int // when non-zero every data route answers with this status
	series   map[int]string
	noTr     map[int]bool     // series ids whose deu translation answers 404
	aliases  map[int][]string // series id -> deu aliases
	movies   map[string]int   // remote id -> movie id
	movieAli map[int][]string
	delay    time.Duration
	agent    atomic.Value  // last User-Agent header seen
	started  chan struct{} // signals a held series request
	release  <-chan struct{}
}

func newStub(t *testing.T) *stub {
	t.Helper()
	s := &stub{
		t: t, loginRC: http.StatusOK,
		series:   map[int]string{81189: "Breaking Bad DE"},
		aliases:  map[int][]string{81189: {"Breaking Bad Alias"}},
		movies:   map[string]int{"tt0111161": 439},
		movieAli: map[int][]string{439: {"Die Verurteilten Alias"}},
	}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *stub) serve(w http.ResponseWriter, r *http.Request) {
	s.total.Add(1)
	s.agent.Store(r.Header.Get("User-Agent"))
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	if r.URL.Path == "/login" {
		s.login(w, r)
		return
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
			return
		}
	}
	s.data(w, r.URL.Path)
}

func (s *stub) login(w http.ResponseWriter, r *http.Request) {
	s.logins.Add(1)
	var body struct{ APIKey, PIN string }
	_ = json.NewDecoder(r.Body).Decode(&body)
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
	logger := slog.New(slog.DiscardHandler)
	if logs != nil {
		logger = slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	p, err := New(key, pin, testPool(t), clock.NewFake(), logger, WithBaseURL(s.srv.URL), WithRetryWait(time.Millisecond), WithRate(rate.Inf, 1))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return p
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

func TestNotFoundIsEmptyAndNegativeCached(t *testing.T) {
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
		t.Fatalf("requests = %d, want %d (negative cache)", n, want)
	}
	unknown := Ref{Kind: domain.KindRadarr, IMDBID: "tt0000000"}
	if got, err := p.TitlesFor(t.Context(), unknown, "de"); err != nil || len(got) != 0 {
		t.Fatalf("unknown remote id = %v, %v, want empty, nil", got, err)
	}
}

func TestNegativeEntryExpires(t *testing.T) {
	t.Parallel()
	s := newStub(t)
	clk := clock.NewFake()
	p, err := New(plantedKey, "", testPool(t), clk, slog.New(slog.DiscardHandler), WithBaseURL(s.srv.URL), WithRate(rate.Inf, 1), WithRetryWait(time.Millisecond))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ref := Ref{Kind: domain.KindSonarr, TVDBID: 7}
	if got, ferr := p.TitlesFor(t.Context(), ref, "de"); ferr != nil || len(got) != 0 {
		t.Fatalf("first = %v, %v", got, ferr)
	}
	s.series[7] = "Sieben"
	clk.Advance(NegativeTTL - time.Second)
	if got, _ := p.TitlesFor(t.Context(), ref, "de"); len(got) != 0 {
		t.Fatalf("before expiry = %v, want still empty", got)
	}
	clk.Advance(2 * time.Second)
	got, err := p.TitlesFor(t.Context(), ref, "de")
	if err != nil || len(got) == 0 || got[0].Title != "Sieben" {
		t.Fatalf("after expiry = %v, %v, want Sieben", got, err)
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
	if n, want := s.total.Load(), int64(3); n != want { // login, series, translation 404
		t.Fatalf("requests = %d, want %d", n, want)
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
	p, err := New(plantedKey, "", testPool(t), clock.NewFake(), slog.New(slog.DiscardHandler), WithBaseURL(big.URL), WithRate(rate.Inf, 1))
	if err != nil {
		t.Fatal(err)
	}
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

// TestWaitingLookupHonoursDeadline checks a lookup queued behind a held slot gives up with
// ErrUnavailable and sends no request: at the caller's deadline, and at the provider's own
// lookup bound when the caller's context has none (HISS-02). The test holds the slot itself
// for the whole case, so no other lookup can free it first.
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
			_, err := p.TitlesFor(ctx, sonarrRef(), "de")
			<-p.slot
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("waiting lookup: error = %v, want ErrUnavailable", err)
			}
			if n := s.total.Load(); n != 0 {
				t.Fatalf("waiting lookup sent %d requests, want 0", n)
			}
		})
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

func TestUserAgentHeader(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, ua string }{
		{"configured", "SnatchArr/test (+probe)"},
		{"unset keeps the client default", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newStub(t)
			p, err := New(plantedKey, "", testPool(t), clock.NewFake(), nil, WithBaseURL(s.srv.URL), WithRate(rate.Inf, 1), WithUserAgent(tc.ua))
			if err != nil {
				t.Fatal(err)
			}
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
