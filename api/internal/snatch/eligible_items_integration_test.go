// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package snatch

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/metrics/metricstest"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

const eligibleMetric = "snatcharr_instance_eligible_items"

type eligibleRun struct {
	kind     string
	items    *int32
	dryRun   bool
	finished bool
}

func insertEligibleRun(t *testing.T, f stateLoopFixture, instID uuid.UUID, r eligibleRun, at time.Time) {
	t.Helper()
	var finished *time.Time
	if r.finished {
		finished = &at
	}
	_, err := f.st.Pool().Exec(t.Context(), `
		INSERT INTO snatch_runs (id, instance_id, kind, status, queued_at, finished_at, dry_run, eligible_items)
		VALUES ($1, $2, $3, 'done', $4, $5, $6, $7)
	`, uuid.New(), instID, r.kind, at, finished, r.dryRun, r.items)
	if err != nil {
		t.Fatalf("insert eligible run: %v", err)
	}
}

func TestStateLoop_InstanceEligibleItems(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		runs []eligibleRun
		want *int32
	}{
		{"before any run", nil, nil},
		{"positive", []eligibleRun{{"missing", new(int32(7)), false, true}}, new(int32(7))},
		{"newest wins", []eligibleRun{{"missing", new(int32(7)), false, true}, {"missing", new(int32(3)), false, true}}, new(int32(3))},
		{"zero", []eligibleRun{{"missing", new(int32(7)), false, true}, {"missing", new(int32(0)), false, true}}, new(int32(0))},
		{"dry-run ignored", []eligibleRun{{"missing", new(int32(7)), false, true}, {"missing", new(int32(99)), true, true}}, new(int32(7))},
		{"NULL ignored", []eligibleRun{{"missing", new(int32(7)), false, true}, {"missing", nil, false, true}}, new(int32(7))},
		{"unfinished ignored", []eligibleRun{{"missing", new(int32(99)), false, false}}, nil},
		{"only dry-run", []eligibleRun{{"missing", new(int32(99)), true, true}}, nil},
		{"only NULL", []eligibleRun{{"missing", nil, false, true}}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newStateLoopFixture(t)
			instID := storetest.Instance(t, f.st, f.clk.Now())
			for i, r := range tc.runs {
				insertEligibleRun(t, f, instID, r, f.clk.Now().Add(time.Duration(i)*time.Minute))
			}
			assertEligibleTick(t, f, instID, "missing", tc.want)
		})
	}
}

func assertEligibleTick(t *testing.T, f stateLoopFixture, instID uuid.UUID, kind string, want *int32) {
	t.Helper()
	inst, err := f.st.Q().GetInstance(t.Context(), instID)
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}
	if err := f.loop.Tick(t.Context()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	labels := map[string]string{"arr_instance": inst.Name, "kind": kind}
	if want == nil {
		assertNoMetric(t, f.reg, eligibleMetric, labels)
		return
	}
	if got, ok := metricstest.ValueLabels(t, f.reg, eligibleMetric, labels); !ok || got != float64(*want) {
		t.Errorf("eligible items = %v (present %t), want %d", got, ok, *want)
	}
}

func TestStateLoop_InstanceEligibleItemsLifecycle(t *testing.T) {
	t.Parallel()
	f := newStateLoopFixture(t)
	instID := storetest.Instance(t, f.st, f.clk.Now())
	insertEligibleRun(t, f, instID, eligibleRun{"missing", new(int32(7)), false, true}, f.clk.Now())
	insertEligibleRun(t, f, instID, eligibleRun{"upgrade", new(int32(2)), false, true}, f.clk.Now())
	assertEligibleTick(t, f, instID, "missing", new(int32(7)))
	assertEligibleTick(t, f, instID, "upgrade", new(int32(2)))

	// A new registry and loop reconstruct both kinds solely from Postgres.
	f.m, f.reg = metricstest.New(t)
	loop := *f.loop
	loop.m = f.m
	f.loop = &loop
	assertEligibleTick(t, f, instID, "missing", new(int32(7)))
	assertEligibleTick(t, f, instID, "upgrade", new(int32(2)))
	setInstanceEnabled(t, f.st, instID, false, f.clk.Now())
	assertEligibleTick(t, f, instID, "missing", nil)
	assertEligibleTick(t, f, instID, "upgrade", nil)

	setInstanceEnabled(t, f.st, instID, true, f.clk.Now())
	assertEligibleTick(t, f, instID, "missing", new(int32(7)))
	inst, err := f.st.Q().GetInstance(t.Context(), instID)
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}
	if _, err := f.st.Q().DeleteInstanceUnlessDeclared(t.Context(), instID); err != nil {
		t.Fatalf("DeleteInstance: %v", err)
	}
	if err := f.loop.Tick(t.Context()); err != nil {
		t.Fatalf("Tick after delete: %v", err)
	}
	assertNoMetric(t, f.reg, eligibleMetric, map[string]string{"arr_instance": inst.Name, "kind": "missing"})
	assertNoMetric(t, f.reg, eligibleMetric, map[string]string{"arr_instance": inst.Name, "kind": "upgrade"})
}

func TestStateLoop_InstanceEligibleItemsStashExcluded(t *testing.T) {
	t.Parallel()
	f := newStateLoopFixture(t)
	instID := storetest.Instance(t, f.st, f.clk.Now())
	insertEligibleRun(t, f, instID, eligibleRun{"missing", new(int32(7)), false, true}, f.clk.Now())
	if _, err := f.st.Pool().Exec(t.Context(), "UPDATE instances SET kind = 'stash' WHERE id = $1", instID); err != nil {
		t.Fatalf("set stash kind: %v", err)
	}
	assertEligibleTick(t, f, instID, "missing", nil)
}
