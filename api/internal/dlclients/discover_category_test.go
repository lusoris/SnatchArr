// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/dlclients"
)

func TestFromProviderCategory(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		fields map[string]any
		want   string
	}{
		{"movie", map[string]any{"movieCategory": "movies", "movieImportedCategory": "imported"}, "movies"},
		{"tv", map[string]any{"tvCategory": "tv"}, "tv"},
		{"music and Readarr", map[string]any{"musicCategory": "books"}, "books"},
		{"unknown field", map[string]any{"category": "unverified"}, ""},
		{"missing", nil, ""},
		{"invalid type", map[string]any{"musicCategory": float64(1)}, ""},
		{"empty", map[string]any{"movieCategory": ""}, ""},
		{"ambiguous", map[string]any{"movieCategory": "movies", "tvCategory": "tv"}, ""},
	} {
		for _, kind := range []string{"QBittorrent", "Nzbget", "Sabnzbd"} {
			t.Run(tc.name+"/"+kind, func(t *testing.T) {
				t.Parallel()
				got, ok := dlclients.FromProvider(uuid.New(), provider(kind, tc.fields))
				if !ok || got.Input.Category != tc.want {
					t.Fatalf("category = %q, supported = %v; want %q", got.Input.Category, ok, tc.want)
				}
			})
		}
	}
}
