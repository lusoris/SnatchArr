// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SourceSeerr marks an instance imported from Seerr's Sonarr/Radarr settings.
const SourceSeerr InstanceSource = "seerr"

// SeerrLink is one Seerr server SnatchArr reads requests from. It never writes back.
type SeerrLink struct {
	ID      uuid.UUID
	Name    string
	BaseURL string
	Enabled bool
	// Fallbacks for requests whose Seerr server cannot be matched to an instance by URL.
	SonarrInstanceID *uuid.UUID
	RadarrInstanceID *uuid.UUID
	LastSeenVersion  string
	LastCheckAt      *time.Time
	LastError        string
	LastSyncAt       *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// SeerrMediaType is what a request asks for.
type SeerrMediaType string

// Media types.
const (
	SeerrMovie SeerrMediaType = "movie"
	SeerrTV    SeerrMediaType = "tv"
)

// Seerr request statuses (MediaRequestStatus).
const (
	SeerrRequestPending   = 1
	SeerrRequestApproved  = 2
	SeerrRequestDeclined  = 3
	SeerrRequestFailed    = 4
	SeerrRequestCompleted = 5
)

// Seerr media statuses (MediaStatus).
const (
	SeerrMediaUnknown            = 1
	SeerrMediaPending            = 2
	SeerrMediaProcessing         = 3
	SeerrMediaPartiallyAvailable = 4
	SeerrMediaAvailable          = 5
)

// SeerrRequest is a cached, approved-but-unavailable request mapped onto SnatchArr.
type SeerrRequest struct {
	LinkID         uuid.UUID
	RequestID      int
	MediaType      SeerrMediaType
	TmdbID         int
	TvdbID         int
	Title          string
	RequestStatus  int
	MediaStatus    int
	Is4K           bool
	RequestedBy    string
	Seasons        []int
	SeerrServerID  int
	InstanceID     *uuid.UUID // resolved SnatchArr instance, nil when unresolved
	EntityID       *int64     // Radarr movie id or Sonarr series id, nil when not in the library
	RequestedAt    *time.Time
	LastSnatchedAt *time.Time
	LastSeenAt     time.Time
	UpdatedAt      time.Time
}

// Resolved reports whether the request maps to an instance and a library entity.
func (r SeerrRequest) Resolved() bool { return r.InstanceID != nil && r.EntityID != nil }

// ValidateSeerrLinkInput checks the user-supplied fields.
func ValidateSeerrLinkInput(name, baseURL, apiKey string) error {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return fmt.Errorf("%w: name must be 1-64 characters", ErrInvalid)
	}
	if _, err := NormalizeBaseURL(baseURL); err != nil {
		return err
	}
	if len(strings.TrimSpace(apiKey)) < 8 {
		return fmt.Errorf("%w: api_key must be at least 8 characters", ErrInvalid)
	}
	return nil
}
