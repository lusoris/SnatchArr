// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain

import (
	"cmp"
	"fmt"
	"slices"
	"time"
)

// DownloadItemState describes an item's scheduling state in a download client.
type DownloadItemState string

const (
	// DownloadItemQueued waits for a download slot.
	DownloadItemQueued DownloadItemState = "queued"
	// DownloadItemActive occupies a download slot.
	DownloadItemActive DownloadItemState = "active"
	// DownloadItemPaused is suspended by a user or SnatchArr.
	DownloadItemPaused DownloadItemState = "paused"
	// DownloadItemFinished no longer requires a download slot.
	DownloadItemFinished DownloadItemState = "finished"
)

// DownloadQueueItem is a caller-supplied snapshot; Rank is already resolved.
// LastWrittenPriority records SnatchArr's last write, nil when none is known.
// Added is supplied by the caller; planners never read the clock.
type DownloadQueueItem struct {
	ID                  string
	Rank                int
	State               DownloadItemState
	Priority            int
	LastWrittenPriority *int
	PausedBySnatchArr   bool
	Added               time.Time
}

// PriorityLevels supplies at least two client levels, best first, without force.
type PriorityLevels struct {
	Best2Worst []int
	Default    int
}

// NZBGetPriorityLevels returns a fresh normal-level table, excluding force (900).
func NZBGetPriorityLevels() PriorityLevels {
	return PriorityLevels{Best2Worst: []int{100, 50, 0, -50, -100}, Default: 0}
}

// SABnzbdPriorityLevels excludes force (2), repair (3), default sentinel (-100),
// and paused (-2); these are commands or states rather than ordinary levels.
func SABnzbdPriorityLevels() PriorityLevels {
	return PriorityLevels{Best2Worst: []int{1, 0, -1}, Default: 0}
}

// SplitRankLevels maps distinct ranks to evenly spaced levels, rounding half up.
// A single rank maps to Default; an empty input yields an empty map.
// Invalid ranks or fewer than two levels wrap ErrInvalid.
func SplitRankLevels(ranks []int, levels PriorityLevels) (map[int]int, error) {
	if len(levels.Best2Worst) < 2 {
		return nil, fmt.Errorf("%w: priority levels require at least two entries", ErrInvalid)
	}
	distinct := slices.Clone(ranks)
	for _, rank := range distinct {
		if err := validateDownloadRank(rank); err != nil {
			return nil, err
		}
	}
	slices.Sort(distinct)
	distinct = slices.Compact(distinct)
	result := make(map[int]int, len(distinct))
	for i, rank := range distinct {
		if len(distinct) == 1 {
			result[rank] = levels.Default
			continue
		}
		denominator := len(distinct) - 1
		index := (i*(len(levels.Best2Worst)-1) + denominator/2) / denominator
		result[rank] = levels.Best2Worst[index]
	}
	return result, nil
}

// PriorityWrite changes an item's priority from its observed value to its target.
type PriorityWrite struct {
	ID       string
	From, To int
}

// DownloadClientPlan contains deterministic actions for one client's snapshot.
// Priority, Pause and Resume are ordered by ID; Order follows scheduling order.
type DownloadClientPlan struct {
	Priority []PriorityWrite
	Order    []string
	Pause    []string
	Resume   []string
}

// PlanLeveledClient splits ranks across all supplied items, but writes only
// queued items whose priority is default or still matches SnatchArr's last write.
// With one distinct rank it leaves priorities untouched. Invalid input wraps ErrInvalid.
func PlanLeveledClient(items []DownloadQueueItem, levels PriorityLevels) (DownloadClientPlan, error) {
	ordered, err := validatedDownloadItems(items)
	if err != nil {
		return DownloadClientPlan{}, err
	}
	ranks := make([]int, len(ordered))
	for i, item := range ordered {
		ranks[i] = item.Rank
	}
	targets, err := SplitRankLevels(ranks, levels)
	if err != nil {
		return DownloadClientPlan{}, fmt.Errorf("download client priority split: %w", err)
	}
	plan := planDownloadPauseResume(ordered)
	if len(targets) < 2 {
		return plan, nil
	}
	for _, item := range ordered {
		if item.State != DownloadItemQueued || downloadPriorityHandSet(item, levels.Default) {
			continue
		}
		if target := targets[item.Rank]; target != item.Priority {
			plan.Priority = append(plan.Priority, PriorityWrite{ID: item.ID, From: item.Priority, To: target})
		}
	}
	return plan, nil
}

// PlanQueueOrderClient orders all supplied IDs by rank, added time, then ID.
// Priority ownership does not affect order. Invalid input wraps ErrInvalid.
func PlanQueueOrderClient(items []DownloadQueueItem) (DownloadClientPlan, error) {
	ordered, err := validatedDownloadItems(items)
	if err != nil {
		return DownloadClientPlan{}, err
	}
	plan := planDownloadPauseResume(ordered)
	slices.SortFunc(ordered, compareDownloadQueueItems)
	for _, item := range ordered {
		plan.Order = append(plan.Order, item.ID)
	}
	return plan, nil
}

func validateDownloadRank(rank int) error {
	if rank < MinPriorityRank || rank > MaxPriorityRank {
		return fmt.Errorf("%w: download rank %d must be in [%d, %d]", ErrInvalid, rank, MinPriorityRank, MaxPriorityRank)
	}
	return nil
}

func validatedDownloadItems(items []DownloadQueueItem) ([]DownloadQueueItem, error) {
	seen := make(map[string]struct{}, len(items))
	for i, item := range items {
		if err := validateDownloadItem(item); err != nil {
			return nil, fmt.Errorf("download item %d (%q): %w", i, item.ID, err)
		}
		if _, exists := seen[item.ID]; exists {
			return nil, fmt.Errorf("%w: duplicate download item ID %q", ErrInvalid, item.ID)
		}
		seen[item.ID] = struct{}{}
	}
	ordered := slices.Clone(items)
	slices.SortFunc(ordered, func(a, b DownloadQueueItem) int { return cmp.Compare(a.ID, b.ID) })
	return ordered, nil
}

func validateDownloadItem(item DownloadQueueItem) error {
	if item.ID == "" {
		return fmt.Errorf("%w: download item ID must not be empty", ErrInvalid)
	}
	if err := validateDownloadRank(item.Rank); err != nil {
		return err
	}
	switch item.State {
	case DownloadItemQueued, DownloadItemActive, DownloadItemPaused, DownloadItemFinished:
		return nil
	default:
		return fmt.Errorf("%w: unknown download item state %q", ErrInvalid, item.State)
	}
}

func downloadPriorityHandSet(item DownloadQueueItem, defaultPriority int) bool {
	return item.Priority != defaultPriority &&
		(item.LastWrittenPriority == nil || item.Priority != *item.LastWrittenPriority)
}

func compareDownloadQueueItems(a, b DownloadQueueItem) int {
	if rank := cmp.Compare(a.Rank, b.Rank); rank != 0 {
		return rank
	}
	if added := a.Added.Compare(b.Added); added != 0 {
		return added
	}
	return cmp.Compare(a.ID, b.ID)
}

// Items arrive sorted by ID. Only queued items justify pausing active work;
// both queued and active work block resumption, preventing pause/resume flaps.
func planDownloadPauseResume(items []DownloadQueueItem) DownloadClientPlan {
	bestQueued, bestUnfinished := MaxPriorityRank+1, MaxPriorityRank+1
	for _, item := range items {
		switch item.State {
		case DownloadItemQueued:
			bestQueued = min(bestQueued, item.Rank)
			bestUnfinished = min(bestUnfinished, item.Rank)
		case DownloadItemActive:
			bestUnfinished = min(bestUnfinished, item.Rank)
		}
	}
	var plan DownloadClientPlan
	for _, item := range items {
		if item.State == DownloadItemActive && bestQueued < item.Rank {
			plan.Pause = append(plan.Pause, item.ID)
		}
		if item.State == DownloadItemPaused && item.PausedBySnatchArr && bestUnfinished >= item.Rank {
			plan.Resume = append(plan.Resume, item.ID)
		}
	}
	return plan
}
