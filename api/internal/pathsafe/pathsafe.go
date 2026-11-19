// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package pathsafe provides lexical path-containment checks.
package pathsafe

import (
	"path/filepath"
	"strings"
)

// StrictlyWithin reports whether target is lexically below base, without resolving symlinks.
// It conservatively refuses a first relative-path segment that starts with "..".
func StrictlyWithin(base, target string) bool {
	cleanBase := filepath.Clean(base)
	cleanTarget := filepath.Clean(target)
	if cleanTarget == cleanBase {
		return false
	}
	rel, err := filepath.Rel(cleanBase, cleanTarget)
	if err != nil {
		return false
	}
	if strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return false
	}
	return true
}
