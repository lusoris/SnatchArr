// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package hunt

import (
	"testing"
	"time"
)

func TestGrant(t *testing.T) {
	t.Parallel()
	window := time.Date(2026, 9, 21, 14, 0, 0, 0, time.UTC)
	tests := []struct {
		name          string
		used, cap     int
		requested     int
		wantGranted   int
		wantUsed      int
		wantRemaining int
	}{
		{"positive fresh window", 0, 20, 5, 5, 5, 15},
		{"positive partial", 18, 20, 5, 2, 20, 0},
		{"boundary exactly at cap", 20, 20, 1, 0, 20, 0},
		{"boundary over cap (cap lowered mid-window)", 25, 20, 3, 0, 25, 0},
		{"boundary zero request", 3, 20, 0, 0, 3, 17},
		{"negative-ish huge request", 0, 20, 1000, 20, 20, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := grant(tc.used, tc.cap, tc.requested, window)
			if g.Granted != tc.wantGranted || g.Used != tc.wantUsed || g.Remaining != tc.wantRemaining {
				t.Fatalf("grant = %+v", g)
			}
			if !g.ResetsAt.Equal(window.Add(time.Hour)) {
				t.Fatalf("resets at %v", g.ResetsAt)
			}
		})
	}
}

func TestWindow(t *testing.T) {
	t.Parallel()
	loc := time.FixedZone("plus2", 2*3600)
	now := time.Date(2026, 9, 21, 16, 59, 59, 0, loc)
	got := Window(now)
	want := time.Date(2026, 9, 21, 14, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("Window = %v, want %v", got, want)
	}
}
