// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

//go:build !linux && !darwin

package httpapi

// peakRSS reports no value where getrusage has no max RSS field (Windows and others); the
// bound check is skipped there with a stated reason.
func peakRSS() (int64, bool) { return 0, false }
