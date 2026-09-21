// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package snatch

import (
	"encoding/binary"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// JitterRatio is how far a cycle may drift either way from its interval.
const JitterRatio = 0.2

// MaxBackoff caps the wait after repeated failures.
const MaxBackoff = 6 * time.Hour

// maxBackoffDoublings bounds the exponent (HISS-02; 2^20 minutes is beyond MaxBackoff).
const maxBackoffDoublings = 20

// Jitter returns a deterministic offset in [-JitterRatio, +JitterRatio] of the interval
// for one instance, kind and previous run. Deterministic so every planner tick computes
// the same due time; different per instance so several instances never hit the same
// indexers in lockstep.
func Jitter(instanceID uuid.UUID, kind domain.SnatchKind, last time.Time, interval time.Duration) time.Duration {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(last.UnixNano())) // #nosec G115 -- bit pattern only
	h := fnv1a(instanceID[:], []byte(kind), buf[:])
	unit := float64(h%2001)/1000.0 - 1.0 // -1.0 .. +1.0
	return time.Duration(unit * JitterRatio * float64(interval))
}

// FNV-1a 64-bit constants.
const (
	fnvOffset64 = 14695981039346656037
	fnvPrime64  = 1099511628211
)

// fnv1a hashes the parts with FNV-1a; inlined because hash.Hash's Write returns an error
// that can never happen, which the unchecked-error gate would still flag.
func fnv1a(parts ...[]byte) uint64 {
	h := uint64(fnvOffset64)
	for _, part := range parts {
		for _, b := range part {
			h ^= uint64(b)
			h *= fnvPrime64
		}
	}
	return h
}

// Backoff is the wait after `failures` consecutive failed runs: interval doubled per
// failure, capped at MaxBackoff. Zero failures mean no extra wait.
func Backoff(interval time.Duration, failures int) time.Duration {
	if failures <= 0 {
		return 0
	}
	wait := interval
	for range min(failures, maxBackoffDoublings) {
		wait *= 2
		if wait >= MaxBackoff {
			return MaxBackoff
		}
	}
	return wait
}

// DueAt is when the next run of a kind may start: the interval plus jitter after the
// last run, or the failure backoff when that is later.
func DueAt(instanceID uuid.UUID, kind domain.SnatchKind, last time.Time, interval time.Duration, failures int) time.Time {
	if last.IsZero() {
		return time.Time{}
	}
	wait := interval + Jitter(instanceID, kind, last, interval)
	if b := Backoff(interval, failures); b > wait {
		wait = b
	}
	return last.Add(wait)
}
