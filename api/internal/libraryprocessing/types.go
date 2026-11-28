// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package libraryprocessing

import (
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/pathsafe"
)

const (
	// MaxRuleLanguages bounds the number of ISO 639 codes in TrackRule.Languages (HISS-02).
	MaxRuleLanguages = 64
	// MaxItemIDLen bounds the character length of an instance-scoped item identifier.
	MaxItemIDLen = 128
	// MaxRootPathBytes bounds the byte length of a library root path.
	MaxRootPathBytes = pathsafe.MaxPathBytes
)

// MediaType names a media category supported at ScopeMediaType (ADR-0041 section 10).
type MediaType string

// Supported media types at ScopeMediaType.
const (
	MediaTypeMovie      MediaType = "movie"
	MediaTypeEpisode    MediaType = "episode"
	MediaTypeAnime      MediaType = "anime"
	MediaTypeMusic      MediaType = "music"
	MediaTypeAudiobook  MediaType = "audiobook"
	MediaTypeStashScene MediaType = "stash_scene"
)

// AllMediaTypes lists every supported MediaType in display order.
var AllMediaTypes = []MediaType{
	MediaTypeMovie,
	MediaTypeEpisode,
	MediaTypeAnime,
	MediaTypeMusic,
	MediaTypeAudiobook,
	MediaTypeStashScene,
}

// Valid reports whether m is one of the six supported media types.
func (m MediaType) Valid() bool {
	return slices.Contains(AllMediaTypes, m)
}

// ParseMediaType validates a media type string.
func ParseMediaType(raw string) (MediaType, error) {
	mt := MediaType(raw)
	if !mt.Valid() {
		return "", fmt.Errorf("%w: invalid media_type %q", domain.ErrInvalid, raw)
	}
	return mt, nil
}

// TrackRule matches torcul/job-request/1 $defs/TrackRule.
type TrackRule struct {
	Languages []string `json:"languages"`
}

// Rule matches torcul/job-request/1 $defs/Rule.
type Rule struct {
	Track TrackRule `json:"track"`
}

// CloneRule returns a deep copy of r so callers cannot alias internal slices.
func CloneRule(r Rule) Rule {
	return Rule{
		Track: TrackRule{
			Languages: slices.Clone(r.Track.Languages),
		},
	}
}

// RuleInput carries parameters for creating or replacing a stored rule.
type RuleInput struct {
	Scope        Scope
	InstanceID   *uuid.UUID
	ItemID       *string
	UserLanguage *string
	RootPath     *string
	MediaType    *MediaType
	Rule         Rule
}

// StoredRule represents a persisted scoped rule in PostgreSQL.
type StoredRule struct {
	ID           uuid.UUID
	Scope        Scope
	InstanceID   *uuid.UUID
	ItemID       *string
	UserLanguage *string
	RootPath     *string
	MediaType    *MediaType
	Rule         Rule
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ResolveQuery carries the optional item context used to resolve the winning rule.
type ResolveQuery struct {
	InstanceID        *uuid.UUID
	ItemID            *string
	RequesterLanguage *string
	RootPath          *string
	MediaType         *MediaType
}

// ResolvedRule carries the winning rule and its provenance from PostgreSQL.
type ResolvedRule struct {
	RuleID uuid.UUID
	Scope  Scope
	Rule   Rule
}
