// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package settings manages the singleton runtime settings row.
package settings

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/fx"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// Service reads and writes the settings row, creating it with defaults on first use.
type Service struct {
	st  *store.Store
	clk clock.Clock
}

// New wires the service.
func New(st *store.Store, clk clock.Clock) *Service {
	return &Service{st: st, clk: clk}
}

// Get returns the settings, creating the defaults row when missing.
func (s *Service) Get(ctx context.Context) (domain.Settings, error) {
	row, err := s.st.Q().EnsureSettings(ctx, s.clk.Now())
	if err != nil {
		return domain.Settings{}, fmt.Errorf("settings: get: %w", store.MapError(err))
	}
	return store.SettingsFromRow(row), nil
}

// Update validates and replaces the editable fields.
func (s *Service) Update(ctx context.Context, in domain.Settings) (domain.Settings, error) {
	in.UserAgent = strings.TrimSpace(in.UserAgent)
	if err := in.Validate(); err != nil {
		return domain.Settings{}, fmt.Errorf("settings: %w", err)
	}
	if _, err := s.st.Q().EnsureSettings(ctx, s.clk.Now()); err != nil {
		return domain.Settings{}, fmt.Errorf("settings: ensure: %w", store.MapError(err))
	}
	row, err := s.st.Q().UpdateSettings(ctx, sqlcgen.UpdateSettingsParams{
		HistoryRetentionDays: int32(in.HistoryRetentionDays), // #nosec G115 -- validated 1..3650
		UserAgent:            in.UserAgent,
		GlobalHourlyCap:      int32(in.GlobalHourlyCap), // #nosec G115 -- validated 0..5000
		UpdatedAt:            s.clk.Now(),
	})
	if err != nil {
		return domain.Settings{}, fmt.Errorf("settings: update: %w", store.MapError(err))
	}
	return store.SettingsFromRow(row), nil
}

// Module provides the Service.
var Module = fx.Module("snatcharr.settings", fx.Provide(New))
