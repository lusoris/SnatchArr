// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package pathsafe_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/pathsafe"
)

func buildCleanPath(t *testing.T, prefix string, wantBytes int) string {
	t.Helper()
	sep := string(filepath.Separator)
	cleanPrefix := filepath.Clean(prefix)
	rem := wantBytes - len(cleanPrefix)
	if rem < 2 {
		t.Fatalf("wantBytes %d too small for prefix %q (%d bytes)", wantBytes, cleanPrefix, len(cleanPrefix))
	}
	var built string
	if rem%2 == 0 {
		built = cleanPrefix + strings.Repeat(sep+"a", rem/2)
	} else {
		built = cleanPrefix + strings.Repeat(sep+"a", (rem-3)/2) + sep + "aa"
	}
	cleaned := filepath.Clean(built)
	if cleaned != built || len(cleaned) != wantBytes {
		t.Fatalf("cleaned length = %d (unchanged=%v), want %d", len(cleaned), cleaned == built, wantBytes)
	}
	return built
}

func TestStrictlyWithin(t *testing.T) {
	t.Parallel()
	sep := string(filepath.Separator)
	base := filepath.Join(sep, "tmp", "media", "imagesets")
	exactMaxTarget := buildCleanPath(t, base, pathsafe.MaxPathBytes)
	overMaxTarget := buildCleanPath(t, base, pathsafe.MaxPathBytes+1)
	overMaxBase := buildCleanPath(t, base, pathsafe.MaxPathBytes+1)
	overMaxBaseTarget := buildCleanPath(t, overMaxBase, pathsafe.MaxPathBytes+3)
	cases := []struct {
		name   string
		base   string
		target string
		want   bool
	}{
		{name: "valid sub dir", target: filepath.Join(base, "gallery1"), want: true},
		{name: "valid nested file", target: filepath.Join(base, "gallery1", "image.jpg"), want: true},
		{name: "prefixed segment nested", target: filepath.Join(base, "..name", "image.jpg"), want: true},
		{name: "prefixed segment file", target: filepath.Join(base, "..name"), want: true},
		{name: "triple dot segment", target: filepath.Join(base, "..."), want: true},
		{name: "trailing space name", target: filepath.Join(base, "name "), want: true},
		{name: "inner space name", target: filepath.Join(base, "a b"), want: true},
		{name: "four dots", target: filepath.Join(base, "...."), want: true},
		{name: "target at MaxPathBytes", target: exactMaxTarget, want: true},
		{name: "same directory", target: base, want: false},
		{name: "base differing only in letter case", base: `C:\Media`, target: `c:\media`, want: false},
		{name: "parent directory", target: filepath.Join(sep, "tmp", "media"), want: false},
		{name: "escape parent", target: filepath.Join(sep, "tmp", "media", "other"), want: false},
		{name: "escape relative", target: base + sep + ".." + sep + "other", want: false},
		{name: "space parent segment", target: base + sep + ".. " + sep + "other", want: false},
		{name: "space parent last", target: base + sep + ".. ", want: false},
		{name: "dot space dot segment", target: base + sep + ".. ." + sep + "x", want: false},
		{name: "dot space segment", target: base + sep + ". " + sep + "x", want: false},
		{name: "root escape", target: filepath.Join(sep, "etc", "passwd"), want: false},
		{name: "sibling prefix", target: filepath.Join(sep, "tmp", "media", "imagesets2", "item"), want: false},
		{name: "relative target", target: filepath.Join("gallery1", "image.jpg"), want: false},
		{name: "target over MaxPathBytes", target: overMaxTarget, want: false},
		{name: "base over MaxPathBytes", base: overMaxBase, target: overMaxBaseTarget, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			caseBase := tc.base
			if caseBase == "" {
				caseBase = base
			}
			got := pathsafe.StrictlyWithin(caseBase, tc.target)
			if got != tc.want {
				t.Fatalf("StrictlyWithin(%q, %q) = %v, want %v", caseBase, tc.target, got, tc.want)
			}
		})
	}
}
