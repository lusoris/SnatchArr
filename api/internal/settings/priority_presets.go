// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

func shippedPriorityPreset() domain.NamedPriorityPreset {
	return domain.NamedPriorityPreset{
		Name: domain.PresetMediaFirst, Builtin: true,
		Search: domain.MediaFirstPreset(), Download: domain.MediaFirstPreset(),
	}
}

// ListPriorityPresets lists the shipped preset followed by saved user snapshots.
func (s *Service) ListPriorityPresets(ctx context.Context) ([]domain.NamedPriorityPreset, error) {
	rows, err := s.st.Q().ListPriorityPresets(ctx)
	if err != nil {
		return nil, fmt.Errorf("priority presets: list: %w", store.MapError(err))
	}
	out := make([]domain.NamedPriorityPreset, 0, len(rows)+1)
	out = append(out, shippedPriorityPreset())
	for _, row := range rows {
		preset, decodeErr := store.PriorityPresetFromRow(row)
		if decodeErr != nil {
			return nil, decodeErr
		}
		out = append(out, preset)
	}
	return out, nil
}

func priorityPresetID(ref string) (uuid.UUID, error) {
	if strings.EqualFold(ref, domain.PresetMediaFirst) {
		return uuid.Nil, fmt.Errorf("%w: shipped preset is read-only", domain.ErrForbidden)
	}
	id, err := uuid.Parse(ref)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: invalid preset id: %w", domain.ErrInvalid, err)
	}
	return id, nil
}

// GetPriorityPreset resolves a user UUID or the shipped media_first identifier.
func (s *Service) GetPriorityPreset(ctx context.Context, ref string) (domain.NamedPriorityPreset, error) {
	if ref == domain.PresetMediaFirst {
		return shippedPriorityPreset(), nil
	}
	id, err := priorityPresetID(ref)
	if err != nil {
		return domain.NamedPriorityPreset{}, err
	}
	row, err := s.st.Q().GetPriorityPreset(ctx, id)
	if err != nil {
		return domain.NamedPriorityPreset{}, fmt.Errorf("priority presets: get: %w", store.MapError(err))
	}
	return store.PriorityPresetFromRow(row)
}

func encodePriorityPreset(in domain.PriorityPresetInput) ([]byte, []byte, error) {
	if err := in.Validate(); err != nil {
		return nil, nil, err
	}
	search, err := json.Marshal(in.Search)
	if err != nil {
		return nil, nil, fmt.Errorf("encode search preset: %w", err)
	}
	download, err := json.Marshal(in.Download)
	if err != nil {
		return nil, nil, fmt.Errorf("encode download preset: %w", err)
	}
	return search, download, nil
}

func checkPriorityPresetCapacity(ctx context.Context, q *sqlcgen.Queries) error {
	if err := q.LockPriorityPresets(ctx); err != nil {
		return fmt.Errorf("priority presets: lock: %w", store.MapError(err))
	}
	count, err := q.CountPriorityPresets(ctx)
	if err != nil {
		return fmt.Errorf("priority presets: count: %w", store.MapError(err))
	}
	if count >= domain.MaxPriorityPresets {
		return fmt.Errorf("%w: at most %d saved priority presets", domain.ErrInvalid, domain.MaxPriorityPresets)
	}
	return nil
}

// CreatePriorityPreset saves both maps, counting under a lock shared by API replicas.
func (s *Service) CreatePriorityPreset(ctx context.Context, in domain.PriorityPresetInput) (domain.NamedPriorityPreset, error) {
	search, download, err := encodePriorityPreset(in)
	if err != nil {
		return domain.NamedPriorityPreset{}, err
	}
	var row sqlcgen.PriorityPreset
	err = s.st.Tx(ctx, func(q *sqlcgen.Queries) error {
		if capacityErr := checkPriorityPresetCapacity(ctx, q); capacityErr != nil {
			return capacityErr
		}
		var createErr error
		row, createErr = q.CreatePriorityPreset(ctx, sqlcgen.CreatePriorityPresetParams{
			ID: uuid.New(), Name: strings.TrimSpace(in.Name), Search: search, Download: download, CreatedAt: s.clk.Now(),
		})
		if createErr != nil {
			return fmt.Errorf("priority presets: create: %w", store.MapError(createErr))
		}
		return nil
	})
	if err != nil {
		return domain.NamedPriorityPreset{}, err
	}
	return store.PriorityPresetFromRow(row)
}

// UpdatePriorityPreset replaces a saved snapshot; active settings stay independent.
func (s *Service) UpdatePriorityPreset(ctx context.Context, ref string, in domain.PriorityPresetInput) (domain.NamedPriorityPreset, error) {
	id, err := priorityPresetID(ref)
	if err != nil {
		return domain.NamedPriorityPreset{}, err
	}
	search, download, err := encodePriorityPreset(in)
	if err != nil {
		return domain.NamedPriorityPreset{}, err
	}
	row, err := s.st.Q().UpdatePriorityPreset(ctx, sqlcgen.UpdatePriorityPresetParams{
		ID: id, Name: strings.TrimSpace(in.Name), Search: search, Download: download, UpdatedAt: s.clk.Now(),
	})
	if err != nil {
		return domain.NamedPriorityPreset{}, fmt.Errorf("priority presets: update: %w", store.MapError(err))
	}
	return store.PriorityPresetFromRow(row)
}

// DeletePriorityPreset removes a user snapshot without changing active settings.
func (s *Service) DeletePriorityPreset(ctx context.Context, ref string) error {
	id, err := priorityPresetID(ref)
	if err != nil {
		return err
	}
	n, err := s.st.Q().DeletePriorityPreset(ctx, id)
	if err != nil {
		return fmt.Errorf("priority presets: delete: %w", store.MapError(err))
	}
	if n == 0 {
		return fmt.Errorf("%w: priority preset", domain.ErrNotFound)
	}
	return nil
}

// ApplyPriorityPreset copies both maps through the existing settings update path.
func (s *Service) ApplyPriorityPreset(ctx context.Context, ref string) (domain.Settings, error) {
	preset, err := s.GetPriorityPreset(ctx, ref)
	if err != nil {
		return domain.Settings{}, err
	}
	in, err := s.Get(ctx)
	if err != nil {
		return domain.Settings{}, err
	}
	return s.Update(ctx, settingsWithPriorityPreset(in, preset))
}

func settingsWithPriorityPreset(in domain.Settings, preset domain.NamedPriorityPreset) domain.Settings {
	in.SearchPriorityPreset = maps.Clone(preset.Search)
	in.DownloadPriorityPreset = maps.Clone(preset.Download)
	return in
}
