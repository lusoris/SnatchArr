// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import "syscall"

// peakRSS returns the peak resident set size of this process in bytes (Linux: ru_maxrss in KiB).
func peakRSS() (int64, bool) {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0, false
	}
	return widen(ru.Maxrss) << 10, true
}

// widen converts Maxrss, which is int32 on 386, arm and mips and int64
// elsewhere, before the KiB shift so the shift cannot overflow.
func widen[T int32 | int64](v T) int64 { return int64(v) }
