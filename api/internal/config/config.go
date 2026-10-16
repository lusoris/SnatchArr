// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package config holds SnatchArr's own settings, loaded from the golusoris config
// tree under the "snatcharr" key (env prefix APP_SNATCHARR_*).
//
// Keys (env):
//
//	snatcharr.profile            APP_SNATCHARR_PROFILE            prod | dev (default prod)
//	snatcharr.public.url         APP_SNATCHARR_PUBLIC_URL         external base URL (for cookies/links)
//	snatcharr.web.dev            APP_SNATCHARR_WEB_DEV            disable the embedded SPA (Vite proxies instead)
//	snatcharr.worker.token       APP_SNATCHARR_WORKER_TOKEN       shared bearer the snatch-worker presents over gRPC
//	snatcharr.configarr.config   APP_SNATCHARR_CONFIGARR_CONFIG   path to Configarr config.yml (linked mode)
//	snatcharr.configarr.secrets  APP_SNATCHARR_CONFIGARR_SECRETS  path to Configarr secrets.yml
//	snatcharr.configarr.watch    APP_SNATCHARR_CONFIGARR_WATCH    re-import on file change (default true)
//	snatcharr.session.ttl        APP_SNATCHARR_SESSION_TTL        cookie session lifetime (default 8h)
//	snatcharr.session.secure     APP_SNATCHARR_SESSION_SECURE     Secure cookie flag (default true)
//	snatcharr.apikey.secret      APP_SNATCHARR_APIKEY_SECRET      HMAC secret for API keys (required in prod)
//	snatcharr.proxy.trusted      APP_SNATCHARR_PROXY_TRUSTED      comma-separated trusted reverse proxy CIDRs
//	snatcharr.setup.secret       APP_SNATCHARR_SETUP_SECRET       bootstrap secret for first-run setup
//	snatcharr.oidc.enabled       APP_SNATCHARR_OIDC_ENABLED       enable OIDC SSO login
//	snatcharr.oidc.issuer_url    APP_SNATCHARR_OIDC_ISSUER_URL    OIDC issuer URL (https, or http in dev)
//	snatcharr.oidc.client_id     APP_SNATCHARR_OIDC_CLIENT_ID     OAuth2 client ID
//	snatcharr.oidc.client_secret APP_SNATCHARR_OIDC_CLIENT_SECRET OAuth2 client secret
//	snatcharr.oidc.redirect_url  APP_SNATCHARR_OIDC_REDIRECT_URL  OAuth2 callback URL
//	snatcharr.oidc.scopes        APP_SNATCHARR_OIDC_SCOPES        comma-separated scopes (default openid,email,profile)
//	snatcharr.oidc.allowed_subjects APP_SNATCHARR_OIDC_ALLOWED_SUBJECTS comma-separated allowed sub claims
//	snatcharr.oidc.allowed_emails   APP_SNATCHARR_OIDC_ALLOWED_EMAILS   comma-separated allowed emails
//	snatcharr.oidc.discovery_timeout APP_SNATCHARR_OIDC_DISCOVERY_TIMEOUT discovery timeout (default 10s)
//	snatcharr.proxy_auth.enabled APP_SNATCHARR_PROXY_AUTH_ENABLED enable trusted proxy-header auth
//	snatcharr.proxy_auth.header  APP_SNATCHARR_PROXY_AUTH_HEADER  trusted proxy user header name (default Remote-User)
//	snatcharr.proxy_auth.trusted_cidrs APP_SNATCHARR_PROXY_AUTH_TRUSTED_CIDRS comma-separated trusted proxy CIDRs
//	snatcharr.proxy_auth.allowed_users APP_SNATCHARR_PROXY_AUTH_ALLOWED_USERS comma-separated allowed proxy users
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
	"time"

	"go.uber.org/fx"

	"github.com/golusoris/golusoris/core/config"
)

// Profile selects the runtime wiring.
type Profile string

// Profiles.
const (
	ProfileProd Profile = "prod"
	ProfileDev  Profile = "dev"
)

// Options is SnatchArr's typed configuration.
type Options struct {
	Profile   Profile          `koanf:"profile"`
	Public    PublicOptions    `koanf:"public"`
	Web       WebOptions       `koanf:"web"`
	Worker    WorkerOptions    `koanf:"worker"`
	Configarr ConfigarrOptions `koanf:"configarr"`
	Session   SessionOptions   `koanf:"session"`
	APIKey    APIKeyOptions    `koanf:"apikey"`
	Proxy     ProxyOptions     `koanf:"proxy"`
	Setup     SetupOptions     `koanf:"setup"`
	OIDC      OIDCOptions      `koanf:"oidc"`
	ProxyAuth ProxyAuthOptions `koanf:"proxy_auth"`
	Prowlarr  ProwlarrOptions  `koanf:"prowlarr"`
}

// ProwlarrOptions configures the Prowlarr link and planner gate.
type ProwlarrOptions struct {
	LimitThreshold float64 `koanf:"limit_threshold"`
}

// Validate checks that the limit threshold is within (0, 1].
func (p ProwlarrOptions) Validate() error {
	if p.LimitThreshold <= 0 || p.LimitThreshold > 1 {
		return fmt.Errorf("config: prowlarr.limit_threshold must be between 0 (exclusive) and 1 (inclusive), got %v", p.LimitThreshold)
	}
	return nil
}

// SetupOptions configure the first-run setup wizard.
type SetupOptions struct {
	// Secret is an optional bootstrap secret required to create the first admin user.
	Secret string `koanf:"secret"`
}

// ProxyOptions configures reverse proxy ingress trust.
type ProxyOptions struct {
	// Trusted is a list of CIDR ranges (e.g. "10.0.0.0/8") trusted to supply
	// X-Forwarded-For headers. An empty slice trusts nothing.
	Trusted []string `koanf:"trusted"`
}

// Validate checks that each configured trusted proxy parses as a CIDR.
func (p ProxyOptions) Validate() error {
	for _, entry := range p.Trusted {
		trimmed := strings.TrimSpace(entry)
		if trimmed == "" {
			continue
		}
		if _, _, err := net.ParseCIDR(trimmed); err != nil {
			return fmt.Errorf("config: invalid trusted proxy CIDR %q: %w", entry, err)
		}
	}
	return nil
}

// OIDCOptions configures OpenID Connect authentication.
type OIDCOptions struct {
	Enabled          bool          `koanf:"enabled"`
	IssuerURL        string        `koanf:"issuer_url"`
	ClientID         string        `koanf:"client_id"`
	ClientSecret     string        `koanf:"client_secret"`
	RedirectURL      string        `koanf:"redirect_url"`
	Scopes           []string      `koanf:"scopes"`
	AllowedSubjects  []string      `koanf:"allowed_subjects"`
	AllowedEmails    []string      `koanf:"allowed_emails"`
	DiscoveryTimeout time.Duration `koanf:"discovery_timeout"`
}

// Validate checks OIDC configuration when enabled.
func (o OIDCOptions) Validate(profile Profile) error {
	if !o.Enabled {
		return nil
	}
	if err := validateOIDCIssuer(o.IssuerURL, profile); err != nil {
		return err
	}
	if o.ClientID == "" {
		return errors.New("config: oidc.client_id is required when oidc is enabled")
	}
	if err := validateOIDCRedirect(o.RedirectURL); err != nil {
		return err
	}
	if len(o.AllowedSubjects) == 0 && len(o.AllowedEmails) == 0 {
		return errors.New("config: oidc requires at least one allowed subject or allowed email")
	}
	return nil
}

func validateOIDCIssuer(issuerURL string, profile Profile) error {
	if issuerURL == "" {
		return errors.New("config: oidc.issuer_url is required when oidc is enabled")
	}
	u, err := url.Parse(issuerURL)
	if err != nil || u.Host == "" {
		return fmt.Errorf("config: invalid oidc.issuer_url %q", issuerURL)
	}
	if profile == ProfileProd && u.Scheme != "https" {
		return fmt.Errorf("config: oidc.issuer_url must use https in production, got %q", issuerURL)
	}
	if profile == ProfileDev && u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("config: oidc.issuer_url must use http or https, got %q", issuerURL)
	}
	return nil
}

func validateOIDCRedirect(redirectURL string) error {
	if redirectURL == "" {
		return errors.New("config: oidc.redirect_url is required when oidc is enabled")
	}
	ru, err := url.Parse(redirectURL)
	if err != nil || ru.Host == "" || (ru.Scheme != "https" && ru.Scheme != "http") {
		return fmt.Errorf("config: invalid oidc.redirect_url %q", redirectURL)
	}
	return nil
}

// ProxyAuthOptions configures trusted reverse-proxy header authentication.
type ProxyAuthOptions struct {
	Enabled      bool     `koanf:"enabled"`
	Header       string   `koanf:"header"`
	TrustedCIDRs []string `koanf:"trusted_cidrs"`
	AllowedUsers []string `koanf:"allowed_users"`
}

// Validate checks proxy auth configuration when enabled.
func (p ProxyAuthOptions) Validate() error {
	if !p.Enabled {
		return nil
	}
	if strings.TrimSpace(p.Header) == "" || !isValidHeaderToken(p.Header) {
		return fmt.Errorf("config: proxy_auth.header must be a valid HTTP header token, got %q", p.Header)
	}
	if err := validateProxyCIDRs(p.TrustedCIDRs); err != nil {
		return err
	}
	return validateProxyUsers(p.AllowedUsers)
}

func validateProxyCIDRs(cidrs []string) error {
	if len(cidrs) == 0 {
		return errors.New("config: proxy_auth.trusted_cidrs requires at least one CIDR when proxy auth is enabled")
	}
	for _, entry := range cidrs {
		trimmed := strings.TrimSpace(entry)
		if trimmed == "" {
			continue
		}
		_, ipnet, err := net.ParseCIDR(trimmed)
		if err != nil {
			return fmt.Errorf("config: invalid proxy_auth CIDR %q: %w", entry, err)
		}
		ones, _ := ipnet.Mask.Size()
		if ones == 0 {
			return fmt.Errorf("config: proxy_auth refuses catch-all CIDR %q", entry)
		}
	}
	return nil
}

func validateProxyUsers(users []string) error {
	if len(users) == 0 {
		return errors.New("config: proxy_auth.allowed_users requires at least one user when proxy auth is enabled")
	}
	for _, u := range users {
		if strings.TrimSpace(u) == "" {
			return errors.New("config: proxy_auth.allowed_users must not contain empty users")
		}
	}
	return nil
}

func isValidHeaderToken(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if !isTokenByte(s[i]) {
			return false
		}
	}
	return true
}

func isTokenByte(c byte) bool {
	if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
		return true
	}
	return strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0
}

// PublicOptions describe how the service is reached from outside.
type PublicOptions struct {
	URL string `koanf:"url"`
}

// WebOptions toggle the embedded SPA.
type WebOptions struct {
	Dev bool `koanf:"dev"`
}

// WorkerOptions configure the gRPC worker trust boundary.
type WorkerOptions struct {
	Token string `koanf:"token"`
}

// ConfigarrOptions link SnatchArr to a Configarr configuration.
type ConfigarrOptions struct {
	Config  string `koanf:"config"`
	Secrets string `koanf:"secrets"`
	Watch   bool   `koanf:"watch"`
}

// Linked reports whether linked mode is on.
func (c ConfigarrOptions) Linked() bool { return c.Config != "" }

// SessionOptions tune cookie sessions.
type SessionOptions struct {
	TTL    time.Duration `koanf:"ttl"`
	Secure bool          `koanf:"secure"`
}

// APIKeyOptions tune API-key issuance.
type APIKeyOptions struct {
	Secret string `koanf:"secret"`
}

// ErrMissingSecret is returned when a production deployment lacks a required secret.
var ErrMissingSecret = errors.New("config: required secret is empty")

// Default returns the defaults applied before config is unmarshalled over them.
func Default() Options {
	return Options{
		Profile:   ProfileProd,
		Configarr: ConfigarrOptions{Watch: true},
		Session:   SessionOptions{TTL: 8 * time.Hour, Secure: true},
		OIDC: OIDCOptions{
			Scopes:           []string{"openid", "email", "profile"},
			DiscoveryTimeout: 10 * time.Second,
		},
		ProxyAuth: ProxyAuthOptions{
			Header: "Remote-User",
		},
		Prowlarr: ProwlarrOptions{
			LimitThreshold: 0.9,
		},
	}
}

// Load reads the "snatcharr" subtree and validates it for the selected profile, including
// which crypto.key the profile may run on (CryptoKey).
func Load(cfg *config.Config) (Options, error) {
	o := Default()
	if err := cfg.Unmarshal("snatcharr", &o); err != nil {
		return Options{}, fmt.Errorf("config: unmarshal snatcharr: %w", err)
	}
	o.Proxy.Trusted = cleanTrusted(o.Proxy.Trusted)
	o.ProxyAuth.TrustedCIDRs = cleanTrusted(o.ProxyAuth.TrustedCIDRs)
	o.ProxyAuth.AllowedUsers = cleanTrusted(o.ProxyAuth.AllowedUsers)
	o.OIDC.AllowedSubjects = cleanTrusted(o.OIDC.AllowedSubjects)
	o.OIDC.AllowedEmails = cleanTrusted(o.OIDC.AllowedEmails)
	o.OIDC.Scopes = cleanTrusted(o.OIDC.Scopes)
	if len(o.OIDC.Scopes) == 0 {
		o.OIDC.Scopes = []string{"openid", "email", "profile"}
	} else if !slices.Contains(o.OIDC.Scopes, "openid") {
		o.OIDC.Scopes = append([]string{"openid"}, o.OIDC.Scopes...)
	}
	if o.OIDC.DiscoveryTimeout <= 0 {
		o.OIDC.DiscoveryTimeout = 10 * time.Second
	}
	if o.OIDC.RedirectURL == "" && o.Public.URL != "" {
		o.OIDC.RedirectURL = strings.TrimRight(o.Public.URL, "/") + "/api/v1/auth/oidc/callback"
	}
	if o.ProxyAuth.Header == "" {
		o.ProxyAuth.Header = "Remote-User"
	}
	if err := o.Validate(); err != nil {
		return Options{}, err
	}
	if _, _, err := o.CryptoKey(cfg.String("crypto.key")); err != nil {
		return Options{}, err
	}
	return o, nil
}

func cleanTrusted(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		for part := range strings.SplitSeq(item, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// DevCryptoKey is the development key the dev profile falls back to when crypto.key is
// empty (hex of the 32 bytes "snatcharr-development-key-000000"). It is published in this
// repository, so anything sealed with it is unprotected. Prod refuses it.
const DevCryptoKey = "736e617463686172722d646576656c6f706d656e742d6b65792d303030303030"

// ErrDevelopmentKey is returned when a production deployment is configured with the
// published development key.
var ErrDevelopmentKey = errors.New("config: crypto.key is the published development key")

// CryptoKey decides which key seals *arr API keys and download-client secrets, given the
// configured crypto.key (APP_CRYPTO_KEY, hex 16/24/32 bytes). Only the explicit dev profile
// may run on DevCryptoKey; development reports that, so the caller can warn. Prod, which is
// also the default profile, refuses an empty key and the development key.
func (o Options) CryptoKey(configured string) (key string, development bool, err error) {
	isDevKey := strings.EqualFold(configured, DevCryptoKey)
	switch {
	case o.Profile == ProfileDev && (configured == "" || isDevKey):
		return DevCryptoKey, true, nil
	case configured == "":
		return "", false, fmt.Errorf("%w: crypto.key (APP_CRYPTO_KEY, hex 16/24/32 bytes)", ErrMissingSecret)
	case isDevKey:
		return "", false, fmt.Errorf("%w: set APP_CRYPTO_KEY to a key of your own (openssl rand -hex 32)", ErrDevelopmentKey)
	default:
		return configured, false, nil
	}
}

// Validate applies cross-field rules. Dev relaxes the secret requirements so a first
// `make dev` boots without ceremony.
func (o Options) Validate() error {
	if err := o.validateBasics(); err != nil {
		return err
	}
	if err := o.OIDC.Validate(o.Profile); err != nil {
		return err
	}
	if err := o.ProxyAuth.Validate(); err != nil {
		return err
	}
	if err := o.Prowlarr.Validate(); err != nil {
		return err
	}
	return o.Proxy.Validate()
}

func (o Options) validateBasics() error {
	switch o.Profile {
	case ProfileProd, ProfileDev:
	default:
		return fmt.Errorf("config: profile must be prod or dev, got %q", o.Profile)
	}
	if o.Session.TTL < time.Minute || o.Session.TTL > 30*24*time.Hour {
		return fmt.Errorf("config: session.ttl must be between 1m and 720h, got %s", o.Session.TTL)
	}
	if o.Profile == ProfileProd {
		if o.Worker.Token == "" {
			return fmt.Errorf("%w: snatcharr.worker.token", ErrMissingSecret)
		}
		if o.APIKey.Secret == "" {
			return fmt.Errorf("%w: snatcharr.apikey.secret", ErrMissingSecret)
		}
	}
	if (o.Configarr.Config == "") != (o.Configarr.Secrets == "") && o.Configarr.Secrets != "" {
		return errors.New("config: configarr.secrets requires configarr.config")
	}
	return nil
}

// CompoundKeys lists the snatcharr.* keys whose names contain underscores. Pass
// them to config.Options.CompoundKeys so APP_SNATCHARR_* variables reach them.
func CompoundKeys() []string {
	return []string{
		"snatcharr.oidc.issuer_url",
		"snatcharr.oidc.client_id",
		"snatcharr.oidc.client_secret",
		"snatcharr.oidc.redirect_url",
		"snatcharr.oidc.allowed_subjects",
		"snatcharr.oidc.allowed_emails",
		"snatcharr.oidc.discovery_timeout",
		"snatcharr.proxy_auth.enabled",
		"snatcharr.proxy_auth.header",
		"snatcharr.proxy_auth.trusted_cidrs",
		"snatcharr.proxy_auth.allowed_users",
		"snatcharr.prowlarr.limit_threshold",
	}
}

// Module provides Options to the fx graph.
var Module = fx.Module("snatcharr.config", fx.Provide(Load))
