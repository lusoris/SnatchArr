// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package modules

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// Descriptor defines the static contract and configuration validator of an optional module.
type Descriptor struct {
	ID             string
	DefaultEnabled bool
	ValidateConfig func(json.RawMessage) error
}

type watchAwareConfig struct {
	SyncIntervalMinutes *int `json:"sync_interval_minutes"`
}

const (
	minSyncIntervalMinutes = 5
	maxSyncIntervalMinutes = 1440
)

func decodeStrictObject(raw json.RawMessage, name string, dst any) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || trimmed[0] != '{' {
		return fmt.Errorf("%w: config must be a JSON object", domain.ErrInvalid)
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("%w: invalid %s config: %w", domain.ErrInvalid, name, err)
	}
	if dec.More() {
		return fmt.Errorf("%w: unexpected trailing data in %s config", domain.ErrInvalid, name)
	}
	return nil
}

// ValidateLLMConfig validates configuration for the llm module.
// The configuration must be an empty JSON object ({}).
func ValidateLLMConfig(raw json.RawMessage) error {
	var cfg struct{}
	return decodeStrictObject(raw, domain.ModuleLLM, &cfg)
}

// ValidateWatchAwareConfig validates configuration for the watchaware module.
// Optional key sync_interval_minutes must be between 5 and 1440.
func ValidateWatchAwareConfig(raw json.RawMessage) error {
	var cfg watchAwareConfig
	if err := decodeStrictObject(raw, domain.ModuleWatchAware, &cfg); err != nil {
		return err
	}
	if cfg.SyncIntervalMinutes != nil {
		val := *cfg.SyncIntervalMinutes
		if val < minSyncIntervalMinutes || val > maxSyncIntervalMinutes {
			return fmt.Errorf(
				"%w: sync_interval_minutes must be between %d and %d",
				domain.ErrInvalid,
				minSyncIntervalMinutes,
				maxSyncIntervalMinutes,
			)
		}
	}
	return nil
}

// DefaultDescriptors returns the descriptors for the standard built-in modules.
func DefaultDescriptors() []Descriptor {
	return []Descriptor{
		{
			ID:             domain.ModuleLLM,
			DefaultEnabled: false,
			ValidateConfig: ValidateLLMConfig,
		},
		{
			ID:             domain.ModuleWatchAware,
			DefaultEnabled: false,
			ValidateConfig: ValidateWatchAwareConfig,
		},
	}
}
