// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

// MaxPriorityPresets limits saved user presets; the shipped preset does not count.
const MaxPriorityPresets = 20

// PriorityPresetInput is a named snapshot of both priority orders.
type PriorityPresetInput struct {
	Name     string
	Search   PriorityPreset
	Download PriorityPreset
}

// Validate checks the name and both complete priority maps.
func (in PriorityPresetInput) Validate() error {
	name := strings.TrimSpace(in.Name)
	if n := utf8.RuneCountInString(name); n < 1 || n > 40 {
		return fmt.Errorf("%w: preset name must have 1..40 characters", ErrInvalid)
	}
	if strings.EqualFold(name, PresetMediaFirst) {
		return fmt.Errorf("%w: shipped preset name is reserved", ErrConflict)
	}
	if err := in.Search.Validate(); err != nil {
		return fmt.Errorf("search: %w", err)
	}
	if err := in.Download.Validate(); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	return nil
}

// NamedPriorityPreset is either a saved snapshot or the read-only shipped preset.
type NamedPriorityPreset struct {
	ID       uuid.UUID
	Builtin  bool
	Name     string
	Search   PriorityPreset
	Download PriorityPreset
}
