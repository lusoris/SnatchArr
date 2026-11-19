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
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golusoris/goenvoy/metadata"
	tvdb "github.com/golusoris/goenvoy/metadata/video/tvdb"
	"github.com/redis/rueidis"
	"golang.org/x/text/language"

	"github.com/golusoris/golusoris/cache/memory"
	rediscache "github.com/golusoris/golusoris/cache/redis"
	"github.com/golusoris/golusoris/cache/twotier"
	"github.com/golusoris/golusoris/core/clock"
	httpclient "github.com/golusoris/golusoris/httpx/client"
	"github.com/golusoris/golusoris/httpx/httpcache"
	"github.com/golusoris/golusoris/httpx/ratelimit"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/foreigntitle"
	"github.com/lusoris/SnatchArr/api/internal/metrics"
)

// Source names TVDB as the origin of a LanguageTitle.
const Source = "tvdb"

const (
	// PositiveTTL is the freshness of every storable TVDB response.
	PositiveTTL = 24 * time.Hour

	maxAliases  = 200
	callTimeout = 20 * time.Second
	// lookupTimeout bounds one load from its start: the wait for the slot plus at most
	// four calls (two remote-id searches, the movie, its translation) of callTimeout each.
	lookupTimeout = 4*callTimeout + 10*time.Second
	maxBodyBytes  = 4 << 20
	// maxScan bounds scans without truncating a decodable response: an array of N smallest
	// {} entries needs 3N+1 bytes ([{},{},…]), so maxBodyBytes cannot hold more than maxScan.
	maxScan = maxBodyBytes / 3
	// TVDB publishes no rate limit (api docs, checked 2026-11-13); this is a conservative
	// ceiling on every real HTTP request, retries and logins included.
	defaultRate = "2-S"
	retryMax    = 2

	// loginFailedMessage is the fixed text goenvoy puts on a refused /login answer. goenvoy
	// exports no sentinel for it yet (https://github.com/golusoris/goenvoy/issues/323);
	// switch to errors.Is once it does.
	loginFailedMessage = "login failed"
	cachePrefix        = "metadata.tvdb.http"
	defaultBaseURL     = "https://api4.thetvdb.com/v4"
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
	// errRedirect keeps redirect destinations and credentials out of error text.
	errRedirect = errors.New("tvdb: redirect refused")
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
	baseURL     string
	rate        string
	retryWait   time.Duration
	userAgent   string
	tlsConfig   *tls.Config
	l2          string
	redis       rediscache.Options
	redisClient rueidis.Client
	shared      bool
	metrics     *metrics.Metrics
}

// WithBaseURL points the provider at another API root (tests).
func WithBaseURL(u string) Option { return func(c *config) { c.baseURL = u } }

// WithRate replaces the outbound rate string (tests); empty means unlimited.
func WithRate(formatted string) Option { return func(c *config) { c.rate = formatted } }

// WithRetryWait replaces the first retry back-off (tests).
func WithRetryWait(d time.Duration) Option { return func(c *config) { c.retryWait = d } }

// WithUserAgent sets the User-Agent header of every TVDB request.
func WithUserAgent(ua string) Option { return func(c *config) { c.userAgent = ua } }

func withTLSConfig(tlsConfig *tls.Config) Option {
	return func(c *config) { c.tlsConfig = tlsConfig }
}

// WithL2 selects the metadata cache L2 and supplies the shared cache.redis.* options.
func WithL2(mode string, opts rediscache.Options) Option {
	return func(c *config) { c.l2, c.redis = mode, opts }
}

// WithMetrics records cache response statuses.
func WithMetrics(m *metrics.Metrics) Option { return func(c *config) { c.metrics = m } }

func withRedisClient(client rueidis.Client) Option {
	return func(c *config) { c.redisClient = client }
}

func withSharedHost(shared bool) Option { return func(c *config) { c.shared = shared } }

// Provider reads translated titles from TheTVDB. A nil or disabled Provider answers every
// call with no titles and makes no network request.
type Provider struct {
	enabled bool
	client  *tvdb.Client
	http    *http.Client
	logger  *slog.Logger
	cache   *httpcache.Cache
	redis   rueidis.Client
	// slot is a one-request semaphore around goenvoy calls: the client rewrites its
	// request auth in shared state on every call (https://github.com/golusoris/goenvoy/issues/321).
	// A channel instead of a mutex lets a waiting lookup give up at its context deadline.
	slot chan struct{}
	// lookup is lookupTimeout; a field so a test can wait on it without the full bound.
	lookup   time.Duration
	failed   atomic.Bool
	closed   sync.Once
	closeErr error
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
	cfg := config{
		rate: defaultRate, l2: twotier.L2None, shared: true,
		redis: rediscache.Options{Addr: "localhost:6379"},
	}
	for _, o := range opts {
		o(&cfg)
	}
	host, err := cacheHost(cfg.baseURL)
	if err != nil {
		return nil, err
	}
	outbound, err := ratelimit.NewOutbound(ratelimit.TransportOptions{Rate: cfg.rate, MaxWait: callTimeout}, clk)
	if err != nil {
		return nil, fmt.Errorf("tvdb: rate limit: %w", err)
	}
	cache, redisClient, err := newHTTPCache(l1, logger, host, cfg)
	if err != nil {
		return nil, err
	}
	transport := cache.Wrap(outbound.Wrap(&capTransport{next: ownTransport(cfg.tlsConfig), max: maxBodyBytes}))
	transport = &cacheMetricsTransport{next: transport, metrics: cfg.metrics}
	hc := httpclient.New(httpclient.Options{
		Name:      "snatcharr.metadata.tvdb",
		Timeout:   callTimeout,
		Retry:     httpclient.RetryOptions{Max: retryMax, Wait: cfg.retryWait, MaxWait: 8 * time.Second},
		Logger:    logger,
		Transport: transport,
	})
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return errRedirect }
	client := newTVDBClient(apiKey, pin, cfg, hc)
	return &Provider{
		enabled: true, client: client, http: hc, logger: logger,
		cache: cache, redis: redisClient, slot: make(chan struct{}, 1), lookup: lookupTimeout,
	}, nil
}

func newTVDBClient(apiKey, pin string, cfg config, hc *http.Client) *tvdb.Client {
	mopts := []metadata.Option{metadata.WithHTTPClient(hc)}
	if cfg.userAgent != "" {
		mopts = append(mopts, metadata.WithUserAgent(cfg.userAgent))
	}
	if cfg.baseURL != "" {
		mopts = append(mopts, metadata.WithBaseURL(cfg.baseURL))
	}
	if pin != "" {
		return tvdb.NewWithPIN(apiKey, pin, mopts...)
	}
	return tvdb.New(apiKey, mopts...)
}

func cacheHost(baseURL string) (string, error) {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme == "" || u.Hostname() == "" {
		return "", errors.New("tvdb: invalid base URL")
	}
	return strings.ToLower(u.Hostname()), nil
}

func newHTTPCache(l1 *memory.Cache, logger *slog.Logger, host string, cfg config) (*httpcache.Cache, rueidis.Client, error) {
	store, redisClient, err := newCacheStore(l1, logger, cfg)
	if err != nil {
		return nil, nil, err
	}
	opts := httpcache.DefaultOptions()
	opts.Prefix = cachePrefix
	opts.MaxBodyBytes = maxBodyBytes
	opts.StaleIfError = 0
	opts.StaleWhileRevalidate = 0
	opts.Store.Retention = PositiveTTL
	opts.Store.L2 = cfg.l2
	opts.Hosts = []httpcache.HostOptions{{Host: host, TTL: PositiveTTL, Shared: cfg.shared}}
	cache, err := httpcache.New(opts, store, logger)
	if err != nil {
		if redisClient != nil {
			redisClient.Close()
		}
		return nil, nil, fmt.Errorf("tvdb: HTTP cache: %w", err)
	}
	return cache, redisClient, nil
}

func newCacheStore(l1 *memory.Cache, logger *slog.Logger, cfg config) (*twotier.TwoTier, rueidis.Client, error) {
	if cfg.l2 != twotier.L2None && cfg.l2 != twotier.L2Redis {
		return nil, nil, fmt.Errorf("tvdb: cache store: unsupported L2 %q", cfg.l2)
	}
	ttl := 30 * time.Second
	var options []twotier.Option
	client := cfg.redisClient
	if cfg.l2 == twotier.L2None {
		ttl = PositiveTTL
	} else if client == nil {
		var err error
		client, err = rueidis.NewClient(redisClientOption(cfg.redis))
		if err != nil {
			return nil, nil, fmt.Errorf("tvdb: Redis client: %w", err)
		}
	}
	if cfg.l2 != twotier.L2None {
		options = append(options, twotier.WithRedis(client))
	}
	store, err := twotier.New(l1, twotier.Options{L1TTL: ttl, L2TTL: PositiveTTL, L2: cfg.l2}, logger, options...)
	if err != nil {
		if client != nil {
			client.Close()
		}
		return nil, nil, fmt.Errorf("tvdb: cache store: %w", err)
	}
	return store, client, nil
}

func redisClientOption(opts rediscache.Options) rueidis.ClientOption {
	addrs := strings.Split(opts.Addr, ",")
	for i := range addrs {
		addrs[i] = strings.TrimSpace(addrs[i])
	}
	clientOpts := rueidis.ClientOption{
		InitAddress: addrs, Username: opts.Username, Password: opts.Password, SelectDB: opts.DB,
	}
	if opts.TLS {
		clientOpts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return clientOpts
}

// Close releases the HTTP cache registration, idle connections and optional Redis client.
func (p *Provider) Close() error {
	if p == nil {
		return nil
	}
	p.closed.Do(func() {
		if p.http != nil {
			p.http.CloseIdleConnections()
		}
		if p.cache != nil {
			p.closeErr = p.cache.Close()
		}
		if p.redis != nil {
			p.redis.Close()
		}
	})
	return p.closeErr
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
	if !hasLookup(ref) {
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
	return p.load(ctx, ref, lang, rule)
}

// hasLookup reports whether the kind and identifiers give TVDB something to ask.
func hasLookup(ref Ref) bool {
	switch ref.Kind {
	case domain.KindSonarr:
		if ref.TVDBID > 0 {
			return true
		}
	case domain.KindRadarr:
		if id := strings.TrimSpace(ref.IMDBID); id != "" {
			return true
		}
		if ref.TMDBID > 0 {
			return true
		}
	case domain.KindLidarr, domain.KindReadarr, domain.KindWhisparrV2, domain.KindWhisparrV3, domain.KindStash:
		// TVDB holds no music, books or adult items.
	}
	return false
}

// load holds the provider slot. Its deadline starts before the slot wait, so a caller
// without one still gives up (HISS-02). The HTTP cache singleflights identical GETs.
func (p *Provider) load(ctx context.Context, ref Ref, lang, rule string) ([]foreigntitle.LanguageTitle, error) {
	ctx, cancel := context.WithTimeout(ctx, p.lookup)
	defer cancel()
	select {
	case p.slot <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, ctx.Err())
	}
	defer func() { <-p.slot }()
	if p.failed.Load() {
		return nil, ErrUnauthorized
	}
	var titles []foreigntitle.LanguageTitle
	var err error
	if ref.Kind == domain.KindSonarr {
		titles, err = p.seriesTitles(ctx, ref.TVDBID, lang, rule)
	} else {
		titles, err = p.movieTitles(ctx, ref, lang, rule)
	}
	if errors.Is(err, errNotFound) {
		return nil, nil
	}
	return titles, err
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
		if id, ok := firstMovieID(res); ok {
			return id, nil
		}
	}
	return 0, errNotFound
}

func firstMovieID(results []tvdb.SearchByRemoteIDResult) (int, bool) {
	for i := range min(len(results), maxScan) {
		if movie := results[i].Movie; movie != nil && movie.ID > 0 {
			return movie.ID, true
		}
	}
	return 0, false
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
		for i := range min(len(tr.Aliases), maxScan) {
			if len(out) >= maxAliases {
				break
			}
			add(tr.Aliases[i])
		}
	}
	for i := range min(len(aliases), maxScan) {
		if len(out) >= maxAliases {
			break
		}
		if strings.EqualFold(aliases[i].Language, lang) {
			add(aliases[i].Name)
		}
	}
	return out
}

// ownTransport is a private clone of the default transport, so closing idle connections
// elsewhere in the process cannot break an in-flight TVDB request.
func ownTransport(tlsConfig *tls.Config) http.RoundTripper {
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		transport := base.Clone()
		if tlsConfig != nil {
			transport.TLSClientConfig = tlsConfig.Clone()
		}
		return transport
	}
	return http.DefaultTransport
}
