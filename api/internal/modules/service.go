// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package modules manages optional module registration, schema validation, and persistent state.
package modules

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

func moduleFromRow(r sqlcgen.Module) domain.Module {
	cfg := json.RawMessage(r.Config)
	if len(cfg) == 0 {
		cfg = json.RawMessage("{}")
	}
	return domain.Module{
		ID:        r.ID,
		Enabled:   r.Enabled,
		Config:    cfg,
		UpdatedAt: r.UpdatedAt,
	}
}

// Service manages persistent state and configuration for optional modules.
type Service struct {
	st  *store.Store
	clk clock.Clock
	reg *Registry
}

// New wires the Service.
func New(st *store.Store, clk clock.Clock, reg *Registry) *Service {
	return &Service{st: st, clk: clk, reg: reg}
}

// Ensure inserts missing registered modules with their defaults.
func (s *Service) Ensure(ctx context.Context) error {
	for _, desc := range s.reg.Descriptors() {
		err := s.st.Q().EnsureModule(ctx, sqlcgen.EnsureModuleParams{
			ID:        desc.ID,
			Enabled:   desc.DefaultEnabled,
			Config:    []byte("{}"),
			UpdatedAt: s.clk.Now(),
		})
		if err != nil {
			return fmt.Errorf("modules: ensure %q: %w", desc.ID, store.MapError(err))
		}
	}
	return nil
}

// Get returns the persistent state and config for the specified module.
func (s *Service) Get(ctx context.Context, id string) (domain.Module, error) {
	row, err := s.st.Q().GetModule(ctx, id)
	if err != nil {
		return domain.Module{}, fmt.Errorf("modules: get %q: %w", id, store.MapError(err))
	}
	return moduleFromRow(row), nil
}

// List returns all persisted modules ordered by ID.
func (s *Service) List(ctx context.Context) ([]domain.Module, error) {
	rows, err := s.st.Q().ListModules(ctx)
	if err != nil {
		return nil, fmt.Errorf("modules: list: %w", store.MapError(err))
	}
	out := make([]domain.Module, 0, len(rows))
	for _, r := range rows {
		out = append(out, moduleFromRow(r))
	}
	return out, nil
}

// SetEnabled toggles a registered module on or off without altering its configuration.
func (s *Service) SetEnabled(ctx context.Context, id string, enabled bool) error {
	if _, ok := s.reg.Lookup(id); !ok {
		return fmt.Errorf("modules: set enabled %q: %w", id, domain.ErrNotFound)
	}
	_, err := s.st.Q().SetModuleEnabled(ctx, sqlcgen.SetModuleEnabledParams{
		ID:        id,
		Enabled:   enabled,
		UpdatedAt: s.clk.Now(),
	})
	if err != nil {
		return fmt.Errorf("modules: set enabled %q: %w", id, store.MapError(err))
	}
	return nil
}

// SetConfig validates and updates the module-specific configuration.
func (s *Service) SetConfig(ctx context.Context, id string, raw json.RawMessage) error {
	desc, ok := s.reg.Lookup(id)
	if !ok {
		return fmt.Errorf("modules: set config %q: %w", id, domain.ErrNotFound)
	}
	if err := desc.ValidateConfig(raw); err != nil {
		return fmt.Errorf("modules: set config %q: %w", id, err)
	}
	trimmed := bytes.TrimSpace(raw)
	_, err := s.st.Q().SetModuleConfig(ctx, sqlcgen.SetModuleConfigParams{
		ID:        id,
		Config:    trimmed,
		UpdatedAt: s.clk.Now(),
	})
	if err != nil {
		return fmt.Errorf("modules: set config %q: %w", id, store.MapError(err))
	}
	return nil
}

// IsEnabled checks whether the given registered module is enabled.
func (s *Service) IsEnabled(ctx context.Context, id string) (bool, error) {
	if _, ok := s.reg.Lookup(id); !ok {
		return false, fmt.Errorf("modules: is enabled %q: %w", id, domain.ErrNotFound)
	}
	row, err := s.st.Q().GetModule(ctx, id)
	if err != nil {
		return false, fmt.Errorf("modules: is enabled %q: %w", id, store.MapError(err))
	}
	return row.Enabled, nil
}
