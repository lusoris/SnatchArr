// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package snatch

import (
	"strings"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/metrics"
	"github.com/lusoris/SnatchArr/api/internal/metrics/metricstest"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

// pastTheBound is how many instances more than one tick plans the test enables.
const pastTheBound = 5

// tickWant runs one tick and checks how many runs it queued.
func tickWant(t *testing.T, f plannerFixture, want int) {
	t.Helper()
	queued, err := f.planner.Tick(t.Context())
	if err != nil || queued != want {
		t.Fatalf("Tick() = %d, %v, want %d, <nil>", queued, err, want)
	}
}

// Boundary past the bound (#176): a tick plans MaxInstancesPerTick instances and says how
// many it left; the next tick plans those first, so every instance gets a run within two
// ticks. Before #176 the same instances were cut in every tick, without a word.
func TestPlanner_TickPastTheBoundPlansTheRestNext(t *testing.T) {
	t.Parallel()
	f := newPlannerFixture(t, AllowAll{})
	for range MaxInstancesPerTick + pastTheBound {
		storetest.Instance(t, f.st, f.clk.Now())
	}

	tickWant(t, f, 2*MaxInstancesPerTick)
	if got, _ := metricstest.Value(t, f.reg, "snatcharr_planner_deferrals_total", "reason", metrics.DeferralTickBound); got != pastTheBound {
		t.Errorf("tick_bound deferrals after the first tick = %v, want %d", got, pastTheBound)
	}
	if !strings.Contains(f.logged.String(), "deferred instances past the tick bound") ||
		!strings.Contains(f.logged.String(), "deferred=5") {
		t.Errorf("first tick logged no deferral line with deferred=5:\n%s", firstLines(f.logged.String()))
	}

	// The instances already queued have an active run, so the second tick queues only the
	// ones the first tick left.
	tickWant(t, f, 2*pastTheBound)
	planned := count(t, f.st, `SELECT count(DISTINCT instance_id) FROM snatch_runs WHERE status = 'queued'`)
	if planned != MaxInstancesPerTick+pastTheBound {
		t.Errorf("instances with a queued run after two ticks = %d, want %d", planned, MaxInstancesPerTick+pastTheBound)
	}
}
