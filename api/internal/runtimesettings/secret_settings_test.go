// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package runtimesettings_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/runtimesettings"
	"github.com/lusoris/SnatchArr/api/internal/store"
)

func storableSecrets() []config.Setting {
	registry := config.Registry()
	secrets := make([]config.Setting, 0, len(registry))
	for _, setting := range registry {
		if setting.Class == config.ClassSecret && !config.IsEnvOnlySecret(setting.Key, setting.Env) {
			secrets = append(secrets, setting)
		}
	}
	return secrets
}

func TestStorableSecretsCoverTVDB(t *testing.T) {
	t.Parallel()
	secrets := storableSecrets()
	if len(secrets) == 0 {
		t.Fatal("no storable secrets in registry")
	}
	for _, key := range []string{"snatcharr.metadata.tvdb.api_key", "snatcharr.metadata.tvdb.pin"} {
		if !slices.ContainsFunc(secrets, func(setting config.Setting) bool { return setting.Key == key }) {
			t.Errorf("storable secrets missing %s", key)
		}
	}
}

func TestStorableSecretsStayWriteOnly(t *testing.T) {
	t.Parallel()
	for i, setting := range storableSecrets() {
		t.Run(setting.Key, func(t *testing.T) {
			t.Parallel()
			h := newTestHarness(t)
			ctx := t.Context()
			userID := uuid.New()
			value := fmt.Sprintf("planted-%d-value", i)
			if _, err := h.svc.Put(ctx, &userID, map[string]string{setting.Key: value}); err != nil {
				t.Fatalf("Put: %v", err)
			}
			if err := checkSecretStorage(ctx, h, setting.Key, value); err != nil {
				t.Error(err)
			}
			if err := checkSecretAbsentFromLog(h.logBuf.String(), value); err != nil {
				t.Error(err)
			}
			if listErr := checkSecretList(ctx, h, setting.Key, true); listErr != nil {
				t.Error(listErr)
			}
			if _, clearErr := h.svc.Put(ctx, &userID, map[string]string{setting.Key: ""}); clearErr != nil {
				t.Fatalf("Put clear: %v", clearErr)
			}
			if _, getErr := h.st.Q().GetRuntimeSetting(ctx, setting.Key); !errors.Is(store.MapError(getErr), domain.ErrNotFound) {
				t.Errorf("GetRuntimeSetting after clear: want not found, got %v", getErr)
			}
			if listErr := checkSecretList(ctx, h, setting.Key, false); listErr != nil {
				t.Error(listErr)
			}
		})
	}
}

func checkSecretStorage(ctx context.Context, h *testHarness, key, value string) error {
	row, err := h.st.Q().GetRuntimeSetting(ctx, key)
	if err != nil {
		return fmt.Errorf("get stored secret %s: %w", key, err)
	}
	if sealedErr := checkSealedSecretRow(row.Value, value); sealedErr != nil {
		return fmt.Errorf("stored secret %s: %w", key, sealedErr)
	}
	overrides, err := runtimesettings.LoadOverrides(ctx, h.st, h.enc)
	if err != nil {
		return fmt.Errorf("load secret overrides: %w", err)
	}
	if overrides[key] != value {
		return fmt.Errorf("secret %s did not round trip through encryption", key)
	}
	return nil
}

func checkSealedSecretRow(stored, value string) error {
	if stored == "" || strings.Contains(stored, value) {
		return errors.New("secret row is empty or contains plaintext")
	}
	return nil
}

func checkSecretAbsentFromLog(logText, value string) error {
	if strings.Contains(logText, value) {
		return errors.New("secret value leaked into log")
	}
	return nil
}

func checkSecretList(ctx context.Context, h *testHarness, key string, wantSet bool) error {
	items, err := h.svc.List(ctx)
	if err != nil {
		return fmt.Errorf("list secret %s: %w", key, err)
	}
	return checkSecretListWriteOnly(items, key, wantSet)
}

func checkSecretListWriteOnly(items []runtimesettings.SettingItem, key string, wantSet bool) error {
	for _, item := range items {
		if item.Key != key {
			continue
		}
		if item.Value != nil {
			return fmt.Errorf("List exposes a value for secret %s", key)
		}
		if item.IsSet == nil || *item.IsSet != wantSet {
			return fmt.Errorf("List secret %s: want IsSet %t", key, wantSet)
		}
		return nil
	}
	return fmt.Errorf("List missing secret %s", key)
}

func TestSecretCheckHelpersRefuse(t *testing.T) {
	t.Parallel()
	const key = "snatcharr.metadata.tvdb.api_key"
	value := "planted-helper-value"
	isSet, notSet := true, false
	for _, tc := range []struct {
		name  string
		check func() error
	}{
		{"plaintext_row", func() error { return checkSealedSecretRow("prefix-"+value, value) }},
		{"empty_row", func() error { return checkSealedSecretRow("", value) }},
		{"plaintext_log", func() error { return checkSecretAbsentFromLog("updated "+value, value) }},
		{"list_value", func() error {
			return checkSecretListWriteOnly([]runtimesettings.SettingItem{{Key: key, Value: &value, IsSet: &isSet}}, key, true)
		}},
		{"list_unset", func() error {
			return checkSecretListWriteOnly([]runtimesettings.SettingItem{{Key: key, IsSet: &notSet}}, key, true)
		}},
		{"list_nil_is_set", func() error {
			return checkSecretListWriteOnly([]runtimesettings.SettingItem{{Key: key}}, key, true)
		}},
		{"list_still_set_after_clear", func() error {
			return checkSecretListWriteOnly([]runtimesettings.SettingItem{{Key: key, IsSet: &isSet}}, key, false)
		}},
		{"list_missing", func() error { return checkSecretListWriteOnly(nil, key, true) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.check(); err == nil {
				t.Fatal("helper accepted invalid secret state")
			}
		})
	}
}
