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

func (s *Service) loadPerformerCandidates(ctx context.Context) ([]Candidate, error) {
	rows, err := s.st.Q().ListAdultFavoritePerformers(ctx)
	if err != nil {
		return nil, fmt.Errorf("adult: list performers: %w", store.MapError(err))
	}
	links, err := s.st.Q().ListAdultPerformerLinks(ctx)
	if err != nil {
		return nil, fmt.Errorf("adult: list performer links: %w", store.MapError(err))
	}
	linkMap := make(map[uuid.UUID][]EntityLink, len(rows))
	for _, l := range links {
		linkMap[l.FavoriteID] = append(linkMap[l.FavoriteID], EntityLink{
			InstanceID: l.InstanceID,
			ForeignID:  l.ForeignID,
		})
	}
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
	rows, err := s.st.Q().ListAdultFavoriteStudios(ctx)
	if err != nil {
		return nil, fmt.Errorf("adult: list studios: %w", store.MapError(err))
	}
	links, err := s.st.Q().ListAdultStudioLinks(ctx)
	if err != nil {
		return nil, fmt.Errorf("adult: list studio links: %w", store.MapError(err))
	}
	linkMap := make(map[uuid.UUID][]EntityLink, len(rows))
	for _, l := range links {
		linkMap[l.FavoriteID] = append(linkMap[l.FavoriteID], EntityLink{
			InstanceID: l.InstanceID,
			ForeignID:  l.ForeignID,
		})
	}
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

	_, err = s.st.Q().UpsertAdultFavoritePerformer(ctx, sqlcgen.UpsertAdultFavoritePerformerParams{
		ID:             favID,
		CanonicalName:  p.Name,
		Disambiguation: dis,
		Urls:           finalURLs,
		CreatedAt:      sweepStart,
		UpdatedAt:      sweepStart,
	})
	if err != nil {
		return fmt.Errorf("adult: upsert performer: %w", store.MapError(err))
	}

	_, err = s.st.Q().UpsertAdultPerformerLink(ctx, sqlcgen.UpsertAdultPerformerLinkParams{
		FavoriteID:   favID,
		InstanceID:   instID,
		ForeignID:    p.ID,
		IsFavorite:   true,
		LastSyncedAt: sweepStart,
	})
	if err != nil {
		return fmt.Errorf("adult: upsert performer link: %w", store.MapError(err))
	}
	return nil
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

	_, err = s.st.Q().UpsertAdultFavoriteStudio(ctx, sqlcgen.UpsertAdultFavoriteStudioParams{
		ID:            favID,
		CanonicalName: st.Name,
		Urls:          finalURLs,
		CreatedAt:     sweepStart,
		UpdatedAt:     sweepStart,
	})
	if err != nil {
		return fmt.Errorf("adult: upsert studio: %w", store.MapError(err))
	}

	_, err = s.st.Q().UpsertAdultStudioLink(ctx, sqlcgen.UpsertAdultStudioLinkParams{
		FavoriteID:   favID,
		InstanceID:   instID,
		ForeignID:    st.ID,
		IsFavorite:   true,
		LastSyncedAt: sweepStart,
	})
	if err != nil {
		return fmt.Errorf("adult: upsert studio link: %w", store.MapError(err))
	}
	return nil
}

func (s *Service) syncPerformers(
	ctx context.Context,
	instID uuid.UUID,
	client *stashbox.Client,
	endpoints []StashboxEndpoint,
	sweepStart time.Time,
) error {
	items, truncated, err := fetchFavoritePerformers(ctx, client)
	if err != nil {
		return err
	}
	cands, err := s.loadPerformerCandidates(ctx)
	if err != nil {
		return err
	}
	for _, p := range items {
		if syncErr := s.syncOnePerformer(ctx, instID, p, &cands, endpoints, sweepStart); syncErr != nil {
			return syncErr
		}
	}
	if truncated {
		s.logger.InfoContext(ctx, "adult: performer sync reached cap; skipping stale link deactivation",
			slog.String("instance_id", instID.String()),
			slog.Int("count", len(items)),
			slog.Int("cap", MaxItemsPerSweep),
		)
		return nil
	}
	_, err = s.st.Q().MarkStaleAdultPerformerLinksUnfavorite(ctx, sqlcgen.MarkStaleAdultPerformerLinksUnfavoriteParams{
		InstanceID:   instID,
		LastSyncedAt: sweepStart,
	})
	if err != nil {
		return fmt.Errorf("adult: mark stale performer links: %w", store.MapError(err))
	}
	return nil
}

func (s *Service) syncStudios(
	ctx context.Context,
	instID uuid.UUID,
	client *stashbox.Client,
	endpoints []StashboxEndpoint,
	sweepStart time.Time,
) error {
	items, truncated, err := fetchFavoriteStudios(ctx, client)
	if err != nil {
		return err
	}
	cands, err := s.loadStudioCandidates(ctx)
	if err != nil {
		return err
	}
	for _, st := range items {
		if syncErr := s.syncOneStudio(ctx, instID, st, &cands, endpoints, sweepStart); syncErr != nil {
			return syncErr
		}
	}
	if truncated {
		s.logger.InfoContext(ctx, "adult: studio sync reached cap; skipping stale link deactivation",
			slog.String("instance_id", instID.String()),
			slog.Int("count", len(items)),
			slog.Int("cap", MaxItemsPerSweep),
		)
		return nil
	}
	_, err = s.st.Q().MarkStaleAdultStudioLinksUnfavorite(ctx, sqlcgen.MarkStaleAdultStudioLinksUnfavoriteParams{
		InstanceID:   instID,
		LastSyncedAt: sweepStart,
	})
	if err != nil {
		return fmt.Errorf("adult: mark stale studio links: %w", store.MapError(err))
	}
	return nil
}

// SyncInstance synchronizes favorite performers and studios for a single stashbox instance.
func (s *Service) SyncInstance(
	ctx context.Context,
	inst sqlcgen.AdultStashboxInstance,
	client *stashbox.Client,
	endpoints []StashboxEndpoint,
) error {
	sweepStart := s.clk.Now()
	if err := s.syncPerformers(ctx, inst.ID, client, endpoints, sweepStart); err != nil {
		return s.wrapSyncError(inst.Name, err)
	}
	if err := s.syncStudios(ctx, inst.ID, client, endpoints, sweepStart); err != nil {
		return s.wrapSyncError(inst.Name, err)
	}
	return nil
}

// SyncAll runs a sync sweep across all enabled stash-box instances that have sync_favorites enabled.
// One bad instance never aborts the sweep.
func (s *Service) SyncAll(ctx context.Context) error {
	rows, err := s.st.Q().ListSyncableAdultStashboxInstances(ctx)
	if err != nil {
		return fmt.Errorf("adult: list syncable instances: %w", store.MapError(err))
	}
	if len(rows) == 0 {
		return nil
	}

	endpoints := make([]StashboxEndpoint, 0, len(rows))
	for _, r := range rows {
		endpoints = append(endpoints, StashboxEndpoint{InstanceID: r.ID, Endpoint: r.Endpoint})
	}

	clients := s.pool.BuildClients(ctx, rows)
	for _, ic := range clients {
		if err := s.SyncInstance(ctx, ic.Instance, ic.Client, endpoints); err != nil {
			s.logger.WarnContext(ctx, "adult: instance sweep failed",
				slog.String("instance_id", ic.Instance.ID.String()),
				slog.String("instance_name", ic.Instance.Name),
				slog.String("error", err.Error()),
			)
		}
	}
	return nil
}
