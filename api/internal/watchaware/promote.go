// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package watchaware

import (
	"fmt"
	"slices"
)

// PromoteSeasonThreshold is the minimum watched episode percentage (50.0%)
// for a season to count as watched for promotion if the last episode in the
// library is not watched.
const PromoteSeasonThreshold = 50.0

// SeasonWatch represents an episode watch state for promotion evaluation.
type SeasonWatch struct {
	UserKey string
	Season  int
	Episode int
	Watched bool
}

// seasonWatched reports whether a season counts as watched given watched episode count,
// total episode count, and whether the last episode in library is watched.
// Per ADR-0024, a season is watched when EITHER the last episode of S present in that
// source's library is Watched() OR Watched() share of episodes of S present in the
// library is >= PromoteSeasonThreshold (50.0%). Zero episodes -> not watched.
// Note: "aired" episodes are approximated by those present in the media-server library.
func seasonWatched(watched, total int, lastWatched bool) bool {
	if total <= 0 {
		return false
	}
	if lastWatched {
		return true
	}
	share := (float64(watched) / float64(total)) * 100.0
	return share >= PromoteSeasonThreshold
}

type userSeasonTracker struct {
	episodes map[int]bool
}

func groupWatchesByUserAndSeason(watches []SeasonWatch) ([]string, map[string]map[int]*userSeasonTracker) {
	userSeasons := make(map[string]map[int]*userSeasonTracker)
	userSeen := make(map[string]struct{})
	var allUsers []string

	for _, w := range watches {
		if _, ok := userSeen[w.UserKey]; !ok {
			userSeen[w.UserKey] = struct{}{}
			allUsers = append(allUsers, w.UserKey)
		}
		if w.Season <= 0 {
			continue
		}
		sm, ok := userSeasons[w.UserKey]
		if !ok {
			sm = make(map[int]*userSeasonTracker)
			userSeasons[w.UserKey] = sm
		}
		tracker, ok := sm[w.Season]
		if !ok {
			tracker = &userSeasonTracker{episodes: make(map[int]bool)}
			sm[w.Season] = tracker
		}
		tracker.episodes[w.Episode] = tracker.episodes[w.Episode] || w.Watched
	}
	return allUsers, userSeasons
}

func isSeasonWatchedForUser(tracker *userSeasonTracker) bool {
	if tracker == nil || len(tracker.episodes) == 0 {
		return false
	}
	total := len(tracker.episodes)
	watched := 0
	lastEpisode := -1
	for ep, isWatched := range tracker.episodes {
		if isWatched {
			watched++
		}
		if ep > lastEpisode {
			lastEpisode = ep
		}
	}
	lastWatched := tracker.episodes[lastEpisode]
	return seasonWatched(watched, total, lastWatched)
}

func isSeasonWatchedUnderRule(
	season int,
	allUsers []string,
	userSeasons map[string]map[int]*userSeasonTracker,
	whose string,
) bool {
	if len(allUsers) == 0 {
		return false
	}
	if whose == "all" {
		for _, u := range allUsers {
			sm := userSeasons[u]
			var tr *userSeasonTracker
			if sm != nil {
				tr = sm[season]
			}
			if !isSeasonWatchedForUser(tr) {
				return false
			}
		}
		return true
	}
	for _, u := range allUsers {
		sm := userSeasons[u]
		if sm != nil && isSeasonWatchedForUser(sm[season]) {
			return true
		}
	}
	return false
}

func findFurthestWatchedSeason(
	allUsers []string,
	userSeasons map[string]map[int]*userSeasonTracker,
	whose string,
) int {
	seasonsSeen := make(map[int]struct{})
	for _, sm := range userSeasons {
		for s := range sm {
			if s > 0 {
				seasonsSeen[s] = struct{}{}
			}
		}
	}

	furthest := 0
	for s := range seasonsSeen {
		if isSeasonWatchedUnderRule(s, allUsers, userSeasons, whose) {
			if s > furthest {
				furthest = s
			}
		}
	}
	return furthest
}

// EvaluatePromotion evaluates season promotion decisions for a series.
//
// Target: every held season H with H <= F + cfg.LookAhead gets a promote decision.
// LookAhead 0 -> nothing promoted.
// Promotion only moves forward; never re-adds an unmonitor.
func EvaluatePromotion(
	seriesID int,
	held []int32,
	watches []SeasonWatch,
	cfg GateConfig,
) []GateDecision {
	if cfg.LookAhead <= 0 || len(held) == 0 || len(watches) == 0 {
		return nil
	}

	whose := cfg.PromoteWhose
	if whose != "all" {
		whose = "any"
	}

	allUsers, userSeasons := groupWatchesByUserAndSeason(watches)
	f := findFurthestWatchedSeason(allUsers, userSeasons, whose)
	if f == 0 {
		return nil
	}

	target := f + cfg.LookAhead
	reason := fmt.Sprintf("season %d watched (%s), look-ahead %d", f, whose, cfg.LookAhead)
	wouldAct := cfg.Mode == GateModeAct

	sortedHeld := slices.Clone(held)
	slices.Sort(sortedHeld)

	var decisions []GateDecision
	for _, h := range sortedHeld {
		if int(h) <= target {
			decisions = append(decisions, GateDecision{
				SeriesID: seriesID,
				Season:   int(h),
				Action:   GateActionPromote,
				WouldAct: wouldAct,
				Reason:   reason,
			})
		}
	}
	return decisions
}
