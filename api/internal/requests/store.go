// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package requests

import (
	"context"
	"errors"
	"fmt"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// Store persists and retrieves requests settings in PostgreSQL.
type Store struct {
	st  *store.Store
	clk clock.Clock
}

// NewStore creates a new requests settings store.
func NewStore(st *store.Store, clk clock.Clock) *Store {
	return &Store{st: st, clk: clk}
}

// Get returns the stored requests settings, or DefaultSettings() when none is stored.
func (s *Store) Get(ctx context.Context) (Settings, error) {
	row, err := s.st.Q().GetRequestsSettings(ctx)
	if err != nil {
		if errors.Is(store.MapError(err), domain.ErrNotFound) {
			return DefaultSettings(), nil
		}
		return Settings{}, fmt.Errorf("requests: get settings: %w", store.MapError(err))
	}
	return Settings{
		VotingMode:       VotingMode(row.VotingMode),
		AllowViewerVotes: row.AllowViewerVotes,
		Visibility:       Visibility(row.Visibility),
		VotingTTLDays:    int(row.VotingTtlDays),
	}, nil
}

// Put validates and persists the requests settings.
func (s *Store) Put(ctx context.Context, in Settings) (Settings, error) {
	if err := in.Validate(); err != nil {
		return Settings{}, err
	}
	now := s.clk.Now()
	row, err := s.st.Q().UpsertRequestsSettings(ctx, sqlcgen.UpsertRequestsSettingsParams{
		VotingMode:       string(in.VotingMode),
		AllowViewerVotes: in.AllowViewerVotes,
		Visibility:       string(in.Visibility),
		VotingTtlDays:    int32(in.VotingTTLDays), // #nosec G115 -- bounded by Validate
		UpdatedAt:        now,
	})
	if err != nil {
		return Settings{}, fmt.Errorf("requests: put settings: %w", store.MapError(err))
	}
	return Settings{
		VotingMode:       VotingMode(row.VotingMode),
		AllowViewerVotes: row.AllowViewerVotes,
		Visibility:       Visibility(row.Visibility),
		VotingTTLDays:    int(row.VotingTtlDays),
	}, nil
}
