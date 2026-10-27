// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golusoris/goenvoy/metadata/adult/stashbox/v2"
	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

const (
	// DefaultPerPage is the number of items requested per page from stash-box GraphQL API.
	DefaultPerPage = 50
	// MaxItemsPerSweep is the hard cap of items synced per entity kind per sweep (HISS-02).
	MaxItemsPerSweep = 500
	// PageTimeout is the deadline for each individual page query (HISS-02).
	PageTimeout = 30 * time.Second
	// MaxURLsPerEntity is the maximum number of URLs stored per favourite entity (HISS-02).
	MaxURLsPerEntity = 64

	// EntityTypePerformer is the polymorphic entity_type value for performers.
	EntityTypePerformer = "performer"
	// EntityTypeStudio is the polymorphic entity_type value for studios.
	EntityTypeStudio = "studio"
	// SourceKindStashbox is the external reference source_kind for stash-box instances.
	SourceKindStashbox = "stashbox"
)

const (
	queryFavoritePerformers = `query ($input: PerformerQueryInput!) {
		query_performers: queryPerformers(input: $input) {
			count
			performers {
				id name disambiguation aliases
				urls { url site { id name url } }
			}
		}
	}`

	queryFavoriteStudios = `query ($input: StudioQueryInput!) {
		query_studios: queryStudios(input: $input) {
			count
			studios {
				id name aliases
				urls { url site { id name url } }
			}
		}
	}`
)

type performersQueryResp struct {
	QueryPerformers struct {
		Count      int                  `json:"count"`
		Performers []stashbox.Performer `json:"performers"`
	} `json:"query_performers"`
}

type studiosQueryResp struct {
	QueryStudios struct {
		Count   int               `json:"count"`
		Studios []stashbox.Studio `json:"studios"`
	} `json:"query_studios"`
}

func isAuthOrGraphQLError(err error) bool {
	if apiErr, ok := errors.AsType[*stashbox.APIError](err); ok {
		if apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden {
			return true
		}
	}
	var gqlErr *stashbox.GraphQLError
	return errors.As(err, &gqlErr)
}

func (s *Service) wrapSyncError(instName string, err error) error {
	if isAuthOrGraphQLError(err) {
		return fmt.Errorf("%w: stashbox sync authentication or query failed for instance %s: %w", domain.ErrInvalid, instName, err)
	}
	return err
}

func normalizeSourceURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return trimmed
	}
	u.Host = strings.ToLower(u.Host)
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Path != "" {
		u.Path = strings.TrimRight(u.Path, "/")
	}
	return u.String()
}

func unionURLs(stored, incoming []string) []string {
	seen := make(map[string]struct{}, len(stored)+len(incoming))
	res := make([]string, 0, min(len(stored)+len(incoming), MaxURLsPerEntity))

	add := func(raw string) {
		u := normalizeSourceURL(raw)
		if u == "" {
			return
		}
		if _, ok := seen[u]; !ok && len(res) < MaxURLsPerEntity {
			seen[u] = struct{}{}
			res = append(res, u)
		}
	}

	for _, u := range stored {
		add(u)
	}
	for _, u := range incoming {
		add(u)
	}
	return res
}

func findCandidateByLink(cands []Candidate, instID uuid.UUID, foreignID string) *Candidate {
	for i := range cands {
		for _, l := range cands[i].Links {
			if l.InstanceID == instID && l.ForeignID == foreignID {
				return &cands[i]
			}
		}
	}
	return nil
}

func fetchFavoritePerformers(ctx context.Context, client *stashbox.Client) ([]stashbox.Performer, bool, error) {
	var all []stashbox.Performer
	var totalCount int
	maxPages := (MaxItemsPerSweep + DefaultPerPage - 1) / DefaultPerPage
	for page := 1; page <= maxPages; page++ {
		if len(all) >= MaxItemsPerSweep {
			break
		}
		perPage := min(DefaultPerPage, MaxItemsPerSweep-len(all))
		pageCtx, cancel := context.WithTimeout(ctx, PageTimeout)
		vars := map[string]any{
			"input": map[string]any{
				"page":        page,
				"per_page":    perPage,
				"is_favorite": true,
			},
		}
		var resp performersQueryResp
		err := client.Query(pageCtx, queryFavoritePerformers, vars, &resp)
		cancel()
		if err != nil {
			return nil, false, fmt.Errorf("adult: query performers: %w", err)
		}
		totalCount = resp.QueryPerformers.Count
		items := resp.QueryPerformers.Performers
		if len(items) == 0 {
			break
		}
		all = append(all, items...)
		if len(all) >= resp.QueryPerformers.Count || len(items) < perPage {
			break
		}
	}
	truncated := totalCount > MaxItemsPerSweep || (totalCount <= 0 && len(all) >= MaxItemsPerSweep)
	return all, truncated, nil
}

func fetchFavoriteStudios(ctx context.Context, client *stashbox.Client) ([]stashbox.Studio, bool, error) {
	var all []stashbox.Studio
	var totalCount int
	maxPages := (MaxItemsPerSweep + DefaultPerPage - 1) / DefaultPerPage
	for page := 1; page <= maxPages; page++ {
		if len(all) >= MaxItemsPerSweep {
			break
		}
		perPage := min(DefaultPerPage, MaxItemsPerSweep-len(all))
		pageCtx, cancel := context.WithTimeout(ctx, PageTimeout)
		vars := map[string]any{
			"input": map[string]any{
				"page":        page,
				"per_page":    perPage,
				"is_favorite": true,
			},
		}
		var resp studiosQueryResp
		err := client.Query(pageCtx, queryFavoriteStudios, vars, &resp)
		cancel()
		if err != nil {
			return nil, false, fmt.Errorf("adult: query studios: %w", err)
		}
		totalCount = resp.QueryStudios.Count
		items := resp.QueryStudios.Studios
		if len(items) == 0 {
			break
		}
		all = append(all, items...)
		if len(all) >= resp.QueryStudios.Count || len(items) < perPage {
			break
		}
	}
	truncated := totalCount > MaxItemsPerSweep || (totalCount <= 0 && len(all) >= MaxItemsPerSweep)
	return all, truncated, nil
}

func mapStashboxRefs(refs []sqlcgen.AdultExternalRef, capHint int) map[uuid.UUID][]EntityLink {
	linkMap := make(map[uuid.UUID][]EntityLink, capHint)
	for _, r := range refs {
		if r.SourceKind == SourceKindStashbox && r.SourceInstanceID != nil {
			linkMap[r.EntityID] = append(linkMap[r.EntityID], EntityLink{
				InstanceID: *r.SourceInstanceID,
				ForeignID:  r.ForeignID,
			})
		}
	}
	return linkMap
}

func (s *Service) loadPerformerCandidates(ctx context.Context) ([]Candidate, error) {
	rows, err := s.st.Q().ListAdultPerformers(ctx)
	if err != nil {
		return nil, fmt.Errorf("adult: list performers: %w", store.MapError(err))
	}
	refs, err := s.st.Q().ListAdultExternalRefsByEntityType(ctx, EntityTypePerformer)
	if err != nil {
		return nil, fmt.Errorf("adult: list performer refs: %w", store.MapError(err))
	}
	linkMap := mapStashboxRefs(refs, len(rows))
	cands := make([]Candidate, 0, len(rows))
	for _, r := range rows {
		dis := ""
		if r.Disambiguation != nil {
			dis = *r.Disambiguation
		}
		cands = append(cands, Candidate{
			ID:             r.ID,
			Name:           r.CanonicalName,
			Disambiguation: dis,
			URLs:           r.Urls,
			Links:          linkMap[r.ID],
		})
	}
	return cands, nil
}

func (s *Service) loadStudioCandidates(ctx context.Context) ([]Candidate, error) {
	rows, err := s.st.Q().ListAdultStudios(ctx)
	if err != nil {
		return nil, fmt.Errorf("adult: list studios: %w", store.MapError(err))
	}
	refs, err := s.st.Q().ListAdultExternalRefsByEntityType(ctx, EntityTypeStudio)
	if err != nil {
		return nil, fmt.Errorf("adult: list studio refs: %w", store.MapError(err))
	}
	linkMap := mapStashboxRefs(refs, len(rows))
	cands := make([]Candidate, 0, len(rows))
	for _, r := range rows {
		cands = append(cands, Candidate{
			ID:    r.ID,
			Name:  r.CanonicalName,
			URLs:  r.Urls,
			Links: linkMap[r.ID],
		})
	}
	return cands, nil
}

func (s *Service) upsertStashboxRef(
	ctx context.Context,
	entityType string,
	entityID uuid.UUID,
	instID uuid.UUID,
	foreignID string,
	extraURLs []string,
	sweepStart time.Time,
) error {
	refID, err := s.ids.NewUUID()
	if err != nil {
		return fmt.Errorf("adult: generate %s ref id: %w", entityType, err)
	}
	_, err = s.st.Q().UpsertAdultExternalRef(ctx, sqlcgen.UpsertAdultExternalRefParams{
		ID:               refID,
		EntityType:       entityType,
		EntityID:         entityID,
		SourceKind:       SourceKindStashbox,
		SourceInstanceID: &instID,
		ForeignID:        foreignID,
		ExtraUrls:        extraURLs,
		IsFavorite:       true,
		FetchedAt:        sweepStart,
		LastSeenAt:       sweepStart,
	})
	if err != nil {
		return fmt.Errorf("adult: upsert %s ref: %w", entityType, store.MapError(err))
	}
	return nil
}

func (s *Service) resolvePerformerMatch(
	instID uuid.UUID,
	p stashbox.Performer,
	urls []string,
	cands *[]Candidate,
	endpoints []StashboxEndpoint,
) (uuid.UUID, []string, error) {
	if matched := findCandidateByLink(*cands, instID, p.ID); matched != nil {
		matched.URLs = unionURLs(matched.URLs, urls)
		return matched.ID, matched.URLs, nil
	}
	inc := IncomingEntity{
		Name:           p.Name,
		Disambiguation: p.Disambiguation,
		URLs:           urls,
	}
	matched, _, kind := MatchEntity(inc, *cands, endpoints)
	if matched != nil && kind == MatchAutoMerge {
		matched.URLs = unionURLs(matched.URLs, urls)
		matched.Links = append(matched.Links, EntityLink{InstanceID: instID, ForeignID: p.ID})
		return matched.ID, matched.URLs, nil
	}
	newID, idErr := s.ids.NewUUID()
	if idErr != nil {
		return uuid.Nil, nil, fmt.Errorf("adult: generate performer id: %w", idErr)
	}
	newURLs := unionURLs(nil, urls)
	*cands = append(*cands, Candidate{
		ID:             newID,
		Name:           p.Name,
		Disambiguation: p.Disambiguation,
		URLs:           newURLs,
		Links:          []EntityLink{{InstanceID: instID, ForeignID: p.ID}},
	})
	return newID, newURLs, nil
}

func (s *Service) syncOnePerformer(
	ctx context.Context,
	instID uuid.UUID,
	p stashbox.Performer,
	cands *[]Candidate,
	endpoints []StashboxEndpoint,
	sweepStart time.Time,
) error {
	var urls []string
	for _, u := range p.URLs {
		if u.URL != "" {
			urls = append(urls, u.URL)
		}
	}
	favID, finalURLs, err := s.resolvePerformerMatch(instID, p, urls, cands, endpoints)
	if err != nil {
		return err
	}

	var dis *string
	if strings.TrimSpace(p.Disambiguation) != "" {
		d := strings.TrimSpace(p.Disambiguation)
		dis = &d
	}

	_, err = s.st.Q().UpsertAdultPerformer(ctx, sqlcgen.UpsertAdultPerformerParams{
		ID:             favID,
		CanonicalName:  p.Name,
		Disambiguation: dis,
		Urls:           finalURLs,
		IsFavorite:     true,
		CreatedAt:      sweepStart,
		UpdatedAt:      sweepStart,
	})
	if err != nil {
		return fmt.Errorf("adult: upsert performer: %w", store.MapError(err))
	}

	return s.upsertStashboxRef(ctx, EntityTypePerformer, favID, instID, p.ID, unionURLs(nil, urls), sweepStart)
}

func (s *Service) resolveStudioMatch(
	instID uuid.UUID,
	st stashbox.Studio,
	urls []string,
	cands *[]Candidate,
	endpoints []StashboxEndpoint,
) (uuid.UUID, []string, error) {
	if matched := findCandidateByLink(*cands, instID, st.ID); matched != nil {
		matched.URLs = unionURLs(matched.URLs, urls)
		return matched.ID, matched.URLs, nil
	}
	inc := IncomingEntity{
		Name: st.Name,
		URLs: urls,
	}
	matched, _, kind := MatchEntity(inc, *cands, endpoints)
	if matched != nil && kind == MatchAutoMerge {
		matched.URLs = unionURLs(matched.URLs, urls)
		matched.Links = append(matched.Links, EntityLink{InstanceID: instID, ForeignID: st.ID})
		return matched.ID, matched.URLs, nil
	}
	newID, idErr := s.ids.NewUUID()
	if idErr != nil {
		return uuid.Nil, nil, fmt.Errorf("adult: generate studio id: %w", idErr)
	}
	newURLs := unionURLs(nil, urls)
	*cands = append(*cands, Candidate{
		ID:    newID,
		Name:  st.Name,
		URLs:  newURLs,
		Links: []EntityLink{{InstanceID: instID, ForeignID: st.ID}},
	})
	return newID, newURLs, nil
}

func (s *Service) syncOneStudio(
	ctx context.Context,
	instID uuid.UUID,
	st stashbox.Studio,
	cands *[]Candidate,
	endpoints []StashboxEndpoint,
	sweepStart time.Time,
) error {
	var urls []string
	for _, u := range st.URLs {
		if u.URL != "" {
			urls = append(urls, u.URL)
		}
	}
	favID, finalURLs, err := s.resolveStudioMatch(instID, st, urls, cands, endpoints)
	if err != nil {
		return err
	}

	_, err = s.st.Q().UpsertAdultStudio(ctx, sqlcgen.UpsertAdultStudioParams{
		ID:            favID,
		CanonicalName: st.Name,
		Urls:          finalURLs,
		IsFavorite:    true,
		CreatedAt:     sweepStart,
		UpdatedAt:     sweepStart,
	})
	if err != nil {
		return fmt.Errorf("adult: upsert studio: %w", store.MapError(err))
	}

	return s.upsertStashboxRef(ctx, EntityTypeStudio, favID, instID, st.ID, unionURLs(nil, urls), sweepStart)
}

func (s *Service) syncPerformers(
	ctx context.Context,
	instID uuid.UUID,
	client *stashbox.Client,
	endpoints []StashboxEndpoint,
	sweepStart time.Time,
) (int, error) {
	items, truncated, err := fetchFavoritePerformers(ctx, client)
	if err != nil {
		return 0, err
	}
	cands, err := s.loadPerformerCandidates(ctx)
	if err != nil {
		return 0, err
	}
	for _, p := range items {
		if syncErr := s.syncOnePerformer(ctx, instID, p, &cands, endpoints, sweepStart); syncErr != nil {
			return 0, syncErr
		}
	}
	if truncated {
		s.logger.InfoContext(ctx, "adult: performer sync reached cap; skipping stale ref deactivation",
			slog.String("instance_id", instID.String()),
			slog.Int("count", len(items)),
			slog.Int("cap", MaxItemsPerSweep),
		)
		return len(items), nil
	}
	_, err = s.st.Q().MarkStaleAdultExternalRefsUnfavorite(ctx, sqlcgen.MarkStaleAdultExternalRefsUnfavoriteParams{
		SourceInstanceID: &instID,
		EntityType:       EntityTypePerformer,
		LastSeenAt:       sweepStart,
	})
	if err != nil {
		return 0, fmt.Errorf("adult: mark stale performer refs: %w", store.MapError(err))
	}
	if _, err = s.st.Q().ClearUnfavoritedAdultPerformers(ctx, sweepStart); err != nil {
		return 0, fmt.Errorf("adult: clear unfavorited performers: %w", store.MapError(err))
	}
	return len(items), nil
}

func (s *Service) syncStudios(
	ctx context.Context,
	instID uuid.UUID,
	client *stashbox.Client,
	endpoints []StashboxEndpoint,
	sweepStart time.Time,
) (int, error) {
	items, truncated, err := fetchFavoriteStudios(ctx, client)
	if err != nil {
		return 0, err
	}
	cands, err := s.loadStudioCandidates(ctx)
	if err != nil {
		return 0, err
	}
	for _, st := range items {
		if syncErr := s.syncOneStudio(ctx, instID, st, &cands, endpoints, sweepStart); syncErr != nil {
			return 0, syncErr
		}
	}
	if truncated {
		s.logger.InfoContext(ctx, "adult: studio sync reached cap; skipping stale ref deactivation",
			slog.String("instance_id", instID.String()),
			slog.Int("count", len(items)),
			slog.Int("cap", MaxItemsPerSweep),
		)
		return len(items), nil
	}
	_, err = s.st.Q().MarkStaleAdultExternalRefsUnfavorite(ctx, sqlcgen.MarkStaleAdultExternalRefsUnfavoriteParams{
		SourceInstanceID: &instID,
		EntityType:       EntityTypeStudio,
		LastSeenAt:       sweepStart,
	})
	if err != nil {
		return 0, fmt.Errorf("adult: mark stale studio refs: %w", store.MapError(err))
	}
	if _, err = s.st.Q().ClearUnfavoritedAdultStudios(ctx, sweepStart); err != nil {
		return 0, fmt.Errorf("adult: clear unfavorited studios: %w", store.MapError(err))
	}
	return len(items), nil
}

// SyncInstance synchronizes favorite performers and studios for a single stashbox instance.
func (s *Service) SyncInstance(
	ctx context.Context,
	inst sqlcgen.AdultStashboxInstance,
	client *stashbox.Client,
	endpoints []StashboxEndpoint,
) error {
	_, _, err := s.syncInstanceCounts(ctx, inst, client, endpoints)
	return err
}

func (s *Service) syncInstanceCounts(
	ctx context.Context,
	inst sqlcgen.AdultStashboxInstance,
	client *stashbox.Client,
	endpoints []StashboxEndpoint,
) (int, int, error) {
	sweepStart := s.clk.Now()
	pCount, err := s.syncPerformers(ctx, inst.ID, client, endpoints, sweepStart)
	if err != nil {
		return 0, 0, s.wrapSyncError(inst.Name, err)
	}
	sCount, err := s.syncStudios(ctx, inst.ID, client, endpoints, sweepStart)
	if err != nil {
		return 0, 0, s.wrapSyncError(inst.Name, err)
	}
	return pCount, sCount, nil
}

// SyncAll runs a sync sweep across all enabled stash-box instances that have sync_favorites enabled.
// One bad instance never aborts the sweep.
func (s *Service) SyncAll(ctx context.Context) error {
	_, err := s.SyncNow(ctx)
	return err
}

// SyncNow runs an immediate favourite synchronization pass across syncable catalogue sources.
func (s *Service) SyncNow(ctx context.Context) (SyncSummary, error) {
	rows, err := s.st.Q().ListSyncableAdultStashboxInstances(ctx)
	if err != nil {
		return SyncSummary{}, fmt.Errorf("adult: list syncable instances: %w", store.MapError(err))
	}
	var summary SyncSummary
	if len(rows) == 0 {
		s.recordSyncResult(summary, "")
		return summary, nil
	}

	endpoints := make([]StashboxEndpoint, 0, len(rows))
	for _, r := range rows {
		endpoints = append(endpoints, StashboxEndpoint{InstanceID: r.ID, Endpoint: r.Endpoint})
	}

	var lastErr string
	clients := s.pool.BuildClients(ctx, rows)
	for _, ic := range clients {
		pCount, sCount, syncErr := s.syncInstanceCounts(ctx, ic.Instance, ic.Client, endpoints)
		if syncErr != nil {
			lastErr = syncErr.Error()
			s.logger.WarnContext(ctx, "adult: instance sweep failed",
				slog.String("instance_id", ic.Instance.ID.String()),
				slog.String("error", syncErr.Error()),
			)
			continue
		}
		summary.SourcesSynced++
		summary.PerformersSynced += pCount
		summary.StudiosSynced += sCount
	}
	s.recordSyncResult(summary, lastErr)
	return summary, nil
}

func (s *Service) recordSyncResult(summary SyncSummary, lastErr string) {
	now := s.clk.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastSyncAt = &now
	s.lastSyncErr = lastErr
	s.lastPerformersSynced = summary.PerformersSynced
	s.lastStudiosSynced = summary.StudiosSynced
}
