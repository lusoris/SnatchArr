// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package watchaware

// WatchedThreshold is the minimum played percentage (90.0%) for an item to be
// considered watched when Played is not explicitly true.
const WatchedThreshold = 90.0

// Watched reports whether w counts as watched: either Played is true or
// PlayedPercentage is at or above WatchedThreshold.
func Watched(w Watch) bool {
	return w.Played || (w.PlayedPercentage != nil && *w.PlayedPercentage >= WatchedThreshold)
}
