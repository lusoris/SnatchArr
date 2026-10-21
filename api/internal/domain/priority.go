// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain

import "fmt"

const (
	// MinPriorityRank is the highest priority rank (runs first).
	MinPriorityRank = 1
	// MaxPriorityRank is the lowest priority rank (runs last).
	MaxPriorityRank = 100
	// PresetMediaFirst is the identifier of the shipped priority preset.
	PresetMediaFirst = "media_first"
)

// PriorityPreset maps each *arr application kind to a priority rank in [1, 100].
type PriorityPreset map[AppKind]int

// MediaFirstPreset returns a fresh copy of the shipped media_first priority preset.
func MediaFirstPreset() PriorityPreset {
	return PriorityPreset{
		KindSonarr:     10,
		KindRadarr:     10,
		KindLidarr:     20,
		KindReadarr:    30,
		KindWhisparrV2: 40,
		KindWhisparrV3: 40,
	}
}

// Validate checks that every kind in AllKinds is present with a rank in [1, 100] and no unknown kind exists.
func (p PriorityPreset) Validate() error {
	for k := range p {
		if _, err := ParseAppKind(string(k)); err != nil {
			return fmt.Errorf("%w: unknown priority preset kind %q", ErrInvalid, k)
		}
	}
	for _, k := range AllKinds {
		rank, ok := p[k]
		if !ok {
			return fmt.Errorf("%w: priority preset missing kind %q", ErrInvalid, k)
		}
		if rank < MinPriorityRank || rank > MaxPriorityRank {
			return fmt.Errorf("%w: priority preset rank for %q must be in [%d, %d]", ErrInvalid, k, MinPriorityRank, MaxPriorityRank)
		}
	}
	return nil
}

// PrioritySource identifies whether an effective priority rank came from the preset or an instance override.
type PrioritySource string

const (
	// PrioritySourcePreset indicates the rank came from the active preset for the instance's kind.
	PrioritySourcePreset PrioritySource = "preset"
	// PrioritySourceInstance indicates the rank came from the instance's own override.
	PrioritySourceInstance PrioritySource = "instance"
)

// InstancePriority holds optional per-instance search and download priority rank overrides.
type InstancePriority struct {
	Search   *int
	Download *int
}

// Validate checks that any non-nil priority override is in [1, 100].
func (p InstancePriority) Validate() error {
	if p.Search != nil && (*p.Search < MinPriorityRank || *p.Search > MaxPriorityRank) {
		return fmt.Errorf("%w: search_priority must be in [%d, %d]", ErrInvalid, MinPriorityRank, MaxPriorityRank)
	}
	if p.Download != nil && (*p.Download < MinPriorityRank || *p.Download > MaxPriorityRank) {
		return fmt.Errorf("%w: download_priority must be in [%d, %d]", ErrInvalid, MinPriorityRank, MaxPriorityRank)
	}
	return nil
}

// ResolvePriorityRank returns the effective rank and its source for an instance of the given kind.
func ResolvePriorityRank(override *int, kind AppKind, preset PriorityPreset) (int, PrioritySource) {
	if override != nil {
		return *override, PrioritySourceInstance
	}
	if rank, ok := preset[kind]; ok {
		return rank, PrioritySourcePreset
	}
	if rank, ok := MediaFirstPreset()[kind]; ok {
		return rank, PrioritySourcePreset
	}
	return MaxPriorityRank, PrioritySourcePreset
}

// ResolveSearch returns the effective search priority rank and source.
func (p InstancePriority) ResolveSearch(kind AppKind, preset PriorityPreset) (int, PrioritySource) {
	return ResolvePriorityRank(p.Search, kind, preset)
}

// ResolveDownload returns the effective download priority rank and source.
func (p InstancePriority) ResolveDownload(kind AppKind, preset PriorityPreset) (int, PrioritySource) {
	return ResolvePriorityRank(p.Download, kind, preset)
}
