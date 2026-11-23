// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package workergrpc

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	snatcharrv1 "github.com/lusoris/SnatchArr/api/internal/gen/snatcharr/v1"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
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

// TestCompleteWithProgressHoldsLease advances the clock past the lease of "worker" while persist runs
// inside CompleteWithProgress and tries to lease the run to "worker-2" from outside the transaction. The
// closing UPDATE holds the run's row lock, so that lease finds no run, the cursor commits with the run,
// and the run stays out of reach after the commit.
func TestCompleteWithProgressHoldsLease(t *testing.T) {
	t.Parallel()
	f := newPreviewFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	runID := leasedByWorker(ctx, t, f)
	var leaseErr error
	run, err := f.srv.runs.CompleteWithProgress(ctx, completionBy(runID, "worker"), func(q *sqlcgen.Queries) error {
		f.clk.Advance(snatch.LeaseTTL + time.Second)
		_, leaseErr = f.srv.runs.Lease(ctx, "worker-2")
		return f.srv.policies.SaveCursorIn(ctx, q, f.instanceID, domain.SnatchMissing, "c-after")
	})
	if err != nil {
		t.Fatalf("CompleteWithProgress() error = %v", err)
	}
	if !errors.Is(leaseErr, snatch.ErrNoRun) {
		t.Errorf("Lease(worker-2) during persist error = %v, want ErrNoRun", leaseErr)
	}
	if run.Status != domain.RunDone || run.LeasedBy != "worker" {
		t.Errorf("run = %s by %q, want done by worker", run.Status, run.LeasedBy)
	}
	if _, err = f.srv.runs.Lease(ctx, "worker-2"); !errors.Is(err, snatch.ErrNoRun) {
		t.Errorf("Lease(worker-2) after commit error = %v, want ErrNoRun", err)
	}
	assertCursor(ctx, t, f, "c-after")
}

// TestCompleteWithProgressRollsBack fails persist after it saved cursor "c-after". The cursor stays
// "c-before" and the run stays leased by "worker", which can then complete it.
func TestCompleteWithProgressRollsBack(t *testing.T) {
	t.Parallel()
	f := newPreviewFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	runID := leasedByWorker(ctx, t, f)
	planted := errors.New("planted persist failure")
	_, err := f.srv.runs.CompleteWithProgress(ctx, completionBy(runID, "worker"), func(q *sqlcgen.Queries) error {
		if saveErr := f.srv.policies.SaveCursorIn(ctx, q, f.instanceID, domain.SnatchMissing, "c-after"); saveErr != nil {
			return saveErr
		}
		return planted
	})
	if !errors.Is(err, planted) {
		t.Fatalf("CompleteWithProgress() error = %v, want the planted failure", err)
	}
	assertCursor(ctx, t, f, "c-before")
	assertLeasedBy(ctx, t, f, runID, "worker")
	if _, err = f.srv.runs.Complete(ctx, runID, "worker", domain.RunDone, 0, "", nil); err != nil {
		t.Errorf("Complete() after the rollback error = %v, want nil", err)
	}
}

// TestCompleteWithProgressRefusesNonHolder completes as "worker" after "worker-2" took the expired lease.
// The closing UPDATE finds no run that "worker" holds, so persist never runs and the cursor stays.
func TestCompleteWithProgressRefusesNonHolder(t *testing.T) {
	t.Parallel()
	f := newPreviewFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	runID, err := uuid.Parse(holderCheckRun(ctx, t, f, false))
	if err != nil {
		t.Fatalf("parse run id: %v", err)
	}
	persisted := false
	_, err = f.srv.runs.CompleteWithProgress(ctx, completionBy(runID, "worker"), func(q *sqlcgen.Queries) error {
		persisted = true
		return f.srv.policies.SaveCursorIn(ctx, q, f.instanceID, domain.SnatchMissing, "c-after")
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("CompleteWithProgress(worker) error = %v, want ErrConflict", err)
	}
	if persisted {
		t.Error("persist ran for a worker that no longer holds the lease")
	}
	assertCursor(ctx, t, f, "c-before")
}

// TestCompleteRunRollsBackOnFailedCursor sends CompleteRun from the holder with one searched movie and a
// cursor one byte over the 256-byte limit. Saving the cursor fails after the movie was marked in the same
// transaction, so the request gets InvalidArgument, the movie stays unprocessed, the cursor stays
// "c-before", the run stays leased by "worker" and the Seerr request for the movie stays unmarked.
func TestCompleteRunRollsBackOnFailedCursor(t *testing.T) {
	t.Parallel()
	f := newPreviewFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	seedSeerrRequest(ctx, t, f, 505)
	runID := leasedByWorker(ctx, t, f)
	_, err := f.srv.CompleteRun(ctx, &snatcharrv1.CompleteRunRequest{
		RunId: runID.String(), WorkerId: "worker", Outcome: snatcharrv1.RunOutcome_RUN_OUTCOME_DONE,
		Cursor: strings.Repeat("c", 257), Searched: []*snatcharrv1.SearchedItem{{EntityType: "movie", EntityId: 505}},
	})
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Fatalf("CompleteRun() code = %v (err %v), want InvalidArgument", got, err)
	}
	assertMemoryAndCursorUnchanged(t, f, "c-before", 505)
	assertLeasedBy(ctx, t, f, runID, "worker")
	if seerrSnatched(ctx, t, f, 505) {
		t.Error("Seerr request for movie 505 marked snatched although the run rolled back")
	}
}

// TestCompleteRunMarksSeerrAfterCommit completes a run that searched movie 606, which a Seerr request
// points at. Memory and cursor commit with the run, and the request is marked snatched afterwards.
func TestCompleteRunMarksSeerrAfterCommit(t *testing.T) {
	t.Parallel()
	f := newPreviewFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	seedSeerrRequest(ctx, t, f, 606)
	runID := leasedByWorker(ctx, t, f)
	_, err := f.srv.CompleteRun(ctx, &snatcharrv1.CompleteRunRequest{
		RunId: runID.String(), WorkerId: "worker", Outcome: snatcharrv1.RunOutcome_RUN_OUTCOME_DONE,
		Cursor: "c-after", Searched: []*snatcharrv1.SearchedItem{{EntityType: "movie", EntityId: 606}},
	})
	if err != nil {
		t.Fatalf("CompleteRun() error = %v", err)
	}
	checkProgressSaved(ctx, t, f, "c-after", 606)
	if !seerrSnatched(ctx, t, f, 606) {
		t.Error("Seerr request for movie 606 not marked snatched after the run committed")
	}
}

// seedSeerrRequest stores a Seerr movie request that points at entityID on the fixture instance.
func seedSeerrRequest(ctx context.Context, t *testing.T, f previewFixture, entityID int64) {
	t.Helper()
	linkID := uuid.New()
	now := f.clk.Now()
	_, err := f.st.Q().CreateSeerrLink(ctx, sqlcgen.CreateSeerrLinkParams{
		ID: linkID, Name: "seerr-" + linkID.String(), BaseUrl: "http://seerr.invalid", ApiKeyEnc: []byte("placeholder"),
		Enabled: true, CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateSeerrLink: %v", err)
	}
	err = f.st.Q().UpsertSeerrRequest(ctx, sqlcgen.UpsertSeerrRequestParams{
		LinkID: linkID, RequestID: 1, MediaType: "movie", TmdbID: 1, Title: "Seerr movie", RequestStatus: 1, MediaStatus: 1,
		Seasons: []int32{}, InstanceID: &f.instanceID, EntityID: &entityID, RequestedAt: &now, LastSeenAt: now,
	})
	if err != nil {
		t.Fatalf("UpsertSeerrRequest: %v", err)
	}
}

// seerrSnatched reports whether the Seerr request that points at entityID was marked snatched.
func seerrSnatched(ctx context.Context, t *testing.T, f previewFixture, entityID int64) bool {
	t.Helper()
	req, err := f.srv.seerr.FindByEntity(ctx, f.instanceID, entityID)
	if err != nil {
		t.Fatalf("FindByEntity(%d): %v", entityID, err)
	}
	return req.LastSnatchedAt != nil
}

// holderCheckRun saves cursor "c-before" and returns a run that "worker-2" holds after the lease of
// "worker" expired, or, when cancelled is set, a run cancelled while it was still queued.
func holderCheckRun(ctx context.Context, t *testing.T, f previewFixture, cancelled bool) string {
	t.Helper()
	if cancelled {
		runID := queuedRun(ctx, t, f)
		if err := f.srv.runs.Cancel(ctx, runID); err != nil {
			t.Fatalf("Cancel: %v", err)
		}
		return runID.String()
	}
	runID := leasedByWorker(ctx, t, f)
	f.clk.Advance(snatch.LeaseTTL + time.Second)
	leased, err := f.srv.runs.Lease(ctx, "worker-2")
	if err != nil {
		t.Fatalf("Lease(worker-2) = %v", err)
	}
	if leased.ID != runID {
		t.Fatalf("leased run = %s, want %s", leased.ID, runID)
	}
	return runID.String()
}

// queuedRun saves cursor "c-before" and enqueues one missing-kind run.
func queuedRun(ctx context.Context, t *testing.T, f previewFixture) uuid.UUID {
	t.Helper()
	if err := f.srv.policies.SaveCursor(ctx, f.instanceID, domain.SnatchMissing, "c-before"); err != nil {
		t.Fatalf("SaveCursor: %v", err)
	}
	run, _, err := f.srv.runs.EnqueueWithOptions(ctx, snatch.EnqueueOptions{InstanceID: f.instanceID, Kind: domain.SnatchMissing})
	if err != nil {
		t.Fatalf("EnqueueWithOptions: %v", err)
	}
	return run.ID
}

// leasedByWorker returns a queued run that "worker" then leased.
func leasedByWorker(ctx context.Context, t *testing.T, f previewFixture) uuid.UUID {
	t.Helper()
	runID := queuedRun(ctx, t, f)
	leased, err := f.srv.runs.Lease(ctx, "worker")
	if err != nil {
		t.Fatalf("Lease(worker) = %v", err)
	}
	if leased.ID != runID {
		t.Fatalf("leased run = %s, want %s", leased.ID, runID)
	}
	return runID
}

// completionBy is a done Completion of runID by workerID with one searched item.
func completionBy(runID uuid.UUID, workerID string) snatch.Completion {
	return snatch.Completion{RunID: runID, WorkerID: workerID, Status: domain.RunDone, Searched: 1}
}

// assertCursor wants the missing-kind policy cursor at want.
func assertCursor(ctx context.Context, t *testing.T, f previewFixture, want string) {
	t.Helper()
	row, err := f.st.Q().GetPolicy(ctx, f.instanceID)
	if err != nil {
		t.Fatalf("GetPolicy: %v", err)
	}
	if row.CursorMissing != want {
		t.Errorf("policy cursor = %q, want %q", row.CursorMissing, want)
	}
}

// assertLeasedBy wants runID still leased by workerID.
func assertLeasedBy(ctx context.Context, t *testing.T, f previewFixture, runID uuid.UUID, workerID string) {
	t.Helper()
	run, err := f.srv.runs.Get(ctx, runID)
	if err != nil {
		t.Fatalf("Get(%s): %v", runID, err)
	}
	if run.Status != domain.RunLeased || run.LeasedBy != workerID {
		t.Errorf("run = %s by %q, want leased by %s", run.Status, run.LeasedBy, workerID)
	}
}

// checkProgressSaved wants the policy cursor at wantCursor and entityID in processed memory.
func checkProgressSaved(ctx context.Context, t *testing.T, f previewFixture, wantCursor string, entityID int64) {
	t.Helper()
	assertCursor(ctx, t, f, wantCursor)
	unprocessed, err := f.srv.memory.FilterUnprocessed(ctx, f.instanceID, domain.SnatchMissing, "movie", []int64{entityID})
	if err != nil {
		t.Fatalf("FilterUnprocessed: %v", err)
	}
	if len(unprocessed) != 0 {
		t.Errorf("FilterUnprocessed = %v, want empty (processed)", unprocessed)
	}
}
