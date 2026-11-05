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
	"github.com/lusoris/SnatchArr/api/internal/snatch"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// ErrNoRelease indicates that no eligible release was found for the item.
var ErrNoRelease = errors.New("no release found")

// searchDBTimeout bounds link lookups and budget transactions, including lock waits.
const searchDBTimeout = 10 * time.Second

// GrabForItem searches Prowlarr for releases matching the query, picks the first release
// whose indexer is enabled and not rate-limited per existing indexer data, and grabs it.
func (s *Service) GrabForItem(ctx context.Context, linkID uuid.UUID, query prowlarrclient.SearchQuery) (prowlarrclient.Release, error) {
	link, err := s.searchLink(ctx, linkID)
	if err != nil {
		return prowlarrclient.Release{}, err
	}
	if _, err = s.acquireGrabBudget(ctx, linkID, 1); err != nil {
		return prowlarrclient.Release{}, fmt.Errorf("prowlarr: %w", err)
	}

	releases, err := s.client.Search(ctx, link.row.BaseUrl, link.apiKey, query)
	if err != nil {
		return prowlarrclient.Release{}, fmt.Errorf("prowlarr: search: %w", err)
	}
	if len(releases) == 0 {
		return prowlarrclient.Release{}, ErrNoRelease
	}

	chosen := pickRelease(releases, link.view.Indexers, s.clk.Now())
	if chosen == nil {
		return prowlarrclient.Release{}, ErrNoRelease
	}
	if err := s.client.Grab(ctx, link.row.BaseUrl, link.apiKey, *chosen); err != nil {
		return prowlarrclient.Release{}, fmt.Errorf("prowlarr: grab: %w", err)
	}
	return *chosen, nil
}

// preparedSearchLink carries the shared grab/variant preflight result.
type preparedSearchLink struct {
	row    sqlcgen.ProwlarrLink
	apiKey string
	view   LinkStatus
}

func (s *Service) searchLink(ctx context.Context, linkID uuid.UUID) (preparedSearchLink, error) {
	dbCtx, cancel := context.WithTimeout(ctx, searchDBTimeout)
	defer cancel()
	row, err := s.st.Q().GetProwlarrLink(dbCtx, linkID)
	if err != nil {
		return preparedSearchLink{}, fmt.Errorf("prowlarr: get link: %w", store.MapError(err))
	}
	if !row.Enabled {
		return preparedSearchLink{}, fmt.Errorf("%w: prowlarr link %s is disabled", domain.ErrForbidden, linkID)
	}
	key, err := s.enc.Open(row.ApiKeyEnc)
	if err != nil {
		return preparedSearchLink{}, fmt.Errorf("prowlarr: decrypt api key: %w", err)
	}
	view := s.cachedObserve(ctx, row)
	if !view.Reachable {
		return preparedSearchLink{}, fmt.Errorf("%w: prowlarr unreachable: %s", domain.ErrUnreachable, view.Error)
	}
	return preparedSearchLink{row: row, apiKey: string(key), view: view}, nil
}

func (s *Service) checkAndDebitGlobal(ctx context.Context, q *sqlcgen.Queries, globalCap, n int, now time.Time) (bool, error) {
	if globalCap <= 0 {
		return false, nil
	}
	if err := q.LockGlobalBudget(ctx); err != nil {
		return false, fmt.Errorf("lock global budget: %w", err)
	}
	globalUsage, err := q.GlobalBucketUsage(ctx, snatch.SpanStart(now))
	if err != nil {
		return false, fmt.Errorf("global usage: %w", err)
	}
	budget, err := planSearchDebit(int(globalUsage.Used), globalCap, n)
	if err != nil {
		return false, fmt.Errorf("global hourly cap: %w", err)
	}
	if err := q.AddGlobalBucketUsed(ctx, sqlcgen.AddGlobalBucketUsedParams{
		WindowStart: snatch.Minute(now),
		Used:        int32(budget.Debited), // #nosec G115 -- n is 1 for grabs or at most maxIndexers for variants
	}); err != nil {
		return false, fmt.Errorf("debit global bucket: %w", err)
	}
	return budget.Warn, nil
}

// acquireGrabBudget debits n searches atomically, locking link before global.
func (s *Service) acquireGrabBudget(ctx context.Context, linkID uuid.UUID, n int) (SearchBudget, error) {
	ctx, cancel := context.WithTimeout(ctx, searchDBTimeout)
	defer cancel()
	var budget SearchBudget
	err := s.st.Tx(ctx, func(q *sqlcgen.Queries) error {
		var err error
		budget, err = s.debitSearchBudget(ctx, q, linkID, n)
		return err
	})
	if err != nil {
		return SearchBudget{}, fmt.Errorf("acquire search budget: %w", err)
	}
	return budget, nil
}

// debitSearchBudget runs inside a transaction, reading the cap after the link lock.
func (s *Service) debitSearchBudget(ctx context.Context, q *sqlcgen.Queries, linkID uuid.UUID, n int) (SearchBudget, error) {
	if err := q.LockProwlarrLinkBudget(ctx, linkID); err != nil {
		return SearchBudget{}, fmt.Errorf("lock prowlarr link budget: %w", err)
	}
	link, err := q.GetProwlarrLink(ctx, linkID)
	if err != nil {
		return SearchBudget{}, fmt.Errorf("get prowlarr link cap: %w", store.MapError(err))
	}
	now := s.clk.Now()
	linkUsage, err := q.ProwlarrBucketUsage(ctx, sqlcgen.ProwlarrBucketUsageParams{
		Since: snatch.SpanStart(now), LinkID: linkID,
	})
	if err != nil {
		return SearchBudget{}, fmt.Errorf("prowlarr link usage: %w", err)
	}
	budget, err := planSearchDebit(int(linkUsage.Used), int(link.HourlyCap), n)
	if err != nil {
		return SearchBudget{}, fmt.Errorf("prowlarr link %s hourly cap: %w", linkID, err)
	}
	settingsRow, err := q.GetSettings(ctx)
	if err != nil {
		return SearchBudget{}, fmt.Errorf("get settings: %w", err)
	}
	globalWarn, err := s.checkAndDebitGlobal(ctx, q, int(settingsRow.GlobalHourlyCap), n, now)
	if err != nil {
		return SearchBudget{}, err
	}
	budget.Warn = budget.Warn || globalWarn
	if err := q.AddProwlarrBucketUsed(ctx, sqlcgen.AddProwlarrBucketUsedParams{
		LinkID: linkID, WindowStart: snatch.Minute(now),
		Used: int32(budget.Debited), // #nosec G115 -- n is 1 for grabs or at most maxIndexers for variants
	}); err != nil {
		return SearchBudget{}, fmt.Errorf("debit prowlarr bucket: %w", err)
	}
	return budget, nil
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
