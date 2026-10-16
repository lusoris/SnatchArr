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

// APIKey represents an active or expired API key metadata record (never the hash, never the secret).
type APIKey struct {
	ID         string
	OwnerID    uuid.UUID
	Name       string
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
