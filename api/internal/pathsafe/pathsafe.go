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
// Following [filepath.IsLocal], it allows segment names such as "..." and "..name" rather than refusing every ".." prefix.
// On every platform it refuses any segment made only of periods and spaces that contains at least one space, which Windows path normalization trims to "." or ".." (Microsoft "File path formats on Windows systems", section "Trim characters").
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
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return false
	}
	segments := strings.Split(rel, string(filepath.Separator))
	// len(segments) is bounded by the separator count of rel.
	for _, segment := range segments {
		if strings.Trim(segment, ". ") == "" && strings.Contains(segment, " ") {
			return false
		}
	}
	return true
}
