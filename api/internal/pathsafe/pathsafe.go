// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package pathsafe provides lexical path-containment checks.
package pathsafe

import (
	"path/filepath"
	"strings"
)

// MaxPathBytes bounds the byte length of cleaned paths accepted by [StrictlyWithin].
// Sourced from Linux PATH_MAX (4096 bytes including the terminating NUL, linux/limits.h);
// a cleaned path this long cannot be opened on Linux anyway.
const MaxPathBytes = 4096

// StrictlyWithin reports whether target is lexically below base, without resolving symlinks.
// Cleaned base and target paths longer than [MaxPathBytes] bytes are refused, so the segment loop runs at most MaxPathBytes/2+1 times.
// Following [filepath.IsLocal], it allows segment names such as "..." and "..name" rather than refusing every ".." prefix.
// On every platform it refuses any segment made only of periods and spaces that contains at least one space, which Windows path normalization trims to "." or ".." (Microsoft "File path formats on Windows systems", section "Trim characters").
func StrictlyWithin(base, target string) bool {
	cleanBase := filepath.Clean(base)
	cleanTarget := filepath.Clean(target)
	if len(cleanBase) > MaxPathBytes || len(cleanTarget) > MaxPathBytes {
		return false
	}
	if cleanTarget == cleanBase {
		return false
	}
	rel, err := filepath.Rel(cleanBase, cleanTarget)
	if err != nil || escapesBase(rel) {
		return false
	}
	segments := strings.SplitSeq(rel, string(filepath.Separator))
	// strings.SplitSeq yields one segment per separator in rel, plus one (at most MaxPathBytes/2+1).
	for segment := range segments {
		if isDotSpaceSegment(segment) {
			return false
		}
	}
	return true
}

func escapesBase(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel)
}

func isDotSpaceSegment(segment string) bool {
	return strings.Trim(segment, ". ") == "" && strings.Contains(segment, " ")
}
