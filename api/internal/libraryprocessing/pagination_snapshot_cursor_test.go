// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package libraryprocessing

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestStreamRuleBatchesUsesOriginalLastRow(t *testing.T) {
	t.Parallel()
	createdAt := time.Unix(1, 0).UTC()
	first := StoredRule{Scope: ScopeLibrary, CreatedAt: createdAt, ID: uuid.MustParse("00000000-0000-0000-0000-000000000001")}
	last := StoredRule{Scope: ScopeLibrary, CreatedAt: createdAt, ID: uuid.MustParse("00000000-0000-0000-0000-000000000002")}
	want := RuleCursorKey{Scope: ScopeLibrary, Rank: 3, CreatedAt: createdAt, ID: last.ID}
	queries, batches := 0, 0
	err := streamRuleBatches(t.Context(), func(_ context.Context, cursor *RuleCursorKey) ([]StoredRule, bool, error) {
		queries++
		switch queries {
		case 1:
			if cursor != nil {
				t.Fatalf("initial cursor = %+v, want nil", cursor)
			}
			return []StoredRule{first, last}, true, nil
		case 2:
			if cursor == nil || *cursor != want {
				t.Fatalf("continuation cursor = %+v, want %+v", cursor, want)
			}
			return []StoredRule{last}, false, nil
		default:
			t.Fatalf("unexpected query %d", queries)
			return nil, false, nil
		}
	}, func(items []StoredRule) error {
		batches++
		if len(items) == 2 {
			items[0], items[1] = items[1], items[0]
		}
		return nil
	})
	if err != nil || queries != 2 || batches != 2 {
		t.Fatalf("stream returned %v after %d queries and %d callbacks; want nil, 2 queries, and 2 callbacks", err, queries, batches)
	}
}
