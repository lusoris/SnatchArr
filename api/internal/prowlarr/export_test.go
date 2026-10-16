// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package prowlarr

import (
	"github.com/golusoris/goenvoy/arr/v2"
	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// MatchedIndexerForTest exports matchedIndexer for external tests.
type MatchedIndexerForTest struct {
	LinkID   uuid.UUID
	LinkName string
	ID       int
}

// MatchIndexersForTest exports matchIndexers for external tests.
func MatchIndexersForTest(autoIndexers []arr.ProviderResource, links []sqlcgen.ProwlarrLink, views map[uuid.UUID]LinkStatus) ([]MatchedIndexerForTest, bool) {
	matched, hasUnmatched := matchIndexers(autoIndexers, links, views)
	out := make([]MatchedIndexerForTest, 0, len(matched))
	for _, m := range matched {
		out = append(out, MatchedIndexerForTest{
			LinkID:   m.linkID,
			LinkName: m.linkName,
			ID:       m.id,
		})
	}
	return out, hasUnmatched
}
