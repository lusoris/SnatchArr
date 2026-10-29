// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package runtimesettings manages persisted overrides for runtime and secret settings.
package runtimesettings

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/golusoris/golusoris/core/clock"
	gconfig "github.com/golusoris/golusoris/core/config"
	"github.com/golusoris/golusoris/core/crypto"

	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// SettingItem represents a configuration setting with its metadata, effective value, and source.
type SettingItem struct {
	Key     string
	Env     string
	Class   string
	Type    string
	Default string
	Value   *string
	IsSet   *bool
	Source  string
	Locked  bool
	Apply   string
}

// PutResult is returned after persisting settings updates.
type PutResult struct {
	PendingRestart bool
}

// Service manages runtime settings persistence, validation, and retrieval.
type Service struct {
	st      *store.Store
	enc     *crypto.Encryptor
	clk     clock.Clock
	baseCfg config.BaseOptions
	logger  *slog.Logger
}

// New creates a new runtimesettings Service.
func New(st *store.Store, enc *crypto.Encryptor, clk clock.Clock, baseCfg config.BaseOptions, logger *slog.Logger) *Service {
	return &Service{
		st:      st,
		enc:     enc,
		clk:     clk,
		baseCfg: baseCfg,
		logger:  logger,
	}
}

// LoadOverrides reads persisted runtime settings from st and decrypts secret settings with enc.
func LoadOverrides(ctx context.Context, st *store.Store, enc *crypto.Encryptor) (map[string]string, error) {
	rows, err := st.Q().ListRuntimeSettings(ctx)
	if isUndefinedTable(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("runtimesettings: list overrides: %w", store.MapError(err))
	}
	regMap := config.RegistryMap()
	current := make(map[string]string, len(rows))
	for _, r := range rows {
		item, exists := regMap[r.Key]
		if !exists || item.Class == config.ClassBootstrap || isEnvOnlySecret(item.Key, item.Env) {
			continue
		}
		val, err := storedValue(item, r.Value, enc)
		if err != nil {
			return nil, err
		}
		current[r.Key] = val
	}
	return current, nil
}

// LoadEffectiveOptions reads persisted overrides from st, decrypts secret rows using enc,
// and merges them into cfg via config.LoadWithSettings (precedence: env > DB > default).
func LoadEffectiveOptions(ctx context.Context, cfg *gconfig.Config, st *store.Store, enc *crypto.Encryptor) (config.Options, error) {
	overrides, err := LoadOverrides(ctx, st, enc)
	if err != nil {
		return config.Options{}, err
	}
	return config.LoadWithSettings(cfg, overrides)
}

// List returns all registered settings with their effective source, value, and lock state.
func (s *Service) List(ctx context.Context) ([]SettingItem, error) {
	rows, err := s.st.Q().ListRuntimeSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("runtimesettings: list: %w", store.MapError(err))
	}
	dbRows := make(map[string]sqlcgen.RuntimeSetting, len(rows))
	for _, r := range rows {
		dbRows[r.Key] = r
	}

	reg := config.Registry()
	out := make([]SettingItem, 0, len(reg))
	for _, item := range reg {
		out = append(out, s.buildSettingItem(item, dbRows[item.Key]))
	}
	return out, nil
}

func (s *Service) buildSettingItem(item config.Setting, dbRow sqlcgen.RuntimeSetting) SettingItem {
	envVal, envSet := os.LookupEnv(item.Env)
	dbSet := dbRow.Key != ""

	source := "default"
	// An environment-only secret is never writable through the API, so it reads as locked even
	// while its variable is unset.
	locked := isEnvOnlySecret(item.Key, item.Env)
	if envSet {
		source = "env"
		locked = true
	} else if dbSet {
		source = "db"
	}

	si := SettingItem{
		Key:     item.Key,
		Env:     item.Env,
		Class:   string(item.Class),
		Type:    item.Type,
		Default: item.Default,
		Source:  source,
		Locked:  locked,
		Apply:   string(item.Apply),
	}

	if item.Class == config.ClassSecret {
		isSet := (envSet && envVal != "") || (dbSet && dbRow.Value != "")
		si.IsSet = &isSet
	} else {
		val := item.Default
		switch source {
		case "env":
			val = envVal
		case "db":
			val = dbRow.Value
		}
		si.Value = &val
	}
	return si
}

// Put validates and persists a batch of runtime setting overrides.
func (s *Service) Put(ctx context.Context, userID *uuid.UUID, values map[string]string) (PutResult, error) {
	regMap := config.RegistryMap()
	if err := s.validateKeys(regMap, values); err != nil {
		return PutResult{}, err
	}

	overrides, err := LoadOverrides(ctx, s.st, s.enc)
	if err != nil {
		return PutResult{}, err
	}

	for k, v := range values {
		if v == "" {
			delete(overrides, k)
		} else {
			overrides[k] = v
		}
	}

	if err := s.validateMergedOptions(overrides); err != nil {
		return PutResult{}, err
	}

	if err := s.persistAndAudit(ctx, userID, values, regMap); err != nil {
		return PutResult{}, err
	}

	return PutResult{PendingRestart: true}, nil
}

func (s *Service) validateKeys(regMap map[string]config.Setting, values map[string]string) error {
	for k := range values {
		item, exists := regMap[k]
		if !exists {
			return fmt.Errorf("%w: unknown setting %q", domain.ErrInvalid, k)
		}
		if item.Class == config.ClassBootstrap || isEnvOnlySecret(item.Key, item.Env) {
			return fmt.Errorf("%w: setting %q is read-only", domain.ErrInvalid, k)
		}
		if _, envSet := os.LookupEnv(item.Env); envSet {
			return fmt.Errorf("%w: setting %q is locked by environment variable %s", domain.ErrConflict, k, item.Env)
		}
	}
	return nil
}

func (s *Service) validateMergedOptions(overrides map[string]string) error {
	o := config.Default()
	if s.baseCfg.Profile != "" {
		o.Profile = s.baseCfg.Profile
	}
	if s.baseCfg.Worker.Token != "" {
		o.Worker.Token = s.baseCfg.Worker.Token
	}
	if s.baseCfg.APIKey.Secret != "" {
		o.APIKey.Secret = s.baseCfg.APIKey.Secret
	}
	if err := applyStored(&o, overrides); err != nil {
		return err
	}
	if err := applyEnv(&o); err != nil {
		return err
	}
	if err := o.Validate(); err != nil {
		return fmt.Errorf("%w: %w", domain.ErrInvalid, err)
	}
	return nil
}

// applyStored applies stored overrides whose environment variable is unset.
func applyStored(o *config.Options, overrides map[string]string) error {
	regMap := config.RegistryMap()
	for k, v := range overrides {
		if _, envSet := os.LookupEnv(regMap[k].Env); envSet {
			continue
		}
		if err := config.ApplySetting(o, k, v); err != nil {
			return fmt.Errorf("%w: invalid value for %s: %w", domain.ErrInvalid, k, err)
		}
	}
	return nil
}

// applyEnv applies every set environment variable on top, so env wins over stored values.
func applyEnv(o *config.Options) error {
	for _, item := range config.Registry() {
		envVal, envSet := os.LookupEnv(item.Env)
		if !envSet {
			continue
		}
		if err := config.ApplySetting(o, item.Key, envVal); err != nil {
			return fmt.Errorf("%w: apply environment setting %s (%s): %w", domain.ErrInvalid, item.Key, item.Env, err)
		}
	}
	return nil
}

// storedValue returns the plain value of a stored row, decrypting secret-class rows.
func storedValue(item config.Setting, raw string, enc *crypto.Encryptor) (string, error) {
	if item.Class != config.ClassSecret {
		return raw, nil
	}
	if enc == nil {
		return "", fmt.Errorf("runtimesettings: decrypt %q: %w: encryptor unavailable", item.Key, domain.ErrInvalid)
	}
	cipher, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return "", fmt.Errorf("runtimesettings: decode secret %q: %w", item.Key, err)
	}
	plain, err := enc.Open(cipher)
	if err != nil {
		return "", fmt.Errorf("runtimesettings: open secret %q: %w", item.Key, err)
	}
	return string(plain), nil
}

func (s *Service) persistAndAudit(ctx context.Context, userID *uuid.UUID, values map[string]string, regMap map[string]config.Setting) error {
	now := s.clk.Now()
	err := s.st.Tx(ctx, func(q *sqlcgen.Queries) error {
		for k, v := range values {
			item := regMap[k]
			if err := s.persistOne(ctx, q, userID, k, v, item, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for k, v := range values {
		item := regMap[k]
		s.auditLog(ctx, userID, k, v, item, now)
	}
	return nil
}

func (s *Service) persistOne(ctx context.Context, q *sqlcgen.Queries, userID *uuid.UUID, k, v string, item config.Setting, now time.Time) error {
	if v == "" {
		if err := q.DeleteRuntimeSetting(ctx, k); err != nil {
			return fmt.Errorf("runtimesettings: delete %q: %w", k, store.MapError(err))
		}
		return nil
	}
	storedVal := v
	if item.Class == config.ClassSecret {
		if s.enc == nil {
			return fmt.Errorf("%w: encryptor unavailable", domain.ErrInvalid)
		}
		sealed, sealErr := s.enc.Seal([]byte(v))
		if sealErr != nil {
			return fmt.Errorf("runtimesettings: seal %q: %w", k, sealErr)
		}
		storedVal = base64.StdEncoding.EncodeToString(sealed)
	}
	_, err := q.UpsertRuntimeSetting(ctx, sqlcgen.UpsertRuntimeSettingParams{
		Key:       k,
		Value:     storedVal,
		UpdatedBy: userID,
		UpdatedAt: now,
	})
	if err != nil {
		return fmt.Errorf("runtimesettings: upsert %q: %w", k, store.MapError(err))
	}
	return nil
}

func (s *Service) auditLog(ctx context.Context, userID *uuid.UUID, k, v string, item config.Setting, now time.Time) {
	if item.Class == config.ClassSecret {
		s.logger.InfoContext(ctx, "runtime setting updated",
			slog.String("key", k),
			slog.Any("user_id", userID),
			slog.Time("time", now),
		)
		return
	}
	s.logger.InfoContext(ctx, "runtime setting updated",
		slog.String("key", k),
		slog.String("value", v),
		slog.Any("user_id", userID),
		slog.Time("time", now),
	)
}

func isEnvOnlySecret(key, env string) bool {
	if strings.HasPrefix(env, "APP_DB_") || env == "APP_CRYPTO_KEY" {
		return true
	}
	return key == "snatcharr.worker.token" || key == "snatcharr.apikey.secret"
}

// isUndefinedTable reports a schema that predates migration 000051: the start-up load runs
// before the migrator's start hook, so on the first start after an upgrade the table may be
// missing and nothing is stored yet.
func isUndefinedTable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42P01"
}
