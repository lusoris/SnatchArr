// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package jellyfin provides Jellyfin authentication and policy inspection.
package jellyfin

import (
	"context"
	"log/slog"
	"strings"

	"go.uber.org/fx"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// DefaultUserAgent is the HTTP User-Agent header used for Jellyfin communication.
const DefaultUserAgent = "SnatchArr/1.0 (https://github.com/lusoris/SnatchArr)"

// Policy holds access and rating restrictions for a Jellyfin user.
type Policy struct {
	MaxParentalRating *int
	BlockedTags       []string
	AllowedTags       []string
	EnableAllFolders  bool
	EnabledFolders    []string
}

// Member represents an authenticated or retrieved Jellyfin user.
type Member struct {
	ID       string
	Name     string
	Admin    bool
	Disabled bool
	Policy   Policy
}

// Authenticator authenticates users and manages Quick Connect against Jellyfin.
type Authenticator interface {
	SignIn(ctx context.Context, user, pw string) (Member, error)
	ByToken(ctx context.Context, token string) (Member, error)
	QuickConnectEnabled(ctx context.Context) (bool, error)
	InitiateQuickConnect(ctx context.Context) (code, secret string, err error)
	QuickConnectAuthorized(ctx context.Context, secret string) (bool, error)
	SignInQuickConnect(ctx context.Context, secret string) (Member, error)
	User(ctx context.Context, id string) (Member, error)
}

// AdultAllowed reports whether the given Jellyfin policy permits adult media access per ADR-0026.
func AdultAllowed(p Policy, threshold int, restrictTags []string, adultLibraryID string) bool {
	if p.MaxParentalRating != nil && *p.MaxParentalRating < threshold {
		return false
	}
	if hasBlockedTag(p.BlockedTags, restrictTags) {
		return false
	}
	if !p.EnableAllFolders && adultLibraryID != "" && !hasEnabledFolder(p.EnabledFolders, adultLibraryID) {
		return false
	}
	return true
}

func hasBlockedTag(blocked, restrict []string) bool {
	for _, bt := range blocked {
		normBT := strings.ToLower(strings.TrimSpace(bt))
		if normBT == "" {
			continue
		}
		for _, rt := range restrict {
			if normBT == strings.ToLower(strings.TrimSpace(rt)) {
				return true
			}
		}
	}
	return false
}

func hasEnabledFolder(folders []string, targetID string) bool {
	target := strings.ToLower(strings.TrimSpace(targetID))
	for _, ef := range folders {
		if strings.ToLower(strings.TrimSpace(ef)) == target {
			return true
		}
	}
	return false
}

type disabledAuthenticator struct{}

func (disabledAuthenticator) SignIn(context.Context, string, string) (Member, error) {
	return Member{}, domain.ErrNotFound
}

func (disabledAuthenticator) ByToken(context.Context, string) (Member, error) {
	return Member{}, domain.ErrNotFound
}

func (disabledAuthenticator) QuickConnectEnabled(context.Context) (bool, error) {
	return false, domain.ErrNotFound
}

func (disabledAuthenticator) InitiateQuickConnect(context.Context) (string, string, error) {
	return "", "", domain.ErrNotFound
}

func (disabledAuthenticator) QuickConnectAuthorized(context.Context, string) (bool, error) {
	return false, domain.ErrNotFound
}

func (disabledAuthenticator) SignInQuickConnect(context.Context, string) (Member, error) {
	return Member{}, domain.ErrNotFound
}

func (disabledAuthenticator) User(context.Context, string) (Member, error) {
	return Member{}, domain.ErrNotFound
}

// ProvideAuthenticator provides an Authenticator configured from cfg to the fx graph.
// A non-nil logger receives one startup warning when token sign-in is refused
// because the prod profile points at a plain-http Jellyfin URL.
func ProvideAuthenticator(cfg config.Options, logger *slog.Logger) (Authenticator, error) {
	if !cfg.Jellyfin.Enabled {
		return disabledAuthenticator{}, nil
	}
	cl, err := NewClient(cfg.Jellyfin.URL, cfg.Jellyfin.APIKey, cfg.Jellyfin.Timeout, DefaultUserAgent, nil)
	if err != nil {
		return nil, err
	}
	allowPlain := cfg.Profile == config.ProfileDev
	cl.SetAllowPlainHTTPToken(allowPlain)
	if !allowPlain && !cl.isHTTPS() && logger != nil {
		logger.Warn("jellyfin token sign-in refused: url is not https")
	}
	return cl, nil
}

// ProvideRevalidator provides the Jellyfin session Revalidator to the fx graph.
func ProvideRevalidator(auth Authenticator, cfg config.Options, clk clock.Clock, logger *slog.Logger) *Revalidator {
	return NewRevalidator(auth, clk, cfg.Jellyfin.DefaultRole, cfg.Jellyfin.Timeout, logger)
}

// Module provides the Jellyfin Authenticator and Revalidator to the fx graph.
var Module = fx.Module("snatcharr.jellyfin",
	fx.Provide(ProvideAuthenticator, ProvideRevalidator),
)
