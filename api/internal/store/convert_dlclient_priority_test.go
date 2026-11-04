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
		name           string
		category       *string
		mode, wantMode string
		wantCat        string
	}{
		{"discovered category", new("movies"), "observe", "observe", "movies"},
		{"category absent", nil, "observe", "observe", ""},
		{"configured act", new("tv"), "act", "act", "tv"},
		{"upgrade defaults empty to observe", new("tv"), "", "observe", "tv"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := store.DownloadClientFromRow(sqlcgen.DownloadClient{Category: tc.category, PriorityMode: tc.mode})
			if got.Category != tc.wantCat || got.PriorityMode != tc.wantMode {
				t.Fatalf("category/mode = %q/%q, want %q/%q", got.Category, got.PriorityMode, tc.wantCat, tc.wantMode)
			}
		})
	}
}
