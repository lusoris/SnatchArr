// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package snatch

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

// tickBudget is the share of one TickInterval a tick may take, so that a tick ends well
// before the next one starts (gap audit C12-02, issue #43).
const tickBudget = TickInterval / 2

// slowGate allows every snatch after a fixed delay, standing in for a planner that got
// slower per instance.
type slowGate struct{ delay time.Duration }

func (g slowGate) Allow(ctx context.Context, _ domain.Instance, _ domain.SnatchKind) (bool, GateRefusal, error) {
	select {
	case <-time.After(g.delay):
		return true, GateRefusal{}, nil
	case <-ctx.Done():
		return false, GateRefusal{}, fmt.Errorf("slow gate: %w", ctx.Err())
	}
}

// overBudget says why a tick that took elapsed broke budget, or "" when it kept to it.
func overBudget(elapsed, budget time.Duration) string {
	if elapsed > budget {
		return fmt.Sprintf("tick took %s, budget %s", elapsed, budget)
	}
	return ""
}

// timeTick seeds instances due instances, runs one tick and returns how long it took.
func timeTick(t *testing.T, gate Gate, instances int) time.Duration {
	t.Helper()
	f := newPlannerFixture(t, gate)
	for range instances {
		storetest.Instance(t, f.st, f.clk.Now())
	}
	start := time.Now()
	queued, err := f.planner.Tick(t.Context())
	elapsed := time.Since(start)
	if err != nil || queued != 2*instances {
		t.Fatalf("Tick() = %d, %v, want %d, <nil>", queued, err, 2*instances)
	}
	return elapsed
}

// Positive: a tick that queues one run per instance stays inside its budget up to the
// bound. The logged durations are the measurements docs/concepts.md quotes.
func TestPlanner_TickKeepsItsBudget(t *testing.T) {
	t.Parallel()
	for _, n := range []int{10, 100, MaxInstancesPerTick} {
		t.Run(fmt.Sprintf("%d instances", n), func(t *testing.T) {
			t.Parallel()
			elapsed := timeTick(t, AllowAll{}, n)
			t.Logf("tick over %d instances took %s", n, elapsed)
			if why := overBudget(elapsed, tickBudget); why != "" {
				t.Error(why)
			}
		})
	}
}

// Negative: a planner that spends a fixed delay per instance breaks a budget smaller than
// instances times that delay, so the check above can fail.
func TestPlanner_SlowTickBreaksItsBudget(t *testing.T) {
	t.Parallel()
	const (
		instances = 10
		delay     = 20 * time.Millisecond
	)
	elapsed := timeTick(t, slowGate{delay: delay}, instances)
	if overBudget(elapsed, instances*delay/2) == "" {
		t.Errorf("tick over %d instances at %s each took %s and kept a budget of %s", instances, delay, elapsed, instances*delay/2)
	}
}

// Boundary: a tick that takes exactly its budget keeps it; one nanosecond more breaks it.
func TestOverBudget_Boundary(t *testing.T) {
	t.Parallel()
	if why := overBudget(tickBudget, tickBudget); why != "" {
		t.Errorf("overBudget(budget, budget) = %q, want none", why)
	}
	if overBudget(tickBudget+time.Nanosecond, tickBudget) == "" {
		t.Error("overBudget(budget+1ns, budget) = none, want a reason")
	}
}
