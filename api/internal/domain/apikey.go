// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// API key bounds and limits.
const (
	MaxKeysPerOwner  = 50
	MinAPIKeyNameLen = 1
	MaxAPIKeyNameLen = 100
)

// APIKeyScope constants name the valid capability scopes for API keys.
const (
	ScopeCoreRead             = "core:read"
	ScopeCoreWrite            = "core:write"
	ScopeRequestsRead         = "requests:read"
	ScopeRequestsWrite        = "requests:write"
	ScopeDownloadClientsRead  = "download_clients:read"
	ScopeDownloadClientsWrite = "download_clients:write"
	ScopeProwlarrRead         = "prowlarr:read"
	ScopeProwlarrWrite        = "prowlarr:write"
	ScopeNotificationsRead    = "notifications:read"
	ScopeNotificationsWrite   = "notifications:write"
	ScopeAdminRead            = "admin:read"
	ScopeAdminWrite           = "admin:write"
	ScopeAdultRead            = "adult:read"
	ScopeAdultWrite           = "adult:write"
)

var validAPIKeyScopes = map[string]struct{}{
	ScopeCoreRead:             {},
	ScopeCoreWrite:            {},
	ScopeRequestsRead:         {},
	ScopeRequestsWrite:        {},
	ScopeDownloadClientsRead:  {},
	ScopeDownloadClientsWrite: {},
	ScopeProwlarrRead:         {},
	ScopeProwlarrWrite:        {},
	ScopeNotificationsRead:    {},
	ScopeNotificationsWrite:   {},
	ScopeAdminRead:            {},
	ScopeAdminWrite:           {},
	ScopeAdultRead:            {},
	ScopeAdultWrite:           {},
}

// APIKey represents an active or expired API key metadata record (never the hash, never the secret).
type APIKey struct {
	ID         string
	OwnerID    uuid.UUID
	Name       string
	Scopes     []string
	CreatedAt  time.Time
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
}

// CreatedAPIKey is returned only upon key issuance and includes the raw token secret.
type CreatedAPIKey struct {
	APIKey
	Secret string
}

// ValidateAPIKeyName checks that name is non-empty and within bounds after trimming.
func ValidateAPIKeyName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	runes := utf8.RuneCountInString(trimmed)
	if runes < MinAPIKeyNameLen || runes > MaxAPIKeyNameLen {
		return "", fmt.Errorf("%w: api key name must be between %d and %d characters", ErrBadRequest, MinAPIKeyNameLen, MaxAPIKeyNameLen)
	}
	return trimmed, nil
}

// ValidateAPIKeyScopes checks that every scope is known and appears at most once.
func ValidateAPIKeyScopes(scopes []string) ([]string, error) {
	if len(scopes) == 0 {
		return []string{}, nil
	}
	seen := make(map[string]struct{}, len(scopes))
	out := make([]string, 0, len(scopes))
	for _, s := range scopes {
		if _, ok := validAPIKeyScopes[s]; !ok {
			return nil, fmt.Errorf("%w: unknown api key scope %q", ErrBadRequest, s)
		}
		if _, dup := seen[s]; dup {
			return nil, fmt.Errorf("%w: duplicate api key scope %q", ErrBadRequest, s)
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out, nil
}
