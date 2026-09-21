// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package domain holds SnatchArr's core types and invariants. It has no I/O and no
// framework dependencies so every other package can import it freely.
package domain

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AppKind identifies an *arr application family and API generation.
type AppKind string

// Supported application kinds.
const (
	KindSonarr     AppKind = "sonarr"
	KindRadarr     AppKind = "radarr"
	KindLidarr     AppKind = "lidarr"
	KindReadarr    AppKind = "readarr"
	KindWhisparrV2 AppKind = "whisparr_v2"
	KindWhisparrV3 AppKind = "whisparr_v3"
)

// AllKinds lists every supported kind in display order.
var AllKinds = []AppKind{KindSonarr, KindRadarr, KindLidarr, KindReadarr, KindWhisparrV2, KindWhisparrV3}

// ParseAppKind validates a kind string.
func ParseAppKind(s string) (AppKind, error) {
	for _, k := range AllKinds {
		if string(k) == s {
			return k, nil
		}
	}
	return "", fmt.Errorf("%w: app kind %q", ErrInvalid, s)
}

// HuntKind selects which wanted list a run walks.
type HuntKind string

// Hunt kinds.
const (
	HuntMissing HuntKind = "missing"
	HuntUpgrade HuntKind = "upgrade"
)

// ParseHuntKind validates a hunt kind string.
func ParseHuntKind(s string) (HuntKind, error) {
	switch HuntKind(s) {
	case HuntMissing, HuntUpgrade:
		return HuntKind(s), nil
	default:
		return "", fmt.Errorf("%w: hunt kind %q", ErrInvalid, s)
	}
}

// InstanceSource says who owns an instance definition.
type InstanceSource string

// Instance sources.
const (
	SourceManual    InstanceSource = "manual"
	SourceConfigarr InstanceSource = "configarr"
)

// Instance is a configured *arr application. APIKey is only populated on the write
// path; reads carry an empty string so the secret never leaves the service layer.
type Instance struct {
	ID              uuid.UUID
	Kind            AppKind
	Name            string
	BaseURL         string
	APIKey          string
	Enabled         bool
	Source          InstanceSource
	ConfigarrKey    string
	LastSeenVersion string
	LastCheckAt     *time.Time
	LastError       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// NormalizeBaseURL trims, lower-cases the scheme/host, and strips a trailing slash so
// two spellings of the same instance compare equal.
func NormalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%w: base_url is required", ErrInvalid)
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("%w: base_url %q is not a valid URL", ErrInvalid, raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("%w: base_url scheme must be http or https", ErrInvalid)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery, u.Fragment = "", ""
	return u.String(), nil
}

// ValidateInstanceInput checks the user-supplied fields of an instance.
func ValidateInstanceInput(kind AppKind, name, baseURL, apiKey string) error {
	if _, err := ParseAppKind(string(kind)); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return fmt.Errorf("%w: name must be 1-64 characters", ErrInvalid)
	}
	if _, err := NormalizeBaseURL(baseURL); err != nil {
		return err
	}
	if len(strings.TrimSpace(apiKey)) < 8 {
		return fmt.Errorf("%w: api_key looks too short", ErrInvalid)
	}
	return nil
}
