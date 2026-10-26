// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package modules

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// Descriptor defines the static contract and configuration validator of an optional module.
type Descriptor struct {
	ID             string
	DefaultEnabled bool
	ValidateConfig func(json.RawMessage) error
}

type adultConfig struct {
	ImageSetDir         *string `json:"image_set_dir"`
	SyncIntervalMinutes *int    `json:"sync_interval_minutes"`
}

type watchAwareGateConfig struct {
	Enabled      *bool    `json:"enabled"`
	Mode         *string  `json:"mode"`
	SeasonsOnAdd *int     `json:"seasons_on_add"`
	LookAhead    *int     `json:"look_ahead"`
	PromoteWhose *string  `json:"promote_whose"`
	ExemptTag    *string  `json:"exempt_tag"`
	InstanceIDs  []string `json:"instance_ids"`
}

type watchAwareConfig struct {
	SyncIntervalMinutes *int                  `json:"sync_interval_minutes"`
	Gate                *watchAwareGateConfig `json:"gate"`
}

const (
	// MinAdultSyncIntervalMinutes is the minimum allowed sync interval for the adult module in minutes.
	MinAdultSyncIntervalMinutes = 15
	// MaxAdultSyncIntervalMinutes is the maximum allowed sync interval for the adult module in minutes.
	MaxAdultSyncIntervalMinutes = 1440
	// MaxAdultImageSetDirLen is the maximum allowed path length for the adult image set directory.
	MaxAdultImageSetDirLen = 4096

	// MinSyncIntervalMinutes is the minimum allowed sync interval for watchaware.
	MinSyncIntervalMinutes = 5
	// MaxSyncIntervalMinutes is the maximum allowed sync interval for watchaware.
	MaxSyncIntervalMinutes = 1440

	// MinGateSeasonsOnAdd is the minimum allowed seasons kept on add.
	MinGateSeasonsOnAdd = 1
	// MaxGateSeasonsOnAdd is the maximum allowed seasons kept on add.
	MaxGateSeasonsOnAdd = 20
	// MinGateLookAhead is the minimum allowed look-ahead seasons.
	MinGateLookAhead = 0
	// MaxGateLookAhead is the maximum allowed look-ahead seasons.
	MaxGateLookAhead = 5
	// MaxGateInstanceIDs is the maximum allowed instance IDs pinned to the gate.
	MaxGateInstanceIDs = 32
	// MinGateExemptTagLen is the minimum allowed exempt tag length.
	MinGateExemptTagLen = 1
	// MaxGateExemptTagLen is the maximum allowed exempt tag length.
	MaxGateExemptTagLen = 64
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

func validateAdultSyncInterval(interval *int) error {
	if interval == nil {
		return nil
	}
	val := *interval
	if val < MinAdultSyncIntervalMinutes || val > MaxAdultSyncIntervalMinutes {
		return fmt.Errorf(
			"%w: sync_interval_minutes must be between %d and %d",
			domain.ErrInvalid,
			MinAdultSyncIntervalMinutes,
			MaxAdultSyncIntervalMinutes,
		)
	}
	return nil
}

func validateAdultImageSetDir(dirPtr *string) error {
	if dirPtr == nil {
		return nil
	}
	dir := *dirPtr
	if len(dir) == 0 {
		return fmt.Errorf("%w: image_set_dir must not be empty", domain.ErrInvalid)
	}
	if len(dir) > MaxAdultImageSetDirLen {
		return fmt.Errorf(
			"%w: image_set_dir exceeds maximum length of %d",
			domain.ErrInvalid,
			MaxAdultImageSetDirLen,
		)
	}
	if strings.ContainsRune(dir, '\x00') {
		return fmt.Errorf("%w: image_set_dir must not contain NUL bytes", domain.ErrInvalid)
	}
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("%w: image_set_dir must be an absolute path", domain.ErrInvalid)
	}
	if filepath.Clean(dir) != dir {
		return fmt.Errorf("%w: image_set_dir must be a clean path", domain.ErrInvalid)
	}
	return nil
}

// ValidateAdultConfig validates configuration for the adult module.
// Optional key sync_interval_minutes must be between 15 and 1440.
// Optional key image_set_dir must be a non-empty, clean absolute path without NUL bytes up to 4096 bytes.
func ValidateAdultConfig(raw json.RawMessage) error {
	var cfg adultConfig
	if err := decodeStrictObject(raw, domain.ModuleAdult, &cfg); err != nil {
		return err
	}
	if err := validateAdultSyncInterval(cfg.SyncIntervalMinutes); err != nil {
		return err
	}
	return validateAdultImageSetDir(cfg.ImageSetDir)
}

// ValidateLLMConfig validates configuration for the llm module.
// The configuration must be an empty JSON object ({}).
func ValidateLLMConfig(raw json.RawMessage) error {
	var cfg struct{}
	return decodeStrictObject(raw, domain.ModuleLLM, &cfg)
}

func validateGateMode(mode *string) error {
	if mode == nil {
		return nil
	}
	switch *mode {
	case "observe", "act":
		return nil
	default:
		return fmt.Errorf("%w: invalid gate mode %q", domain.ErrInvalid, *mode)
	}
}

func validateGateNumericBounds(gate *watchAwareGateConfig) error {
	if gate.SeasonsOnAdd != nil {
		val := *gate.SeasonsOnAdd
		if val < MinGateSeasonsOnAdd || val > MaxGateSeasonsOnAdd {
			return fmt.Errorf("%w: seasons_on_add must be between %d and %d", domain.ErrInvalid, MinGateSeasonsOnAdd, MaxGateSeasonsOnAdd)
		}
	}
	if gate.LookAhead != nil {
		val := *gate.LookAhead
		if val < MinGateLookAhead || val > MaxGateLookAhead {
			return fmt.Errorf("%w: look_ahead must be between %d and %d", domain.ErrInvalid, MinGateLookAhead, MaxGateLookAhead)
		}
	}
	return nil
}

func validateGateStringBounds(gate *watchAwareGateConfig) error {
	if gate.PromoteWhose != nil {
		val := *gate.PromoteWhose
		if val != "any" && val != "all" {
			return fmt.Errorf("%w: promote_whose must be \"any\" or \"all\"", domain.ErrInvalid)
		}
	}
	if gate.ExemptTag != nil {
		tag := *gate.ExemptTag
		if len(tag) < MinGateExemptTagLen || len(tag) > MaxGateExemptTagLen {
			return fmt.Errorf("%w: exempt_tag length must be between %d and %d", domain.ErrInvalid, MinGateExemptTagLen, MaxGateExemptTagLen)
		}
	}
	return nil
}

func validateGateBounds(gate *watchAwareGateConfig) error {
	if err := validateGateNumericBounds(gate); err != nil {
		return err
	}
	return validateGateStringBounds(gate)
}

func validateGateInstanceIDs(ids []string) error {
	if len(ids) > MaxGateInstanceIDs {
		return fmt.Errorf("%w: instance_ids exceeds maximum of %d", domain.ErrInvalid, MaxGateInstanceIDs)
	}
	for _, idStr := range ids {
		if _, err := uuid.Parse(idStr); err != nil {
			return fmt.Errorf("%w: invalid instance_id %q: %w", domain.ErrInvalid, idStr, err)
		}
	}
	return nil
}

func validateWatchAwareGateConfig(gate *watchAwareGateConfig) error {
	if gate == nil {
		return nil
	}
	if err := validateGateMode(gate.Mode); err != nil {
		return err
	}
	if err := validateGateBounds(gate); err != nil {
		return err
	}
	return validateGateInstanceIDs(gate.InstanceIDs)
}

// ValidateWatchAwareConfig validates configuration for the watchaware module.
// Optional key sync_interval_minutes must be between 5 and 1440.
// Optional object gate configures the season gate.
func ValidateWatchAwareConfig(raw json.RawMessage) error {
	var cfg watchAwareConfig
	if err := decodeStrictObject(raw, domain.ModuleWatchAware, &cfg); err != nil {
		return err
	}
	if cfg.SyncIntervalMinutes != nil {
		val := *cfg.SyncIntervalMinutes
		if val < MinSyncIntervalMinutes || val > MaxSyncIntervalMinutes {
			return fmt.Errorf(
				"%w: sync_interval_minutes must be between %d and %d",
				domain.ErrInvalid,
				MinSyncIntervalMinutes,
				MaxSyncIntervalMinutes,
			)
		}
	}
	return validateWatchAwareGateConfig(cfg.Gate)
}

// DefaultDescriptors returns the descriptors for the standard built-in modules.
func DefaultDescriptors() []Descriptor {
	return []Descriptor{
		{
			ID:             domain.ModuleAdult,
			DefaultEnabled: false,
			ValidateConfig: ValidateAdultConfig,
		},
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
