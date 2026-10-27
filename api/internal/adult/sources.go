// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

const (
	// MaxSourceNameLen is the maximum character length of a catalogue source name.
	MaxSourceNameLen = 128
	// MaxSourceEndpointLen is the maximum character length of a catalogue source endpoint URL.
	MaxSourceEndpointLen = 512
)

// Source represents a configured catalogue source without exposing its secret API key.
type Source struct {
	ID            uuid.UUID
	Name          string
	Endpoint      string
	HasAPIKey     bool
	Enabled       bool
	SyncFavorites bool
	Priority      int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// SourceInput carries parameters for creating or updating a catalogue source.
type SourceInput struct {
	Name          string
	Endpoint      string
	APIKey        string
	Enabled       bool
	SyncFavorites bool
	Priority      int
}

// SyncSummary reports counts from an on-demand favourite synchronization sweep.
type SyncSummary struct {
	SourcesSynced    int
	PerformersSynced int
	StudiosSynced    int
}

// SyncStatus reports the aggregate synchronization state across catalogue sources.
type SyncStatus struct {
	TotalSources     int
	EnabledSources   int
	PerformersSynced int
	StudiosSynced    int
	LastSyncAt       *time.Time
	LastError        string
}

func toSource(row sqlcgen.AdultStashboxInstance) Source {
	return Source{
		ID:            row.ID,
		Name:          row.Name,
		Endpoint:      row.Endpoint,
		HasAPIKey:     len(row.ApiKeyEnc) > 0,
		Enabled:       row.Enabled,
		SyncFavorites: row.SyncFavorites,
		Priority:      int(row.Priority),
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}
}

func validateEndpoint(raw string) (string, error) {
	endpoint := strings.TrimSpace(raw)
	if endpoint == "" || len(endpoint) > MaxSourceEndpointLen {
		return "", fmt.Errorf("%w: endpoint must be 1..%d characters", domain.ErrInvalid, MaxSourceEndpointLen)
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("%w: invalid endpoint url", domain.ErrInvalid)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("%w: invalid endpoint url", domain.ErrInvalid)
	}
	return endpoint, nil
}

func validateSourceInput(in SourceInput, requireKey bool) (SourceInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > MaxSourceNameLen {
		return SourceInput{}, fmt.Errorf("%w: source name must be 1..%d characters", domain.ErrInvalid, MaxSourceNameLen)
	}
	ep, err := validateEndpoint(in.Endpoint)
	if err != nil {
		return SourceInput{}, err
	}
	in.Endpoint = ep
	in.APIKey = strings.TrimSpace(in.APIKey)
	if requireKey && in.APIKey == "" {
		return SourceInput{}, fmt.Errorf("%w: api_key is required", domain.ErrInvalid)
	}
	return in, nil
}

// ListSources returns all configured catalogue sources ordered by priority and name.
func (s *Service) ListSources(ctx context.Context) ([]Source, error) {
	rows, err := s.st.Q().ListAdultStashboxInstances(ctx)
	if err != nil {
		return nil, fmt.Errorf("adult: list sources: %w", store.MapError(err))
	}
	out := make([]Source, 0, len(rows))
	for _, r := range rows {
		out = append(out, toSource(r))
	}
	return out, nil
}

// GetSource returns a single catalogue source by ID.
func (s *Service) GetSource(ctx context.Context, id uuid.UUID) (Source, error) {
	row, err := s.st.Q().GetAdultStashboxInstance(ctx, id)
	if err != nil {
		return Source{}, fmt.Errorf("adult: get source: %w", store.MapError(err))
	}
	return toSource(row), nil
}

// CreateSource validates, encrypts the API key, and persists a new catalogue source.
func (s *Service) CreateSource(ctx context.Context, in SourceInput) (Source, error) {
	validated, err := validateSourceInput(in, true)
	if err != nil {
		return Source{}, err
	}
	newID, err := s.ids.NewUUID()
	if err != nil {
		return Source{}, fmt.Errorf("adult: generate source id: %w", err)
	}
	sealed, err := s.enc.Seal([]byte(validated.APIKey))
	if err != nil {
		return Source{}, fmt.Errorf("adult: seal api key: %w", err)
	}
	now := s.clk.Now()
	row, err := s.st.Q().UpsertAdultStashboxInstance(ctx, sqlcgen.UpsertAdultStashboxInstanceParams{
		ID:            newID,
		Name:          validated.Name,
		Endpoint:      validated.Endpoint,
		ApiKeyEnc:     sealed,
		Enabled:       validated.Enabled,
		SyncFavorites: validated.SyncFavorites,
		Priority:      int32(validated.Priority), // #nosec G115 -- priority is bounded configuration rank
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if err != nil {
		return Source{}, fmt.Errorf("adult: create source: %w", store.MapError(err))
	}
	s.logger.InfoContext(ctx, "adult: source created", slog.String("source_id", row.ID.String()))
	return toSource(row), nil
}

// UpdateSource validates and updates an existing catalogue source, keeping the stored key when APIKey is empty.
func (s *Service) UpdateSource(ctx context.Context, id uuid.UUID, in SourceInput) (Source, error) {
	existing, err := s.st.Q().GetAdultStashboxInstance(ctx, id)
	if err != nil {
		return Source{}, fmt.Errorf("adult: get source for update: %w", store.MapError(err))
	}
	validated, err := validateSourceInput(in, false)
	if err != nil {
		return Source{}, err
	}
	sealed := existing.ApiKeyEnc
	if validated.APIKey != "" {
		sealed, err = s.enc.Seal([]byte(validated.APIKey))
		if err != nil {
			return Source{}, fmt.Errorf("adult: seal api key: %w", err)
		}
	}
	row, err := s.st.Q().UpsertAdultStashboxInstance(ctx, sqlcgen.UpsertAdultStashboxInstanceParams{
		ID:            id,
		Name:          validated.Name,
		Endpoint:      validated.Endpoint,
		ApiKeyEnc:     sealed,
		Enabled:       validated.Enabled,
		SyncFavorites: validated.SyncFavorites,
		Priority:      int32(validated.Priority), // #nosec G115 -- priority is bounded configuration rank
		CreatedAt:     existing.CreatedAt,
		UpdatedAt:     s.clk.Now(),
	})
	if err != nil {
		return Source{}, fmt.Errorf("adult: update source: %w", store.MapError(err))
	}
	s.logger.InfoContext(ctx, "adult: source updated", slog.String("source_id", row.ID.String()))
	return toSource(row), nil
}

// DeleteSource deletes a catalogue source by ID.
func (s *Service) DeleteSource(ctx context.Context, id uuid.UUID) error {
	n, err := s.st.Q().DeleteAdultStashboxInstance(ctx, id)
	if err != nil {
		return fmt.Errorf("adult: delete source: %w", store.MapError(err))
	}
	if n == 0 {
		return fmt.Errorf("adult: delete source: %w", domain.ErrNotFound)
	}
	s.logger.InfoContext(ctx, "adult: source deleted", slog.String("source_id", id.String()))
	return nil
}

// GetSyncStatus returns the current catalogue source counts and last synchronization telemetry.
func (s *Service) GetSyncStatus(ctx context.Context) (SyncStatus, error) {
	rows, err := s.st.Q().ListAdultStashboxInstances(ctx)
	if err != nil {
		return SyncStatus{}, fmt.Errorf("adult: list sources for status: %w", store.MapError(err))
	}
	enabled := 0
	for _, r := range rows {
		if r.Enabled {
			enabled++
		}
	}
	s.mu.Lock()
	lastAt := s.lastSyncAt
	lastErr := s.lastSyncErr
	perfSynced := s.lastPerformersSynced
	studSynced := s.lastStudiosSynced
	s.mu.Unlock()
	return SyncStatus{
		TotalSources:     len(rows),
		EnabledSources:   enabled,
		PerformersSynced: perfSynced,
		StudiosSynced:    studSynced,
		LastSyncAt:       lastAt,
		LastError:        lastErr,
	}, nil
}
