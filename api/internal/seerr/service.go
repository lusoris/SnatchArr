// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package seerr

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/seerrclient"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// Input is the user-supplied part of a Seerr link.
type Input struct {
	Name             string
	BaseURL          string
	APIKey           string
	Enabled          bool
	SonarrInstanceID *uuid.UUID
	RadarrInstanceID *uuid.UUID
}

// SyncResult summarises one request sync.
type SyncResult struct {
	Seen       int
	Resolved   int
	Unresolved int
	Removed    int
}

// ImportResult summarises an instance import from Seerr's server settings.
type ImportResult struct {
	Created []string
	Skipped []string
}

// RequestView is a cached request with the names the dashboard shows.
type RequestView struct {
	domain.SeerrRequest
	LinkName     string
	InstanceName string
}

// MaxRequestsListed bounds the dashboard query (HISS-02).
const MaxRequestsListed = 500

const unchangedKey = "<unchanged>"

// Service is the Seerr use-case layer.
type Service struct {
	st        *store.Store
	enc       *crypto.Encryptor
	clk       clock.Clock
	ids       id.Generator
	client    seerrclient.Client
	resolver  arrclient.Resolver
	instances *instances.Service
	logger    *slog.Logger
}

// New wires the service.
func New(st *store.Store, enc *crypto.Encryptor, clk clock.Clock, ids id.Generator, client seerrclient.Client,
	resolver arrclient.Resolver, inst *instances.Service, logger *slog.Logger,
) *Service {
	return &Service{st: st, enc: enc, clk: clk, ids: ids, client: client, resolver: resolver, instances: inst, logger: logger}
}

// List returns every link without secrets.
func (s *Service) List(ctx context.Context) ([]domain.SeerrLink, error) {
	rows, err := s.st.Q().ListSeerrLinks(ctx)
	if err != nil {
		return nil, fmt.Errorf("seerr: list: %w", store.MapError(err))
	}
	out := make([]domain.SeerrLink, 0, len(rows))
	for _, r := range rows {
		out = append(out, store.SeerrLinkFromRow(r))
	}
	return out, nil
}

// Get returns one link without its secret.
func (s *Service) Get(ctx context.Context, linkID uuid.UUID) (domain.SeerrLink, error) {
	row, err := s.st.Q().GetSeerrLink(ctx, linkID)
	if err != nil {
		return domain.SeerrLink{}, fmt.Errorf("seerr: get: %w", store.MapError(err))
	}
	return store.SeerrLinkFromRow(row), nil
}

// Create validates, encrypts and stores a link.
func (s *Service) Create(ctx context.Context, in Input) (domain.SeerrLink, error) {
	in, err := s.normalize(ctx, in)
	if err != nil {
		return domain.SeerrLink{}, err
	}
	linkID, err := s.ids.NewUUID()
	if err != nil {
		return domain.SeerrLink{}, fmt.Errorf("seerr: new id: %w", err)
	}
	sealed, err := s.enc.Seal([]byte(in.APIKey))
	if err != nil {
		return domain.SeerrLink{}, fmt.Errorf("seerr: seal api key: %w", err)
	}
	row, err := s.st.Q().CreateSeerrLink(ctx, sqlcgen.CreateSeerrLinkParams{
		ID: linkID, Name: in.Name, BaseUrl: in.BaseURL, ApiKeyEnc: sealed, Enabled: in.Enabled,
		SonarrInstanceID: in.SonarrInstanceID, RadarrInstanceID: in.RadarrInstanceID, CreatedAt: s.clk.Now(),
	})
	if err != nil {
		return domain.SeerrLink{}, fmt.Errorf("seerr: create: %w", store.MapError(err))
	}
	return store.SeerrLinkFromRow(row), nil
}

// Update replaces the editable fields; an empty APIKey keeps the stored one.
func (s *Service) Update(ctx context.Context, linkID uuid.UUID, in Input) (domain.SeerrLink, error) {
	current, err := s.st.Q().GetSeerrLink(ctx, linkID)
	if err != nil {
		return domain.SeerrLink{}, fmt.Errorf("seerr: get: %w", store.MapError(err))
	}
	if in.APIKey == "" {
		in.APIKey = unchangedKey
	}
	in, err = s.normalize(ctx, in)
	if err != nil {
		return domain.SeerrLink{}, err
	}
	sealed := current.ApiKeyEnc
	if in.APIKey != unchangedKey {
		if sealed, err = s.enc.Seal([]byte(in.APIKey)); err != nil {
			return domain.SeerrLink{}, fmt.Errorf("seerr: seal api key: %w", err)
		}
	}
	row, err := s.st.Q().UpdateSeerrLink(ctx, sqlcgen.UpdateSeerrLinkParams{
		ID: linkID, Name: in.Name, BaseUrl: in.BaseURL, ApiKeyEnc: sealed, Enabled: in.Enabled,
		SonarrInstanceID: in.SonarrInstanceID, RadarrInstanceID: in.RadarrInstanceID, UpdatedAt: s.clk.Now(),
	})
	if err != nil {
		return domain.SeerrLink{}, fmt.Errorf("seerr: update: %w", store.MapError(err))
	}
	return store.SeerrLinkFromRow(row), nil
}

// Delete removes a link and its cached requests.
func (s *Service) Delete(ctx context.Context, linkID uuid.UUID) error {
	n, err := s.st.Q().DeleteSeerrLink(ctx, linkID)
	if err != nil {
		return fmt.Errorf("seerr: delete: %w", store.MapError(err))
	}
	if n == 0 {
		return fmt.Errorf("seerr: delete: %w", domain.ErrNotFound)
	}
	return nil
}

// Test probes a stored link and records the outcome.
func (s *Service) Test(ctx context.Context, linkID uuid.UUID) (seerrclient.Status, error) {
	link, key, err := s.credentials(ctx, linkID)
	if err != nil {
		return seerrclient.Status{}, err
	}
	st, probeErr := s.client.Status(ctx, link.BaseURL, key)
	s.recordCheck(ctx, linkID, st, probeErr)
	if probeErr != nil {
		return st, fmt.Errorf("seerr: test: %w", probeErr)
	}
	return st, nil
}

// TestInput probes credentials that are not stored yet.
func (s *Service) TestInput(ctx context.Context, in Input) (seerrclient.Status, error) {
	in, err := s.normalize(ctx, in)
	if err != nil {
		return seerrclient.Status{}, err
	}
	st, err := s.client.Status(ctx, in.BaseURL, in.APIKey)
	if err != nil {
		return st, fmt.Errorf("seerr: test input: %w", err)
	}
	return st, nil
}

func (s *Service) recordCheck(ctx context.Context, linkID uuid.UUID, st seerrclient.Status, probeErr error) {
	var lastErr, version *string
	if probeErr != nil {
		msg := probeErr.Error()
		lastErr = &msg
	} else {
		version = &st.Version
	}
	err := s.st.Q().RecordSeerrCheck(ctx, sqlcgen.RecordSeerrCheckParams{ID: linkID, LastSeenVersion: version, LastCheckAt: new(s.clk.Now()), LastError: lastErr})
	if err != nil {
		s.logger.WarnContext(ctx, "seerr: record check", slog.String("error", err.Error()))
	}
}

func (s *Service) credentials(ctx context.Context, linkID uuid.UUID) (domain.SeerrLink, string, error) {
	row, err := s.st.Q().GetSeerrLink(ctx, linkID)
	if err != nil {
		return domain.SeerrLink{}, "", fmt.Errorf("seerr: get: %w", store.MapError(err))
	}
	key, err := s.enc.Open(row.ApiKeyEnc)
	if err != nil {
		return domain.SeerrLink{}, "", fmt.Errorf("seerr: open api key: %w", err)
	}
	return store.SeerrLinkFromRow(row), string(key), nil
}

func (s *Service) normalize(ctx context.Context, in Input) (Input, error) {
	if err := domain.ValidateSeerrLinkInput(in.Name, in.BaseURL, in.APIKey); err != nil {
		return Input{}, fmt.Errorf("seerr: %w", err)
	}
	normalized, err := domain.NormalizeBaseURL(in.BaseURL)
	if err != nil {
		return Input{}, fmt.Errorf("seerr: %w", err)
	}
	for _, ref := range []*uuid.UUID{in.SonarrInstanceID, in.RadarrInstanceID} {
		if ref == nil {
			continue
		}
		if _, err := s.instances.Get(ctx, *ref); err != nil {
			return Input{}, fmt.Errorf("seerr: fallback instance: %w", err)
		}
	}
	in.BaseURL = normalized
	in.Name = strings.TrimSpace(in.Name)
	in.APIKey = strings.TrimSpace(in.APIKey)
	return in, nil
}

// Requests lists the cached requests for the dashboard, newest first.
func (s *Service) Requests(ctx context.Context) ([]RequestView, error) {
	rows, err := s.st.Q().ListSeerrRequests(ctx, MaxRequestsListed)
	if err != nil {
		return nil, fmt.Errorf("seerr: requests: %w", store.MapError(err))
	}
	out := make([]RequestView, 0, len(rows))
	for _, row := range rows {
		out = append(out, RequestView{SeerrRequest: store.SeerrRequestFromListRow(row), LinkName: row.LinkName, InstanceName: derefStr(row.InstanceName)})
	}
	return out, nil
}

// PriorityFor returns the library entity ids (movies or series) that open requests point
// at for one instance; the worker snatches those first.
func (s *Service) PriorityFor(ctx context.Context, instanceID uuid.UUID) ([]int64, error) {
	ids, err := s.st.Q().PriorityEntityIDs(ctx, &instanceID)
	if err != nil {
		return nil, fmt.Errorf("seerr: priority: %w", store.MapError(err))
	}
	return ids, nil
}

// MarkSnatched stamps requests whose entity was just searched on an instance.
func (s *Service) MarkSnatched(ctx context.Context, instanceID uuid.UUID, entityIDs []int64) error {
	if len(entityIDs) == 0 {
		return nil
	}
	_, err := s.st.Q().MarkSeerrRequestsSnatched(ctx, sqlcgen.MarkSeerrRequestsSnatchedParams{InstanceID: &instanceID, LastSnatchedAt: new(s.clk.Now()), EntityIds: entityIDs})
	if err != nil {
		return fmt.Errorf("seerr: mark snatched: %w", store.MapError(err))
	}
	return nil
}

// ImportInstances creates a SnatchArr instance for every Sonarr/Radarr server Seerr has
// configured, skipping URLs that already exist.
func (s *Service) ImportInstances(ctx context.Context, linkID uuid.UUID) (ImportResult, error) {
	link, key, err := s.credentials(ctx, linkID)
	if err != nil {
		return ImportResult{}, err
	}
	servers, err := s.client.Servers(ctx, link.BaseURL, key)
	if err != nil {
		return ImportResult{}, fmt.Errorf("seerr: import: %w", err)
	}
	var res ImportResult
	for _, srv := range servers {
		name := "Seerr: " + strings.TrimSpace(srv.Name)
		if srv.Name == "" {
			name = fmt.Sprintf("Seerr: %s %d", srv.Kind, srv.ID)
		}
		_, err := s.instances.Create(ctx, instances.Input{Kind: srv.Kind, Name: name, BaseURL: srv.BaseURL, APIKey: srv.APIKey, Enabled: true, Source: domain.SourceSeerr})
		switch {
		case err == nil:
			res.Created = append(res.Created, name)
		case errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrInvalid):
			res.Skipped = append(res.Skipped, name+": "+err.Error())
		default:
			return res, err
		}
	}
	return res, nil
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// UnresolvedReason explains why a request is not snatched yet.
func UnresolvedReason(r domain.SeerrRequest) string {
	switch {
	case r.InstanceID == nil:
		return "no SnatchArr instance matches the Seerr server; set a fallback instance on the link"
	case r.EntityID == nil:
		return "not in the *arr library yet (Seerr has not added it, or the lookup id is missing)"
	default:
		return ""
	}
}

// staleAfter is how far in the past a request's last_seen_at may lie before a sync
// removes it (one sync's tolerance for clock skew).
const staleAfter = time.Second
