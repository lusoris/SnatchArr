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
package config

import (
	"errors"
	"fmt"
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
	}
}

// Load reads the "snatcharr" subtree and validates it for the selected profile. In prod
// it also insists on golusoris' crypto.key (APP_CRYPTO_KEY): without it *arr API keys
// would be sealed with a built-in development key.
func Load(cfg *config.Config) (Options, error) {
	o := Default()
	if err := cfg.Unmarshal("snatcharr", &o); err != nil {
		return Options{}, fmt.Errorf("config: unmarshal snatcharr: %w", err)
	}
	if err := o.Validate(); err != nil {
		return Options{}, err
	}
	if o.Profile == ProfileProd && cfg.String("crypto.key") == "" {
		return Options{}, fmt.Errorf("%w: crypto.key (APP_CRYPTO_KEY, hex 16/24/32 bytes)", ErrMissingSecret)
	}
	return o, nil
}

// Validate applies cross-field rules. Dev relaxes the secret requirements so a first
// `make dev` boots without ceremony.
func (o Options) Validate() error {
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

// Module provides Options to the fx graph.
var Module = fx.Module("snatcharr.config", fx.Provide(Load))
