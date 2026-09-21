// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package instances manages *arr instance definitions: validation, encryption of API
// keys at rest, connectivity tests, and the default snatch policy each instance gets.
package instances

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/fx"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// Input is the user-supplied part of an instance.
type Input struct {
	Kind    domain.AppKind
	Name    string
	BaseURL string
	APIKey  string
	Enabled bool
	// Source records who created the instance; empty means manual.
	Source domain.InstanceSource
}

// Credentials are what the snatch-worker needs to talk to an instance.
type Credentials struct {
	BaseURL string
	APIKey  string
}

// unchangedKey marks "keep the stored API key" on update.
const unchangedKey = "<unchanged>"

// Service is the instance use-case layer.
type Service struct {
	st     *store.Store
	enc    *crypto.Encryptor
	clk    clock.Clock
	ids    id.Generator
	probe  arrclient.Prober
	logger *slog.Logger
}

// New wires the service.
func New(st *store.Store, enc *crypto.Encryptor, clk clock.Clock, ids id.Generator, probe arrclient.Prober, logger *slog.Logger) *Service {
	return &Service{st: st, enc: enc, clk: clk, ids: ids, probe: probe, logger: logger}
}

// List returns every instance without secrets.
func (s *Service) List(ctx context.Context) ([]domain.Instance, error) {
	rows, err := s.st.Q().ListInstances(ctx)
	if err != nil {
		return nil, fmt.Errorf("instances: list: %w", store.MapError(err))
	}
	return store.InstancesFromRows(rows), nil
}

// Get returns one instance without its secret.
func (s *Service) Get(ctx context.Context, instID uuid.UUID) (domain.Instance, error) {
	row, err := s.st.Q().GetInstance(ctx, instID)
	if err != nil {
		return domain.Instance{}, fmt.Errorf("instances: get: %w", store.MapError(err))
	}
	return store.InstanceFromRow(row), nil
}

// Create validates, encrypts and stores a manual instance with a default policy.
func (s *Service) Create(ctx context.Context, in Input) (domain.Instance, error) {
	in, err := s.normalize(in)
	if err != nil {
		return domain.Instance{}, err
	}
	instID, err := s.ids.NewUUID()
	if err != nil {
		return domain.Instance{}, fmt.Errorf("instances: new id: %w", err)
	}
	sealed, err := s.enc.Seal([]byte(in.APIKey))
	if err != nil {
		return domain.Instance{}, fmt.Errorf("instances: seal api key: %w", err)
	}
	if dupErr := s.ensureNoDuplicateURL(ctx, in.BaseURL, instID); dupErr != nil {
		return domain.Instance{}, dupErr
	}
	now := s.clk.Now()
	var row sqlcgen.Instance
	err = s.st.Tx(ctx, func(q *sqlcgen.Queries) error {
		var txErr error
		row, txErr = q.CreateInstance(ctx, sqlcgen.CreateInstanceParams{
			ID: instID, Kind: string(in.Kind), Name: in.Name, BaseUrl: in.BaseURL, ApiKeyEnc: sealed,
			Enabled: in.Enabled, Source: string(sourceOrManual(in.Source)), CreatedAt: now,
		})
		if txErr != nil {
			return fmt.Errorf("create instance: %w", txErr)
		}
		if _, txErr = q.EnsureDefaultPolicy(ctx, sqlcgen.EnsureDefaultPolicyParams{InstanceID: instID, UpdatedAt: now}); txErr != nil {
			return fmt.Errorf("default policy: %w", txErr)
		}
		return nil
	})
	if err != nil {
		return domain.Instance{}, fmt.Errorf("instances: create: %w", store.MapError(err))
	}
	return store.InstanceFromRow(row), nil
}

// Update replaces the editable fields. An empty APIKey keeps the stored one.
func (s *Service) Update(ctx context.Context, instID uuid.UUID, in Input) (domain.Instance, error) {
	current, err := s.st.Q().GetInstance(ctx, instID)
	if err != nil {
		return domain.Instance{}, fmt.Errorf("instances: get: %w", store.MapError(err))
	}
	if current.Source == string(domain.SourceConfigarr) {
		return domain.Instance{}, fmt.Errorf("%w: instance is managed by Configarr; edit config.yml instead", domain.ErrForbidden)
	}
	if in.APIKey == "" {
		in.APIKey = unchangedKey
	}
	in, err = s.normalize(in)
	if err != nil {
		return domain.Instance{}, err
	}
	sealed, err := s.sealUnlessUnchanged(in.APIKey, current.ApiKeyEnc)
	if err != nil {
		return domain.Instance{}, err
	}
	if dupErr := s.ensureNoDuplicateURL(ctx, in.BaseURL, instID); dupErr != nil {
		return domain.Instance{}, dupErr
	}
	row, err := s.st.Q().UpdateInstance(ctx, sqlcgen.UpdateInstanceParams{
		ID: instID, Kind: string(in.Kind), Name: in.Name, BaseUrl: in.BaseURL, ApiKeyEnc: sealed,
		Enabled: in.Enabled, UpdatedAt: s.clk.Now(),
	})
	if err != nil {
		return domain.Instance{}, fmt.Errorf("instances: update: %w", store.MapError(err))
	}
	return store.InstanceFromRow(row), nil
}

func sourceOrManual(src domain.InstanceSource) domain.InstanceSource {
	if src == "" {
		return domain.SourceManual
	}
	return src
}

func (s *Service) sealUnlessUnchanged(apiKey string, current []byte) ([]byte, error) {
	if apiKey == unchangedKey {
		return current, nil
	}
	sealed, err := s.enc.Seal([]byte(apiKey))
	if err != nil {
		return nil, fmt.Errorf("instances: seal api key: %w", err)
	}
	return sealed, nil
}

// Delete removes an instance and, by cascade, its policy, runs, memory and history.
func (s *Service) Delete(ctx context.Context, instID uuid.UUID) error {
	n, err := s.st.Q().DeleteInstance(ctx, instID)
	if err != nil {
		return fmt.Errorf("instances: delete: %w", store.MapError(err))
	}
	if n == 0 {
		return fmt.Errorf("instances: delete: %w", domain.ErrNotFound)
	}
	return nil
}

// Test probes a stored instance and records the outcome.
func (s *Service) Test(ctx context.Context, instID uuid.UUID) (arrclient.Result, error) {
	creds, err := s.Credentials(ctx, instID)
	if err != nil {
		return arrclient.Result{}, err
	}
	inst, err := s.Get(ctx, instID)
	if err != nil {
		return arrclient.Result{}, err
	}
	res, probeErr := s.probe.Probe(ctx, inst.Kind, creds.BaseURL, creds.APIKey)
	s.recordCheck(ctx, instID, res, probeErr)
	if probeErr != nil {
		return res, fmt.Errorf("instances: test: %w", probeErr)
	}
	return res, nil
}

func (s *Service) recordCheck(ctx context.Context, instID uuid.UUID, res arrclient.Result, probeErr error) {
	var lastErr, version *string
	if probeErr != nil {
		msg := probeErr.Error()
		lastErr = &msg
	} else {
		version = &res.Version
	}
	if err := s.st.Q().RecordInstanceCheck(ctx, sqlcgen.RecordInstanceCheckParams{
		ID: instID, LastSeenVersion: version, LastCheckAt: new(s.clk.Now()), LastError: lastErr,
	}); err != nil {
		s.logger.WarnContext(ctx, "instances: record check", slog.String("error", err.Error()))
	}
}

// TestInput probes credentials that are not stored yet (the "test" button on the form).
func (s *Service) TestInput(ctx context.Context, in Input) (arrclient.Result, error) {
	in, err := s.normalize(in)
	if err != nil {
		return arrclient.Result{}, err
	}
	res, err := s.probe.Probe(ctx, in.Kind, in.BaseURL, in.APIKey)
	if err != nil {
		return res, fmt.Errorf("instances: test input: %w", err)
	}
	return res, nil
}

// Credentials decrypts what the worker needs for a lease.
func (s *Service) Credentials(ctx context.Context, instID uuid.UUID) (Credentials, error) {
	row, err := s.st.Q().GetInstance(ctx, instID)
	if err != nil {
		return Credentials{}, fmt.Errorf("instances: get: %w", store.MapError(err))
	}
	apiKey, err := s.enc.Open(row.ApiKeyEnc)
	if err != nil {
		return Credentials{}, fmt.Errorf("instances: open api key: %w", err)
	}
	return Credentials{BaseURL: row.BaseUrl, APIKey: string(apiKey)}, nil
}

func (s *Service) normalize(in Input) (Input, error) {
	if err := domain.ValidateInstanceInput(in.Kind, in.Name, in.BaseURL, in.APIKey); err != nil {
		return Input{}, fmt.Errorf("instances: %w", err)
	}
	normalized, err := domain.NormalizeBaseURL(in.BaseURL)
	if err != nil {
		return Input{}, fmt.Errorf("instances: %w", err)
	}
	in.BaseURL = normalized
	in.Name = strings.TrimSpace(in.Name)
	in.APIKey = strings.TrimSpace(in.APIKey)
	return in, nil
}

func (s *Service) ensureNoDuplicateURL(ctx context.Context, baseURL string, self uuid.UUID) error {
	n, err := s.st.Q().CountInstancesByBaseURL(ctx, sqlcgen.CountInstancesByBaseURLParams{Lower: baseURL, ID: self})
	if err != nil {
		return fmt.Errorf("instances: duplicate check: %w", store.MapError(err))
	}
	if n > 0 {
		return fmt.Errorf("%w: another instance already uses %s", domain.ErrConflict, baseURL)
	}
	return nil
}

// IsNotFound reports whether err is the not-found sentinel.
func IsNotFound(err error) bool { return errors.Is(err, domain.ErrNotFound) }

// Module provides the Service.
var Module = fx.Module("snatcharr.instances", fx.Provide(New))
