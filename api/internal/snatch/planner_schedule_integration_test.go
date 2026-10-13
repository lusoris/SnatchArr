// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package snatch

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/lusoris/SnatchArr/api/internal/metrics/metricstest"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

const (
	// badZone is a time zone no tzdata holds. The API refuses it on write; a row an older
	// release or a removed zone left behind still reaches the planner.
	badZone = "Mars/Olympus_Mons"
	// planFailures is the planner's per-instance failure counter.
	planFailures = "snatcharr_planner_instance_failures_total"
	// schedulePrefix is the context decision adds to a schedule it cannot read.
	schedulePrefix = "schedule "
)

// scheduleRow describes one schedule a test inserts; it covers every minute of every day.
type scheduleRow struct {
	instance *uuid.UUID // nil = global
	tz       string
	action   ScheduleAction
	capValue int32 // 0 = none
	disabled bool
}

func (r scheduleRow) insert(t *testing.T, f plannerFixture) {
	t.Helper()
	var capValue *int32
	if r.capValue > 0 {
		capValue = &r.capValue
	}
	_, err := f.st.Q().CreateSchedule(t.Context(), sqlcgen.CreateScheduleParams{
		ID: uuid.New(), InstanceID: r.instance, Name: "test window", DaysMask: 0b1111111, Tz: r.tz,
		Action: string(r.action), CapValue: capValue, Enabled: !r.disabled,
		StartTime: pgtype.Time{Valid: true},
		EndTime:   pgtype.Time{Microseconds: (24 * time.Hour).Microseconds(), Valid: true},
		CreatedAt: f.clk.Now(),
	})
	if err != nil {
		t.Fatalf("create schedule: %v", err)
	}
}

// instanceName is the name storetest.Instance gives the instance with id.
func instanceName(id uuid.UUID) string { return "storetest-" + id.String() }

type unreadableCase struct {
	name string
	// schedule builds the row from the two instances' ids; nil inserts none.
	schedule   func(a, b uuid.UUID) *scheduleRow
	wantQueued int
	// wantFailed is how often each instance counts as skipped.
	wantFailedA, wantFailedB float64
}

func (tc unreadableCase) run(t *testing.T) {
	t.Helper()
	f := newPlannerFixture(t, AllowAll{})
	a := storetest.Instance(t, f.st, f.clk.Now())
	b := storetest.Instance(t, f.st, f.clk.Now())
	if tc.schedule != nil {
		tc.schedule(a, b).insert(t, f)
	}
	queued, err := f.planner.Tick(t.Context())
	if err != nil {
		t.Fatalf("Tick() = %v, want nil: a skipped instance does not fail the tick", err)
	}
	if queued != tc.wantQueued {
		t.Errorf("Tick() = %d, want %d", queued, tc.wantQueued)
	}
	assertQueued(t, f.st, tc.wantQueued)
	for id, want := range map[uuid.UUID]float64{a: tc.wantFailedA, b: tc.wantFailedB} {
		got, ok := metricstest.Value(t, f.reg, planFailures, "instance", instanceName(id))
		if got != want || (want == 0 && ok) {
			t.Errorf("%s{%s} = %v (present %t), want %v", planFailures, instanceName(id), got, ok, want)
		}
	}
	if skipped := strings.Count(f.logged.String(), "snatch: planner skipped instance"); float64(skipped) != tc.wantFailedA+tc.wantFailedB {
		t.Errorf("%d instances logged as skipped, want %v:\n%s", skipped, tc.wantFailedA+tc.wantFailedB, firstLines(f.logged.String()))
	}
}

// A schedule the planner cannot read fails closed: its instance is skipped and counted
// instead of being planned as if the window did not exist (gap audit A1-05, A1-06).
func TestPlanner_UnreadableSchedule(t *testing.T) {
	t.Parallel()
	cases := []unreadableCase{
		// Positive: a readable pause in a named zone pauses both, and nothing counts as failed.
		{name: "a readable pause in a named zone", wantQueued: 0, schedule: func(_, _ uuid.UUID) *scheduleRow {
			return &scheduleRow{tz: "Europe/Berlin", action: ActionPause}
		}},
		// Negative.
		{name: "an instance's own schedule is unreadable", wantQueued: 1, wantFailedA: 1, schedule: func(a, _ uuid.UUID) *scheduleRow {
			return &scheduleRow{instance: &a, tz: badZone, action: ActionPause}
		}},
		{name: "a global schedule is unreadable", wantQueued: 0, wantFailedA: 1, wantFailedB: 1, schedule: func(_, _ uuid.UUID) *scheduleRow {
			return &scheduleRow{tz: badZone, action: ActionCapOverride, capValue: 5}
		}},
		// Boundary: a disabled unreadable schedule is never read, and no schedule plans both.
		{name: "a disabled schedule is unreadable", wantQueued: 2, schedule: func(_, _ uuid.UUID) *scheduleRow {
			return &scheduleRow{tz: badZone, action: ActionPause, disabled: true}
		}},
		{name: "no schedule", wantQueued: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

func TestPlanner_EffectiveCap(t *testing.T) {
	t.Parallel()
	const policyCap = 10
	cases := []struct {
		name    string
		rows    []scheduleRow
		want    int
		wantErr bool
	}{
		// Positive.
		{name: "no schedule keeps the policy cap", want: policyCap},
		{name: "an override below the policy cap applies", rows: []scheduleRow{{tz: "UTC", action: ActionCapOverride, capValue: 4}}, want: 4},
		{name: "the lowest of two overrides applies", rows: []scheduleRow{
			{tz: "UTC", action: ActionCapOverride, capValue: 7}, {tz: "Europe/Berlin", action: ActionCapOverride, capValue: 3},
		}, want: 3},
		// Negative: an unreadable schedule grants nothing, even next to a readable one.
		{name: "an unreadable schedule", rows: []scheduleRow{{tz: badZone, action: ActionCapOverride, capValue: 4}}, wantErr: true},
		{name: "an unreadable next to a readable schedule", rows: []scheduleRow{
			{tz: "UTC", action: ActionCapOverride, capValue: 4}, {tz: badZone, action: ActionPause},
		}, wantErr: true},
		// Boundary: an override never raises the cap, and one equal to it changes nothing.
		{name: "an override above the policy cap", rows: []scheduleRow{{tz: "UTC", action: ActionCapOverride, capValue: 20}}, want: policyCap},
		{name: "an override equal to the policy cap", rows: []scheduleRow{{tz: "UTC", action: ActionCapOverride, capValue: policyCap}}, want: policyCap},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newPlannerFixture(t, AllowAll{})
			inst := storetest.Instance(t, f.st, f.clk.Now())
			for _, r := range tc.rows {
				r.instance = &inst
				r.insert(t, f)
			}
			got, err := f.planner.EffectiveCap(t.Context(), inst, policyCap)
			if tc.wantErr {
				if err == nil || !strings.HasPrefix(err.Error(), schedulePrefix) || got != 0 {
					t.Fatalf("EffectiveCap() = %d, %v, want 0 and an error with the prefix %q", got, err, schedulePrefix)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("EffectiveCap() = %d, %v, want %d, <nil>", got, err, tc.want)
			}
		})
	}
}
