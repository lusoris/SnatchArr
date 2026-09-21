// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package cleanuparr shows what Cleanuparr is doing next to SnatchArr: server status and
// the latest strikes on the dashboard. Read-only; the two tools complement each other
// (ADR-0004): Cleanuparr cleans up what download clients choke on, SnatchArr decides what
// to search for next.
package cleanuparr

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/fx"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/cleanuparrclient"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// Input is the user-supplied part of a link.
type Input struct {
	Name    string
	BaseURL string
	APIKey  string
	Enabled bool
}

// LinkStatus is one link's live view for the dashboard.
type LinkStatus struct {
	Link      domain.CleanuparrLink
	Reachable bool
	Error     string
	Status    cleanuparrclient.Status
	Summary   cleanuparrclient.StrikeSummary
	Recent    []cleanuparrclient.Strike
	CheckedAt time.Time
}

// StatusTTL is how long a live view is reused before Cleanuparr is asked again.
const StatusTTL = 30 * time.Second

// RecentShown is how many strikes the dashboard lists per link.
const RecentShown = 10

const unchangedKey = "<unchanged>"

// Service is the Cleanuparr use-case layer.
type Service struct {
	st     *store.Store
	enc    *crypto.Encryptor
	clk    clock.Clock
	ids    id.Generator
	client cleanuparrclient.Client
	logger *slog.Logger

	mu    sync.Mutex
	cache map[uuid.UUID]LinkStatus
}

// New wires the service.
func New(st *store.Store, enc *crypto.Encryptor, clk clock.Clock, ids id.Generator, client cleanuparrclient.Client, logger *slog.Logger) *Service {
	return &Service{st: st, enc: enc, clk: clk, ids: ids, client: client, logger: logger, cache: make(map[uuid.UUID]LinkStatus)}
}

// List returns every link without secrets.
func (s *Service) List(ctx context.Context) ([]domain.CleanuparrLink, error) {
	rows, err := s.st.Q().ListCleanuparrLinks(ctx)
	if err != nil {
		return nil, fmt.Errorf("cleanuparr: list: %w", store.MapError(err))
	}
	out := make([]domain.CleanuparrLink, 0, len(rows))
	for _, r := range rows {
		out = append(out, store.CleanuparrLinkFromRow(r))
	}
	return out, nil
}

// Get returns one link without its secret.
func (s *Service) Get(ctx context.Context, linkID uuid.UUID) (domain.CleanuparrLink, error) {
	row, err := s.st.Q().GetCleanuparrLink(ctx, linkID)
	if err != nil {
		return domain.CleanuparrLink{}, fmt.Errorf("cleanuparr: get: %w", store.MapError(err))
	}
	return store.CleanuparrLinkFromRow(row), nil
}

// Create validates, encrypts and stores a link.
func (s *Service) Create(ctx context.Context, in Input) (domain.CleanuparrLink, error) {
	in, err := normalize(in)
	if err != nil {
		return domain.CleanuparrLink{}, err
	}
	linkID, err := s.ids.NewUUID()
	if err != nil {
		return domain.CleanuparrLink{}, fmt.Errorf("cleanuparr: new id: %w", err)
	}
	sealed, err := s.enc.Seal([]byte(in.APIKey))
	if err != nil {
		return domain.CleanuparrLink{}, fmt.Errorf("cleanuparr: seal api key: %w", err)
	}
	row, err := s.st.Q().CreateCleanuparrLink(ctx, sqlcgen.CreateCleanuparrLinkParams{
		ID: linkID, Name: in.Name, BaseUrl: in.BaseURL, ApiKeyEnc: sealed, Enabled: in.Enabled, CreatedAt: s.clk.Now(),
	})
	if err != nil {
		return domain.CleanuparrLink{}, fmt.Errorf("cleanuparr: create: %w", store.MapError(err))
	}
	return store.CleanuparrLinkFromRow(row), nil
}

// Update replaces the editable fields; an empty APIKey keeps the stored one.
func (s *Service) Update(ctx context.Context, linkID uuid.UUID, in Input) (domain.CleanuparrLink, error) {
	current, err := s.st.Q().GetCleanuparrLink(ctx, linkID)
	if err != nil {
		return domain.CleanuparrLink{}, fmt.Errorf("cleanuparr: get: %w", store.MapError(err))
	}
	if in.APIKey == "" {
		in.APIKey = unchangedKey
	}
	in, err = normalize(in)
	if err != nil {
		return domain.CleanuparrLink{}, err
	}
	sealed := current.ApiKeyEnc
	if in.APIKey != unchangedKey {
		if sealed, err = s.enc.Seal([]byte(in.APIKey)); err != nil {
			return domain.CleanuparrLink{}, fmt.Errorf("cleanuparr: seal api key: %w", err)
		}
	}
	row, err := s.st.Q().UpdateCleanuparrLink(ctx, sqlcgen.UpdateCleanuparrLinkParams{
		ID: linkID, Name: in.Name, BaseUrl: in.BaseURL, ApiKeyEnc: sealed, Enabled: in.Enabled, UpdatedAt: s.clk.Now(),
	})
	if err != nil {
		return domain.CleanuparrLink{}, fmt.Errorf("cleanuparr: update: %w", store.MapError(err))
	}
	s.forget(linkID)
	return store.CleanuparrLinkFromRow(row), nil
}

// Delete removes a link.
func (s *Service) Delete(ctx context.Context, linkID uuid.UUID) error {
	n, err := s.st.Q().DeleteCleanuparrLink(ctx, linkID)
	if err != nil {
		return fmt.Errorf("cleanuparr: delete: %w", store.MapError(err))
	}
	if n == 0 {
		return fmt.Errorf("cleanuparr: delete: %w", domain.ErrNotFound)
	}
	s.forget(linkID)
	return nil
}

// Test probes a stored link now and records the outcome.
func (s *Service) Test(ctx context.Context, linkID uuid.UUID) (cleanuparrclient.Status, error) {
	row, err := s.st.Q().GetCleanuparrLink(ctx, linkID)
	if err != nil {
		return cleanuparrclient.Status{}, fmt.Errorf("cleanuparr: get: %w", store.MapError(err))
	}
	view := s.observe(ctx, row)
	if !view.Reachable {
		return cleanuparrclient.Status{}, fmt.Errorf("cleanuparr: test: %w: %s", domain.ErrUnreachable, view.Error)
	}
	return view.Status, nil
}

// TestInput probes credentials that are not stored yet.
func (s *Service) TestInput(ctx context.Context, in Input) (cleanuparrclient.Status, error) {
	in, err := normalize(in)
	if err != nil {
		return cleanuparrclient.Status{}, err
	}
	st, err := s.client.Status(ctx, in.BaseURL, in.APIKey)
	if err != nil {
		return st, fmt.Errorf("cleanuparr: test input: %w", err)
	}
	return st, nil
}

// Status returns the (cached) live view of every enabled link.
func (s *Service) Status(ctx context.Context) ([]LinkStatus, error) {
	rows, err := s.st.Q().ListEnabledCleanuparrLinks(ctx)
	if err != nil {
		return nil, fmt.Errorf("cleanuparr: status: %w", store.MapError(err))
	}
	out := make([]LinkStatus, 0, len(rows))
	for _, row := range rows {
		s.mu.Lock()
		view, ok := s.cache[row.ID]
		s.mu.Unlock()
		if ok && s.clk.Now().Before(view.CheckedAt.Add(StatusTTL)) {
			out = append(out, view)
			continue
		}
		out = append(out, s.observe(ctx, row))
	}
	return out, nil
}

// observe asks Cleanuparr for status, summary and recent strikes; a failure anywhere
// marks the link unreachable with the cause.
func (s *Service) observe(ctx context.Context, row sqlcgen.CleanuparrLink) LinkStatus {
	view := LinkStatus{Link: store.CleanuparrLinkFromRow(row), CheckedAt: s.clk.Now()}
	key, err := s.enc.Open(row.ApiKeyEnc)
	if err != nil {
		view.Error = "stored api key cannot be decrypted"
		return s.remember(ctx, view)
	}
	if view.Status, err = s.client.Status(ctx, row.BaseUrl, string(key)); err != nil {
		view.Error = err.Error()
		return s.remember(ctx, view)
	}
	if view.Summary, err = s.client.Summary(ctx, row.BaseUrl, string(key)); err != nil {
		view.Error = err.Error()
		return s.remember(ctx, view)
	}
	if view.Recent, err = s.client.RecentStrikes(ctx, row.BaseUrl, string(key), RecentShown); err != nil {
		view.Error = err.Error()
		return s.remember(ctx, view)
	}
	view.Reachable = true
	return s.remember(ctx, view)
}

func (s *Service) remember(ctx context.Context, view LinkStatus) LinkStatus {
	s.mu.Lock()
	s.cache[view.Link.ID] = view
	s.mu.Unlock()
	var lastErr, version *string
	if view.Reachable {
		version = &view.Status.Version
	} else {
		msg := view.Error
		lastErr = &msg
	}
	err := s.st.Q().RecordCleanuparrCheck(ctx, sqlcgen.RecordCleanuparrCheckParams{ID: view.Link.ID, LastSeenVersion: version, LastCheckAt: &view.CheckedAt, LastError: lastErr})
	if err != nil {
		s.logger.WarnContext(ctx, "cleanuparr: record check", slog.String("error", err.Error()))
	}
	return view
}

func (s *Service) forget(linkID uuid.UUID) {
	s.mu.Lock()
	delete(s.cache, linkID)
	s.mu.Unlock()
}

func normalize(in Input) (Input, error) {
	if err := domain.ValidateCleanuparrLinkInput(in.Name, in.BaseURL, in.APIKey); err != nil {
		return Input{}, fmt.Errorf("cleanuparr: %w", err)
	}
	normalized, err := domain.NormalizeBaseURL(in.BaseURL)
	if err != nil {
		return Input{}, fmt.Errorf("cleanuparr: %w", err)
	}
	in.BaseURL = normalized
	in.Name = strings.TrimSpace(in.Name)
	in.APIKey = strings.TrimSpace(in.APIKey)
	return in, nil
}

// Module provides the service and its HTTP client.
var Module = fx.Module("snatcharr.cleanuparr",
	fx.Provide(
		New,
		func() cleanuparrclient.Client {
			o := arrclient.DefaultOptions()
			return cleanuparrclient.New(o.Timeout, o.UserAgent)
		},
	),
)
