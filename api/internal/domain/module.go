// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain

import (
	"encoding/json"
	"time"
)

// Well-known optional module identifiers.
const (
	ModuleAdult      = "adult"
	ModuleLLM        = "llm"
	ModuleWatchAware = "watchaware"
)

// Module represents the persistent state and configuration of an optional module.
type Module struct {
	ID        string          `json:"id"`
	Enabled   bool            `json:"enabled"`
	Config    json.RawMessage `json:"config"`
	UpdatedAt time.Time       `json:"updated_at"`
}
