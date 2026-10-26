// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Prowlarr hourly stamina cap bounds.
const (
	DefaultProwlarrHourlyCap = 15
	MinProwlarrHourlyCap     = 1
	MaxProwlarrHourlyCap     = 500
)

// ProwlarrLink is one Prowlarr server SnatchArr reads indexer stamina and limits from (#38).
// It never writes to Prowlarr.
type ProwlarrLink struct {
	ID              uuid.UUID
	Name            string
	BaseURL         string
	HourlyCap       int
	Enabled         bool
	LastSeenVersion string
	LastCheckAt     *time.Time
	LastError       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// ValidateProwlarrHourlyCap checks that the cap is between 1 and 500.
func ValidateProwlarrHourlyCap(hourlyCap int) error {
	if hourlyCap < MinProwlarrHourlyCap || hourlyCap > MaxProwlarrHourlyCap {
		return fmt.Errorf("%w: hourly_cap must be between %d and %d", ErrInvalid, MinProwlarrHourlyCap, MaxProwlarrHourlyCap)
	}
	return nil
}

// ValidateProwlarrLinkInput checks the user-supplied fields.
func ValidateProwlarrLinkInput(name, baseURL, apiKey string, hourlyCap int) error {
	if err := ValidateSeerrLinkInput(name, baseURL, apiKey); err != nil {
		return err
	}
	return ValidateProwlarrHourlyCap(hourlyCap)
}
