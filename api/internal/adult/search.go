// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/golusoris/goenvoy/metadata/adult/stashbox/v2"

	"github.com/lusoris/SnatchArr/api/internal/store"
)

const (
	// MaxSearchConcurrency limits the maximum number of concurrent instance workers during search (HISS-06).
	MaxSearchConcurrency = 4
	// SearchTimeout is the deadline for each instance search call.
	SearchTimeout = 15 * time.Second
)

// SearchResults encapsulates deduplicated performer and studio matches from federated search.
type SearchResults struct {
	Performers []stashbox.Performer `json:"performers"`
	Studios    []stashbox.Studio    `json:"studios"`
}

type instanceSearchResult struct {
	performers []stashbox.Performer
	studios    []stashbox.Studio
	err        error
}

func deduplicatePerformers(items []stashbox.Performer, endpoints []StashboxEndpoint) []stashbox.Performer {
	var deduped []stashbox.Performer
	var candidates []Candidate
	for _, p := range items {
		var urls []string
		for _, u := range p.URLs {
			if u.URL != "" {
				urls = append(urls, u.URL)
			}
		}
		inc := IncomingEntity{
			Name:           p.Name,
			Disambiguation: p.Disambiguation,
			URLs:           urls,
		}
		matched, _ := MatchEntity(inc, candidates, endpoints)
		if matched != nil {
			matched.URLs = append(matched.URLs, urls...)
			continue
		}
		candidates = append(candidates, Candidate{
			Name:           p.Name,
			Disambiguation: p.Disambiguation,
			URLs:           urls,
		})
		deduped = append(deduped, p)
	}
	return deduped
}

func deduplicateStudios(items []stashbox.Studio, endpoints []StashboxEndpoint) []stashbox.Studio {
	var deduped []stashbox.Studio
	var candidates []Candidate
	for _, st := range items {
		var urls []string
		for _, u := range st.URLs {
			if u.URL != "" {
				urls = append(urls, u.URL)
			}
		}
		inc := IncomingEntity{
			Name: st.Name,
			URLs: urls,
		}
		matched, _ := MatchEntity(inc, candidates, endpoints)
		if matched != nil {
			matched.URLs = append(matched.URLs, urls...)
			continue
		}
		candidates = append(candidates, Candidate{
			Name: st.Name,
			URLs: urls,
		})
		deduped = append(deduped, st)
	}
	return deduped
}

func (s *Service) searchInstance(ctx context.Context, ic InstanceClient, query string) instanceSearchResult {
	searchCtx, cancel := context.WithTimeout(ctx, SearchTimeout)
	defer cancel()

	performers, pErr := ic.Client.SearchPerformers(searchCtx, query, DefaultPerPage)
	studios, sErr := ic.Client.SearchStudios(searchCtx, query)
	if pErr != nil && sErr != nil {
		return instanceSearchResult{err: fmt.Errorf("search instance %q: performers: %w, studios: %w", ic.Instance.Name, pErr, sErr)}
	}
	if pErr != nil {
		s.logger.WarnContext(ctx, "adult: performer search failed for instance",
			slog.String("instance_id", ic.Instance.ID.String()),
			slog.String("error", pErr.Error()),
		)
	}
	if sErr != nil {
		s.logger.WarnContext(ctx, "adult: studio search failed for instance",
			slog.String("instance_id", ic.Instance.ID.String()),
			slog.String("error", sErr.Error()),
		)
	}
	return instanceSearchResult{performers: performers, studios: studios}
}

// Search fans out across enabled stash-box instances using a bounded worker pool and deduplicates matches.
func (s *Service) Search(ctx context.Context, query string) (SearchResults, error) {
	rows, err := s.st.Q().ListEnabledAdultStashboxInstances(ctx)
	if err != nil {
		return SearchResults{}, fmt.Errorf("adult: list instances for search: %w", store.MapError(err))
	}
	if len(rows) == 0 {
		return SearchResults{}, nil
	}

	endpoints := make([]StashboxEndpoint, 0, len(rows))
	for _, r := range rows {
		endpoints = append(endpoints, StashboxEndpoint{InstanceID: r.ID, Endpoint: r.Endpoint})
	}

	clients := s.pool.BuildClients(ctx, rows)
	if len(clients) == 0 {
		return SearchResults{}, nil
	}

	concurrency := min(len(clients), MaxSearchConcurrency)
	jobs := make(chan InstanceClient, len(clients))
	for _, ic := range clients {
		jobs <- ic
	}
	close(jobs)

	resChan := make(chan instanceSearchResult, len(clients))
	var wg sync.WaitGroup
	for range concurrency {
		wg.Go(func() {
			for job := range jobs {
				res := s.searchInstance(ctx, job, query)
				resChan <- res
			}
		})
	}
	wg.Wait()
	close(resChan)

	var allPerformers []stashbox.Performer
	var allStudios []stashbox.Studio
	for res := range resChan {
		if res.err != nil {
			s.logger.WarnContext(ctx, "adult: instance search error", slog.String("error", res.err.Error()))
			continue
		}
		allPerformers = append(allPerformers, res.performers...)
		allStudios = append(allStudios, res.studios...)
	}

	return SearchResults{
		Performers: deduplicatePerformers(allPerformers, endpoints),
		Studios:    deduplicateStudios(allStudios, endpoints),
	}, nil
}
