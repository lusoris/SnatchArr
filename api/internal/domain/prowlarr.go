// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain

import (
	"time"

	"github.com/google/uuid"
)

// ProwlarrLink is one Prowlarr server SnatchArr reads indexer stamina and limits from (#38).
// It never writes to Prowlarr.
type ProwlarrLink struct {
	ID              uuid.UUID
	Name            string
	BaseURL         string
	Enabled         bool
	LastSeenVersion string
	LastCheckAt     *time.Time
	LastError       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// ValidateProwlarrLinkInput checks the user-supplied fields.
func ValidateProwlarrLinkInput(name, baseURL, apiKey string) error {
	return ValidateSeerrLinkInput(name, baseURL, apiKey)
}
