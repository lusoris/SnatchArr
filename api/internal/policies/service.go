// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package policies reads and writes per-instance snatch policies.
package policies

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"go.uber.org/fx"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// Service is the policy use-case layer.
type Service struct {
	st  *store.Store
	clk clock.Clock
}

// New wires the service.
func New(st *store.Store, clk clock.Clock) *Service {
	return &Service{st: st, clk: clk}
}

// Get returns the policy, creating the defaults if the row is missing.
func (s *Service) Get(ctx context.Context, instanceID uuid.UUID) (domain.Policy, error) {
	if _, err := s.st.Q().GetInstance(ctx, instanceID); err != nil {
		return domain.Policy{}, fmt.Errorf("policies: instance: %w", store.MapError(err))
	}
	row, err := s.st.Q().EnsureDefaultPolicy(ctx, sqlcgen.EnsureDefaultPolicyParams{InstanceID: instanceID, UpdatedAt: s.clk.Now()})
	if err != nil {
		return domain.Policy{}, fmt.Errorf("policies: ensure default: %w", store.MapError(err))
	}
	return store.PolicyFromRow(row), nil
}

// Update validates and replaces the policy.
func (s *Service) Update(ctx context.Context, p domain.Policy) (domain.Policy, error) {
	if err := p.Validate(); err != nil {
		return domain.Policy{}, fmt.Errorf("policies: %w", err)
	}
	if _, err := s.st.Q().GetInstance(ctx, p.InstanceID); err != nil {
		return domain.Policy{}, fmt.Errorf("policies: instance: %w", store.MapError(err))
	}
	row, err := s.st.Q().UpsertPolicy(ctx, store.PolicyParams(p, s.clk.Now()))
	if err != nil {
		return domain.Policy{}, fmt.Errorf("policies: upsert: %w", store.MapError(err))
	}
	return store.PolicyFromRow(row), nil
}

// SaveCursor persists the sequential-selection cursor for one snatch kind.
func (s *Service) SaveCursor(ctx context.Context, instanceID uuid.UUID, kind domain.SnatchKind, cursor string) error {
	if len(cursor) > 256 {
		return fmt.Errorf("%w: cursor too long", domain.ErrInvalid)
	}
	err := s.st.Q().SavePolicyCursor(ctx, sqlcgen.SavePolicyCursorParams{
		Kind: string(kind), Cursor: cursor, UpdatedAt: s.clk.Now(), InstanceID: instanceID,
	})
	if err != nil {
		return fmt.Errorf("policies: save cursor: %w", store.MapError(err))
	}
	return nil
}

// Module provides the Service.
var Module = fx.Module("snatcharr.policies", fx.Provide(New))
