// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// OwnsSetting reports whether ApplySetting handles key; other registry keys belong to golusoris
// modules, which read them from koanf.
func OwnsSetting(key string) bool {
	return strings.HasPrefix(key, "snatcharr.")
}

// ApplySetting parses val and sets the corresponding field on o for key.
func ApplySetting(o *Options, key, val string) error {
	switch {
	case strings.HasPrefix(key, "snatcharr.oidc."):
		return applyOIDC(o, key, val)
	case strings.HasPrefix(key, "snatcharr.proxy_auth."):
		return applyProxyAuth(o, key, val)
	case strings.HasPrefix(key, "snatcharr.jellyfin."):
		return applyJellyfin(o, key, val)
	case strings.HasPrefix(key, "snatcharr.configarr."):
		return applyConfigarr(o, key, val)
	default:
		return applyCore(o, key, val)
	}
}

func applyCore(o *Options, key, val string) error {
	switch key {
	case "snatcharr.profile":
		o.Profile = Profile(val)
	case "snatcharr.public.url":
		o.Public.URL = val
	case "snatcharr.web.dev":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("%w: invalid bool for %s", domain.ErrInvalid, key)
		}
		o.Web.Dev = b
	case "snatcharr.worker.token":
		o.Worker.Token = val
	case "snatcharr.apikey.secret":
		o.APIKey.Secret = val
	case "snatcharr.setup.secret":
		o.Setup.Secret = val
	case "snatcharr.proxy.trusted":
		o.Proxy.Trusted = cleanTrusted(strings.Split(val, ","))
	case "snatcharr.metrics.addr":
		o.Metrics.Addr = val
	default:
		return applyCoreScalars(o, key, val)
	}
	return nil
}

func applyCoreScalars(o *Options, key, val string) error {
	handled, err := applySessionScalars(o, key, val)
	if err != nil || handled {
		return err
	}
	return applyOperationalScalars(o, key, val)
}

func applySessionScalars(o *Options, key, val string) (bool, error) {
	switch key {
	case "snatcharr.session.ttl":
		d, err := time.ParseDuration(val)
		if err != nil {
			return true, fmt.Errorf("%w: invalid duration for %s", domain.ErrInvalid, key)
		}
		o.Session.TTL = d
		return true, nil
	case "snatcharr.session.secure":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return true, fmt.Errorf("%w: invalid bool for %s", domain.ErrInvalid, key)
		}
		o.Session.Secure = b
		return true, nil
	default:
		return false, nil
	}
}

func applyOperationalScalars(o *Options, key, val string) error {
	switch key {
	case "snatcharr.prowlarr.limit_threshold":
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return fmt.Errorf("%w: invalid float for %s", domain.ErrInvalid, key)
		}
		o.Prowlarr.LimitThreshold = f
	case "snatcharr.item_search.cooldown_seconds":
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("%w: invalid int for %s", domain.ErrInvalid, key)
		}
		o.ItemSearch.CooldownSeconds = n
	case "snatcharr.download_priority.interval_seconds":
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("%w: invalid int for %s", domain.ErrInvalid, key)
		}
		o.DownloadPriority.IntervalSeconds = n
	case "snatcharr.logs.ring_size":
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("%w: invalid int for %s", domain.ErrInvalid, key)
		}
		o.Logs.RingSize = n
	default:
		return fmt.Errorf("%w: unknown option key %q", domain.ErrInvalid, key)
	}
	return nil
}

func applyConfigarr(o *Options, key, val string) error {
	switch key {
	case "snatcharr.configarr.config":
		o.Configarr.Config = val
	case "snatcharr.configarr.secrets":
		o.Configarr.Secrets = val
	case "snatcharr.configarr.download_clients":
		o.Configarr.DownloadClients = val
	case "snatcharr.configarr.policies":
		o.Configarr.Policies = val
	case "snatcharr.configarr.watch":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("%w: invalid bool for %s", domain.ErrInvalid, key)
		}
		o.Configarr.Watch = b
	default:
		return fmt.Errorf("%w: unknown configarr option %q", domain.ErrInvalid, key)
	}
	return nil
}

func applyOIDC(o *Options, key, val string) error {
	switch key {
	case "snatcharr.oidc.enabled":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("%w: invalid bool for %s", domain.ErrInvalid, key)
		}
		o.OIDC.Enabled = b
	case "snatcharr.oidc.issuer_url":
		o.OIDC.IssuerURL = val
	case "snatcharr.oidc.client_id":
		o.OIDC.ClientID = val
	case "snatcharr.oidc.client_secret":
		o.OIDC.ClientSecret = val
	case "snatcharr.oidc.redirect_url":
		o.OIDC.RedirectURL = val
	case "snatcharr.oidc.groups_claim":
		o.OIDC.GroupsClaim = val
	case "snatcharr.oidc.discovery_timeout":
		d, err := time.ParseDuration(val)
		if err != nil {
			return fmt.Errorf("%w: invalid duration for %s", domain.ErrInvalid, key)
		}
		o.OIDC.DiscoveryTimeout = d
	default:
		return applyOIDCLists(o, key, val)
	}
	return nil
}

func applyOIDCLists(o *Options, key, val string) error {
	switch key {
	case "snatcharr.oidc.scopes":
		o.OIDC.Scopes = cleanTrusted(strings.Split(val, ","))
	case "snatcharr.oidc.allowed_subjects":
		o.OIDC.AllowedSubjects = cleanTrusted(strings.Split(val, ","))
	case "snatcharr.oidc.allowed_emails":
		o.OIDC.AllowedEmails = cleanTrusted(strings.Split(val, ","))
	case "snatcharr.oidc.admin_groups":
		o.OIDC.AdminGroups = cleanTrusted(strings.Split(val, ","))
	case "snatcharr.oidc.requester_groups":
		o.OIDC.RequesterGroups = cleanTrusted(strings.Split(val, ","))
	case "snatcharr.oidc.viewer_groups":
		o.OIDC.ViewerGroups = cleanTrusted(strings.Split(val, ","))
	default:
		return fmt.Errorf("%w: unknown oidc option %q", domain.ErrInvalid, key)
	}
	return nil
}

func applyProxyAuth(o *Options, key, val string) error {
	switch key {
	case "snatcharr.proxy_auth.enabled":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("%w: invalid bool for %s", domain.ErrInvalid, key)
		}
		o.ProxyAuth.Enabled = b
	case "snatcharr.proxy_auth.header":
		o.ProxyAuth.Header = val
	case "snatcharr.proxy_auth.groups_header":
		o.ProxyAuth.GroupsHeader = val
	case "snatcharr.proxy_auth.secret_header":
		o.ProxyAuth.SecretHeader = val
	case "snatcharr.proxy_auth.secret":
		o.ProxyAuth.Secret = val
	default:
		return applyProxyAuthLists(o, key, val)
	}
	return nil
}

func applyProxyAuthLists(o *Options, key, val string) error {
	switch key {
	case "snatcharr.proxy_auth.trusted_cidrs":
		o.ProxyAuth.TrustedCIDRs = cleanTrusted(strings.Split(val, ","))
	case "snatcharr.proxy_auth.allowed_users":
		o.ProxyAuth.AllowedUsers = cleanTrusted(strings.Split(val, ","))
	case "snatcharr.proxy_auth.admin_groups":
		o.ProxyAuth.AdminGroups = cleanTrusted(strings.Split(val, ","))
	case "snatcharr.proxy_auth.requester_groups":
		o.ProxyAuth.RequesterGroups = cleanTrusted(strings.Split(val, ","))
	case "snatcharr.proxy_auth.viewer_groups":
		o.ProxyAuth.ViewerGroups = cleanTrusted(strings.Split(val, ","))
	default:
		return fmt.Errorf("%w: unknown proxy_auth option %q", domain.ErrInvalid, key)
	}
	return nil
}

func applyJellyfin(o *Options, key, val string) error {
	switch key {
	case "snatcharr.jellyfin.enabled":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return fmt.Errorf("%w: invalid bool for %s", domain.ErrInvalid, key)
		}
		o.Jellyfin.Enabled = b
	case "snatcharr.jellyfin.url":
		o.Jellyfin.URL = val
	case "snatcharr.jellyfin.api_key":
		o.Jellyfin.APIKey = val
	case "snatcharr.jellyfin.timeout":
		d, err := time.ParseDuration(val)
		if err != nil {
			return fmt.Errorf("%w: invalid duration for %s", domain.ErrInvalid, key)
		}
		o.Jellyfin.Timeout = d
	case "snatcharr.jellyfin.default_role":
		o.Jellyfin.DefaultRole = domain.Role(val)
	default:
		return fmt.Errorf("%w: unknown jellyfin option %q", domain.ErrInvalid, key)
	}
	return nil
}
