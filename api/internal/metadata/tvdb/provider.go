// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package tvdbprovider is the opt-in TheTVDB metadata provider (ADR-0048). It only reads
// translated titles and aliases through the goenvoy TVDB client and never writes to TVDB.
//
// Data courtesy of TheTVDB.com (https://thetvdb.com): TVDB requires its attribution
// wherever its data is shown; the operator docs and the foreign-titles API carry it.
package tvdbprovider

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/golusoris/goenvoy/metadata"
	tvdb "github.com/golusoris/goenvoy/metadata/video/tvdb"
	"golang.org/x/text/language"
	"golang.org/x/time/rate"

	"github.com/golusoris/golusoris/cache/memory"
	"github.com/golusoris/golusoris/cache/twotier"
	"github.com/golusoris/golusoris/core/clock"
	httpclient "github.com/golusoris/golusoris/httpx/client"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/foreigntitle"
)

// Source names TVDB as the origin of a LanguageTitle.
const Source = "tvdb"

const (
	// PositiveTTL is how long a found title set stays cached.
	PositiveTTL = 24 * time.Hour
	// NegativeTTL is how long an unknown item stays cached before TVDB is asked again.
	NegativeTTL = time.Hour

	maxAliases  = 200
	callTimeout = 20 * time.Second
	// lookupTimeout bounds one load from its start: the wait for the slot plus at most
	// four calls (two remote-id searches, the movie, its translation) of callTimeout each.
	lookupTimeout = 4*callTimeout + 10*time.Second
	maxBodyBytes  = 4 << 20
	// TVDB publishes no rate limit (api docs, checked 2026-11-13); this is a conservative
	// ceiling on every real HTTP request, retries and logins included.
	defaultRate  = rate.Limit(2)
	defaultBurst = 4
	retryMax     = 2

	// loginFailedMessage is the fixed text goenvoy puts on a refused /login answer. goenvoy
	// exports no sentinel for it yet (https://github.com/golusoris/goenvoy/issues/323);
	// switch to errors.Is once it does.
	loginFailedMessage = "login failed"
	// cachePrefix namespaces this provider's keys in the shared L1 pool.
	cachePrefix = "metadata.tvdb"
)

var (
	// ErrUnauthorized means TVDB refused the API key or PIN. The provider stops calling
	// TVDB until the process restarts with new credentials.
	ErrUnauthorized = errors.New("tvdb: credentials refused")
	// ErrRateLimited means TVDB answered 429 after the bounded retries.
	ErrRateLimited = errors.New("tvdb: rate limited")
	// ErrUnavailable means TVDB could not be reached or answered unusably.
	ErrUnavailable = errors.New("tvdb: unavailable")
	// ErrLanguage means the rule language has no TVDB language code.
	ErrLanguage = errors.New("tvdb: unsupported language")
	// errNoCache means an enabled provider was built without the shared L1 pool.
	errNoCache = errors.New("tvdb: no cache pool")
)

// Ref identifies one item by the IDs its *arr app holds.
type Ref struct {
	Kind   domain.AppKind
	TVDBID int
	TMDBID int
	IMDBID string
}

// Option tunes a Provider; tests use them to reach a stub server fast.
type Option func(*config)

type config struct {
	baseURL   string
	limit     rate.Limit
	burst     int
	retryWait time.Duration
	userAgent string
}

// WithBaseURL points the provider at another API root (tests).
func WithBaseURL(u string) Option { return func(c *config) { c.baseURL = u } }

// WithRate replaces the request-rate ceiling (tests).
func WithRate(limit rate.Limit, burst int) Option {
	return func(c *config) { c.limit, c.burst = limit, burst }
}

// WithRetryWait replaces the first retry back-off (tests).
func WithRetryWait(d time.Duration) Option { return func(c *config) { c.retryWait = d } }

// WithUserAgent sets the User-Agent header of every TVDB request.
func WithUserAgent(ua string) Option { return func(c *config) { c.userAgent = ua } }

// entry is one cached lookup result; NotFound entries expire after NegativeTTL.
type entry struct {
	Titles   []foreigntitle.LanguageTitle
	NotFound bool
	At       time.Time
}

// Provider reads translated titles from TheTVDB. A nil or disabled Provider answers every
// call with no titles and makes no network request.
type Provider struct {
	enabled bool
	client  *tvdb.Client
	clk     clock.Clock
	logger  *slog.Logger
	cache   *twotier.Typed[entry]
	// slot is a one-request semaphore around goenvoy calls: the client rewrites its
	// request auth in shared state on every call (https://github.com/golusoris/goenvoy/issues/321).
	// A channel instead of a mutex lets a waiting lookup give up at its context deadline.
	slot chan struct{}
	// lookup is lookupTimeout; a field so a test can wait on it without the full bound.
	lookup time.Duration
	failed atomic.Bool
}

// New builds the provider over l1, the process-wide in-process cache pool. An empty apiKey
// yields a disabled provider (no client, no cache, no network) and l1 may then be nil. The
// key and PIN are only handed to the goenvoy client.
func New(apiKey, pin string, l1 *memory.Cache, clk clock.Clock, logger *slog.Logger, opts ...Option) (*Provider, error) {
	if apiKey == "" {
		return &Provider{}, nil
	}
	if l1 == nil {
		return nil, errNoCache
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	cfg := config{limit: defaultRate, burst: defaultBurst}
	for _, o := range opts {
		o(&cfg)
	}
	tt, err := twotier.New(l1, twotier.Options{L1TTL: PositiveTTL, L2: twotier.L2None}, logger)
	if err != nil {
		return nil, fmt.Errorf("tvdb: cache: %w", err)
	}
	hc := httpclient.New(httpclient.Options{
		Name:    "snatcharr.metadata.tvdb",
		Timeout: callTimeout,
		Retry:   httpclient.RetryOptions{Max: retryMax, Wait: cfg.retryWait, MaxWait: 8 * time.Second},
		Logger:  logger,
		Transport: &limitTransport{
			next:    &capTransport{next: ownTransport(), max: maxBodyBytes},
			limiter: rate.NewLimiter(cfg.limit, cfg.burst),
		},
	})
	mopts := []metadata.Option{metadata.WithHTTPClient(hc)}
	if cfg.userAgent != "" {
		mopts = append(mopts, metadata.WithUserAgent(cfg.userAgent))
	}
	if cfg.baseURL != "" {
		mopts = append(mopts, metadata.WithBaseURL(cfg.baseURL))
	}
	var client *tvdb.Client
	if pin != "" {
		client = tvdb.NewWithPIN(apiKey, pin, mopts...)
	} else {
		client = tvdb.New(apiKey, mopts...)
	}
	return &Provider{
		enabled: true, client: client, clk: clk, logger: logger,
		cache: twotier.NewTyped[entry](tt, cachePrefix), slot: make(chan struct{}, 1),
		lookup: lookupTimeout,
	}, nil
}

// Enabled reports whether the provider holds credentials. A provider TVDB locked out stays
// enabled; Failed reports the lockout.
func (p *Provider) Enabled() bool { return p != nil && p.enabled }

// Failed reports whether TVDB refused the credentials; the provider then makes no call.
func (p *Provider) Failed() bool { return p != nil && p.failed.Load() }

// LanguageCode maps a rule language code to the three-letter code TVDB uses ("de" to "deu").
func LanguageCode(rule string) (string, error) {
	tag, err := language.Parse(strings.TrimSpace(rule))
	if err != nil {
		return "", fmt.Errorf("%w: %q", ErrLanguage, rule)
	}
	base, _ := tag.Base()
	code := base.ISO3()
	if code == "" || code == "und" {
		return "", fmt.Errorf("%w: %q", ErrLanguage, rule)
	}
	return code, nil
}

// TitlesFor returns the titles TVDB holds for ref in the rule's language. Sonarr uses the
// series translation and aliases; Radarr finds the movie by remote ID first; every other
// kind answers empty without a call. An unknown item answers empty and nil.
func (p *Provider) TitlesFor(ctx context.Context, ref Ref, ruleCode string) ([]foreigntitle.LanguageTitle, error) {
	if !p.Enabled() {
		return nil, nil
	}
	key, ok := cacheKey(ref)
	if !ok {
		return nil, nil
	}
	lang, err := LanguageCode(ruleCode)
	if err != nil {
		return nil, err
	}
	if p.failed.Load() {
		return nil, ErrUnauthorized
	}
	rule := strings.ToLower(strings.TrimSpace(ruleCode))
	loader := func(ctx context.Context) (entry, error) { return p.load(ctx, ref, lang, rule) }
	k := key + ":" + lang
	e, err := p.cached(ctx, k, loader)
	if err == nil && e.NotFound && p.clk.Since(e.At) >= NegativeTTL {
		// The negative entry is stale: evict it from the local cache (no TVDB call) and ask once more.
		evict := p.cache.Delete
		if derr := evict(ctx, k); derr != nil {
			p.logger.WarnContext(ctx, "tvdb: drop stale negative entry", slog.String("key", k))
		}
		e, err = p.cached(ctx, k, loader)
	}
	if err != nil {
		return nil, unwrapLoad(err)
	}
	return append([]foreigntitle.LanguageTitle(nil), e.Titles...), nil
}

// cached lets a waiter stop at its own deadline. In golusoris v0.20.0,
// cache/twotier/twotier.go:131-157 calls group.Do; cache/singleflight/singleflight.go:52-63
// calls x/sync singleflight.Group.Do synchronously without checking a waiter's context.
// The goroutine ends when the shared load ends, bounded by p.lookup; the buffered
// result lets it finish even after this caller leaves. Get receives ctx unchanged.
func (p *Provider) cached(ctx context.Context, key string, loader func(context.Context) (entry, error)) (entry, error) {
	type result struct {
		value entry
		err   error
	}
	done := make(chan result, 1)
	go func() {
		value, err := p.cache.Get(ctx, key, loader)
		done <- result{value: value, err: err}
	}()
	select {
	case r := <-done:
		return r.value, r.err
	case <-ctx.Done():
		return entry{}, fmt.Errorf("%w: %w", ErrUnavailable, ctx.Err())
	}
}

// unwrapLoad strips the cache layer's "loader:" prefix so callers match the sentinels.
func unwrapLoad(err error) error {
	for _, s := range []error{ErrUnauthorized, ErrRateLimited, ErrUnavailable} {
		if errors.Is(err, s) {
			return s
		}
	}
	return ErrUnavailable
}

// cacheKey is kind:id for the item, or false when the kind or the IDs give nothing to ask.
func cacheKey(ref Ref) (string, bool) {
	switch ref.Kind {
	case domain.KindSonarr:
		if ref.TVDBID > 0 {
			return "series:" + strconv.Itoa(ref.TVDBID), true
		}
	case domain.KindRadarr:
		if id := strings.TrimSpace(ref.IMDBID); id != "" {
			return "movie:imdb-" + id, true
		}
		if ref.TMDBID > 0 {
			return "movie:tmdb-" + strconv.Itoa(ref.TMDBID), true
		}
	case domain.KindLidarr, domain.KindReadarr, domain.KindWhisparrV2, domain.KindWhisparrV3, domain.KindStash:
		// TVDB holds no music, books or adult items.
	}
	return "", false
}

// load runs once per key (singleflight) while holding the provider slot. Its deadline
// starts before the slot wait, so a caller without one still gives up (HISS-02).
func (p *Provider) load(ctx context.Context, ref Ref, lang, rule string) (entry, error) {
	ctx, cancel := context.WithTimeout(ctx, p.lookup)
	defer cancel()
	select {
	case p.slot <- struct{}{}:
	case <-ctx.Done():
		return entry{}, fmt.Errorf("%w: %w", ErrUnavailable, ctx.Err())
	}
	defer func() { <-p.slot }()
	if p.failed.Load() {
		return entry{}, ErrUnauthorized
	}
	var titles []foreigntitle.LanguageTitle
	var err error
	if ref.Kind == domain.KindSonarr {
		titles, err = p.seriesTitles(ctx, ref.TVDBID, lang, rule)
	} else {
		titles, err = p.movieTitles(ctx, ref, lang, rule)
	}
	switch {
	case err == nil:
		return entry{Titles: titles, At: p.clk.Now()}, nil
	case errors.Is(err, errNotFound):
		return entry{NotFound: true, At: p.clk.Now()}, nil
	default:
		return entry{}, err
	}
}

var errNotFound = errors.New("tvdb: not found")

func (p *Provider) seriesTitles(ctx context.Context, id int, lang, rule string) ([]foreigntitle.LanguageTitle, error) {
	var base *tvdb.SeriesBase
	err := p.call(ctx, func(c context.Context) (err error) { base, err = p.client.GetSeries(c, id); return })
	if err != nil {
		return nil, err
	}
	fetch := func(c context.Context) (*tvdb.Translation, error) { return p.client.GetSeriesTranslation(c, id, lang) }
	return p.translated(ctx, fetch, base.Aliases, lang, rule)
}

func (p *Provider) movieTitles(ctx context.Context, ref Ref, lang, rule string) ([]foreigntitle.LanguageTitle, error) {
	id, err := p.findMovie(ctx, ref)
	if err != nil {
		return nil, err
	}
	var base *tvdb.MovieBase
	err = p.call(ctx, func(c context.Context) (err error) { base, err = p.client.GetMovie(c, id); return })
	if err != nil {
		return nil, err
	}
	fetch := func(c context.Context) (*tvdb.Translation, error) { return p.client.GetMovieTranslation(c, id, lang) }
	return p.translated(ctx, fetch, base.Aliases, lang, rule)
}

// translated fetches one translation through fetch and joins it with the entity's aliases.
// A missing translation (404) is no error: the aliases still count.
func (p *Provider) translated(ctx context.Context, fetch func(context.Context) (*tvdb.Translation, error), aliases []tvdb.Alias, lang, rule string) ([]foreigntitle.LanguageTitle, error) {
	var tr *tvdb.Translation
	err := p.call(ctx, func(c context.Context) (err error) { tr, err = fetch(c); return })
	if err != nil && !errors.Is(err, errNotFound) {
		return nil, err
	}
	return collect(rule, lang, tr, aliases), nil
}

// findMovie resolves the TVDB movie ID by IMDB ID first, then TMDB ID. Only a movie hit
// counts: a TMDB number can also name a series.
func (p *Provider) findMovie(ctx context.Context, ref Ref) (int, error) {
	remote := make([]string, 0, 2)
	if id := strings.TrimSpace(ref.IMDBID); id != "" {
		remote = append(remote, id)
	}
	if ref.TMDBID > 0 {
		remote = append(remote, strconv.Itoa(ref.TMDBID))
	}
	for i := range remote {
		var res []tvdb.SearchByRemoteIDResult
		err := p.call(ctx, func(c context.Context) (err error) { res, err = p.client.SearchByRemoteID(c, remote[i]); return })
		if err != nil && !errors.Is(err, errNotFound) {
			return 0, err
		}
		for j := range min(len(res), maxAliases) {
			if m := res[j].Movie; m != nil && m.ID > 0 {
				return m.ID, nil
			}
		}
	}
	return 0, errNotFound
}

// call runs one goenvoy call under a deadline and maps its error to this package's
// sentinels. The goenvoy error text can quote a response body, so it is never returned.
func (p *Provider) call(ctx context.Context, fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	err := fn(ctx)
	if err == nil {
		return nil
	}
	if apiErr, ok := errors.AsType[*tvdb.APIError](err); ok {
		return p.mapStatus(apiErr)
	}
	if cerr := ctx.Err(); cerr != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, cerr)
	}
	return fmt.Errorf("%w: request failed", ErrUnavailable)
}

func (p *Provider) mapStatus(e *tvdb.APIError) error {
	switch e.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		if e.Message == loginFailedMessage {
			p.failed.Store(true)
			p.logger.Warn("tvdb: credentials refused, provider stopped until restart", slog.Int("status", e.StatusCode))
		}
		return ErrUnauthorized
	case http.StatusNotFound:
		return errNotFound
	case http.StatusTooManyRequests:
		return ErrRateLimited
	default:
		return fmt.Errorf("%w: status %d", ErrUnavailable, e.StatusCode)
	}
}

// collect merges the translation name, its aliases and the item aliases of the language,
// deduplicated case-insensitively and bounded by maxAliases.
func collect(rule, lang string, tr *tvdb.Translation, aliases []tvdb.Alias) []foreigntitle.LanguageTitle {
	var out []foreigntitle.LanguageTitle
	seen := map[string]struct{}{}
	add := func(title string) {
		title = strings.Join(strings.Fields(title), " ")
		low := strings.ToLower(title)
		if _, dup := seen[low]; title == "" || dup || len(out) >= maxAliases {
			return
		}
		seen[low] = struct{}{}
		out = append(out, foreigntitle.LanguageTitle{Language: rule, Title: title, Source: Source})
	}
	if tr != nil {
		add(tr.Name)
		for i := range min(len(tr.Aliases), maxAliases) {
			add(tr.Aliases[i])
		}
	}
	for i := range min(len(aliases), maxAliases) {
		if strings.EqualFold(aliases[i].Language, lang) {
			add(aliases[i].Name)
		}
	}
	return out
}

// ownTransport is a private clone of the default transport, so closing idle connections
// elsewhere in the process cannot break an in-flight TVDB request.
func ownTransport() http.RoundTripper {
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		return base.Clone()
	}
	return http.DefaultTransport
}
