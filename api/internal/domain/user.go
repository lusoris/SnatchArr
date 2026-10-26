// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// Role is a coarse permission level.
type Role string

// Roles.
const (
	RoleAdmin     Role = "admin"
	RoleRequester Role = "requester"
	RoleViewer    Role = "viewer"
)

// Valid returns whether r is a known, supported role.
func (r Role) Valid() bool {
	return r == RoleAdmin || r == RoleRequester || r == RoleViewer
}

// CanRequest reports whether the role is authorized to submit requests and manage own keys.
func (r Role) CanRequest() bool {
	return r == RoleAdmin || r == RoleRequester
}

// RoleForJellyfinMember decides the role of a Jellyfin member on first sign-in.
// Jellyfin administrators always map to RoleAdmin; non-administrators receive
// the configured default role (ADR-0026).
func RoleForJellyfinMember(isAdministrator bool, defaultRole Role) Role {
	if isAdministrator {
		return RoleAdmin
	}
	return defaultRole
}

// User is a local account.
type User struct {
	ID        uuid.UUID
	Username  string
	Role      Role
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Password policy bounds.
const (
	MinPasswordLen = 12
	MaxPasswordLen = 256
	MinUsernameLen = 3
	MaxUsernameLen = 64
)

// ValidateUsername enforces a simple, unambiguous handle: letters, digits, dot, dash,
// underscore.
func ValidateUsername(u string) error {
	u = strings.TrimSpace(u)
	if len(u) < MinUsernameLen || len(u) > MaxUsernameLen {
		return fmt.Errorf("%w: username must be %d-%d characters", ErrInvalid, MinUsernameLen, MaxUsernameLen)
	}
	for _, r := range u {
		if !usernameRune(r) {
			return fmt.Errorf("%w: username may contain letters, digits, '.', '-' and '_' only", ErrInvalid)
		}
	}
	return nil
}

func usernameRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_'
}

// ValidatePassword enforces length only; strength scoring lives in the UI (zxcvbn) and
// is advisory, per NIST SP 800-63B.
func ValidatePassword(p string) error {
	if len(p) < MinPasswordLen || len(p) > MaxPasswordLen {
		return fmt.Errorf("%w: password must be %d-%d characters", ErrInvalid, MinPasswordLen, MaxPasswordLen)
	}
	return nil
}
