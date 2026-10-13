// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package snatch

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// Workers records when each worker last reached the control plane, so the system status
// shows whether the data plane is alive (gap audit A1-01).
type Workers struct {
	st  *store.Store
	clk clock.Clock
}

// NewWorkers wires the worker contact service.
func NewWorkers(st *store.Store, clk clock.Clock) *Workers {
	return &Workers{st: st, clk: clk}
}

// Seen records a call from workerID now. A contact older than the stored one never
// replaces it.
func (w *Workers) Seen(ctx context.Context, workerID string) error {
	err := w.st.Q().TouchWorker(ctx, sqlcgen.TouchWorkerParams{ID: workerID, At: w.clk.Now()})
	if err != nil {
		return fmt.Errorf("snatch: record contact of worker %q: %w", workerID, store.MapError(err))
	}
	return nil
}

// LastContact is the newest contact over all workers; ok is false when none is recorded.
func (w *Workers) LastContact(ctx context.Context) (at time.Time, ok bool, err error) {
	at, err = w.st.Q().LastWorkerContact(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("snatch: last worker contact: %w", store.MapError(err))
	}
	return at, true, nil
}

// Purge deletes up to limit workers silent since before, for the retention janitor.
func (w *Workers) Purge(ctx context.Context, before time.Time, limit int32) (int64, error) {
	n, err := w.st.Q().PurgeWorkersBefore(ctx, sqlcgen.PurgeWorkersBeforeParams{Before: before, Batch: limit})
	if err != nil {
		return 0, fmt.Errorf("snatch: purge workers: %w", store.MapError(err))
	}
	return n, nil
}
