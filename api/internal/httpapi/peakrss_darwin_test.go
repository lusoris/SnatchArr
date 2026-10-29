// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import "syscall"

// peakRSS returns the peak resident set size of this process in bytes (macOS: ru_maxrss in bytes).
func peakRSS() (int64, bool) {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0, false
	}
	return ru.Maxrss, true
}
