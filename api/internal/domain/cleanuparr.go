// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain

import (
	"time"

	"github.com/google/uuid"
)

// CleanuparrLink is one Cleanuparr server SnatchArr reads status and strikes from. It
// never writes to Cleanuparr; the two are complementary (ADR-0004).
type CleanuparrLink struct {
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

// ValidateCleanuparrLinkInput checks the user-supplied fields.
func ValidateCleanuparrLinkInput(name, baseURL, apiKey string) error {
	return ValidateSeerrLinkInput(name, baseURL, apiKey)
}

// CleanuparrSeekerState describes the overall state of Cleanuparr Seeker.
type CleanuparrSeekerState string

// Seeker states.
const (
	SeekerStateActive   CleanuparrSeekerState = "active"
	SeekerStateInactive CleanuparrSeekerState = "inactive"
	SeekerStateDryRun   CleanuparrSeekerState = "dry_run"
	SeekerStateUnknown  CleanuparrSeekerState = "unknown"
)

// CleanuparrSeekerInstance is one *arr instance observed in Cleanuparr Seeker.
type CleanuparrSeekerInstance struct {
	SnatcharrInstanceID *uuid.UUID
	Name                string
	Type                string
	Active              bool
	Matched             bool
}

// CleanuparrSeeker represents the Seeker status for a Cleanuparr link.
type CleanuparrSeeker struct {
	State     CleanuparrSeekerState
	Reason    string
	Instances []CleanuparrSeekerInstance
}
