// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package prowlarr

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/prowlarrclient"
	"github.com/lusoris/SnatchArr/api/internal/store"
)

// ErrNoRelease indicates that no eligible release was found for the item.
var ErrNoRelease = errors.New("no release found")

// GrabForItem searches Prowlarr for releases matching the query, picks the first release
// whose indexer is enabled and not rate-limited per existing indexer data, and grabs it.
func (s *Service) GrabForItem(ctx context.Context, linkID uuid.UUID, query prowlarrclient.SearchQuery) (prowlarrclient.Release, error) {
	row, err := s.st.Q().GetProwlarrLink(ctx, linkID)
	if err != nil {
		return prowlarrclient.Release{}, fmt.Errorf("prowlarr: get link: %w", store.MapError(err))
	}
	if !row.Enabled {
		return prowlarrclient.Release{}, fmt.Errorf("%w: prowlarr link %s is disabled", domain.ErrForbidden, linkID)
	}
	key, err := s.enc.Open(row.ApiKeyEnc)
	if err != nil {
		return prowlarrclient.Release{}, fmt.Errorf("prowlarr: decrypt api key: %w", err)
	}
	view := s.cachedObserve(ctx, row)
	if !view.Reachable {
		return prowlarrclient.Release{}, fmt.Errorf("%w: prowlarr unreachable: %s", domain.ErrUnreachable, view.Error)
	}

	apiKey := string(key)
	releases, err := s.client.Search(ctx, row.BaseUrl, apiKey, query)
	if err != nil {
		return prowlarrclient.Release{}, fmt.Errorf("prowlarr: search: %w", err)
	}
	if len(releases) == 0 {
		return prowlarrclient.Release{}, ErrNoRelease
	}

	chosen := pickRelease(releases, view.Indexers, s.clk.Now())
	if chosen == nil {
		return prowlarrclient.Release{}, ErrNoRelease
	}
	if err := s.client.Grab(ctx, row.BaseUrl, apiKey, *chosen); err != nil {
		return prowlarrclient.Release{}, fmt.Errorf("prowlarr: grab: %w", err)
	}
	return *chosen, nil
}

func pickRelease(releases []prowlarrclient.Release, indexers []IndexerStatus, now time.Time) *prowlarrclient.Release {
	indexerMap := make(map[int]IndexerStatus, len(indexers))
	for _, idx := range indexers {
		indexerMap[idx.ID] = idx
	}
	for i := range releases {
		rel := &releases[i]
		idx, ok := indexerMap[rel.IndexerID]
		if !ok {
			continue
		}
		if isIndexerAvailable(idx, now) {
			return rel
		}
	}
	return nil
}

func isIndexerAvailable(idx IndexerStatus, now time.Time) bool {
	if !idx.Enabled {
		return false
	}
	if idx.DisabledTill != nil && idx.DisabledTill.After(now) {
		return false
	}
	if idx.Exhausted {
		return false
	}
	return true
}
