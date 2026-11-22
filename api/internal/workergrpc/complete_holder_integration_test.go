// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package workergrpc

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	snatcharrv1 "github.com/lusoris/SnatchArr/api/internal/gen/snatcharr/v1"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
)

// TestCompleteRunHolderCheck sends CompleteRun with cursor "c-after" and one searched movie. A worker
// whose expired lease "worker-2" took, a request without a worker id, and a request without a worker
// id for a run cancelled before any lease get FailedPrecondition and leave the cursor and processed
// memory as they were; the current holder completes the run and saves both.
func TestCompleteRunHolderCheck(t *testing.T) {
	t.Parallel()
	refused := codes.FailedPrecondition
	cases := []struct {
		name      string
		cancelled bool
		worker    string
		want      codes.Code
	}{
		{"stale holder of a reassigned lease", false, "worker", refused},
		{"request without a worker id", false, "", refused},
		{"run cancelled before any lease without a worker id", true, "", refused},
		{"current holder of a reassigned lease", false, "worker-2", codes.OK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newPreviewFixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			runID := holderCheckRun(ctx, t, f, tc.cancelled)
			_, err := f.srv.CompleteRun(ctx, &snatcharrv1.CompleteRunRequest{
				RunId: runID, WorkerId: tc.worker, Outcome: snatcharrv1.RunOutcome_RUN_OUTCOME_DONE, Cursor: "c-after",
				Searched: []*snatcharrv1.SearchedItem{{EntityType: "movie", EntityId: 505}},
			})
			if got := status.Code(err); got != tc.want {
				t.Fatalf("CompleteRun(worker %q) code = %v (err %v), want %v", tc.worker, got, err, tc.want)
			}
			if tc.want == codes.OK {
				checkProgressSaved(ctx, t, f, "c-after", 505)
				return
			}
			assertMemoryAndCursorUnchanged(t, f, "c-before", 505)
		})
	}
}

// holderCheckRun saves cursor "c-before" and returns a run that "worker-2" holds after the lease of
// "worker" expired, or, when cancelled is set, a run cancelled while it was still queued.
func holderCheckRun(ctx context.Context, t *testing.T, f previewFixture, cancelled bool) string {
	t.Helper()
	if err := f.srv.policies.SaveCursor(ctx, f.instanceID, domain.SnatchMissing, "c-before"); err != nil {
		t.Fatalf("SaveCursor: %v", err)
	}
	run, _, err := f.srv.runs.EnqueueWithOptions(ctx, snatch.EnqueueOptions{InstanceID: f.instanceID, Kind: domain.SnatchMissing})
	if err != nil {
		t.Fatalf("EnqueueWithOptions: %v", err)
	}
	runID := run.ID.String()
	if cancelled {
		if err = f.srv.runs.Cancel(ctx, run.ID); err != nil {
			t.Fatalf("Cancel: %v", err)
		}
		return runID
	}
	if _, err = f.srv.runs.Lease(ctx, "worker"); err != nil {
		t.Fatalf("Lease(worker) = %v", err)
	}
	f.clk.Advance(snatch.LeaseTTL + time.Second)
	leased, err := f.srv.runs.Lease(ctx, "worker-2")
	if err != nil {
		t.Fatalf("Lease(worker-2) = %v", err)
	}
	if leased.ID.String() != runID {
		t.Fatalf("leased run = %s, want %s", leased.ID, runID)
	}
	return runID
}

// checkProgressSaved wants the policy cursor at wantCursor and entityID in processed memory.
func checkProgressSaved(ctx context.Context, t *testing.T, f previewFixture, wantCursor string, entityID int64) {
	t.Helper()
	row, err := f.st.Q().GetPolicy(ctx, f.instanceID)
	if err != nil {
		t.Fatalf("GetPolicy: %v", err)
	}
	if row.CursorMissing != wantCursor {
		t.Errorf("policy cursor = %q, want %q", row.CursorMissing, wantCursor)
	}
	unprocessed, err := f.srv.memory.FilterUnprocessed(ctx, f.instanceID, domain.SnatchMissing, "movie", []int64{entityID})
	if err != nil {
		t.Fatalf("FilterUnprocessed: %v", err)
	}
	if len(unprocessed) != 0 {
		t.Errorf("FilterUnprocessed = %v, want empty (processed)", unprocessed)
	}
}
