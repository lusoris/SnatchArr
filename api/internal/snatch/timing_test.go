// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package snatch_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
)

func TestJitterIsBoundedDeterministicAndSpread(t *testing.T) {
	t.Parallel()
	last := time.Unix(1_700_000_000, 0)
	interval := 15 * time.Minute
	limit := time.Duration(snatch.JitterRatio * float64(interval))
	seen := map[time.Duration]bool{}
	for range 50 {
		id := uuid.New()
		j := snatch.Jitter(id, domain.SnatchMissing, last, interval)
		if j < -limit || j > limit {
			t.Fatalf("jitter %v outside ±%v", j, limit)
		}
		if again := snatch.Jitter(id, domain.SnatchMissing, last, interval); again != j {
			t.Fatal("jitter must be deterministic for the same inputs")
		}
		seen[j] = true
	}
	if len(seen) < 10 {
		t.Fatalf("jitter barely varies across instances: %d distinct values", len(seen))
	}
	id := uuid.New()
	if snatch.Jitter(id, domain.SnatchMissing, last, interval) == snatch.Jitter(id, domain.SnatchUpgrade, last, interval) {
		t.Fatal("kinds should not share a jitter")
	}
}

func TestBackoff(t *testing.T) {
	t.Parallel()
	interval := 15 * time.Minute
	cases := map[int]time.Duration{0: 0, 1: 30 * time.Minute, 2: time.Hour, 3: 2 * time.Hour, 5: snatch.MaxBackoff, 40: snatch.MaxBackoff}
	for failures, want := range cases {
		if got := snatch.Backoff(interval, failures); got != want {
			t.Errorf("Backoff(%v, %d) = %v, want %v", interval, failures, got, want)
		}
	}
}

func TestDueAt(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	interval := 10 * time.Minute
	if !snatch.DueAt(id, domain.SnatchMissing, time.Time{}, interval, 0).IsZero() {
		t.Fatal("never run means due now")
	}
	last := time.Unix(1_700_000_000, 0)
	due := snatch.DueAt(id, domain.SnatchMissing, last, interval, 0)
	if d := due.Sub(last); d < 8*time.Minute || d > 12*time.Minute {
		t.Fatalf("due after %v, want interval ±20%%", d)
	}
	if backed := snatch.DueAt(id, domain.SnatchMissing, last, interval, 3); backed.Sub(last) != 80*time.Minute {
		t.Fatalf("three failures should wait 8x the interval, got %v", backed.Sub(last))
	}
}
