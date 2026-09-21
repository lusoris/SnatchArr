// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package seerr

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/seerrclient"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// syncContext carries what every request of one sync shares.
type syncContext struct {
	link        domain.SeerrLink
	key         string
	servers     []seerrclient.Server
	idx         instanceIndex
	titleBudget int
	credentials map[uuid.UUID]instanceCreds
}

type instanceCreds struct {
	kind    domain.AppKind
	baseURL string
	apiKey  string
}

// Sync refreshes the request cache of one link: fetches the approved-but-unavailable
// requests, maps each onto an instance and a library entity, and drops requests Seerr no
// longer lists.
func (s *Service) Sync(ctx context.Context, linkID uuid.UUID) (SyncResult, error) {
	link, key, err := s.credentials(ctx, linkID)
	if err != nil {
		return SyncResult{}, err
	}
	sc, err := s.prepareSync(ctx, link, key)
	if err != nil {
		s.recordSync(ctx, linkID, err)
		return SyncResult{}, err
	}
	reqs, err := s.client.ProcessingRequests(ctx, link.BaseURL, key)
	if err != nil {
		s.recordSync(ctx, linkID, err)
		return SyncResult{}, fmt.Errorf("seerr: sync: %w", err)
	}
	startedAt := s.clk.Now()
	var res SyncResult
	for _, r := range reqs {
		resolved, syncErr := s.syncOne(ctx, sc, r)
		if syncErr != nil {
			s.recordSync(ctx, linkID, syncErr)
			return res, syncErr
		}
		res.Seen++
		if resolved {
			res.Resolved++
		} else {
			res.Unresolved++
		}
	}
	removed, err := s.st.Q().DeleteUnseenSeerrRequests(ctx, sqlcgen.DeleteUnseenSeerrRequestsParams{LinkID: linkID, LastSeenAt: startedAt.Add(-staleAfter)})
	if err != nil {
		return res, fmt.Errorf("seerr: prune: %w", store.MapError(err))
	}
	res.Removed = int(removed)
	s.recordSync(ctx, linkID, nil)
	return res, nil
}

func (s *Service) prepareSync(ctx context.Context, link domain.SeerrLink, key string) (*syncContext, error) {
	servers, err := s.client.Servers(ctx, link.BaseURL, key)
	if err != nil {
		// Settings need an admin key; without them the link's fallback instances still work.
		s.logger.WarnContext(ctx, "seerr: servers unavailable; using fallbacks", slog.String("link", link.Name), slog.String("error", err.Error()))
	}
	list, err := s.instances.List(ctx)
	if err != nil {
		return nil, err
	}
	return &syncContext{
		link: link, key: key, servers: servers, idx: indexInstances(list),
		titleBudget: seerrclient.MaxTitleLookups, credentials: make(map[uuid.UUID]instanceCreds),
	}, nil
}

// syncOne upserts one request; the bool says whether it maps to an instance and entity.
func (s *Service) syncOne(ctx context.Context, sc *syncContext, r seerrclient.Request) (bool, error) {
	mt, ok := mediaTypeOf(r)
	if !ok {
		return false, nil
	}
	instID := resolveInstance(mt, r, sc.servers, sc.idx, sc.link)
	entityID := s.resolveEntity(ctx, sc, instID, mt, r)
	title := s.titleFor(ctx, sc, mt, r)
	var serverID *int32
	if r.ServerID != nil {
		v := int32(min(max(*r.ServerID, 0), 1<<30)) // #nosec G115 -- clamped
		serverID = &v
	}
	tvdb := 0
	if r.Media.TvdbID != nil {
		tvdb = *r.Media.TvdbID
	}
	err := s.st.Q().UpsertSeerrRequest(ctx, sqlcgen.UpsertSeerrRequestParams{
		LinkID: sc.link.ID, RequestID: int32(min(max(r.ID, 0), 1<<30)), MediaType: string(mt), // #nosec G115 -- clamped
		TmdbID: int32(min(max(r.Media.TmdbID, 0), 1<<30)), TvdbID: int32(min(max(tvdb, 0), 1<<30)), // #nosec G115 -- clamped
		Title: title, RequestStatus: int32(min(max(r.Status, 0), 100)), MediaStatus: int32(min(max(r.Media.Status, 0), 100)), // #nosec G115 -- clamped
		Is4k: r.Is4K, RequestedBy: r.Requester(), Seasons: seasonsOf(r), SeerrServerID: serverID,
		InstanceID: instID, EntityID: entityID, RequestedAt: parseSeerrTime(r.CreatedAt), LastSeenAt: s.clk.Now(),
	})
	if err != nil {
		return false, fmt.Errorf("seerr: upsert request: %w", store.MapError(err))
	}
	return instID != nil && entityID != nil, nil
}

// resolveEntity asks the instance for the library id behind the request's TMDB/TVDB id.
// Lookup failures leave the request unresolved for this sync rather than failing it.
func (s *Service) resolveEntity(ctx context.Context, sc *syncContext, instID *uuid.UUID, mt domain.SeerrMediaType, r seerrclient.Request) *int64 {
	if instID == nil {
		return nil
	}
	creds, ok := sc.credentials[*instID]
	if !ok {
		c, err := s.instances.Credentials(ctx, *instID)
		if err != nil {
			s.logger.WarnContext(ctx, "seerr: instance credentials", slog.String("error", err.Error()))
			return nil
		}
		inst, err := s.instances.Get(ctx, *instID)
		if err != nil {
			return nil
		}
		creds = instanceCreds{kind: inst.Kind, baseURL: c.BaseURL, apiKey: c.APIKey}
		sc.credentials[*instID] = creds
	}
	tvdb := 0
	if r.Media.TvdbID != nil {
		tvdb = *r.Media.TvdbID
	}
	entity, found, err := s.resolver.EntityByExternalID(ctx, creds.kind, creds.baseURL, creds.apiKey, r.Media.TmdbID, tvdb)
	if err != nil {
		s.logger.WarnContext(ctx, "seerr: entity lookup", slog.String("media", string(mt)), slog.Int("request", r.ID), slog.String("error", err.Error()))
		return nil
	}
	if !found {
		return nil
	}
	return &entity
}

// titleFor reuses the cached title and otherwise spends one lookup from the sync budget.
func (s *Service) titleFor(ctx context.Context, sc *syncContext, mt domain.SeerrMediaType, r seerrclient.Request) string {
	existing, err := s.st.Q().GetSeerrRequestTitle(ctx, sqlcgen.GetSeerrRequestTitleParams{LinkID: sc.link.ID, RequestID: int32(min(max(r.ID, 0), 1<<30))}) // #nosec G115 -- clamped
	if err == nil && existing != "" {
		return existing
	}
	if sc.titleBudget <= 0 || r.Media.TmdbID <= 0 {
		return ""
	}
	sc.titleBudget--
	title, err := s.client.Title(ctx, sc.link.BaseURL, sc.key, mt, r.Media.TmdbID)
	if err != nil {
		s.logger.DebugContext(ctx, "seerr: title lookup", slog.Int("tmdb", r.Media.TmdbID), slog.String("error", err.Error()))
		return ""
	}
	return title
}

func (s *Service) recordSync(ctx context.Context, linkID uuid.UUID, syncErr error) {
	var lastErr *string
	if syncErr != nil {
		msg := syncErr.Error()
		lastErr = &msg
	}
	if err := s.st.Q().RecordSeerrSync(ctx, sqlcgen.RecordSeerrSyncParams{ID: linkID, LastSyncAt: new(s.clk.Now()), LastError: lastErr}); err != nil {
		s.logger.WarnContext(ctx, "seerr: record sync", slog.String("error", err.Error()))
	}
}

// SyncAll syncs every enabled link; errors are logged per link and never abort the loop.
func (s *Service) SyncAll(ctx context.Context) {
	rows, err := s.st.Q().ListEnabledSeerrLinks(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "seerr: list links", slog.String("error", err.Error()))
		return
	}
	for _, row := range rows {
		if _, err := s.Sync(ctx, row.ID); err != nil {
			s.logger.WarnContext(ctx, "seerr: sync", slog.String("link", row.Name), slog.String("error", err.Error()))
		}
	}
}
