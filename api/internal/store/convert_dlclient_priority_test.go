// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package store_test

import (
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

func TestDownloadClientPriorityFields(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		category   *string
		mode, want string
	}{
		{"discovered category", new("movies"), "observe", "movies"},
		{"category absent", nil, "observe", ""},
		{"configured act", new("tv"), "act", "tv"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := store.DownloadClientFromRow(sqlcgen.DownloadClient{Category: tc.category, PriorityMode: tc.mode})
			if got.Category != tc.want || got.PriorityMode != tc.mode {
				t.Fatalf("category/mode = %q/%q", got.Category, got.PriorityMode)
			}
		})
	}
}
