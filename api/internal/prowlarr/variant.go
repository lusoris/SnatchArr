// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package prowlarr

import (
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/prowlarrclient"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
)

// MaxVariantsPerItem bounds title variants searched for one item in one cycle.
const MaxVariantsPerItem = 2

// LimitVariants returns an independent copy of at most the first two variants.
// A nil input returns nil.
func LimitVariants(v []string) []string {
	return slices.Clone(v[:min(len(v), MaxVariantsPerItem)])
}

// SearchBudget describes a committed search debit. LinkUsed is usage after debit.
// Warn means link usage or an enabled global cap reached ceil(snatch.WarnRatio*cap).
// Cache hits have Debited zero; the other fields retain the original search snapshot.
type SearchBudget struct {
	Debited  int
	LinkUsed int
	LinkCap  int
	Warn     bool
}

// planSearchDebit applies all-or-nothing cap arithmetic shared by both buckets.
func planSearchDebit(used, capacity, n int) (SearchBudget, error) {
	if n < 1 || n > maxIndexers {
		return SearchBudget{}, fmt.Errorf("%w: search cost must be between 1 and %d", domain.ErrInvalid, maxIndexers)
	}
	if n > capacity-used {
		return SearchBudget{}, fmt.Errorf("%w: hourly cap exhausted (%d+%d/%d)", domain.ErrTooManyRequests, used, n, capacity)
	}
	return SearchBudget{
		Debited: n, LinkUsed: used + n, LinkCap: capacity,
		Warn: used+n >= int(math.Ceil(snatch.WarnRatio*float64(capacity))),
	}, nil
}

// SearchVariant searches explicit indexers, charging both buckets per indexer.
// Successful searches, including empty results, are cached for one hour.
// A search error retains the committed debit and is never cached.
func (s *Service) SearchVariant(ctx context.Context, linkID uuid.UUID, q prowlarrclient.SearchQuery) ([]prowlarrclient.Release, SearchBudget, error) {
	if len(q.IndexerIDs) < 1 || len(q.IndexerIDs) > maxIndexers {
		return nil, SearchBudget{}, fmt.Errorf("%w: variant search requires 1 to %d explicit indexers", domain.ErrInvalid, maxIndexers)
	}
	link, err := s.searchLink(ctx, linkID)
	if err != nil {
		return nil, SearchBudget{}, err
	}
	key := variantCacheKey(linkID, q)
	if entry, ok := s.searchCache.get(key, s.clk.Now()); ok {
		entry.budget.Debited = 0
		return entry.releases, entry.budget, nil
	}
	budget, err := s.acquireGrabBudget(ctx, linkID, int(link.row.HourlyCap), len(q.IndexerIDs))
	if err != nil {
		return nil, SearchBudget{}, fmt.Errorf("prowlarr: variant budget: %w", err)
	}
	releases, err := s.client.Search(ctx, link.row.BaseUrl, link.apiKey, q)
	if err != nil {
		return nil, budget, fmt.Errorf("prowlarr: variant search: %w", err)
	}
	s.searchCache.put(key, cachedSearch{releases: releases, budget: budget, fetchedAt: s.clk.Now()})
	return releases, budget, nil
}
