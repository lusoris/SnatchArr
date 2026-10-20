// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package workergrpc

import (
	"bytes"
	"log/slog"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/events"
	snatcharrv1 "github.com/lusoris/SnatchArr/api/internal/gen/snatcharr/v1"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/metrics/metricstest"
	"github.com/lusoris/SnatchArr/api/internal/policies"
	"github.com/lusoris/SnatchArr/api/internal/seerr"
	"github.com/lusoris/SnatchArr/api/internal/settings"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

type previewFixture struct {
	srv        *Server
	st         *store.Store
	clk        *clockwork.FakeClock
	instanceID uuid.UUID
}

func newPreviewFixture(t *testing.T) previewFixture {
	t.Helper()
	st := storetest.New(t)
	clk := clock.NewFake()
	logger := slog.New(slog.DiscardHandler)
	runs := snatch.NewRuns(st, clk, id.New())
	rec := snatch.NewRecorder(st, clk, events.New(logger))
	enc, encErr := crypto.NewEncryptor(bytes.Repeat([]byte{0x42}, 32))
	if encErr != nil {
		t.Fatalf("encryptor: %v", encErr)
	}
	inst := instances.New(st, enc, clk, id.New(), nil, logger)
	m, _ := metricstest.New(t)
	pols := policies.New(st, clk)
	srv := &Server{
		runs: runs, workers: snatch.NewWorkers(st, clk), memory: snatch.NewMemory(st, clk), rec: rec,
		instances: inst, policies: pols, seerr: seerr.New(st, enc, clk, id.New(), nil, nil, inst, runs, rec, logger),
		budget: snatch.NewBudget(st, clk), settings: settings.New(st, clk),
		planner: snatch.NewPlanner(st, clk, runs, rec, logger, m),
		clk:     clk, logger: logger, m: m, stopping: make(chan struct{}),
	}
	_ = withMetrics(t, srv)
	instanceID := storetest.Instance(t, st, clk.Now())
	if _, err := pols.Get(t.Context(), instanceID); err != nil {
		t.Fatalf("ensure default policy: %v", err)
	}
	return previewFixture{srv: srv, st: st, clk: clk, instanceID: instanceID}
}

func leaseRun(t *testing.T, f previewFixture, dryRun bool, seed int64) string {
	t.Helper()
	run, _, err := f.srv.runs.EnqueueWithOptions(t.Context(), snatch.EnqueueOptions{
		InstanceID: f.instanceID,
		Kind:       domain.SnatchMissing,
		DryRun:     dryRun,
		Seed:       seed,
	})
	if err != nil {
		t.Fatalf("EnqueueWithOptions(dryRun=%v): %v", dryRun, err)
	}
	if _, err = f.srv.runs.Lease(t.Context(), "worker"); err != nil {
		t.Fatalf("Lease(): %v", err)
	}
	return run.ID.String()
}

// TestAcquireBudgetDryRunQuotesWithoutDebit tests that a dry run gets the expected quote
// without mutating bucket rows, so a subsequent real run still receives full budget.
func TestAcquireBudgetDryRunQuotesWithoutDebit(t *testing.T) {
	t.Parallel()
	f := newPreviewFixture(t)
	ctx := t.Context()

	dryRunID := leaseRun(t, f, true, 42)
	dryResp, err := f.srv.AcquireBudget(ctx, &snatcharrv1.AcquireBudgetRequest{
		RunId:     dryRunID,
		Requested: 8,
		Items:     []*snatcharrv1.BudgetItem{whole(3), part(5)},
	})
	if err != nil {
		t.Fatalf("dry run AcquireBudget() = %v", err)
	}
	if !slices.Equal(dryResp.GetGrantedItems(), []uint32{3, 5}) || dryResp.GetGranted() != 8 {
		t.Fatalf("dry run grant mismatch: %+v", dryResp)
	}
	used, _, err := f.srv.budget.Used(ctx, f.instanceID)
	if err != nil || used != 0 {
		t.Fatalf("budget Used() after dry run = %d, %v; want 0", used, err)
	}

	completeRunDone(t, f, dryRunID)

	realRunID := leaseRun(t, f, false, 0)
	realResp, err := f.srv.AcquireBudget(ctx, &snatcharrv1.AcquireBudgetRequest{
		RunId:     realRunID,
		Requested: 15,
	})
	if err != nil || realResp.GetGranted() != 15 {
		t.Fatalf("real run AcquireBudget() = %v, granted=%d; want 15", err, realResp.GetGranted())
	}
	usedAfterReal, _, _ := f.srv.budget.Used(ctx, f.instanceID)
	if usedAfterReal != 15 {
		t.Fatalf("budget Used() after real run = %d, want 15", usedAfterReal)
	}

	testDryRunAtCapBoundary(t, f, realRunID)
}

func completeRunDone(t *testing.T, f previewFixture, runID string) {
	t.Helper()
	_, err := f.srv.CompleteRun(t.Context(), &snatcharrv1.CompleteRunRequest{
		RunId:    runID,
		WorkerId: "worker",
		Outcome:  snatcharrv1.RunOutcome_RUN_OUTCOME_DONE,
	})
	if err != nil {
		t.Fatalf("CompleteRun(%s) = %v", runID, err)
	}
}

func testDryRunAtCapBoundary(t *testing.T, f previewFixture, previousRunID string) {
	t.Helper()
	completeRunDone(t, f, previousRunID)

	dryCapRunID := leaseRun(t, f, true, 99)
	dryCapResp, err := f.srv.AcquireBudget(t.Context(), &snatcharrv1.AcquireBudgetRequest{
		RunId:     dryCapRunID,
		Requested: 5,
	})
	if err != nil {
		t.Fatalf("dry run at cap AcquireBudget() = %v", err)
	}
	if dryCapResp.GetGranted() != 0 || dryCapResp.GetRemainingInWindow() != 0 {
		t.Fatalf("dry run at cap granted=%d, remaining=%d; want 0, 0",
			dryCapResp.GetGranted(), dryCapResp.GetRemainingInWindow())
	}
	used, _, _ := f.srv.budget.Used(t.Context(), f.instanceID)
	if used != 15 {
		t.Fatalf("budget Used() after dry run at cap = %d, want 15", used)
	}
}

// TestCompleteRunDryRunPreservesMemoryAndCursor tests that a completed dry run never
// marks processed afterglow memory and never advances policy cursors.
func TestCompleteRunDryRunPreservesMemoryAndCursor(t *testing.T) {
	t.Parallel()
	f := newPreviewFixture(t)
	ctx := t.Context()

	if err := f.srv.policies.SaveCursor(ctx, f.instanceID, domain.SnatchMissing, "c-orig"); err != nil {
		t.Fatalf("SaveCursor: %v", err)
	}

	dryRunID := leaseRun(t, f, true, 77)
	_, err := f.srv.CompleteRun(ctx, &snatcharrv1.CompleteRunRequest{
		RunId:    dryRunID,
		WorkerId: "worker",
		Outcome:  snatcharrv1.RunOutcome_RUN_OUTCOME_DONE,
		Cursor:   "c-preview-advanced",
		Searched: []*snatcharrv1.SearchedItem{
			{EntityType: "movie", EntityId: 404},
		},
	})
	if err != nil {
		t.Fatalf("CompleteRun(dry-run) = %v", err)
	}

	assertMemoryAndCursorUnchanged(t, f, "c-orig", 404)
	assertRealRunUpdatesMemoryAndCursor(t, f, "c-real-advanced", 404)
}

func assertMemoryAndCursorUnchanged(t *testing.T, f previewFixture, wantCursor string, entityID int64) {
	t.Helper()
	ctx := t.Context()
	row, err := f.st.Q().GetPolicy(ctx, f.instanceID)
	if err != nil {
		t.Fatalf("Get policy: %v", err)
	}
	if row.CursorMissing != wantCursor {
		t.Errorf("policy cursor = %q, want %q", row.CursorMissing, wantCursor)
	}

	unprocessed, err := f.srv.memory.FilterUnprocessed(ctx, f.instanceID, domain.SnatchMissing, "movie", []int64{entityID})
	if err != nil {
		t.Fatalf("FilterUnprocessed: %v", err)
	}
	if len(unprocessed) != 1 || unprocessed[0] != entityID {
		t.Errorf("FilterUnprocessed = %v, want [%d] (unprocessed)", unprocessed, entityID)
	}
}

func assertRealRunUpdatesMemoryAndCursor(t *testing.T, f previewFixture, newCursor string, entityID int64) {
	t.Helper()
	ctx := t.Context()
	realRunID := leaseRun(t, f, false, 0)
	_, err := f.srv.CompleteRun(ctx, &snatcharrv1.CompleteRunRequest{
		RunId:    realRunID,
		WorkerId: "worker",
		Outcome:  snatcharrv1.RunOutcome_RUN_OUTCOME_DONE,
		Cursor:   newCursor,
		Searched: []*snatcharrv1.SearchedItem{
			{EntityType: "movie", EntityId: entityID},
		},
	})
	if err != nil {
		t.Fatalf("CompleteRun(real) = %v", err)
	}

	row, _ := f.st.Q().GetPolicy(ctx, f.instanceID)
	if row.CursorMissing != newCursor {
		t.Errorf("real run did not advance cursor: got %q, want %q", row.CursorMissing, newCursor)
	}

	unprocessed, _ := f.srv.memory.FilterUnprocessed(ctx, f.instanceID, domain.SnatchMissing, "movie", []int64{entityID})
	if len(unprocessed) != 0 {
		t.Errorf("real run did not mark memory: unprocessed = %v, want empty", unprocessed)
	}
}
