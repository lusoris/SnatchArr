// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package watchaware_test

import (
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/watchaware"
)

func TestWatched(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		w    watchaware.Watch
		want bool
	}{
		{
			name: "played true with nil percentage",
			w: watchaware.Watch{
				Played:           true,
				PlayedPercentage: nil,
			},
			want: true,
		},
		{
			name: "played true with low percentage",
			w: watchaware.Watch{
				Played:           true,
				PlayedPercentage: new(12.5),
			},
			want: true,
		},
		{
			name: "played false with 90 percent threshold",
			w: watchaware.Watch{
				Played:           false,
				PlayedPercentage: new(90.0),
			},
			want: true,
		},
		{
			name: "boundary 89.9 percent not watched",
			w: watchaware.Watch{
				Played:           false,
				PlayedPercentage: new(89.9),
			},
			want: false,
		},
		{
			name: "boundary 90.0 percent watched",
			w: watchaware.Watch{
				Played:           false,
				PlayedPercentage: new(watchaware.WatchedThreshold),
			},
			want: true,
		},
		{
			name: "boundary above 90 percent watched",
			w: watchaware.Watch{
				Played:           false,
				PlayedPercentage: new(95.0),
			},
			want: true,
		},
		{
			name: "nil percentage with played false not watched",
			w: watchaware.Watch{
				Played:           false,
				PlayedPercentage: nil,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := watchaware.Watched(tt.w)
			if got != tt.want {
				t.Errorf("Watched() = %v, want %v", got, tt.want)
			}
		})
	}
}
