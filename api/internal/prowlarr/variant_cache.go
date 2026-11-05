// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package prowlarr

import (
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/prowlarrclient"
)

const (
	maxSearchCacheEntries = 1024
	searchCacheTTL        = time.Hour
)

type searchKey struct {
	linkID     uuid.UUID
	query      string
	kind       string
	indexers   string
	categories string
	limit      int
	offset     int
}

// variantCacheKey preserves query text and type; only unordered ID lists normalize.
func variantCacheKey(linkID uuid.UUID, q prowlarrclient.SearchQuery) searchKey {
	indexers, categories := slices.Clone(q.IndexerIDs), slices.Clone(q.Categories)
	slices.Sort(indexers)
	slices.Sort(categories)
	return searchKey{
		linkID: linkID, query: q.Query, kind: q.Type,
		indexers: fmt.Sprint(slices.Compact(indexers)), categories: fmt.Sprint(categories),
		limit: q.Limit, offset: q.Offset,
	}
}

type cachedSearch struct {
	releases  []prowlarrclient.Release
	budget    SearchBudget
	fetchedAt time.Time
}

// searchCache keeps eviction and insertion under one lock; the zero value is ready.
type searchCache struct {
	mu      sync.Mutex
	entries map[searchKey]cachedSearch
}

func (c *searchCache) get(key searchKey, now time.Time) (cachedSearch, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return cachedSearch{}, false
	}
	if now.After(entry.fetchedAt.Add(searchCacheTTL)) {
		delete(c.entries, key)
		return cachedSearch{}, false
	}
	entry.releases = cloneSearchReleases(entry.releases)
	return entry, true
}

func (c *searchCache) put(key searchKey, entry cachedSearch) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[searchKey]cachedSearch)
	}
	if _, exists := c.entries[key]; !exists && len(c.entries) >= maxSearchCacheEntries {
		c.evict(entry.fetchedAt)
	}
	entry.releases = cloneSearchReleases(entry.releases)
	c.entries[key] = entry
}

// evict requires mu. The map contains at most maxSearchCacheEntries entries.
func (c *searchCache) evict(now time.Time) {
	var oldestKey searchKey
	var oldest time.Time
	for key, entry := range c.entries {
		if now.After(entry.fetchedAt.Add(searchCacheTTL)) {
			delete(c.entries, key)
			continue
		}
		if oldest.IsZero() || entry.fetchedAt.Before(oldest) {
			oldestKey, oldest = key, entry.fetchedAt
		}
	}
	if len(c.entries) >= maxSearchCacheEntries {
		delete(c.entries, oldestKey)
	}
}

// cloneSearchReleases copies each response element and its mutable category list.
func cloneSearchReleases(releases []prowlarrclient.Release) []prowlarrclient.Release {
	out := slices.Clone(releases)
	for i := range out {
		out[i].Categories = slices.Clone(out[i].Categories)
	}
	return out
}
