// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package runtimesettings_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"
	gconfig "github.com/golusoris/golusoris/core/config"
	"github.com/golusoris/golusoris/core/crypto"

	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/runtimesettings"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

type testHarness struct {
	svc    *runtimesettings.Service
	st     *store.Store
	enc    *crypto.Encryptor
	clk    clock.Clock
	logBuf *bytes.Buffer
}

func newTestHarness(t *testing.T) *testHarness {
	t.Helper()
	st := storetest.New(t)
	clk := clock.NewFake()
	enc, err := crypto.NewEncryptor(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("crypto.NewEncryptor: %v", err)
	}
	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cfg := config.Default()
	cfg.Profile = config.ProfileDev
	svc := runtimesettings.New(st, enc, clk, config.BaseOptions{Options: cfg}, logger)
	return &testHarness{
		svc:    svc,
		st:     st,
		enc:    enc,
		clk:    clk,
		logBuf: logBuf,
	}
}

func TestListPrecedence(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	ctx := t.Context()

	// Seed DB override for session.ttl
	_, err := h.st.Q().UpsertRuntimeSetting(ctx, sqlcgen.UpsertRuntimeSettingParams{
		Key:       "snatcharr.session.ttl",
		Value:     "12h",
		UpdatedAt: h.clk.Now(),
	})
	if err != nil {
		t.Fatalf("UpsertRuntimeSetting: %v", err)
	}

	list, err := h.svc.List(ctx)
	if err != nil {
		t.Fatalf("svc.List: %v", err)
	}

	foundTTL := false
	for _, item := range list {
		if item.Key == "snatcharr.session.ttl" {
			foundTTL = true
			if item.Source != "db" || item.Locked {
				t.Errorf("expected source=db, locked=false, got source=%s, locked=%v", item.Source, item.Locked)
			}
			if item.Value == nil || *item.Value != "12h" {
				t.Errorf("expected value=12h, got %v", item.Value)
			}
		}
	}
	if !foundTTL {
		t.Errorf("snatcharr.session.ttl not found in List")
	}
}

func TestListEnvPrecedence(t *testing.T) {
	h := newTestHarness(t)
	ctx := t.Context()

	const testKey = "snatcharr.session.ttl"
	const envVar = "APP_SNATCHARR_SESSION_TTL"
	t.Setenv(envVar, "36h")

	_, err := h.st.Q().UpsertRuntimeSetting(ctx, sqlcgen.UpsertRuntimeSettingParams{
		Key:       testKey,
		Value:     "12h",
		UpdatedAt: h.clk.Now(),
	})
	if err != nil {
		t.Fatalf("UpsertRuntimeSetting: %v", err)
	}

	list, err := h.svc.List(ctx)
	if err != nil {
		t.Fatalf("svc.List: %v", err)
	}

	for _, item := range list {
		if item.Key == testKey {
			if item.Source != "env" || !item.Locked {
				t.Errorf("expected source=env, locked=true, got source=%s, locked=%v", item.Source, item.Locked)
			}
			if item.Value == nil || *item.Value != "36h" {
				t.Errorf("expected value=36h, got %v", item.Value)
			}
		}
	}
}

func TestPutEnvLockedConflict(t *testing.T) {
	t.Setenv("APP_SNATCHARR_ITEM_SEARCH_COOLDOWN_SECONDS", "500")
	h := newTestHarness(t)
	uID := uuid.New()
	_, err := h.svc.Put(t.Context(), &uID, map[string]string{
		"snatcharr.item_search.cooldown_seconds": "1000",
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestPutTableDriven(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		values  map[string]string
		wantErr bool
		errIs   error
	}{
		{
			name:    "negative unknown key",
			values:  map[string]string{"snatcharr.nonexistent": "foo"},
			wantErr: true,
			errIs:   domain.ErrInvalid,
		},
		{
			name:    "negative bootstrap key read-only",
			values:  map[string]string{"snatcharr.profile": "prod"},
			wantErr: true,
			errIs:   domain.ErrInvalid,
		},
		{
			name:    "negative worker token env-only read-only",
			values:  map[string]string{"snatcharr.worker.token": "token123"},
			wantErr: true,
			errIs:   domain.ErrInvalid,
		},
		{
			name:    "negative apikey secret env-only read-only",
			values:  map[string]string{"snatcharr.apikey.secret": "sample-test-apikey-secret"},
			wantErr: true,
			errIs:   domain.ErrInvalid,
		},
		{
			name:    "negative crypto key env-only read-only",
			values:  map[string]string{"crypto.key": "sample-test-crypto-key"},
			wantErr: true,
			errIs:   domain.ErrInvalid,
		},
		{
			name:    "negative db dsn env-only read-only",
			values:  map[string]string{"db.dsn": "postgres://localhost/test"},
			wantErr: true,
			errIs:   domain.ErrInvalid,
		},
		{
			name:    "negative invalid value failing options validate",
			values:  map[string]string{"snatcharr.session.ttl": "10s"},
			wantErr: true,
			errIs:   domain.ErrInvalid,
		},
		{
			name:    "positive valid runtime setting",
			values:  map[string]string{"snatcharr.session.ttl": "24h"},
			wantErr: false,
		},
		{
			name:    "boundary min session ttl allowed",
			values:  map[string]string{"snatcharr.session.ttl": "1m"},
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := newTestHarness(t)
			uID := uuid.New()
			res, err := h.svc.Put(t.Context(), &uID, tc.values)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tc.errIs != nil && !errors.Is(err, tc.errIs) {
					t.Fatalf("expected error %v, got %v", tc.errIs, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !res.PendingRestart {
				t.Errorf("expected pending_restart=true")
			}
		})
	}
}

func TestPutSecretEncryptionAndAudit(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	ctx := t.Context()
	uID := uuid.New()

	const plantedValue = "fake-api-key-0123456789"
	values := map[string]string{
		"snatcharr.setup.secret": plantedValue,
	}

	res, err := h.svc.Put(ctx, &uID, values)
	if err != nil {
		t.Fatalf("svc.Put: %v", err)
	}
	if !res.PendingRestart {
		t.Errorf("expected pending_restart=true")
	}

	row, err := h.st.Q().GetRuntimeSetting(ctx, "snatcharr.setup.secret")
	if err != nil {
		t.Fatalf("GetRuntimeSetting: %v", err)
	}
	if row.Value == plantedValue {
		t.Fatalf("secret value was stored unencrypted in database")
	}

	logStr := h.logBuf.String()
	if strings.Contains(logStr, plantedValue) {
		t.Fatalf("secret value leaked into audit log")
	}
	if !strings.Contains(logStr, "snatcharr.setup.secret") {
		t.Errorf("expected audit log to contain key name")
	}
}

func TestPutClearSettingDeletesRow(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	ctx := t.Context()
	uID := uuid.New()

	_, err := h.svc.Put(ctx, &uID, map[string]string{
		"snatcharr.session.ttl": "12h",
	})
	if err != nil {
		t.Fatalf("svc.Put set: %v", err)
	}

	_, err = h.svc.Put(ctx, &uID, map[string]string{
		"snatcharr.session.ttl": "",
	})
	if err != nil {
		t.Fatalf("svc.Put clear: %v", err)
	}

	_, err = h.st.Q().GetRuntimeSetting(ctx, "snatcharr.session.ttl")
	if !errors.Is(store.MapError(err), domain.ErrNotFound) {
		t.Fatalf("expected row to be deleted, got err=%v", err)
	}
}

func TestEffectiveOptionsPrecedence(t *testing.T) {
	const testKey = "snatcharr.session.ttl"
	const envVar = "APP_SNATCHARR_SESSION_TTL"

	cases := []struct {
		name    string
		dbVal   string
		envVal  string
		wantTTL time.Duration
		wantErr bool
	}{
		{name: "none", wantTTL: 8 * time.Hour},
		{name: "db-only", dbVal: "12h", wantTTL: 12 * time.Hour},
		{name: "env-only", envVal: "36h", wantTTL: 36 * time.Hour},
		{name: "both", dbVal: "12h", envVal: "36h", wantTTL: 36 * time.Hour},
		{name: "negative planted invalid duration in db", dbVal: "invalid-duration-format", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APP_SNATCHARR_PROFILE", "dev")
			h := newTestHarness(t)
			ctx := t.Context()
			if tc.envVal != "" {
				t.Setenv(envVar, tc.envVal)
			}
			if tc.dbVal != "" {
				_, err := h.st.Q().UpsertRuntimeSetting(ctx, sqlcgen.UpsertRuntimeSettingParams{
					Key:       testKey,
					Value:     tc.dbVal,
					UpdatedAt: h.clk.Now(),
				})
				if err != nil {
					t.Fatalf("UpsertRuntimeSetting: %v", err)
				}
			}
			cfg, err := gconfig.New(gconfig.Options{EnvPrefix: "APP_", CompoundKeys: config.CompoundKeys()})
			if err != nil {
				t.Fatalf("gconfig.New: %v", err)
			}
			opts, err := runtimesettings.LoadEffectiveOptions(ctx, cfg, h.st, h.enc)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadEffectiveOptions: %v", err)
			}
			if opts.Session.TTL != tc.wantTTL {
				t.Errorf("expected session.ttl=%v, got %v", tc.wantTTL, opts.Session.TTL)
			}
		})
	}
}

func TestLoadOverridesUndecryptableSecret(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	ctx := t.Context()

	_, err := h.st.Q().UpsertRuntimeSetting(ctx, sqlcgen.UpsertRuntimeSettingParams{
		Key:       "snatcharr.setup.secret",
		Value:     "!!!not-base64!!!",
		UpdatedAt: h.clk.Now(),
	})
	if err != nil {
		t.Fatalf("UpsertRuntimeSetting: %v", err)
	}

	_, err = runtimesettings.LoadOverrides(ctx, h.st, h.enc)
	if err == nil {
		t.Fatal("expected LoadOverrides to fail on undecryptable secret row, got nil")
	}
	if !strings.Contains(err.Error(), "decode secret") {
		t.Errorf("expected decode secret error, got %v", err)
	}

	corruptCipher := base64.StdEncoding.EncodeToString([]byte("invalid-sealed-bytes-corrupt"))
	_, err = h.st.Q().UpsertRuntimeSetting(ctx, sqlcgen.UpsertRuntimeSettingParams{
		Key:       "snatcharr.setup.secret",
		Value:     corruptCipher,
		UpdatedAt: h.clk.Now(),
	})
	if err != nil {
		t.Fatalf("UpsertRuntimeSetting: %v", err)
	}

	_, err = runtimesettings.LoadOverrides(ctx, h.st, h.enc)
	if err == nil {
		t.Fatal("expected LoadOverrides to fail on corrupt ciphertext, got nil")
	}
	if !strings.Contains(err.Error(), "open secret") {
		t.Errorf("expected open secret error, got %v", err)
	}
}

func TestPutWithFrameworkEnv(t *testing.T) {
	tests := []struct {
		name       string
		values     map[string]string
		wantErr    bool
		errIs      error
		errContain string
		checkRes   func(t *testing.T, res runtimesettings.PutResult)
	}{
		{
			name:   "positive_valid_runtime_key_with_framework_env",
			values: map[string]string{"snatcharr.item_search.cooldown_seconds": "300"},
			checkRes: func(t *testing.T, res runtimesettings.PutResult) {
				t.Helper()
				if !res.PendingRestart {
					t.Errorf("expected PendingRestart = true")
				}
			},
		},
		{
			name:       "negative_otel_enabled_read_only",
			values:     map[string]string{"otel.enabled": "false"},
			wantErr:    true,
			errIs:      domain.ErrInvalid,
			errContain: "read-only",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("APP_HTTP_ADDR", ":8080")
			t.Setenv("APP_GRPC_LISTEN", ":9090")
			h := newTestHarness(t)
			uID := uuid.New()
			ctx := t.Context()
			res, err := h.svc.Put(ctx, &uID, tt.values)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if tt.errIs != nil && !errors.Is(err, tt.errIs) {
					t.Fatalf("expected error %v, got %v", tt.errIs, err)
				}
				if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
					t.Fatalf("expected error containing %q, got %v", tt.errContain, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.checkRes != nil {
				tt.checkRes(t, res)
			}
		})
	}
}

func TestPutInvalidEnvSettingFails(t *testing.T) {
	t.Setenv("APP_SNATCHARR_SESSION_TTL", "invalid-duration-value")
	h := newTestHarness(t)
	uID := uuid.New()

	_, err := h.svc.Put(t.Context(), &uID, map[string]string{
		"snatcharr.item_search.cooldown_seconds": "500",
	})
	if err == nil {
		t.Fatal("expected Put to fail when environment contains invalid setting, got nil")
	}
	if !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("expected ErrInvalid, got %v", err)
	}
}

func TestPutTransactionRollbackAndAudit(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	clk := clock.NewFake()
	logBuf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cfg := config.Default()
	cfg.Profile = config.ProfileDev
	svc := runtimesettings.New(st, nil, clk, config.BaseOptions{Options: cfg}, logger)

	uID := uuid.New()
	ctx := t.Context()

	values := map[string]string{
		"snatcharr.session.ttl":  "12h",
		"snatcharr.setup.secret": "some-secret",
	}

	_, err := svc.Put(ctx, &uID, values)
	if err == nil {
		t.Fatal("expected Put to fail when encryptor is unavailable for secret setting, got nil")
	}

	_, err = st.Q().GetRuntimeSetting(ctx, "snatcharr.session.ttl")
	if !errors.Is(store.MapError(err), domain.ErrNotFound) {
		t.Fatalf("expected snatcharr.session.ttl to be rolled back, got err=%v", err)
	}

	if strings.Contains(logBuf.String(), "runtime setting updated") {
		t.Fatalf("audit log emitted despite transaction failure: %s", logBuf.String())
	}
}

func TestLoadOverridesSkipsReadOnlyRows(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	ctx := t.Context()

	rows := map[string]string{
		"snatcharr.profile":      "dev",
		"snatcharr.worker.token": "planted-worker-token",
		"snatcharr.public.url":   "https://example.test",
	}
	for k, v := range rows {
		if _, err := h.st.Q().UpsertRuntimeSetting(ctx, sqlcgen.UpsertRuntimeSettingParams{
			Key: k, Value: v, UpdatedAt: h.clk.Now(),
		}); err != nil {
			t.Fatalf("UpsertRuntimeSetting %s: %v", k, err)
		}
	}

	got, err := runtimesettings.LoadOverrides(ctx, h.st, h.enc)
	if err != nil {
		t.Fatalf("LoadOverrides: %v", err)
	}
	tests := []struct {
		key  string
		want bool
	}{
		{key: "snatcharr.profile", want: false},
		{key: "snatcharr.worker.token", want: false},
		{key: "snatcharr.public.url", want: true},
	}
	for _, tc := range tests {
		if _, ok := got[tc.key]; ok != tc.want {
			t.Errorf("override %s present = %v, want %v", tc.key, ok, tc.want)
		}
	}
}

func TestLoadOverridesMissingTable(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	ctx := t.Context()

	if _, err := h.st.Pool().Exec(ctx, "DROP TABLE runtime_settings"); err != nil {
		t.Fatalf("drop table: %v", err)
	}
	got, err := runtimesettings.LoadOverrides(ctx, h.st, h.enc)
	if err != nil {
		t.Fatalf("LoadOverrides on a schema without runtime_settings: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("overrides = %v, want none", got)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := runtimesettings.LoadOverrides(cancelled, h.st, h.enc); err == nil {
		t.Error("LoadOverrides with a cancelled context = nil error, want failure")
	}
}

func TestListLockedFlag(t *testing.T) {
	t.Parallel()
	h := newTestHarness(t)
	list, err := h.svc.List(t.Context())
	if err != nil {
		t.Fatalf("svc.List: %v", err)
	}
	byKey := make(map[string]runtimesettings.SettingItem, len(list))
	for _, item := range list {
		byKey[item.Key] = item
	}
	tests := []struct {
		name   string
		key    string
		locked bool
	}{
		{name: "environment-only secret with its variable unset", key: "snatcharr.worker.token", locked: true},
		{name: "writable runtime setting", key: "snatcharr.public.url", locked: false},
		{name: "database variable, environment only", key: "db.dsn", locked: true},
	}
	for _, tc := range tests {
		item, ok := byKey[tc.key]
		if !ok {
			t.Errorf("%s: %q not listed", tc.name, tc.key)
			continue
		}
		if item.Locked != tc.locked {
			t.Errorf("%s: %q locked = %v, want %v", tc.name, tc.key, item.Locked, tc.locked)
		}
	}
}
