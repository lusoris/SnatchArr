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
	"net"
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
	// lookupTimeout bounds one load from its start and at most four upstream calls
	// of callTimeout each.
	lookupTimeout    = 4*callTimeout + 10*time.Second
	closeWaitTimeout = 1500 * time.Millisecond
	// maxDetached bounds lookups that continue after their caller leaves.
	maxDetached = 16
	// storeTimeout bounds Redis I/O and stays below closeWaitTimeout so shutdown can settle it.
	storeTimeout = time.Second
	maxBodyBytes = 4 << 20
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
	maxRedisAddresses  = 16
	redisPlaintextKey  = "snatcharr.metadata.cache.redis_plaintext"
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
	// errRedisNotLoopback rejects plaintext credentialed Redis connections outside loopback.
	errRedisNotLoopback = errors.New("tvdb: Redis plaintext connection is not loopback")
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
	baseURL        string
	rate           string
	retryWait      time.Duration
	userAgent      string
	tlsConfig      *tls.Config
	l2             string
	redis          rediscache.Options
	redisClient    rueidis.Client
	redisPlaintext bool
	shared         bool
	metrics        *metrics.Metrics
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

// WithRedisPlaintext explicitly permits a password over an unencrypted remote Redis connection.
func WithRedisPlaintext(allowed bool) Option {
	return func(c *config) { c.redisPlaintext = allowed }
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
	enabled   bool
	client    *tvdb.Client
	http      *http.Client
	transport *http.Transport
	logger    *slog.Logger
	cache     *httpcache.Cache
	redis     rueidis.Client
	loginMu   sync.Mutex
	loggedIn  bool
	detached  chan struct{}
	// lookup is lookupTimeout; a field so a test can wait on it without the full bound.
	lookup time.Duration
	// fetchStart is a test hook called as fetch starts; it is nil in production.
	fetchStart   func(context.Context)
	lifetimeDone <-chan struct{}
	cancel       context.CancelFunc
	failed       atomic.Bool
	closed       sync.Once
	closeErr     error
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
	basePath, err := cacheBasePath(cfg.baseURL)
	if err != nil {
		return nil, err
	}
	outbound, err := ratelimit.NewOutbound(ratelimit.TransportOptions{Rate: cfg.rate, MaxWait: callTimeout}, clk)
	if err != nil {
		return nil, fmt.Errorf("tvdb: rate limit: %w", err)
	}
	lifetime, cancel := context.WithCancel(context.Background())
	p := &Provider{
		detached: make(chan struct{}, maxDetached), lookup: lookupTimeout,
		lifetimeDone: lifetime.Done(), cancel: cancel,
	}
	bounds := redisBounds{lifetime: func() context.Context { return lifetime }}
	cache, redisClient, err := newHTTPCache(l1, logger, host, cfg, bounds)
	if err != nil {
		cancel()
		return nil, err
	}
	baseTransport, ownedTransport := ownTransport(cfg.tlsConfig)
	transport := cache.Wrap(&originGuard{next: outbound.Wrap(&capTransport{next: baseTransport, max: maxBodyBytes}), basePath: basePath})
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
	p.enabled, p.client, p.http, p.transport, p.logger = true, client, hc, ownedTransport, logger
	p.cache, p.redis = cache, redisClient
	return p, nil
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

func cacheBasePath(baseURL string) (string, error) {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme == "" || u.Hostname() == "" {
		return "", errors.New("tvdb: invalid base URL")
	}
	return strings.TrimRight(u.Path, "/"), nil
}

func newHTTPCache(l1 *memory.Cache, logger *slog.Logger, host string, cfg config, bounds redisBounds) (*httpcache.Cache, rueidis.Client, error) {
	store, redisClient, err := newCacheStore(l1, logger, cfg, bounds)
	if err != nil {
		return nil, nil, err
	}
	opts := httpCacheOptions(host, cfg)
	cache, err := httpcache.New(opts, store, logger)
	if err != nil {
		if redisClient != nil {
			redisClient.Close()
		}
		return nil, nil, fmt.Errorf("tvdb: HTTP cache: %w", err)
	}
	return cache, redisClient, nil
}

func httpCacheOptions(host string, cfg config) httpcache.Options {
	opts := httpcache.DefaultOptions()
	opts.Prefix = cachePrefix
	opts.MaxBodyBytes = maxBodyBytes
	opts.StoreTimeout = storeTimeout
	opts.StaleIfError = 0
	opts.StaleWhileRevalidate = 0
	opts.Store.Retention = PositiveTTL
	opts.Store.L2 = cfg.l2
	opts.Hosts = []httpcache.HostOptions{{Host: host, TTL: PositiveTTL, Shared: cfg.shared}}
	return opts
}

func newCacheStore(l1 *memory.Cache, logger *slog.Logger, cfg config, bounds redisBounds) (*twotier.TwoTier, rueidis.Client, error) {
	if cfg.l2 != twotier.L2None && cfg.l2 != twotier.L2Redis {
		return nil, nil, fmt.Errorf("tvdb: cache store: unsupported L2 %q", cfg.l2)
	}
	ttl := 30 * time.Second
	var options []twotier.Option
	client := cfg.redisClient
	if cfg.l2 == twotier.L2None {
		ttl = PositiveTTL
	} else {
		var err error
		client, err = redisForCache(cfg, logger)
		if err != nil {
			return nil, nil, err
		}
		client = newBoundRedis(client, bounds)
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

func redisForCache(cfg config, logger *slog.Logger) (rueidis.Client, error) {
	clientOpts, err := redisClientOptionForCache(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("tvdb: Redis options: %w", err)
	}
	if cfg.redisClient != nil {
		return cfg.redisClient, nil
	}
	client, err := rueidis.NewClient(clientOpts)
	if err != nil {
		return nil, fmt.Errorf("tvdb: Redis client: %w", err)
	}
	return client, nil
}

func redisClientOptionForCache(cfg config, logger *slog.Logger) (rueidis.ClientOption, error) {
	clientOpts, err := redisClientOption(cfg.redis)
	if err != nil {
		return rueidis.ClientOption{}, err
	}
	guarded := redisNeedsLoopbackGuard(cfg)
	remote := hasRemoteRedisAddress(clientOpts.InitAddress)
	if guarded && remote {
		return rueidis.ClientOption{}, fmt.Errorf("redis password requires TLS or %s", redisPlaintextKey)
	}
	if redisPlaintextWarning(cfg, remote) {
		logger.Warn("tvdb: plaintext Redis password explicitly allowed", "option", redisPlaintextKey)
	}
	if guarded {
		clientOpts.DialCtxFn = loopbackOnlyDial
	}
	return clientOpts, nil
}

func redisNeedsLoopbackGuard(cfg config) bool {
	return cfg.redis.Password != "" && !cfg.redis.TLS && !cfg.redisPlaintext
}

func redisPlaintextWarning(cfg config, remote bool) bool {
	return cfg.redis.Password != "" && !cfg.redis.TLS && cfg.redisPlaintext && remote
}

func redisClientOption(opts rediscache.Options) (rueidis.ClientOption, error) {
	if opts.Addr == "" || strings.Count(opts.Addr, ",")+1 > maxRedisAddresses {
		return rueidis.ClientOption{}, fmt.Errorf("redis address count must be 1..%d", maxRedisAddresses)
	}
	addrs := strings.Split(opts.Addr, ",")
	for i := range addrs {
		addrs[i] = strings.TrimSpace(addrs[i])
		if addrs[i] == "" {
			return rueidis.ClientOption{}, errors.New("empty Redis address")
		}
	}
	clientOpts := rueidis.ClientOption{
		InitAddress: addrs, Username: opts.Username, Password: opts.Password, SelectDB: opts.DB,
		DisableCache: true, // Redis is a plain L2 store; SnatchArr does not use client-side tracking.
	}
	if opts.TLS {
		clientOpts.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return clientOpts, nil
}

func hasRemoteRedisAddress(addrs []string) bool {
	for _, addr := range addrs {
		host, _, err := net.SplitHostPort(addr)
		if err != nil || !isLoopbackHost(host) {
			return true
		}
	}
	return false
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func loopbackOnlyDial(ctx context.Context, dst string, dialer *net.Dialer, tlsConfig *tls.Config) (net.Conn, error) {
	if tlsConfig != nil {
		return nil, fmt.Errorf("%w: TLS configuration is not plaintext", errRedisNotLoopback)
	}
	dstHost, _, err := net.SplitHostPort(dst)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid Redis destination: %w", errRedisNotLoopback, err)
	}
	if !isLoopbackHost(dstHost) {
		return nil, fmt.Errorf("%w: destination is not loopback", errRedisNotLoopback)
	}
	conn, err := dialer.DialContext(ctx, "tcp", dst)
	if err != nil {
		return nil, fmt.Errorf("tvdb: dial loopback Redis: %w", err)
	}
	if !isLoopbackPeer(conn.RemoteAddr()) {
		if closeErr := conn.Close(); closeErr != nil {
			return nil, errors.Join(errRedisNotLoopback, fmt.Errorf("tvdb: close rejected Redis connection: %w", closeErr))
		}
		return nil, fmt.Errorf("%w: peer is not loopback", errRedisNotLoopback)
	}
	return conn, nil
}

func isLoopbackPeer(addr net.Addr) bool {
	peer, ok := addr.(*net.TCPAddr)
	return ok && peer.IP.IsLoopback()
}

type redisBounds struct {
	lifetime func() context.Context
}

// boundRedis preserves the store driver's background context, bounded by storeTimeout and the
// provider lifetime so Redis I/O is independent of any caller lookup context.
type boundRedis struct {
	rueidis.Client
	bounds redisBounds
}

func newBoundRedis(client rueidis.Client, bounds redisBounds) rueidis.Client {
	return &boundRedis{Client: client, bounds: bounds}
}

func (r *boundRedis) Do(ctx context.Context, cmd rueidis.Completed) rueidis.RedisResult {
	ctx, cancel := r.bound(ctx)
	defer cancel()
	return r.Client.Do(ctx, cmd)
}

func (r *boundRedis) DoMulti(ctx context.Context, multi ...rueidis.Completed) []rueidis.RedisResult {
	ctx, cancel := r.bound(ctx)
	defer cancel()
	return r.Client.DoMulti(ctx, multi...)
}

func (r *boundRedis) bound(ctx context.Context) (context.Context, context.CancelFunc) {
	merged, mergedCancel := context.WithCancel(ctx)
	stopLifetime := context.AfterFunc(r.bounds.lifetime(), mergedCancel)
	stopBounds := context.AfterFunc(merged, func() {
		stopLifetime()
	})
	cancel := func() {
		stopBounds()
		stopLifetime()
		mergedCancel()
	}
	return merged, cancel
}

// Close cancels every lookup, waits for detached lookups, then releases the HTTP cache, idle
// connections and Redis client. Its timeout handles only I/O that ignores its context; cache and
// Redis still close so shutdown remains bounded.
func (p *Provider) Close() error {
	if p == nil {
		return nil
	}
	p.closed.Do(func() {
		if p.cancel != nil {
			p.cancel()
		}
		p.closeErr = p.waitDetached()
		if p.transport != nil {
			p.transport.CloseIdleConnections()
		}
		if p.cache != nil {
			p.closeErr = errors.Join(p.closeErr, p.cache.Close())
		}
		if p.redis != nil {
			p.redis.Close()
		}
	})
	return p.closeErr
}

func (p *Provider) waitDetached() error {
	if p.detached == nil {
		return nil
	}
	wait, cancel := context.WithTimeout(context.Background(), closeWaitTimeout)
	defer cancel()
	for range cap(p.detached) {
		select {
		case p.detached <- struct{}{}:
		case <-wait.Done():
			return fmt.Errorf("tvdb: close wait for lookup: %w", wait.Err())
		}
	}
	return nil
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

// load starts a lookup immediately. A caller can leave while an admitted detached lookup fills
// the shared cache; the detached channel bounds those background lookups.
func (p *Provider) load(ctx context.Context, ref Ref, lang, rule string) ([]foreigntitle.LanguageTitle, error) {
	if p.isClosed() {
		return nil, fmt.Errorf("%w: provider closed", ErrUnavailable)
	}
	lookupCtx, cancel, detached, err := p.beginLookup(ctx)
	if err != nil {
		return nil, err
	}
	go func() {
		select {
		case <-p.lifetimeDone:
			cancel()
		case <-lookupCtx.Done():
		}
	}()
	result := make(chan lookupResult, 1)
	go func() {
		defer cancel()
		if detached {
			defer func() { <-p.detached }()
		}
		titles, err := p.fetch(lookupCtx, ref, lang, rule)
		result <- lookupResult{titles: titles, err: err}
	}()
	select {
	case done := <-result:
		return done.titles, done.err
	case <-p.lifetimeDone:
		return nil, fmt.Errorf("%w: provider closed", ErrUnavailable)
	case <-ctx.Done():
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, ctx.Err())
	}
}

func (p *Provider) beginLookup(ctx context.Context) (context.Context, context.CancelFunc, bool, error) {
	detached := false
	select {
	case p.detached <- struct{}{}:
		detached = true
	default:
	}
	lookupBase := ctx
	if detached {
		lookupBase = context.WithoutCancel(ctx)
	}
	lookupCtx, cancel := context.WithTimeout(lookupBase, p.lookup)
	if err := p.lookupStopped(ctx, lookupCtx); err != nil {
		cancel()
		if detached {
			<-p.detached
		}
		return nil, nil, false, err
	}
	return lookupCtx, cancel, detached, nil
}

func (p *Provider) lookupStopped(ctx, lookupCtx context.Context) error {
	if p.isClosed() {
		return fmt.Errorf("%w: provider closed", ErrUnavailable)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	if err := lookupCtx.Err(); err != nil {
		return fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	return nil
}

func (p *Provider) isClosed() bool {
	select {
	case <-p.lifetimeDone:
		return true
	default:
		return false
	}
}

type lookupResult struct {
	titles []foreigntitle.LanguageTitle
	err    error
}

func (p *Provider) fetch(ctx context.Context, ref Ref, lang, rule string) ([]foreigntitle.LanguageTitle, error) {
	if p.fetchStart != nil {
		p.fetchStart(ctx)
	}
	if p.failed.Load() {
		return nil, ErrUnauthorized
	}
	if err := p.call(ctx, p.login); err != nil {
		return nil, err
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

func (p *Provider) login(ctx context.Context) error {
	p.loginMu.Lock()
	defer p.loginMu.Unlock()
	if p.loggedIn {
		return nil
	}
	if err := p.client.Login(ctx); err != nil {
		return fmt.Errorf("tvdb: login: %w", err)
	}
	p.loggedIn = true
	return nil
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
func ownTransport(tlsConfig *tls.Config) (http.RoundTripper, *http.Transport) {
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		transport := base.Clone()
		if tlsConfig != nil {
			transport.TLSClientConfig = tlsConfig.Clone()
		}
		return transport, transport
	}
	return http.DefaultTransport, nil
}
