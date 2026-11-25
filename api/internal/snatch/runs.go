// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package snatch

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// LeaseTTL is how long a worker may go silent before its run is handed to another.
const LeaseTTL = 90 * time.Second

// completeTimeout bounds the completion transaction (HISS-02) so it releases the run row lock well inside LeaseTTL even if a progress write waits on a lock.
const completeTimeout = 10 * time.Second

// MaxLeases is how often one run is leased before an expired lease ends it as failed: a
// run that crashes every worker would otherwise be handed out forever (#17).
const MaxLeases = 3

// exhaustedBatch bounds the runs FailExhausted ends per call (HISS-02).
const exhaustedBatch = 100

// ExhaustedReason is the error a run gets when its last lease expired.
var ExhaustedReason = fmt.Sprintf("lease expired %d times without the run completing", MaxLeases)

// ErrNoRun means no run is queued right now.
var ErrNoRun = errors.New("snatch: no run available")

// Runs owns the snatch_runs lifecycle.
type Runs struct {
	st        *store.Store
	clk       clock.Clock
	ids       id.Generator
	txTimeout time.Duration
}

// NewRuns wires the run service.
func NewRuns(st *store.Store, clk clock.Clock, ids id.Generator) *Runs {
	return &Runs{st: st, clk: clk, ids: ids, txTimeout: completeTimeout}
}

// EnqueueOptions configures a newly queued run.
type EnqueueOptions struct {
	InstanceID    uuid.UUID
	Kind          domain.SnatchKind
	FocusEntityID *int64
	FocusGroupID  *int64
	DryRun        bool
	Seed          int64
}

// Enqueue creates a queued run unless one is already queued or leased for the pair.
func (r *Runs) Enqueue(ctx context.Context, instanceID uuid.UUID, kind domain.SnatchKind) (domain.Run, bool, error) {
	return r.EnqueueFocused(ctx, instanceID, kind, nil, nil)
}

// EnqueueFocused queues a run limited to one entity (movie) or group (series), for a
// quickie on a single Seerr request. Like Enqueue it refuses while a run is active.
func (r *Runs) EnqueueFocused(ctx context.Context, instanceID uuid.UUID, kind domain.SnatchKind, focusEntity, focusGroup *int64) (domain.Run, bool, error) {
	return r.EnqueueWithOptions(ctx, EnqueueOptions{
		InstanceID:    instanceID,
		Kind:          kind,
		FocusEntityID: focusEntity,
		FocusGroupID:  focusGroup,
	})
}

// EnqueueWithOptions queues a run with custom dry_run and seed settings.
func (r *Runs) EnqueueWithOptions(ctx context.Context, opts EnqueueOptions) (domain.Run, bool, error) {
	active, err := r.st.Q().HasActiveRun(ctx, sqlcgen.HasActiveRunParams{InstanceID: opts.InstanceID, Kind: string(opts.Kind)})
	if err != nil {
		return domain.Run{}, false, fmt.Errorf("snatch: active run check: %w", store.MapError(err))
	}
	if active {
		return domain.Run{}, false, nil
	}
	runID, err := r.ids.NewUUID()
	if err != nil {
		return domain.Run{}, false, fmt.Errorf("snatch: new run id: %w", err)
	}
	row, err := r.st.Q().EnqueueRun(ctx, sqlcgen.EnqueueRunParams{
		ID:            runID,
		InstanceID:    opts.InstanceID,
		Kind:          string(opts.Kind),
		QueuedAt:      r.clk.Now(),
		FocusEntityID: opts.FocusEntityID,
		FocusGroupID:  opts.FocusGroupID,
		DryRun:        opts.DryRun,
		Seed:          opts.Seed,
	})
	if err != nil {
		return domain.Run{}, false, fmt.Errorf("snatch: enqueue: %w", store.MapError(err))
	}
	return runFromRow(row), true, nil
}

// CheckAndRecordItemSearch records an item search for userID if outside the cooldownSeconds window.
// It executes check + write in one transaction.
// If outside the cooldown window, it returns (0, true, nil).
// If inside the cooldown window, it returns (retryAfterSeconds, false, nil).
func (r *Runs) CheckAndRecordItemSearch(ctx context.Context, userID uuid.UUID, cooldownSeconds int) (int, bool, error) {
	now := r.clk.Now()
	var (
		allowed    bool
		retryAfter int
	)
	err := r.st.Tx(ctx, func(q *sqlcgen.Queries) error {
		_, err := q.CheckAndRecordItemSearch(ctx, sqlcgen.CheckAndRecordItemSearchParams{
			UserID:          userID,
			Now:             now,
			CooldownSeconds: float64(cooldownSeconds),
		})
		if err == nil {
			allowed = true
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("snatch: check item search cooldown: %w", store.MapError(err))
		}
		lastSearchedAt, getErr := q.GetItemSearchCooldown(ctx, userID)
		if getErr != nil {
			return fmt.Errorf("snatch: get item search cooldown: %w", store.MapError(getErr))
		}
		elapsed := now.Sub(lastSearchedAt)
		remaining := time.Duration(cooldownSeconds)*time.Second - elapsed
		secs := max(int(math.Ceil(remaining.Seconds())), 1)
		retryAfter = secs
		allowed = false
		return nil
	})
	if err != nil {
		return 0, false, err
	}
	return retryAfter, allowed, nil
}

// ResetItemSearchCooldown removes the cooldown record for a user.
func (r *Runs) ResetItemSearchCooldown(ctx context.Context, userID uuid.UUID) error {
	if err := r.st.Q().ResetItemSearchCooldown(ctx, userID); err != nil {
		return fmt.Errorf("snatch: reset item search cooldown: %w", store.MapError(err))
	}
	return nil
}

// priorityMaxWait bounds how long a low-priority run waits before overtaking higher-priority runs.
const priorityMaxWait = time.Hour

// Lease hands the highest-priority (or starvation-promoted) queued or expired-lease run to workerID.
func (r *Runs) Lease(ctx context.Context, workerID string) (domain.Run, error) {
	now := r.clk.Now()
	row, err := r.st.Q().LeaseRun(ctx, sqlcgen.LeaseRunParams{
		LeasedBy:       &workerID,
		LeaseExpiresAt: new(now.Add(LeaseTTL)),
		Now:            now,
		MaxLeases:      MaxLeases,
		MaxWait:        priorityMaxWait.Seconds(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Run{}, ErrNoRun
	}
	if err != nil {
		return domain.Run{}, fmt.Errorf("snatch: lease: %w", store.MapError(err))
	}
	return runFromRow(row), nil
}

// FailExhausted ends, as failed, the runs whose lease expired after MaxLeases leases and
// returns them, at most exhaustedBatch per call.
func (r *Runs) FailExhausted(ctx context.Context) ([]domain.Run, error) {
	rows, err := r.st.Q().FailExhaustedLeases(ctx, sqlcgen.FailExhaustedLeasesParams{
		Now: r.clk.Now(), Error: ExhaustedReason, MaxLeases: MaxLeases, Batch: exhaustedBatch,
	})
	if err != nil {
		return nil, fmt.Errorf("snatch: fail exhausted leases: %w", store.MapError(err))
	}
	out := make([]domain.Run, 0, len(rows))
	for _, row := range rows {
		out = append(out, runFromRow(row))
	}
	return out, nil
}

// Purge deletes up to limit runs that finished (done, failed or cancelled) before `before`
// (one batch of the retention janitor). Their events stay, with run_id cleared.
func (r *Runs) Purge(ctx context.Context, before time.Time, limit int32) (int64, error) {
	n, err := r.st.Q().PurgeRunsBefore(ctx, sqlcgen.PurgeRunsBeforeParams{Before: before, Batch: limit})
	if err != nil {
		return 0, fmt.Errorf("snatch: purge runs: %w", store.MapError(err))
	}
	return n, nil
}

// Heartbeat extends the lease; returns the current status so a cancelled run is noticed.
func (r *Runs) Heartbeat(ctx context.Context, runID uuid.UUID, workerID string) (domain.Run, error) {
	row, err := r.st.Q().HeartbeatRun(ctx, sqlcgen.HeartbeatRunParams{ID: runID, LeasedBy: &workerID, LeaseExpiresAt: new(r.clk.Now().Add(LeaseTTL))})
	if errors.Is(err, pgx.ErrNoRows) {
		return r.Get(ctx, runID)
	}
	if err != nil {
		return domain.Run{}, fmt.Errorf("snatch: heartbeat: %w", store.MapError(err))
	}
	return runFromRow(row), nil
}

// Get loads a run.
func (r *Runs) Get(ctx context.Context, runID uuid.UUID) (domain.Run, error) {
	row, err := r.st.Q().GetRun(ctx, runID)
	if err != nil {
		return domain.Run{}, fmt.Errorf("snatch: get run: %w", store.MapError(err))
	}
	return runFromRow(row), nil
}

// Completion is one worker's request to close the run it leased.
type Completion struct {
	RunID    uuid.UUID
	WorkerID string
	Status   domain.RunStatus
	Searched int
	Error    string
	Eligible *uint32
}

// Complete closes a leased run.
func (r *Runs) Complete(ctx context.Context, runID uuid.UUID, workerID string, status domain.RunStatus, searched int, errMsg string, eligible *uint32) (domain.Run, error) {
	return r.CompleteWithProgress(ctx, Completion{
		RunID: runID, WorkerID: workerID, Status: status, Searched: searched, Error: errMsg, Eligible: eligible,
	}, nil)
}

// CompleteWithProgress closes a leased run, then runs persist with the queries of the same transaction.
// The closing UPDATE checks the holder and takes the run's row lock first, and LeaseRun skips locked
// rows, so the lease cannot pass to another worker while persist writes; an error from persist rolls
// the run back to leased. The callback must run every statement with the context it receives.
// A nil persist only closes the run.
func (r *Runs) CompleteWithProgress(ctx context.Context, c Completion, persist func(ctx context.Context, q *sqlcgen.Queries) error) (domain.Run, error) {
	params, err := r.completeParams(c)
	if err != nil {
		return domain.Run{}, err
	}
	var row sqlcgen.SnatchRun
	txCtx, cancel := context.WithTimeout(ctx, r.txTimeout)
	defer cancel()
	err = r.st.Tx(txCtx, func(q *sqlcgen.Queries) error {
		var txErr error
		row, txErr = q.CompleteRun(txCtx, params)
		if errors.Is(txErr, pgx.ErrNoRows) {
			return fmt.Errorf("%w: run %s is not leased by %s", domain.ErrConflict, c.RunID, c.WorkerID)
		}
		if txErr != nil {
			return store.MapError(txErr)
		}
		if persist == nil {
			return nil
		}
		return persist(txCtx, q)
	})
	if err != nil {
		if errors.Is(txCtx.Err(), context.DeadlineExceeded) && !errors.Is(err, context.DeadlineExceeded) {
			err = errors.Join(context.DeadlineExceeded, err)
		}
		return domain.Run{}, fmt.Errorf("snatch: complete: %w", err)
	}
	return runFromRow(row), nil
}

// completeParams checks that c ends the run in a terminal status and builds the closing UPDATE.
func (r *Runs) completeParams(c Completion) (sqlcgen.CompleteRunParams, error) {
	if c.Status != domain.RunDone && c.Status != domain.RunFailed && c.Status != domain.RunCancelled {
		return sqlcgen.CompleteRunParams{}, fmt.Errorf("%w: run status %q is not terminal", domain.ErrInvalid, c.Status)
	}
	var errPtr *string
	if c.Error != "" {
		errPtr = &c.Error
	}
	var eligibleItems *int32
	if c.Eligible != nil {
		// PostgreSQL INTEGER cannot represent the upper half of uint32.
		eligibleItems = new(int32(min(*c.Eligible, math.MaxInt32))) // #nosec G115 -- clamped to INTEGER range
	}
	return sqlcgen.CompleteRunParams{
		ID: c.RunID, LeasedBy: &c.WorkerID, Status: string(c.Status), FinishedAt: new(r.clk.Now()),
		SearchedCount: int32(min(c.Searched, 1<<30)), // #nosec G115 -- clamped
		Error:         errPtr,
		EligibleItems: eligibleItems,
	}, nil
}

// Cancel marks a queued or leased run cancelled; the worker learns via Heartbeat.
func (r *Runs) Cancel(ctx context.Context, runID uuid.UUID) error {
	n, err := r.st.Q().CancelRun(ctx, sqlcgen.CancelRunParams{ID: runID, FinishedAt: new(r.clk.Now())})
	if err != nil {
		return fmt.Errorf("snatch: cancel: %w", store.MapError(err))
	}
	if n == 0 {
		return fmt.Errorf("%w: run %s is not active", domain.ErrNotFound, runID)
	}
	return nil
}

// List pages runs, newest first, optionally for one instance.
func (r *Runs) List(ctx context.Context, instanceID *uuid.UUID, limit, offset int) ([]domain.Run, error) {
	rows, err := r.st.Q().ListRuns(ctx, sqlcgen.ListRunsParams{Limit: int32(min(max(limit, 1), 500)), Offset: int32(max(offset, 0)), InstanceID: instanceID}) // #nosec G115 -- clamped
	if err != nil {
		return nil, fmt.Errorf("snatch: list runs: %w", store.MapError(err))
	}
	out := make([]domain.Run, 0, len(rows))
	for _, row := range rows {
		out = append(out, runFromRow(row))
	}
	return out, nil
}

// CountTrailingFailures returns how many leading statuses are domain.RunFailed.
func CountTrailingFailures(statuses []string) int {
	n := 0
	for _, st := range statuses {
		if st != string(domain.RunFailed) {
			break
		}
		n++
	}
	return n
}

// TrailingFailures counts how many of the most recent finished runs of a kind failed in a
// row; the planner and the worker use it to back off a broken instance or indexer.
func (r *Runs) TrailingFailures(ctx context.Context, instanceID uuid.UUID, kind domain.SnatchKind) (int, error) {
	statuses, err := r.st.Q().RecentRunStatuses(ctx, sqlcgen.RecentRunStatusesParams{InstanceID: instanceID, Kind: string(kind)})
	if err != nil {
		return 0, fmt.Errorf("snatch: recent runs: %w", store.MapError(err))
	}
	return CountTrailingFailures(statuses), nil
}

// LastFinished returns when the last run of a kind finished, or zero when never.
func (r *Runs) LastFinished(ctx context.Context, instanceID uuid.UUID, kind domain.SnatchKind) (time.Time, error) {
	t, err := r.st.Q().LastFinishedAt(ctx, sqlcgen.LastFinishedAtParams{InstanceID: instanceID, Kind: string(kind)})
	if err != nil {
		return time.Time{}, fmt.Errorf("snatch: last finished: %w", store.MapError(err))
	}
	if t.Year() < 1971 {
		return time.Time{}, nil
	}
	return t, nil
}

func runFromRow(row sqlcgen.SnatchRun) domain.Run {
	var leasedBy, errMsg string
	if row.LeasedBy != nil {
		leasedBy = *row.LeasedBy
	}
	if row.Error != nil {
		errMsg = *row.Error
	}
	return domain.Run{
		ID: row.ID, InstanceID: row.InstanceID, Kind: domain.SnatchKind(row.Kind), Status: domain.RunStatus(row.Status),
		LeasedBy: leasedBy, LeaseExpiresAt: row.LeaseExpiresAt, QueuedAt: row.QueuedAt, StartedAt: row.StartedAt,
		FinishedAt: row.FinishedAt, SearchedCount: int(row.SearchedCount), Error: errMsg,
		FocusEntityID: row.FocusEntityID, FocusGroupID: row.FocusGroupID,
		DryRun: row.DryRun, Seed: row.Seed,
	}
}
