// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Selection picks how candidates are chosen from a wanted list.
type Selection string

// Selection strategies.
const (
	SelectionRandom     Selection = "random"
	SelectionSequential Selection = "sequential"
)

// Policy is the per-instance hunt policy. Defaults mirror newtarr's gentle defaults:
// one missing item per cycle, no upgrades, 15 minutes between cycles, 20 items per hour.
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
	UpdatedAt          time.Time
}

// DefaultPolicy returns the gentle defaults for a new instance.
func DefaultPolicy(instanceID uuid.UUID) Policy {
	return Policy{
		InstanceID:         instanceID,
		MissingPerCycle:    1,
		UpgradePerCycle:    0,
		CycleInterval:      15 * time.Minute,
		HourlyCap:          20,
		Selection:          SelectionRandom,
		MonitoredOnly:      true,
		SkipFutureReleases: true,
		RadarrReleaseType:  "physical",
		SonarrMissingMode:  "episodes",
		SonarrUpgradeMode:  "episodes",
		LidarrMissingMode:  "artist",
		ProcessedTTL:       168 * time.Hour,
		MaxQueueSize:       -1,
		AwaitCommand:       false,
		PageSize:           100,
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
	for _, a := range allowed {
		if a == value {
			return nil
		}
	}
	return fmt.Errorf("%w: %s must be one of %v", ErrInvalid, name, allowed)
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
	}); err != nil {
		return err
	}
	if err := checkEnum("selection", string(p.Selection), string(SelectionRandom), string(SelectionSequential)); err != nil {
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
