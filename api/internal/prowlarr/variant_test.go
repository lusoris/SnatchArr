// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package prowlarr

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/prowlarrclient"
)

func TestLimitVariants(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		in   []string
		want []string
	}{
		{name: "nil"},
		{name: "empty", in: []string{}, want: []string{}},
		{name: "one", in: []string{"a"}, want: []string{"a"}},
		{name: "two", in: []string{"a", "b"}, want: []string{"a", "b"}},
		{name: "three", in: []string{"a", "b", "c"}, want: []string{"a", "b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := LimitVariants(tc.in)
			if !slices.Equal(got, tc.want) || (got == nil) != (tc.want == nil) {
				t.Fatalf("LimitVariants() = %#v, want %#v", got, tc.want)
			}
			if len(got) > 0 {
				got[0] = "changed"
				if tc.in[0] != tc.want[0] {
					t.Fatal("result aliases input")
				}
			}
		})
	}
}

func TestPlanSearchDebit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		used, cap int
		n         int
		want      SearchBudget
		wantErr   error
	}{
		{name: "below warning", used: 5, cap: 10, n: 2, want: SearchBudget{Debited: 2, LinkUsed: 7, LinkCap: 10}},
		{name: "at warning", used: 6, cap: 10, n: 2, want: SearchBudget{Debited: 2, LinkUsed: 8, LinkCap: 10, Warn: true}},
		{name: "rounded warning below", used: 8, cap: 13, n: 2, want: SearchBudget{Debited: 2, LinkUsed: 10, LinkCap: 13}},
		{name: "rounded warning at", used: 9, cap: 13, n: 2, want: SearchBudget{Debited: 2, LinkUsed: 11, LinkCap: 13, Warn: true}},
		{name: "exact cap", used: 8, cap: 10, n: 2, want: SearchBudget{Debited: 2, LinkUsed: 10, LinkCap: 10, Warn: true}},
		{name: "over cap by one", used: 9, cap: 10, n: 2, wantErr: domain.ErrTooManyRequests},
		{name: "already exhausted", used: 11, cap: 10, n: 2, wantErr: domain.ErrTooManyRequests},
		{name: "grab cost", cap: 10, n: 1, want: SearchBudget{Debited: 1, LinkUsed: 1, LinkCap: 10}},
		{name: "max indexers", cap: 1000, n: 1000, want: SearchBudget{Debited: 1000, LinkUsed: 1000, LinkCap: 1000, Warn: true}},
		{name: "empty", cap: 10, wantErr: domain.ErrInvalid},
		{name: "negative", cap: 10, n: -1, wantErr: domain.ErrInvalid},
		{name: "too many", cap: 2000, n: 1001, wantErr: domain.ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := planSearchDebit(tc.used, tc.cap, tc.n)
			if !errors.Is(err, tc.wantErr) || got != tc.want {
				t.Fatalf("planSearchDebit() = %+v, %v; want %+v, %v", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestVariantCacheKey(t *testing.T) {
	t.Parallel()
	linkID := uuid.New()
	q := prowlarrclient.SearchQuery{Query: "Title", Type: "search", IndexerIDs: []int{2, 1, 2}, Categories: []int{5000, 2000}, Limit: 10, Offset: 5}
	key := variantCacheKey(linkID, q)
	for _, tc := range []struct {
		name   string
		change func(*prowlarrclient.SearchQuery)
		same   bool
	}{
		{name: "indexer order and duplicates", change: func(q *prowlarrclient.SearchQuery) { q.IndexerIDs = []int{1, 2} }, same: true},
		{name: "category order", change: func(q *prowlarrclient.SearchQuery) { q.Categories = []int{2000, 5000} }, same: true},
		{name: "indexers", change: func(q *prowlarrclient.SearchQuery) { q.IndexerIDs = []int{3} }},
		{name: "categories", change: func(q *prowlarrclient.SearchQuery) { q.Categories = []int{2000} }},
		{name: "query", change: func(q *prowlarrclient.SearchQuery) { q.Query = "Other" }},
		{name: "type", change: func(q *prowlarrclient.SearchQuery) { q.Type = "tvsearch" }},
		{name: "limit", change: func(q *prowlarrclient.SearchQuery) { q.Limit++ }},
		{name: "offset", change: func(q *prowlarrclient.SearchQuery) { q.Offset++ }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			other := q
			tc.change(&other)
			if same := variantCacheKey(linkID, other) == key; same != tc.same {
				t.Fatalf("key equal = %v, want %v", same, tc.same)
			}
		})
	}
	if variantCacheKey(uuid.New(), q) == key {
		t.Fatal("cache key omits link identity")
	}
	if !slices.Equal(q.IndexerIDs, []int{2, 1, 2}) || !slices.Equal(q.Categories, []int{5000, 2000}) {
		t.Fatal("normalization mutated query")
	}
}

func TestSearchCacheExpiry(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.November, 5, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		age  time.Duration
		hit  bool
	}{
		{name: "fresh", hit: true},
		{name: "one hour", age: time.Hour, hit: true},
		{name: "expired", age: time.Hour + time.Nanosecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var cache searchCache
			key := searchKey{query: "Title"}
			cache.put(key, cachedSearch{fetchedAt: now})
			if _, ok := cache.get(key, now.Add(tc.age)); ok != tc.hit {
				t.Fatalf("cache hit = %v, want %v", ok, tc.hit)
			}
			if !tc.hit && len(cache.entries) != 0 {
				t.Fatal("expired entry retained")
			}
		})
	}
}

func TestSearchCacheBound(t *testing.T) {
	t.Parallel()
	for _, expired := range []bool{false, true} {
		t.Run(fmt.Sprintf("expired=%v", expired), func(t *testing.T) {
			t.Parallel()
			var cache searchCache
			now := time.Date(2026, time.November, 5, 0, 0, 0, 0, time.UTC)
			for i := range maxSearchCacheEntries {
				cache.put(searchKey{offset: i}, cachedSearch{fetchedAt: now.Add(time.Duration(i))})
			}
			// Replacement at the bound must not evict another key.
			cache.put(searchKey{offset: 1}, cachedSearch{fetchedAt: now.Add(time.Second)})
			if len(cache.entries) != maxSearchCacheEntries {
				t.Fatalf("replacement changed cache size: %d", len(cache.entries))
			}
			insertAt := now.Add(time.Minute)
			if expired {
				insertAt = now.Add(time.Hour + 2*time.Second)
			}
			cache.put(searchKey{offset: maxSearchCacheEntries}, cachedSearch{fetchedAt: insertAt})
			wantLen := maxSearchCacheEntries
			if expired {
				wantLen = 1
			}
			if len(cache.entries) != wantLen {
				t.Fatalf("1025th query cache size = %d, want %d", len(cache.entries), wantLen)
			}
			if _, ok := cache.get(searchKey{}, insertAt); ok {
				t.Fatal("oldest entry retained")
			}
			if _, ok := cache.get(searchKey{offset: maxSearchCacheEntries}, insertAt); !ok {
				t.Fatal("new entry missing")
			}
		})
	}
}

func TestSearchCacheExpiredBeforeOldest(t *testing.T) {
	t.Parallel()
	var cache searchCache
	now := time.Date(2026, time.November, 5, 0, 0, 0, 0, time.UTC)
	cache.put(searchKey{}, cachedSearch{fetchedAt: now.Add(-time.Hour - time.Nanosecond)})
	for i := 1; i < maxSearchCacheEntries; i++ {
		cache.put(searchKey{offset: i}, cachedSearch{fetchedAt: now})
	}
	cache.put(searchKey{offset: maxSearchCacheEntries}, cachedSearch{fetchedAt: now})
	if _, ok := cache.get(searchKey{offset: 1}, now); !ok {
		t.Fatal("live entry evicted although an expired slot was available")
	}
}

func TestSearchCacheCopies(t *testing.T) {
	t.Parallel()
	var cache searchCache
	now := time.Date(2026, time.November, 5, 0, 0, 0, 0, time.UTC)
	releases := []prowlarrclient.Release{{GUID: "original", Categories: []int{2000}}}
	cache.put(searchKey{}, cachedSearch{fetchedAt: now, releases: releases})
	releases[0].GUID, releases[0].Categories[0] = "changed", 5000
	got, ok := cache.get(searchKey{}, now)
	if !ok || got.releases[0].GUID != "original" || got.releases[0].Categories[0] != 2000 {
		t.Fatalf("input aliases cache: %+v", got)
	}
	got.releases[0].GUID, got.releases[0].Categories[0] = "changed again", 6000
	again, ok := cache.get(searchKey{}, now)
	if !ok || again.releases[0].GUID != "original" || again.releases[0].Categories[0] != 2000 {
		t.Fatalf("output aliases cache: %+v", again)
	}
}

func TestSearchCacheConcurrent(t *testing.T) {
	t.Parallel()
	var cache searchCache
	var wg sync.WaitGroup
	now := time.Date(2026, time.November, 5, 0, 0, 0, 0, time.UTC)
	for worker := range 8 {
		wg.Go(func() {
			for i := range 256 {
				key := searchKey{offset: worker*256 + i}
				cache.put(key, cachedSearch{fetchedAt: now})
				cache.get(key, now)
			}
		})
	}
	wg.Wait()
	if len(cache.entries) > maxSearchCacheEntries {
		t.Fatalf("concurrent cache size = %d", len(cache.entries))
	}
}

func TestSearchVariantInvalidBeforeIO(t *testing.T) {
	t.Parallel()
	for _, n := range []int{0, 1001} {
		t.Run(fmt.Sprintf("indexers=%d", n), func(t *testing.T) {
			t.Parallel()
			// No store or client: validation must reject before any I/O or debit.
			var svc Service
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			releases, budget, err := svc.SearchVariant(ctx, uuid.New(), prowlarrclient.SearchQuery{IndexerIDs: make([]int, n)})
			if !errors.Is(err, domain.ErrInvalid) || releases != nil || budget != (SearchBudget{}) {
				t.Fatalf("invalid search = %+v, %+v, %v", releases, budget, err)
			}
		})
	}
}
