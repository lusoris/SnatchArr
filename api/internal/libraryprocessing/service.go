// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package libraryprocessing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/modules"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// ModuleChecker reports whether the library_processing optional module is active.
type ModuleChecker interface {
	IsEnabled(ctx context.Context, id string) (bool, error)
}

// Service manages stored library processing rules and deterministic scope resolution.
type Service struct {
	st  *store.Store
	clk clock.Clock
	ids id.Generator
	mod ModuleChecker
}

// NewService constructs a Service wired to the central module switch registry.
func NewService(st *store.Store, clk clock.Clock, ids id.Generator, mod *modules.Service) *Service {
	var mc ModuleChecker
	if mod != nil {
		mc = mod
	}
	return NewServiceWithChecker(st, clk, ids, mc)
}

// NewServiceWithChecker constructs a Service with an explicit ModuleChecker.
func NewServiceWithChecker(st *store.Store, clk clock.Clock, ids id.Generator, mod ModuleChecker) *Service {
	return &Service{
		st:  st,
		clk: clk,
		ids: ids,
		mod: mod,
	}
}

// operationTimeout bounds each public Service operation. A shorter caller
// deadline or cancellation takes precedence.
const operationTimeout = 5 * time.Second

// WithOperationDeadline derives a child context bounded by the service
// operation timeout while preserving shorter caller deadlines and cancellation.
func WithOperationDeadline(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, operationTimeout)
}

func (s *Service) ensureEnabled(ctx context.Context) error {
	if s.mod == nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		return fmt.Errorf("%w: module %s is disabled", domain.ErrNotFound, domain.ModuleLibraryProcessing)
	}
	enabled, err := s.mod.IsEnabled(ctx, domain.ModuleLibraryProcessing)
	if err != nil {
		return fmt.Errorf("libraryprocessing: check module enabled: %w", err)
	}
	if !enabled {
		return fmt.Errorf("%w: module %s is disabled", domain.ErrNotFound, domain.ModuleLibraryProcessing)
	}
	return nil
}

func (s *Service) checkInstance(ctx context.Context, instanceID *uuid.UUID) error {
	if instanceID == nil {
		return nil
	}
	if _, err := s.st.Q().GetInstance(ctx, *instanceID); err != nil {
		return fmt.Errorf("libraryprocessing: instance %s: %w", *instanceID, store.MapError(err))
	}
	return nil
}

func mapWriteError(err error) error {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		switch {
		case pgErr.Code == "23503":
			return fmt.Errorf("%w: referenced instance not found", domain.ErrNotFound)
		case pgErr.Code == "23P01" && pgErr.ConstraintName == "library_processing_rules_library_path_excl":
			return fmt.Errorf("%w: library processing rule already exists for root path", domain.ErrConflict)
		}
	}
	return store.MapError(err)
}

func cloneStringPtr(s *string) *string {
	if s == nil {
		return nil
	}
	cp := *s
	return &cp
}

func cloneUUIDPtr(u *uuid.UUID) *uuid.UUID {
	if u == nil {
		return nil
	}
	cp := *u
	return &cp
}

func storedRuleFromRow(row sqlcgen.LibraryProcessingRule) (StoredRule, error) {
	scope, err := ParseScope(row.Scope)
	if err != nil {
		return StoredRule{}, fmt.Errorf("libraryprocessing: stored row %s: %w", row.ID, err)
	}
	parsedRule, err := DecodeRuleJSON(row.Rule)
	if err != nil {
		return StoredRule{}, fmt.Errorf("libraryprocessing: stored row %s: %w", row.ID, err)
	}
	var mtPtr *MediaType
	if row.MediaType != nil {
		mt, mtErr := ParseMediaType(*row.MediaType)
		if mtErr != nil {
			return StoredRule{}, fmt.Errorf("libraryprocessing: stored row %s: %w", row.ID, mtErr)
		}
		mtPtr = &mt
	}
	return StoredRule{
		ID:           row.ID,
		Scope:        scope,
		InstanceID:   cloneUUIDPtr(row.InstanceID),
		ItemID:       cloneStringPtr(row.ItemID),
		UserLanguage: cloneStringPtr(row.UserLanguage),
		RootPath:     cloneStringPtr(row.RootPath),
		MediaType:    mtPtr,
		Rule:         parsedRule,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}, nil
}

// Create validates and persists a new scoped library processing rule.
func (s *Service) Create(ctx context.Context, in RuleInput) (StoredRule, error) {
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	if err := s.ensureEnabled(ctx); err != nil {
		return StoredRule{}, err
	}
	norm, err := normalizeInput(in)
	if err != nil {
		return StoredRule{}, err
	}
	if err = s.checkInstance(ctx, norm.instanceID); err != nil {
		return StoredRule{}, err
	}
	ruleBytes, err := json.Marshal(norm.rule)
	if err != nil {
		return StoredRule{}, fmt.Errorf("libraryprocessing: marshal rule: %w", err)
	}
	ruleID, err := s.ids.NewUUID()
	if err != nil {
		return StoredRule{}, fmt.Errorf("libraryprocessing: generate id: %w", err)
	}
	now := s.clk.Now()
	row, err := s.st.Q().CreateLibraryProcessingRule(ctx, sqlcgen.CreateLibraryProcessingRuleParams{
		ID:               ruleID,
		Scope:            norm.scope.String(),
		InstanceID:       norm.instanceID,
		ItemID:           norm.itemID,
		UserLanguage:     norm.userLanguage,
		UserLanguageCode: norm.userLanguageCode,
		RootPath:         norm.rootPath,
		MediaType:        norm.mediaType,
		Rule:             ruleBytes,
		CreatedAt:        now,
		UpdatedAt:        now,
	})
	if err != nil {
		return StoredRule{}, fmt.Errorf("libraryprocessing: create rule: %w", mapWriteError(err))
	}
	return storedRuleFromRow(row)
}

// Get returns a stored library processing rule by its ID.
func (s *Service) Get(ctx context.Context, ruleID uuid.UUID) (StoredRule, error) {
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	if err := s.ensureEnabled(ctx); err != nil {
		return StoredRule{}, err
	}
	row, err := s.st.Q().GetLibraryProcessingRule(ctx, ruleID)
	if err != nil {
		return StoredRule{}, fmt.Errorf("libraryprocessing: get rule %s: %w", ruleID, store.MapError(err))
	}
	return storedRuleFromRow(row)
}

// List returns all stored library processing rules in precedence order using bounded
// database batches over a single repeatable-read snapshot.
func (s *Service) List(ctx context.Context) ([]StoredRule, error) {
	out := make([]StoredRule, 0)
	err := s.StreamAll(ctx, defaultSnapshotBatchSize, func(batch []StoredRule) error {
		out = append(out, batch...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Update validates and replaces the scope target and rule body of an existing stored rule.
func (s *Service) Update(ctx context.Context, ruleID uuid.UUID, in RuleInput) (StoredRule, error) {
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	if err := s.ensureEnabled(ctx); err != nil {
		return StoredRule{}, err
	}
	norm, err := normalizeInput(in)
	if err != nil {
		return StoredRule{}, err
	}
	if _, err = s.st.Q().GetLibraryProcessingRule(ctx, ruleID); err != nil {
		return StoredRule{}, fmt.Errorf("libraryprocessing: get rule %s: %w", ruleID, store.MapError(err))
	}
	if err = s.checkInstance(ctx, norm.instanceID); err != nil {
		return StoredRule{}, err
	}
	ruleBytes, err := json.Marshal(norm.rule)
	if err != nil {
		return StoredRule{}, fmt.Errorf("libraryprocessing: marshal rule: %w", err)
	}
	row, err := s.st.Q().UpdateLibraryProcessingRule(ctx, sqlcgen.UpdateLibraryProcessingRuleParams{
		ID:               ruleID,
		Scope:            norm.scope.String(),
		InstanceID:       norm.instanceID,
		ItemID:           norm.itemID,
		UserLanguage:     norm.userLanguage,
		UserLanguageCode: norm.userLanguageCode,
		RootPath:         norm.rootPath,
		MediaType:        norm.mediaType,
		Rule:             ruleBytes,
		UpdatedAt:        s.clk.Now(),
	})
	if err != nil {
		return StoredRule{}, fmt.Errorf("libraryprocessing: update rule %s: %w", ruleID, mapWriteError(err))
	}
	return storedRuleFromRow(row)
}

// Delete removes a stored library processing rule by ID.
func (s *Service) Delete(ctx context.Context, ruleID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	if err := s.ensureEnabled(ctx); err != nil {
		return err
	}
	n, err := s.st.Q().DeleteLibraryProcessingRule(ctx, ruleID)
	if err != nil {
		return fmt.Errorf("libraryprocessing: delete rule %s: %w", ruleID, store.MapError(err))
	}
	if n == 0 {
		return fmt.Errorf("libraryprocessing: delete rule %s: %w", ruleID, domain.ErrNotFound)
	}
	return nil
}

// Resolve evaluates stored PostgreSQL rules across all six scopes using Select.
func (s *Service) Resolve(ctx context.Context, q ResolveQuery) (ResolvedRule, error) {
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	if err := s.ensureEnabled(ctx); err != nil {
		return ResolvedRule{}, err
	}
	norm, err := normalizeResolveQuery(q)
	if err != nil {
		return ResolvedRule{}, err
	}
	rows, err := s.st.Q().ListMatchingLibraryProcessingRules(ctx, sqlcgen.ListMatchingLibraryProcessingRulesParams{
		InstanceID:       norm.instanceID,
		ItemID:           norm.itemID,
		UserLanguageCode: norm.userLanguageCode,
		RootPath:         norm.rootPath,
		MediaType:        norm.mediaType,
	})
	if err != nil {
		return ResolvedRule{}, fmt.Errorf("libraryprocessing: resolve query: %w", store.MapError(err))
	}
	cands, err := buildCandidateRules(rows)
	if err != nil {
		return ResolvedRule{}, err
	}
	sel, ok := Select(cands)
	if !ok || sel.Rule == nil {
		return ResolvedRule{}, fmt.Errorf("libraryprocessing: resolve: %w: no matching rule", domain.ErrNotFound)
	}
	return ResolvedRule{
		RuleID: sel.Rule.ID,
		Scope:  sel.Scope,
		Rule:   CloneRule(sel.Rule.Rule),
	}, nil
}

func buildCandidateRules(rows []sqlcgen.LibraryProcessingRule) (Rules[StoredRule], error) {
	var cands Rules[StoredRule]
	for _, row := range rows {
		sr, err := storedRuleFromRow(row)
		if err != nil {
			return Rules[StoredRule]{}, err
		}
		assignCandidate(&cands, sr)
	}
	return cands, nil
}

func assignCandidate(cands *Rules[StoredRule], sr StoredRule) {
	cp := sr
	switch sr.Scope {
	case ScopeItemOverride:
		cands.ItemOverride = &cp
	case ScopeUserLanguage:
		cands.UserLanguage = &cp
	case ScopeLibrary:
		cands.Library = &cp
	case ScopeInstance:
		cands.Instance = &cp
	case ScopeMediaType:
		cands.MediaType = &cp
	case ScopeGlobalDefault:
		cands.GlobalDefault = &cp
	default:
	}
}
