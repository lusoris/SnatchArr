// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package instances manages *arr instance definitions: validation, encryption of API
// keys at rest, connectivity tests, and the default snatch policy each instance gets.
package instances

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

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
// An instance that the Configarr configuration still declares cannot be deleted.
func (s *Service) Delete(ctx context.Context, instID uuid.UUID) error {
	n, err := s.st.Q().DeleteInstanceUnlessDeclared(ctx, instID)
	if err != nil {
		return fmt.Errorf("instances: delete: %w", store.MapError(err))
	}
	if n == 0 {
		if _, getErr := s.st.Q().GetInstance(ctx, instID); getErr != nil {
			return fmt.Errorf("instances: delete: %w", store.MapError(getErr))
		}
		return fmt.Errorf("%w: instance is declared in the Configarr configuration; remove it from config.yml first", domain.ErrForbidden)
	}
	return nil
}

// Test probes a stored instance and records the outcome.
func (s *Service) Test(ctx context.Context, instID uuid.UUID) (arrclient.Result, error) {
	row, err := s.st.Q().GetInstance(ctx, instID)
	if err != nil {
		return arrclient.Result{}, fmt.Errorf("instances: get: %w", store.MapError(err))
	}
	return s.testRow(ctx, row)
}

func (s *Service) testRow(ctx context.Context, row sqlcgen.Instance) (arrclient.Result, error) {
	apiKey, err := s.decryptAPIKey(row.ApiKeyEnc)
	if err != nil {
		return arrclient.Result{}, err
	}
	res, probeErr := s.probe.Probe(ctx, domain.AppKind(row.Kind), row.BaseUrl, apiKey)
	s.recordCheck(ctx, row.ID, res, probeErr)
	if probeErr != nil {
		return res, fmt.Errorf("instances: test: %w", probeErr)
	}
	return res, nil
}

// CheckAll probes every enabled instance. A failed probe is recorded on the instance
// and logged, never aborting the pass.
func (s *Service) CheckAll(ctx context.Context) error {
	rows, err := s.st.Q().ListEnabledInstances(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "instances: list enabled", slog.String("error", err.Error()))
		return fmt.Errorf("instances: list enabled: %w", store.MapError(err))
	}
	timeout := arrclient.DefaultOptions().Timeout
	for _, row := range rows {
		probeCtx, cancel := context.WithTimeout(ctx, timeout)
		if _, err := s.testRow(probeCtx, row); err != nil {
			s.logger.WarnContext(ctx, "instances: check", slog.String("instance", row.Name), slog.String("error", err.Error()))
		}
		cancel()
	}
	return nil
}

func (s *Service) recordCheck(ctx context.Context, instID uuid.UUID, res arrclient.Result, probeErr error) {
	var lastErr, version, product *string
	if probeErr != nil {
		msg := probeErr.Error()
		lastErr = &msg
	} else {
		version = &res.Version
		product = productOrNil(res.Product)
	}
	if err := s.st.Q().RecordInstanceCheck(ctx, sqlcgen.RecordInstanceCheckParams{
		ID: instID, LastSeenVersion: version, LastSeenProduct: product, LastCheckAt: new(s.clk.Now()), LastError: lastErr,
	}); err != nil {
		s.logger.WarnContext(ctx, "instances: record check", slog.String("error", err.Error()))
	}
}

// productOrNil is the product as the store keeps it: no row value for a kind one product serves.
func productOrNil(p domain.Product) *string {
	if p == domain.ProductNone {
		return nil
	}
	return new(string(p))
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
	apiKey, err := s.decryptAPIKey(row.ApiKeyEnc)
	if err != nil {
		return Credentials{}, err
	}
	return Credentials{BaseURL: row.BaseUrl, APIKey: apiKey}, nil
}

func (s *Service) decryptAPIKey(sealed []byte) (string, error) {
	apiKey, err := s.enc.Open(sealed)
	if err != nil {
		return "", fmt.Errorf("instances: open api key: %w", err)
	}
	return string(apiKey), nil
}

// GetPriority returns the instance kind and stored priority overrides.
func (s *Service) GetPriority(ctx context.Context, instID uuid.UUID) (domain.AppKind, domain.InstancePriority, error) {
	row, err := s.st.Q().GetInstancePriority(ctx, instID)
	if err != nil {
		return "", domain.InstancePriority{}, fmt.Errorf("instances: get priority: %w", store.MapError(err))
	}
	return domain.AppKind(row.Kind), store.InstancePriorityFromValues(row.SearchPriority, row.DownloadPriority), nil
}

// UpdatePriority validates and updates the per-instance priority overrides (allowed for both manual and configarr instances).
func (s *Service) UpdatePriority(ctx context.Context, instID uuid.UUID, prio domain.InstancePriority) (domain.AppKind, domain.InstancePriority, error) {
	if err := prio.Validate(); err != nil {
		return "", domain.InstancePriority{}, fmt.Errorf("instances: %w", err)
	}
	searchVal, downloadVal := store.InstancePriorityToValues(prio)
	row, err := s.st.Q().UpdateInstancePriority(ctx, sqlcgen.UpdateInstancePriorityParams{
		ID:               instID,
		SearchPriority:   searchVal,
		DownloadPriority: downloadVal,
		UpdatedAt:        s.clk.Now(),
	})
	if err != nil {
		return "", domain.InstancePriority{}, fmt.Errorf("instances: update priority: %w", store.MapError(err))
	}
	return domain.AppKind(row.Kind), store.InstancePriorityFromValues(row.SearchPriority, row.DownloadPriority), nil
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

// UpsertOutcome says whether an upsert created or refreshed an instance.
type UpsertOutcome struct {
	Instance domain.Instance
	Inserted bool
}

// UpsertConfigarr creates or refreshes an instance owned by Configarr under a stable key.
// A URL already used by an instance from another source is refused with ErrConflict.
func (s *Service) UpsertConfigarr(ctx context.Context, key string, in Input) (UpsertOutcome, error) {
	in, err := s.normalize(in)
	if err != nil {
		return UpsertOutcome{}, err
	}
	n, err := s.st.Q().CountInstancesByBaseURLAndNotSource(ctx, sqlcgen.CountInstancesByBaseURLAndNotSourceParams{Lower: in.BaseURL, Source: string(domain.SourceConfigarr)})
	if err != nil {
		return UpsertOutcome{}, fmt.Errorf("instances: duplicate check: %w", store.MapError(err))
	}
	if n > 0 {
		return UpsertOutcome{}, fmt.Errorf("%w: %s is already configured by hand; remove it or the Configarr entry", domain.ErrConflict, in.BaseURL)
	}
	instID, err := s.ids.NewUUID()
	if err != nil {
		return UpsertOutcome{}, fmt.Errorf("instances: new id: %w", err)
	}
	sealed, err := s.enc.Seal([]byte(in.APIKey))
	if err != nil {
		return UpsertOutcome{}, fmt.Errorf("instances: seal api key: %w", err)
	}
	now := s.clk.Now()
	var out UpsertOutcome
	err = s.st.Tx(ctx, func(q *sqlcgen.Queries) error {
		row, txErr := q.UpsertConfigarrInstance(ctx, sqlcgen.UpsertConfigarrInstanceParams{
			ID: instID, Kind: string(in.Kind), Name: in.Name, BaseUrl: in.BaseURL, ApiKeyEnc: sealed, Enabled: in.Enabled, ConfigarrKey: &key, CreatedAt: now,
		})
		if txErr != nil {
			return fmt.Errorf("upsert configarr instance: %w", txErr)
		}
		if _, txErr = q.EnsureDefaultPolicy(ctx, sqlcgen.EnsureDefaultPolicyParams{InstanceID: row.ID, UpdatedAt: now}); txErr != nil {
			return fmt.Errorf("default policy: %w", txErr)
		}
		out = UpsertOutcome{Instance: store.InstanceFromRow(sqlcgen.Instance{
			ID: row.ID, Kind: row.Kind, Name: row.Name, BaseUrl: row.BaseUrl, ApiKeyEnc: row.ApiKeyEnc, Enabled: row.Enabled, Source: row.Source,
			ConfigarrKey: row.ConfigarrKey, LastSeenVersion: row.LastSeenVersion, LastSeenProduct: row.LastSeenProduct,
			LastCheckAt: row.LastCheckAt, LastError: row.LastError,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			ConfigarrOrphanedAt: row.ConfigarrOrphanedAt,
		}), Inserted: row.Inserted}
		return nil
	})
	if err != nil {
		return UpsertOutcome{}, fmt.Errorf("instances: upsert configarr: %w", store.MapError(err))
	}
	return out, nil
}

// DisableConfigarrNotIn disables Configarr-owned instances whose key is no longer defined.
func (s *Service) DisableConfigarrNotIn(ctx context.Context, keepKeys []string) (int, error) {
	if keepKeys == nil {
		keepKeys = []string{}
	}
	n, err := s.st.Q().DisableConfigarrInstancesNotIn(ctx, sqlcgen.DisableConfigarrInstancesNotInParams{UpdatedAt: s.clk.Now(), KeepKeys: keepKeys})
	if err != nil {
		return 0, fmt.Errorf("instances: disable configarr: %w", store.MapError(err))
	}
	return int(n), nil
}

// KindByConfigarrKey returns the stored kind of a Configarr-owned instance, if any.
func (s *Service) KindByConfigarrKey(ctx context.Context, key string) (domain.AppKind, bool) {
	row, err := s.st.Q().GetInstanceByConfigarrKey(ctx, &key)
	if err != nil {
		return "", false
	}
	return domain.AppKind(row.Kind), true
}

// IDByConfigarrKey returns the stored ID of a Configarr-owned instance, if any.
func (s *Service) IDByConfigarrKey(ctx context.Context, key string) (uuid.UUID, bool) {
	row, err := s.st.Q().GetInstanceByConfigarrKey(ctx, &key)
	if err != nil {
		return uuid.Nil, false
	}
	return row.ID, true
}
