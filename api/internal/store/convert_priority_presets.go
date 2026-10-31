// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package store

import (
	"encoding/json"
	"fmt"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// PriorityPresetFromRow decodes a saved snapshot without sharing maps with settings.
func PriorityPresetFromRow(row sqlcgen.PriorityPreset) (domain.NamedPriorityPreset, error) {
	out := domain.NamedPriorityPreset{ID: row.ID, Name: row.Name}
	if err := json.Unmarshal(row.Search, &out.Search); err != nil {
		return out, fmt.Errorf("decode search priority preset: %w", err)
	}
	if err := json.Unmarshal(row.Download, &out.Download); err != nil {
		return out, fmt.Errorf("decode download priority preset: %w", err)
	}
	return out, nil
}
