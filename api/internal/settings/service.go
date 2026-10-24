// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package settings manages the singleton runtime settings row.
package settings

import (
	"context"
	"encoding/json"
	"errors"
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
	out := store.SettingsFromRow(row)
	mod, err := s.st.Q().GetModule(ctx, domain.ModuleLLM)
	if err != nil {
		if errors.Is(store.MapError(err), domain.ErrNotFound) {
			out.LLMEnabled = false
			return out, nil
		}
		return domain.Settings{}, fmt.Errorf("settings: get llm module: %w", store.MapError(err))
	}
	out.LLMEnabled = mod.Enabled
	return out, nil
}

// Update validates and replaces the editable fields.
func (s *Service) Update(ctx context.Context, in domain.Settings) (domain.Settings, error) {
	in.UserAgent = strings.TrimSpace(in.UserAgent)
	if err := in.Validate(); err != nil {
		return domain.Settings{}, fmt.Errorf("settings: %w", err)
	}
	writeTools, err := domain.ValidateLLMWriteTools(in.LLMWriteTools)
	if err != nil {
		return domain.Settings{}, fmt.Errorf("settings: %w", err)
	}
	if _, err = s.st.Q().EnsureSettings(ctx, s.clk.Now()); err != nil {
		return domain.Settings{}, fmt.Errorf("settings: ensure: %w", store.MapError(err))
	}
	searchJSON, err := json.Marshal(in.SearchPriorityPreset)
	if err != nil {
		return domain.Settings{}, fmt.Errorf("settings: marshal search preset: %w", err)
	}
	downloadJSON, err := json.Marshal(in.DownloadPriorityPreset)
	if err != nil {
		return domain.Settings{}, fmt.Errorf("settings: marshal download preset: %w", err)
	}
	now := s.clk.Now()
	var row sqlcgen.Setting
	err = s.st.Tx(ctx, func(q *sqlcgen.Queries) error {
		var txErr error
		row, txErr = q.UpdateSettings(ctx, sqlcgen.UpdateSettingsParams{
			HistoryRetentionDays:   int32(in.HistoryRetentionDays), // #nosec G115 -- validated 1..3650
			UserAgent:              in.UserAgent,
			GlobalHourlyCap:        int32(in.GlobalHourlyCap), // #nosec G115 -- validated 0..5000
			SearchPriorityPreset:   searchJSON,
			DownloadPriorityPreset: downloadJSON,
			LlmEnabled:             in.LLMEnabled,
			LlmWriteTools:          writeTools,
			UpdatedAt:              now,
		})
		if txErr != nil {
			return fmt.Errorf("update settings: %w", store.MapError(txErr))
		}
		_, txErr = q.SetModuleEnabled(ctx, sqlcgen.SetModuleEnabledParams{
			ID:        domain.ModuleLLM,
			Enabled:   in.LLMEnabled,
			UpdatedAt: now,
		})
		if txErr != nil {
			return fmt.Errorf("set module enabled: %w", store.MapError(txErr))
		}
		return nil
	})
	if err != nil {
		return domain.Settings{}, fmt.Errorf("settings: update: %w", err)
	}
	out := store.SettingsFromRow(row)
	out.LLMEnabled = in.LLMEnabled
	return out, nil
}

// Module provides the Service.
var Module = fx.Module("snatcharr.settings", fx.Provide(New))
