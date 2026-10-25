// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package watchaware

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/modules"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

const (
	unchangedKey     = "<unchanged>"
	maxStoredWatches = 100000 // HISS-02 upper bound
	batchSize        = 200    // HISS-02 bounded batch size
	defaultGateLimit = 100
	maxGateLimit     = 500
)

// GateSeriesRecord represents stored gate status for a series on an instance.
type GateSeriesRecord struct {
	InstanceID      uuid.UUID
	SeriesID        int
	TvdbID          int
	Title           string
	GateSeason      int
	State           string
	AddedBeforeGate bool
	HeldSeasons     []int
	UpdatedAt       time.Time
}

// GateDecisionRecord represents a recorded gate decision.
type GateDecisionRecord struct {
	ID           uuid.UUID
	InstanceID   uuid.UUID
	SeriesID     int
	SeasonNumber int
	Action       string
	WouldAct     bool
	Reason       string
	DecidedAt    time.Time
}

// Source represents a configured watch-aware media server source without secret credentials.
type Source struct {
	ID         uuid.UUID
	Kind       Kind
	Name       string
	URL        string
	HasAPIKey  bool
	Enabled    bool
	LastSyncAt *time.Time
	LastError  *string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// SourceInput carries user-supplied parameters to create or update a source.
type SourceInput struct {
	Kind    Kind
	Name    string
	URL     string
	APIKey  string
	Enabled bool
}

// SyncStatus summarizes watch state synchronization across all configured sources.
type SyncStatus struct {
	LastSyncAt     *time.Time
	TotalSources   int
	EnabledSources int
	TotalWatches   int
	LastError      *string
}

type moduleManager interface {
	IsEnabled(ctx context.Context, id string) (bool, error)
	Get(ctx context.Context, id string) (domain.Module, error)
}

type instanceManager interface {
	List(ctx context.Context) ([]domain.Instance, error)
	Credentials(ctx context.Context, instID uuid.UUID) (instances.Credentials, error)
}

// Service manages watchaware sources, watch-state persistence (Sink), and sync passes.
type Service struct {
	st       *store.Store
	enc      *crypto.Encryptor
	clk      clock.Clock
	ids      id.Generator
	logger   *slog.Logger
	mod      moduleManager
	inst     instanceManager
	reader   SeriesReader
	mu       sync.Mutex
	lastPass time.Time
}

// NewService wires a new Service.
func NewService(
	st *store.Store,
	enc *crypto.Encryptor,
	clk clock.Clock,
	ids id.Generator,
	logger *slog.Logger,
	mod *modules.Service,
	inst *instances.Service,
	reader SeriesReader,
) *Service {
	var im instanceManager
	if inst != nil {
		im = inst
	}
	return &Service{
		st:     st,
		enc:    enc,
		clk:    clk,
		ids:    ids,
		logger: logger,
		mod:    mod,
		inst:   im,
		reader: reader,
	}
}

// SetSeriesReader sets the SeriesReader, primarily for testing.
func (s *Service) SetSeriesReader(r SeriesReader) {
	s.reader = r
}

// SetInstanceManager sets the instance manager, primarily for testing.
func (s *Service) SetInstanceManager(im instanceManager) {
	s.inst = im
}

func clampGateLimit(limit int) int {
	if limit <= 0 {
		return defaultGateLimit
	}
	if limit > maxGateLimit {
		return maxGateLimit
	}
	return limit
}

// ListGateSeries returns gate series for an instance, including held seasons.
func (s *Service) ListGateSeries(ctx context.Context, instanceID uuid.UUID, limit int) ([]GateSeriesRecord, error) {
	lim := int32(clampGateLimit(limit)) // #nosec G115 -- limit clamped to 500
	rows, err := s.st.Q().ListGateSeries(ctx, sqlcgen.ListGateSeriesParams{
		InstanceID: instanceID,
		Limit:      lim,
	})
	if err != nil {
		return nil, fmt.Errorf("watchaware: list gate series: %w", store.MapError(err))
	}

	out := make([]GateSeriesRecord, 0, len(rows))
	for _, r := range rows {
		held := make([]int, len(r.HeldSeasons))
		for i, v := range r.HeldSeasons {
			held[i] = int(v)
		}
		out = append(out, GateSeriesRecord{
			InstanceID:      r.InstanceID,
			SeriesID:        int(r.SeriesID),
			TvdbID:          int(r.TvdbID),
			Title:           r.Title,
			GateSeason:      int(r.GateSeason),
			State:           r.State,
			AddedBeforeGate: r.AddedBeforeGate,
			HeldSeasons:     held,
			UpdatedAt:       r.UpdatedAt,
		})
	}
	return out, nil
}

// ListGateDecisions returns recorded gate decisions, optionally filtered by instance.
func (s *Service) ListGateDecisions(ctx context.Context, instanceID *uuid.UUID, limit int) ([]GateDecisionRecord, error) {
	lim := int32(clampGateLimit(limit)) // #nosec G115 -- limit clamped to 500
	rows, err := s.st.Q().ListGateDecisions(ctx, sqlcgen.ListGateDecisionsParams{
		InstanceID: instanceID,
		Limit:      lim,
	})
	if err != nil {
		return nil, fmt.Errorf("watchaware: list gate decisions: %w", store.MapError(err))
	}
	out := make([]GateDecisionRecord, 0, len(rows))
	for _, r := range rows {
		out = append(out, GateDecisionRecord{
			ID:           r.ID,
			InstanceID:   r.InstanceID,
			SeriesID:     int(r.SeriesID),
			SeasonNumber: int(r.SeasonNumber),
			Action:       r.Action,
			WouldAct:     r.WouldAct,
			Reason:       r.Reason,
			DecidedAt:    r.DecidedAt,
		})
	}
	return out, nil
}

var _ Sink = (*Service)(nil)

func upsertWatch(ctx context.Context, q *sqlcgen.Queries, srcUUID uuid.UUID, w Watch, now time.Time) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	itemType := w.ItemType
	if itemType == "" {
		itemType = "Episode"
	}
	upsertErr := q.UpsertWatchAwareItemWatch(ctx, sqlcgen.UpsertWatchAwareItemWatchParams{
		SourceID:         srcUUID,
		UserID:           w.UserID,
		ItemID:           w.ItemID,
		UserName:         w.UserName,
		ItemType:         itemType,
		SeriesID:         w.SeriesID,
		SeasonNumber:     int32(min(max(w.SeasonNumber, 0), 1<<20)),  // #nosec G115 -- clamped season number
		EpisodeNumber:    int32(min(max(w.EpisodeNumber, 0), 1<<20)), // #nosec G115 -- clamped episode number
		Played:           w.Played,
		PlayedPercentage: w.PlayedPercentage,
		ResumePosition:   w.PositionTicks,
		LastPlayedAt:     w.LastPlayed,
		SyncedAt:         now,
	})
	if upsertErr != nil {
		return fmt.Errorf("upsert watch %s/%s: %w", w.UserID, w.ItemID, store.MapError(upsertErr))
	}
	return nil
}

// StoreWatches implements Sink. It upserts the watches for a source in bounded batches within a single transaction.
func (s *Service) StoreWatches(ctx context.Context, _ Kind, sourceID string, ws []Watch) error {
	srcUUID, err := uuid.Parse(sourceID)
	if err != nil {
		return fmt.Errorf("watchaware: invalid source id %q: %w", sourceID, err)
	}
	if len(ws) > maxStoredWatches {
		ws = ws[:maxStoredWatches]
	}
	now := s.clk.Now()
	err = s.st.Tx(ctx, func(q *sqlcgen.Queries) error {
		for i := 0; i < len(ws); i += batchSize {
			end := min(i+batchSize, len(ws))
			chunk := ws[i:end]
			for _, w := range chunk {
				if uErr := upsertWatch(ctx, q, srcUUID, w, now); uErr != nil {
					return uErr
				}
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("watchaware: store watches: %w", err)
	}
	return nil
}

// List returns all configured watch-aware sources ordered by name.
func (s *Service) List(ctx context.Context) ([]Source, error) {
	rows, err := s.st.Q().ListWatchAwareSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("watchaware: list sources: %w", store.MapError(err))
	}
	out := make([]Source, 0, len(rows))
	for _, r := range rows {
		out = append(out, sourceFromRow(r))
	}
	return out, nil
}

// Get returns one watch-aware source by ID.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (Source, error) {
	row, err := s.st.Q().GetWatchAwareSource(ctx, id)
	if err != nil {
		return Source{}, fmt.Errorf("watchaware: get source %s: %w", id, store.MapError(err))
	}
	return sourceFromRow(row), nil
}

// Create validates, encrypts the API key, and persists a new watch-aware source.
func (s *Service) Create(ctx context.Context, in SourceInput) (Source, error) {
	norm, err := s.normalize(in, true)
	if err != nil {
		return Source{}, err
	}
	srcID, err := s.ids.NewUUID()
	if err != nil {
		return Source{}, fmt.Errorf("watchaware: generate id: %w", err)
	}
	sealed, err := s.enc.Seal([]byte(norm.APIKey))
	if err != nil {
		return Source{}, fmt.Errorf("watchaware: seal api key: %w", err)
	}
	now := s.clk.Now()
	row, err := s.st.Q().CreateWatchAwareSource(ctx, sqlcgen.CreateWatchAwareSourceParams{
		ID:        srcID,
		Kind:      string(norm.Kind),
		Name:      norm.Name,
		Url:       norm.URL,
		ApiKeyEnc: sealed,
		Enabled:   norm.Enabled,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		return Source{}, fmt.Errorf("watchaware: create source: %w", store.MapError(err))
	}
	return sourceFromRow(row), nil
}

// Update edits a source; an empty API key keeps the existing encrypted key.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in SourceInput) (Source, error) {
	current, err := s.st.Q().GetWatchAwareSource(ctx, id)
	if err != nil {
		return Source{}, fmt.Errorf("watchaware: get source %s: %w", id, store.MapError(err))
	}
	norm, err := s.normalize(in, false)
	if err != nil {
		return Source{}, err
	}
	sealed := current.ApiKeyEnc
	if norm.APIKey != unchangedKey {
		var sealErr error
		sealed, sealErr = s.enc.Seal([]byte(norm.APIKey))
		if sealErr != nil {
			return Source{}, fmt.Errorf("watchaware: seal api key: %w", sealErr)
		}
	}
	now := s.clk.Now()
	row, err := s.st.Q().UpdateWatchAwareSource(ctx, sqlcgen.UpdateWatchAwareSourceParams{
		ID:        id,
		Name:      norm.Name,
		Url:       norm.URL,
		ApiKeyEnc: sealed,
		Enabled:   norm.Enabled,
		UpdatedAt: now,
	})
	if err != nil {
		return Source{}, fmt.Errorf("watchaware: update source %s: %w", id, store.MapError(err))
	}
	return sourceFromRow(row), nil
}

// Delete removes a watch-aware source and cascades to its stored item watches.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	n, err := s.st.Q().DeleteWatchAwareSource(ctx, id)
	if err != nil {
		return fmt.Errorf("watchaware: delete source %s: %w", id, store.MapError(err))
	}
	if n == 0 {
		return fmt.Errorf("watchaware: delete source %s: %w", id, domain.ErrNotFound)
	}
	return nil
}

// GetSyncStatus aggregates synchronization statistics across sources.
func (s *Service) GetSyncStatus(ctx context.Context) (SyncStatus, error) {
	summary, err := s.st.Q().GetWatchAwareSyncSummary(ctx)
	if err != nil {
		return SyncStatus{}, fmt.Errorf("watchaware: get sync summary: %w", store.MapError(err))
	}
	watchCount, err := s.st.Q().CountWatchAwareItemWatches(ctx)
	if err != nil {
		return SyncStatus{}, fmt.Errorf("watchaware: count watches: %w", store.MapError(err))
	}
	var lastErrStr *string
	errRow, err := s.st.Q().GetLatestWatchAwareSourceError(ctx)
	if err == nil && errRow != nil && *errRow != "" {
		lastErrStr = errRow
	}
	var lastSyncAt *time.Time
	if !summary.LastSyncAt.IsZero() && summary.LastSyncAt.Year() > 1 {
		lastSyncAt = &summary.LastSyncAt
	}
	return SyncStatus{
		LastSyncAt:     lastSyncAt,
		TotalSources:   int(summary.TotalSources),
		EnabledSources: int(summary.EnabledSources),
		TotalWatches:   int(watchCount),
		LastError:      lastErrStr,
	}, nil
}

// RecordSync records the success or failure of a sync pass for a source.
func (s *Service) RecordSync(ctx context.Context, sourceID uuid.UUID, syncErr error) error {
	now := s.clk.Now()
	if syncErr == nil {
		err := s.st.Q().RecordWatchAwareSourceSyncSuccess(ctx, sqlcgen.RecordWatchAwareSourceSyncSuccessParams{
			ID:         sourceID,
			LastSyncAt: &now,
		})
		if err != nil {
			return fmt.Errorf("watchaware: record sync success %s: %w", sourceID, store.MapError(err))
		}
		return nil
	}
	errMsg := syncErr.Error()
	err := s.st.Q().RecordWatchAwareSourceSyncError(ctx, sqlcgen.RecordWatchAwareSourceSyncErrorParams{
		ID:        sourceID,
		LastError: &errMsg,
		UpdatedAt: now,
	})
	if err != nil {
		return fmt.Errorf("watchaware: record sync error %s: %w", sourceID, store.MapError(err))
	}
	return nil
}

func validateURL(raw string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(trimmed)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("%w: invalid url %q", domain.ErrInvalid, trimmed)
	}
	return trimmed, nil
}

func validateName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "", fmt.Errorf("%w: name is required", domain.ErrInvalid)
	}
	if len(trimmed) > 64 {
		return "", fmt.Errorf("%w: name must be 64 characters or fewer", domain.ErrInvalid)
	}
	return trimmed, nil
}

func (s *Service) normalize(in SourceInput, isCreate bool) (SourceInput, error) {
	if in.Kind != KindJellyfin && in.Kind != KindEmby {
		return in, fmt.Errorf("%w: kind must be jellyfin or emby", domain.ErrInvalid)
	}
	name, err := validateName(in.Name)
	if err != nil {
		return in, err
	}
	in.Name = name

	u, err := validateURL(in.URL)
	if err != nil {
		return in, err
	}
	in.URL = u

	if isCreate && in.APIKey == "" {
		return in, fmt.Errorf("%w: api_key is required", domain.ErrInvalid)
	}
	if !isCreate && in.APIKey == "" {
		in.APIKey = unchangedKey
	}
	return in, nil
}

func sourceFromRow(r sqlcgen.WatchawareSource) Source {
	return Source{
		ID:         r.ID,
		Kind:       Kind(r.Kind),
		Name:       r.Name,
		URL:        r.Url,
		HasAPIKey:  len(r.ApiKeyEnc) > 0,
		Enabled:    r.Enabled,
		LastSyncAt: r.LastSyncAt,
		LastError:  r.LastError,
		CreatedAt:  r.CreatedAt,
		UpdatedAt:  r.UpdatedAt,
	}
}
