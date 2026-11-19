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
	base := "/tmp/media/imagesets"
	cases := []struct {
		name   string
		target string
		want   bool
	}{
		{name: "valid sub dir", target: filepath.Join(base, "gallery1"), want: true},
		{name: "valid nested file", target: filepath.Join(base, "gallery1", "image.jpg"), want: true},
		{name: "same directory", target: base, want: false},
		{name: "escape parent", target: "/tmp/media/other", want: false},
		{name: "escape relative", target: "/tmp/media/imagesets/../other", want: false},
		{name: "root escape", target: "/etc/passwd", want: false},
		{name: "sibling prefix", target: "/tmp/media/imagesets2/item", want: false},
		{name: "relative target", target: "gallery1/image.jpg", want: false},
		{name: "conservative prefixed segment", target: filepath.Join(base, "..name", "image.jpg"), want: false},
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
