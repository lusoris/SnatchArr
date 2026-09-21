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
	RoleAdmin  Role = "admin"
	RoleViewer Role = "viewer"
)

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
