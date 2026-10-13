// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package workergrpc

import (
	"log/slog"
	"slices"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/events"
	snatcharrv1 "github.com/lusoris/SnatchArr/api/internal/gen/snatcharr/v1"
	"github.com/lusoris/SnatchArr/api/internal/metrics/metricstest"
	"github.com/lusoris/SnatchArr/api/internal/policies"
	"github.com/lusoris/SnatchArr/api/internal/settings"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

// budgetServer is a Server wired for AcquireBudget against a fresh database, with one leased
// run on an instance whose policy is the default (hourly cap 20, no global cap).
func budgetServer(t *testing.T) (*Server, uuid.UUID, string) {
	t.Helper()
	srv, _, instanceID, runID := budgetServerOn(t)
	return srv, instanceID, runID
}

// budgetServerOn is budgetServer that also returns its store.
func budgetServerOn(t *testing.T) (*Server, *store.Store, uuid.UUID, string) {
	t.Helper()
	st := storetest.New(t)
	clk := clock.NewFake()
	logger := slog.New(slog.DiscardHandler)
	runs := snatch.NewRuns(st, clk, id.New())
	rec := snatch.NewRecorder(st, clk, events.New(logger))
	m, _ := metricstest.New(t)
	srv := &Server{
		runs: runs, budget: snatch.NewBudget(st, clk), planner: snatch.NewPlanner(st, clk, runs, rec, logger, m),
		policies: policies.New(st, clk), settings: settings.New(st, clk), clk: clk, logger: logger,
	}
	instanceID := storetest.Instance(t, st, clk.Now())
	run, _, err := runs.Enqueue(t.Context(), instanceID, domain.SnatchMissing)
	if err != nil {
		t.Fatalf("Enqueue() = %v", err)
	}
	if _, err = runs.Lease(t.Context(), "worker"); err != nil {
		t.Fatalf("Lease() = %v", err)
	}
	return srv, st, instanceID, run.ID.String()
}

func whole(cost uint32) *snatcharrv1.BudgetItem {
	return &snatcharrv1.BudgetItem{Cost: cost, Atomic: true}
}

func part(cost uint32) *snatcharrv1.BudgetItem {
	return &snatcharrv1.BudgetItem{Cost: cost}
}

// AcquireBudget answers one grant per item, in request order, and debits their sum (#142).
func TestAcquireBudgetGrantsPerItem(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		requested uint32
		items     []*snatcharrv1.BudgetItem
		want      []uint32
		granted   uint32
	}{
		{"positive all fit", 8, []*snatcharrv1.BudgetItem{whole(3), part(5)}, []uint32{3, 5}, 8},
		{"negative atomic over the cap waits, later items are served", 36, []*snatcharrv1.BudgetItem{whole(21), part(12), whole(3)}, []uint32{0, 12, 3}, 15},
		{"positive divisible takes what is left", 29, []*snatcharrv1.BudgetItem{whole(4), part(25)}, []uint32{4, 16}, 20},
		{"boundary atomic exactly the cap", 21, []*snatcharrv1.BudgetItem{whole(20), part(1)}, []uint32{20, 0}, 20},
		{"boundary no items: the plain total, no per-item grants", 25, nil, nil, 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, instanceID, runID := budgetServer(t)
			resp, err := srv.AcquireBudget(t.Context(), &snatcharrv1.AcquireBudgetRequest{RunId: runID, Requested: tc.requested, Items: tc.items})
			if err != nil {
				t.Fatalf("AcquireBudget() = %v", err)
			}
			if !slices.Equal(resp.GetGrantedItems(), tc.want) || resp.GetGranted() != tc.granted || resp.GetRemainingInWindow() != 20-tc.granted {
				t.Fatalf("AcquireBudget() = %v; want items %v, granted %d", resp, tc.want, tc.granted)
			}
			if used, _, usedErr := srv.budget.Used(t.Context(), instanceID); usedErr != nil || used != int(tc.granted) {
				t.Fatalf("Used() = %d, %v; want %d", used, usedErr, tc.granted)
			}
		})
	}
}

// Boundary: maxBudgetItems items are served, one more is refused before anything is debited.
func TestAcquireBudgetBoundsItems(t *testing.T) {
	t.Parallel()
	srv, instanceID, runID := budgetServer(t)
	items := make([]*snatcharrv1.BudgetItem, maxBudgetItems+1)
	for i := range items {
		items[i] = part(0)
	}
	_, err := srv.AcquireBudget(t.Context(), &snatcharrv1.AcquireBudgetRequest{RunId: runID, Items: items})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("AcquireBudget(%d items) = %v; want InvalidArgument", len(items), err)
	}
	resp, err := srv.AcquireBudget(t.Context(), &snatcharrv1.AcquireBudgetRequest{RunId: runID, Items: items[:maxBudgetItems]})
	if err != nil || len(resp.GetGrantedItems()) != maxBudgetItems {
		t.Fatalf("AcquireBudget(%d items) = %d grants, %v", maxBudgetItems, len(resp.GetGrantedItems()), err)
	}
	if used, _, usedErr := srv.budget.Used(t.Context(), instanceID); usedErr != nil || used != 0 {
		t.Fatalf("Used() = %d, %v; want 0", used, usedErr)
	}
}

// Negative: a schedule the planner cannot read denies the budget instead of granting the
// policy cap, and debits nothing (gap audit A1-06).
func TestAcquireBudgetFailsClosedOnAnUnreadableSchedule(t *testing.T) {
	t.Parallel()
	srv, st, instanceID, runID := budgetServerOn(t)
	_, err := st.Pool().Exec(t.Context(), `INSERT INTO schedules
		(id, instance_id, name, days_mask, start_time, end_time, tz, action, cap_value, enabled, created_at, updated_at)
		VALUES ($1, $2, 'cap', 127, '00:00', '24:00', 'Mars/Olympus_Mons', 'cap_override', 5, TRUE, now(), now())`,
		uuid.New(), instanceID)
	if err != nil {
		t.Fatalf("insert schedule: %v", err)
	}
	resp, err := srv.AcquireBudget(t.Context(), &snatcharrv1.AcquireBudgetRequest{RunId: runID, Requested: 5})
	if status.Code(err) != codes.Internal || resp.GetGranted() != 0 {
		t.Fatalf("AcquireBudget() = %v, %v; want Internal and nothing granted", resp, err)
	}
	if used, _, usedErr := srv.budget.Used(t.Context(), instanceID); usedErr != nil || used != 0 {
		t.Fatalf("Used() = %d, %v; want 0", used, usedErr)
	}
}
