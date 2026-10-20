// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain

import (
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
)

// Selection picks how candidates are chosen from a wanted list.
type Selection string

// Selection strategies.
const (
	SelectionRandom     Selection = "random"
	SelectionSequential Selection = "sequential"
	// SelectionRecent prefers the newest releases with a slice reserved for the backlog.
	SelectionRecent Selection = "recent"
)

// AllSelections lists every selection strategy.
var AllSelections = []Selection{SelectionRandom, SelectionSequential, SelectionRecent}

// Policy is the per-instance snatch policy. Defaults follow balanced defaults (ADR-0020):
// two missing items per cycle, one upgrade, 15 minutes between cycles, 15 items per hour.
type Policy struct {
	InstanceID         uuid.UUID
	MissingPerCycle    int
	UpgradePerCycle    int
	CycleInterval      time.Duration
	HourlyCap          int
	Selection          Selection
	MonitoredOnly      bool
	SkipFutureReleases bool
	RadarrReleaseType  string
	SonarrMissingMode  string
	SonarrUpgradeMode  string
	LidarrMissingMode  string
	ProcessedTTL       time.Duration
	MaxQueueSize       int
	AwaitCommand       bool
	PageSize           int
	// RecentGrabWindow skips items the *arr grabbed this recently (0 = only the queue).
	RecentGrabWindow time.Duration
	// AfterglowMax caps the exponential rest time of repeatedly snatched items.
	AfterglowMax time.Duration
	// IncludeTags, when not empty, limits searches to items whose *arr tags include one of
	// these ids; ExcludeTags skips items that carry any of them. The same holds for the
	// quality profile lists. An episode, album or book has the tags and profile of its parent.
	IncludeTags            []int32
	ExcludeTags            []int32
	IncludeQualityProfiles []int32
	ExcludeQualityProfiles []int32
	// ReleaseDelay skips items released more recently than this (0 = no delay). The release
	// date is the one the app kind uses; for Radarr it is the date of RadarrReleaseType.
	ReleaseDelay time.Duration
	DryRun       bool
	// SkipUnavailableMovies skips Radarr movies whose minimum availability is not reached
	// (isAvailable false); records without the flag are searched.
	SkipUnavailableMovies bool
	UpdatedAt             time.Time
}

// DefaultPolicy returns the balanced defaults, ADR-0020, for a new instance.
func DefaultPolicy(instanceID uuid.UUID) Policy {
	return Policy{
		InstanceID:             instanceID,
		MissingPerCycle:        2,
		UpgradePerCycle:        1,
		CycleInterval:          15 * time.Minute,
		HourlyCap:              15,
		Selection:              SelectionRandom,
		MonitoredOnly:          true,
		SkipFutureReleases:     true,
		RadarrReleaseType:      "physical",
		SonarrMissingMode:      "episodes",
		SonarrUpgradeMode:      "episodes",
		LidarrMissingMode:      "artist",
		ProcessedTTL:           168 * time.Hour,
		MaxQueueSize:           -1,
		AwaitCommand:           false,
		PageSize:               100,
		RecentGrabWindow:       24 * time.Hour,
		AfterglowMax:           720 * time.Hour,
		IncludeTags:            []int32{},
		ExcludeTags:            []int32{},
		IncludeQualityProfiles: []int32{},
		ExcludeQualityProfiles: []int32{},
		DryRun:                 false,
		SkipUnavailableMovies:  true,
	}
}

type rangeRule struct {
	name     string
	value    int
	min, max int
}

func checkRanges(rules []rangeRule) error {
	for _, r := range rules {
		if r.value < r.min || r.value > r.max {
			return fmt.Errorf("%w: %s must be between %d and %d", ErrInvalid, r.name, r.min, r.max)
		}
	}
	return nil
}

func checkEnum(name, value string, allowed ...string) error {
	if slices.Contains(allowed, value) {
		return nil
	}
	return fmt.Errorf("%w: %s must be one of %v", ErrInvalid, name, allowed)
}

// FilterIDs returns an empty list for nil: the store keeps the filter lists as NOT NULL arrays
// and the API always shows them.
func FilterIDs(v []int32) []int32 {
	if v == nil {
		return []int32{}
	}
	return v
}

// maxFilterIDs bounds each include and exclude list of a policy.
const maxFilterIDs = 100

// checkIDs enforces positive *arr ids, no repeats and at most maxFilterIDs per list.
func checkIDs(name string, ids []int32) error {
	if len(ids) > maxFilterIDs {
		return fmt.Errorf("%w: %s holds at most %d ids", ErrInvalid, name, maxFilterIDs)
	}
	for i, id := range ids {
		if id < 1 {
			return fmt.Errorf("%w: %s ids must be positive", ErrInvalid, name)
		}
		if slices.Contains(ids[:i], id) {
			return fmt.Errorf("%w: %s lists id %d twice", ErrInvalid, name, id)
		}
	}
	return nil
}

// checkFilter validates one include and exclude pair; an id may not be in both lists.
func checkFilter(name string, include, exclude []int32) error {
	if err := checkIDs("include_"+name, include); err != nil {
		return err
	}
	if err := checkIDs("exclude_"+name, exclude); err != nil {
		return err
	}
	for _, id := range include {
		if slices.Contains(exclude, id) {
			return fmt.Errorf("%w: %s id %d is both included and excluded", ErrInvalid, name, id)
		}
	}
	return nil
}

// Validate enforces the ranges the UI advertises (and the DB CHECK constraints).
func (p Policy) Validate() error {
	if err := checkRanges([]rangeRule{
		{"missing_per_cycle", p.MissingPerCycle, 0, 100},
		{"upgrade_per_cycle", p.UpgradePerCycle, 0, 100},
		{"cycle_interval_s", int(p.CycleInterval / time.Second), 60, 86400 * 7},
		{"hourly_cap", p.HourlyCap, 1, 500},
		{"processed_ttl_h", int(p.ProcessedTTL / time.Hour), 1, 8760},
		{"max_queue_size", p.MaxQueueSize, -1, 100000},
		{"page_size", p.PageSize, 10, 1000},
		{"recent_grab_window_h", int(p.RecentGrabWindow / time.Hour), 0, 720},
		{"afterglow_max_h", int(p.AfterglowMax / time.Hour), 1, 8760},
		{"release_delay_h", int(p.ReleaseDelay / time.Hour), 0, 8760},
	}); err != nil {
		return err
	}
	if err := checkFilter("tags", p.IncludeTags, p.ExcludeTags); err != nil {
		return err
	}
	if err := checkFilter("quality_profiles", p.IncludeQualityProfiles, p.ExcludeQualityProfiles); err != nil {
		return err
	}
	if err := checkEnum("selection", string(p.Selection), string(SelectionRandom), string(SelectionSequential), string(SelectionRecent)); err != nil {
		return err
	}
	if err := checkEnum("radarr_release_type", p.RadarrReleaseType, "physical", "digital", "cinema"); err != nil {
		return err
	}
	if err := checkEnum("sonarr_missing_mode", p.SonarrMissingMode, "episodes", "season_packs", "shows"); err != nil {
		return err
	}
	if err := checkEnum("sonarr_upgrade_mode", p.SonarrUpgradeMode, "episodes", "season_packs"); err != nil {
		return err
	}
	return checkEnum("lidarr_missing_mode", p.LidarrMissingMode, "artist", "album")
}
