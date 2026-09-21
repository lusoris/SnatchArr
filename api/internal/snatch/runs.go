// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package snatch

import (
	"context"
	"errors"
	"fmt"
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

// ErrNoRun means no run is queued right now.
var ErrNoRun = errors.New("snatch: no run available")

// Runs owns the snatch_runs lifecycle.
type Runs struct {
	st  *store.Store
	clk clock.Clock
	ids id.Generator
}

// NewRuns wires the run service.
func NewRuns(st *store.Store, clk clock.Clock, ids id.Generator) *Runs {
	return &Runs{st: st, clk: clk, ids: ids}
}

// Enqueue creates a queued run unless one is already queued or leased for the pair.
func (r *Runs) Enqueue(ctx context.Context, instanceID uuid.UUID, kind domain.SnatchKind) (domain.Run, bool, error) {
	active, err := r.st.Q().HasActiveRun(ctx, sqlcgen.HasActiveRunParams{InstanceID: instanceID, Kind: string(kind)})
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
	row, err := r.st.Q().EnqueueRun(ctx, sqlcgen.EnqueueRunParams{ID: runID, InstanceID: instanceID, Kind: string(kind), QueuedAt: r.clk.Now()})
	if err != nil {
		return domain.Run{}, false, fmt.Errorf("snatch: enqueue: %w", store.MapError(err))
	}
	return runFromRow(row), true, nil
}

// Lease hands the oldest queued (or expired-lease) run to workerID.
func (r *Runs) Lease(ctx context.Context, workerID string) (domain.Run, error) {
	now := r.clk.Now()
	row, err := r.st.Q().LeaseRun(ctx, sqlcgen.LeaseRunParams{LeasedBy: &workerID, LeaseExpiresAt: new(now.Add(LeaseTTL)), StartedAt: new(now)})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Run{}, ErrNoRun
	}
	if err != nil {
		return domain.Run{}, fmt.Errorf("snatch: lease: %w", store.MapError(err))
	}
	return runFromRow(row), nil
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

// Complete closes a leased run.
func (r *Runs) Complete(ctx context.Context, runID uuid.UUID, workerID string, status domain.RunStatus, searched int, errMsg string) (domain.Run, error) {
	if status != domain.RunDone && status != domain.RunFailed && status != domain.RunCancelled {
		return domain.Run{}, fmt.Errorf("%w: run status %q is not terminal", domain.ErrInvalid, status)
	}
	var errPtr *string
	if errMsg != "" {
		errPtr = &errMsg
	}
	row, err := r.st.Q().CompleteRun(ctx, sqlcgen.CompleteRunParams{
		ID: runID, LeasedBy: &workerID, Status: string(status), FinishedAt: new(r.clk.Now()),
		SearchedCount: int32(min(searched, 1<<30)), // #nosec G115 -- clamped
		Error:         errPtr,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Run{}, fmt.Errorf("%w: run %s is not leased by %s", domain.ErrConflict, runID, workerID)
	}
	if err != nil {
		return domain.Run{}, fmt.Errorf("snatch: complete: %w", store.MapError(err))
	}
	return runFromRow(row), nil
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

// TrailingFailures counts how many of the most recent finished runs of a kind failed in a
// row; the planner and the worker use it to back off a broken instance or indexer.
func (r *Runs) TrailingFailures(ctx context.Context, instanceID uuid.UUID, kind domain.SnatchKind) (int, error) {
	statuses, err := r.st.Q().RecentRunStatuses(ctx, sqlcgen.RecentRunStatusesParams{InstanceID: instanceID, Kind: string(kind)})
	if err != nil {
		return 0, fmt.Errorf("snatch: recent runs: %w", store.MapError(err))
	}
	n := 0
	for _, st := range statuses {
		if st != string(domain.RunFailed) {
			break
		}
		n++
	}
	return n, nil
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
	}
}
