// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package pathsafe_test

import (
	"path/filepath"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/pathsafe"
)

func TestStrictlyWithin(t *testing.T) {
	t.Parallel()
	sep := string(filepath.Separator)
	base := filepath.Join(sep, "tmp", "media", "imagesets")
	cases := []struct {
		name   string
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
		{name: "same directory", target: base, want: false},
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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := pathsafe.StrictlyWithin(base, tc.target)
			if got != tc.want {
				t.Fatalf("StrictlyWithin(%q, %q) = %v, want %v", base, tc.target, got, tc.want)
			}
		})
	}
}
