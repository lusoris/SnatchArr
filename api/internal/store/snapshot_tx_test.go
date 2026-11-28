// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package store

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

type fakeSnapshotTx struct {
	mu               sync.Mutex
	commitErr        error
	rollbackErr      error
	closed           bool
	commits          int
	rollbacks        int
	commitDeadline   time.Time
	commitHasDead    bool
	rollbackDeadline time.Time
	rollbackHasDead  bool
	rollbackCtxErr   error
}

func (f *fakeSnapshotTx) Begin(_ context.Context) (pgx.Tx, error) { return nil, errors.New("unused") }

func (f *fakeSnapshotTx) Commit(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.commits++
	f.commitDeadline, f.commitHasDead = ctx.Deadline()
	if err := ctx.Err(); err != nil {
		return err
	}
	if f.commitErr != nil {
		return f.commitErr
	}
	if f.closed {
		return pgx.ErrTxClosed
	}
	f.closed = true
	return nil
}

func (f *fakeSnapshotTx) Rollback(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rollbacks++
	f.rollbackDeadline, f.rollbackHasDead = ctx.Deadline()
	f.rollbackCtxErr = ctx.Err()
	if f.rollbackErr != nil {
		return f.rollbackErr
	}
	if f.closed {
		return pgx.ErrTxClosed
	}
	f.closed = true
	return nil
}

func (f *fakeSnapshotTx) CopyFrom(_ context.Context, _ pgx.Identifier, _ []string, _ pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("unused")
}

func (f *fakeSnapshotTx) SendBatch(_ context.Context, _ *pgx.Batch) pgx.BatchResults { return nil }
func (f *fakeSnapshotTx) LargeObjects() pgx.LargeObjects                             { return pgx.LargeObjects{} }

func (f *fakeSnapshotTx) Prepare(_ context.Context, _, _ string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("unused")
}

func (f *fakeSnapshotTx) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unused")
}

func (f *fakeSnapshotTx) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return nil, errors.New("unused")
}

func (f *fakeSnapshotTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row { return nil }
func (f *fakeSnapshotTx) Conn() *pgx.Conn                                        { return nil }

func newFakeBeginner(t *testing.T, tx *fakeSnapshotTx, beginErr error) txBeginner {
	t.Helper()
	return func(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
		if opts.IsoLevel != pgx.RepeatableRead || opts.AccessMode != pgx.ReadOnly {
			t.Errorf("tx options = %+v, want RepeatableRead + ReadOnly", opts)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Error("begin context missing deadline")
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if beginErr != nil {
			return nil, beginErr
		}
		return tx, nil
	}
}

func assertBoundedRollback(t *testing.T, tx *fakeSnapshotTx, before, after time.Time) {
	t.Helper()
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.rollbacks != 1 {
		t.Fatalf("rollbacks = %d, want 1", tx.rollbacks)
	}
	if tx.rollbackCtxErr != nil {
		t.Fatalf("rollback context err = %v, want nil (detached from caller cancellation)", tx.rollbackCtxErr)
	}
	lo := before.Add(rollbackTimeout)
	hi := after.Add(rollbackTimeout)
	if !tx.rollbackHasDead || tx.rollbackDeadline.Before(lo) || tx.rollbackDeadline.After(hi) {
		t.Fatalf("rollback deadline = %v (has=%v), want in [%v, %v]", tx.rollbackDeadline, tx.rollbackHasDead, lo, hi)
	}
}

func TestSnapshotReadTxCleanupAndContracts(t *testing.T) {
	t.Parallel()

	t.Run("commits read-only snapshot and ignores expected ErrTxClosed on cleanup", func(t *testing.T) {
		t.Parallel()
		tx := &fakeSnapshotTx{}
		before := time.Now()
		err := runSnapshotReadTx(t.Context(), newFakeBeginner(t, tx, nil), nil, func(*sqlcgen.Queries) error {
			return nil
		})
		after := time.Now()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tx.commits != 1 {
			t.Fatalf("commits = %d, want 1", tx.commits)
		}
		assertBoundedRollback(t, tx, before, after)
	})

	t.Run("rolls back with bounded timeout when callback panics even after caller cancellation", func(t *testing.T) {
		t.Parallel()
		tx := &fakeSnapshotTx{}
		ctx, cancel := context.WithCancel(t.Context())
		const wantPanic = "callback exploded"
		var gotPanic any
		before := time.Now()
		func() {
			defer func() {
				gotPanic = recover()
			}()
			_ = runSnapshotReadTx(ctx, newFakeBeginner(t, tx, nil), nil, func(*sqlcgen.Queries) error {
				cancel()
				panic(wantPanic)
			})
		}()
		after := time.Now()
		if gotPanic != wantPanic {
			t.Fatalf("recovered = %v, want %q", gotPanic, wantPanic)
		}
		if tx.commits != 0 {
			t.Fatalf("commits = %d, want 0", tx.commits)
		}
		assertBoundedRollback(t, tx, before, after)
	})

	t.Run("rolls back with bounded timeout when callback exits via runtime.Goexit", func(t *testing.T) {
		t.Parallel()
		tx := &fakeSnapshotTx{}
		done := make(chan struct{})
		before := time.Now()
		go func() {
			defer close(done)
			_ = runSnapshotReadTx(t.Context(), newFakeBeginner(t, tx, nil), nil, func(*sqlcgen.Queries) error {
				runtime.Goexit()
				return nil
			})
		}()
		<-done
		after := time.Now()
		if tx.commits != 0 {
			t.Fatalf("commits = %d, want 0", tx.commits)
		}
		assertBoundedRollback(t, tx, before, after)
	})

	t.Run("preserves callback error and detached rollback bound under canceled caller", func(t *testing.T) {
		t.Parallel()
		tx := &fakeSnapshotTx{}
		sentinel := errors.New("batch consumer failed")
		ctx, cancel := context.WithCancel(t.Context())
		before := time.Now()
		err := runSnapshotReadTx(ctx, newFakeBeginner(t, tx, nil), nil, func(*sqlcgen.Queries) error {
			cancel()
			return sentinel
		})
		after := time.Now()
		if !errors.Is(err, sentinel) {
			t.Fatalf("err = %v, want %v", err, sentinel)
		}
		if tx.commits != 0 {
			t.Fatalf("commits = %d, want 0", tx.commits)
		}
		assertBoundedRollback(t, tx, before, after)
	})

	t.Run("ignores ErrTxClosed when callback error path sees already-closed tx", func(t *testing.T) {
		t.Parallel()
		tx := &fakeSnapshotTx{closed: true}
		sentinel := errors.New("already closed failure")
		err := runSnapshotReadTx(t.Context(), newFakeBeginner(t, tx, nil), nil, func(*sqlcgen.Queries) error {
			return sentinel
		})
		if !errors.Is(err, sentinel) || errors.Is(err, pgx.ErrTxClosed) {
			t.Fatalf("err = %v, want only %v", err, sentinel)
		}
	})

	t.Run("joins unexpected rollback error onto callback error", func(t *testing.T) {
		t.Parallel()
		cbErr := errors.New("callback err")
		rbErr := errors.New("rollback broken")
		tx := &fakeSnapshotTx{rollbackErr: rbErr}
		err := runSnapshotReadTx(t.Context(), newFakeBeginner(t, tx, nil), nil, func(*sqlcgen.Queries) error {
			return cbErr
		})
		if !errors.Is(err, cbErr) || !errors.Is(err, rbErr) {
			t.Fatalf("err = %v, want joined (%v, %v)", err, cbErr, rbErr)
		}
	})

	t.Run("preserves caller cancellation on begin and before commit", func(t *testing.T) {
		t.Parallel()
		preCtx, preCancel := context.WithCancel(t.Context())
		preCancel()
		preTx := &fakeSnapshotTx{}
		if err := runSnapshotReadTx(preCtx, newFakeBeginner(t, preTx, nil), nil, func(*sqlcgen.Queries) error {
			return nil
		}); !errors.Is(err, context.Canceled) {
			t.Fatalf("pre-canceled begin err = %v, want context.Canceled", err)
		}
		if preTx.rollbacks != 0 {
			t.Fatalf("pre-canceled begin rollbacks = %d, want 0", preTx.rollbacks)
		}

		beginErr := errors.New("begin dial failed")
		if err := runSnapshotReadTx(t.Context(), newFakeBeginner(t, preTx, beginErr), nil, func(*sqlcgen.Queries) error {
			return nil
		}); !errors.Is(err, beginErr) {
			t.Fatalf("begin error = %v, want %v", err, beginErr)
		}

		commitCtx, commitCancel := context.WithCancel(t.Context())
		commitTx := &fakeSnapshotTx{}
		before := time.Now()
		err := runSnapshotReadTx(commitCtx, newFakeBeginner(t, commitTx, nil), nil, func(*sqlcgen.Queries) error {
			commitCancel()
			return nil
		})
		after := time.Now()
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled commit err = %v, want context.Canceled", err)
		}
		assertBoundedRollback(t, commitTx, before, after)
	})

	t.Run("preserves successful snapshot commit even if post-commit rollback errors", func(t *testing.T) {
		t.Parallel()
		tx := &fakeSnapshotTx{rollbackErr: errors.New("spurious post-commit rollback error")}
		err := runSnapshotReadTx(t.Context(), newFakeBeginner(t, tx, nil), nil, func(*sqlcgen.Queries) error {
			return nil
		})
		if err != nil {
			t.Fatalf("runSnapshotReadTx post-commit rollback err = %v, want nil", err)
		}
		if tx.commits != 1 {
			t.Fatalf("commits = %d, want 1", tx.commits)
		}
	})
}

func newFakeRWBeginner(tx *fakeSnapshotTx, beginErr error, gotDeadline *time.Time) rwTxBeginner {
	return func(ctx context.Context) (pgx.Tx, error) {
		if gotDeadline != nil {
			*gotDeadline, _ = ctx.Deadline()
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if beginErr != nil {
			return nil, beginErr
		}
		return tx, nil
	}
}

func TestStoreTxCleanupAndContracts(t *testing.T) {
	t.Parallel()

	t.Run("preserves caller deadlines on begin and commit and keeps successful commit clean", func(t *testing.T) {
		t.Parallel()
		callerCtx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		wantDeadline, _ := callerCtx.Deadline()
		var beginDeadline time.Time
		tx := &fakeSnapshotTx{rollbackErr: errors.New("spurious post-commit cleanup error")}
		before := time.Now()
		err := runTx(callerCtx, newFakeRWBeginner(tx, nil, &beginDeadline), nil, func(*sqlcgen.Queries) error {
			return nil
		})
		after := time.Now()
		if err != nil {
			t.Fatalf("runTx committed err = %v, want nil", err)
		}
		if !beginDeadline.Equal(wantDeadline) || !tx.commitDeadline.Equal(wantDeadline) {
			t.Fatalf("deadlines begin=%v commit=%v, want %v", beginDeadline, tx.commitDeadline, wantDeadline)
		}
		assertBoundedRollback(t, tx, before, after)
	})

	t.Run("rolls back with detached timeout on panic and Goexit", func(t *testing.T) {
		t.Parallel()
		panicTx := &fakeSnapshotTx{}
		ctx, cancel := context.WithCancel(t.Context())
		var recovered any
		before := time.Now()
		func() {
			defer func() { recovered = recover() }()
			_ = runTx(ctx, newFakeRWBeginner(panicTx, nil, nil), nil, func(*sqlcgen.Queries) error {
				cancel()
				panic("write tx panic")
			})
		}()
		if recovered != "write tx panic" || panicTx.commits != 0 {
			t.Fatalf("recovered=%v commits=%d, want panic and 0 commits", recovered, panicTx.commits)
		}
		assertBoundedRollback(t, panicTx, before, time.Now())

		exitTx := &fakeSnapshotTx{}
		done := make(chan struct{})
		exitStart := time.Now()
		go func() {
			defer close(done)
			_ = runTx(t.Context(), newFakeRWBeginner(exitTx, nil, nil), nil, func(*sqlcgen.Queries) error {
				runtime.Goexit()
				return nil
			})
		}()
		<-done
		assertBoundedRollback(t, exitTx, exitStart, time.Now())
	})

	t.Run("preserves primary callback, commit, and begin errors and joins rollback failure", func(t *testing.T) {
		t.Parallel()
		cbSentinel := errors.New("write callback failed")
		ctx, cancel := context.WithCancel(t.Context())
		cbTx := &fakeSnapshotTx{}
		before := time.Now()
		err := runTx(ctx, newFakeRWBeginner(cbTx, nil, nil), nil, func(*sqlcgen.Queries) error {
			cancel()
			return cbSentinel
		})
		if !errors.Is(err, cbSentinel) || errors.Is(err, context.Canceled) {
			t.Fatalf("callback err = %v, want only %v", err, cbSentinel)
		}
		assertBoundedRollback(t, cbTx, before, time.Now())

		commitSentinel := errors.New("commit serialization failure")
		commitTx := &fakeSnapshotTx{commitErr: commitSentinel}
		commitStart := time.Now()
		commitErr := runTx(t.Context(), newFakeRWBeginner(commitTx, nil, nil), nil, func(*sqlcgen.Queries) error {
			return nil
		})
		if !errors.Is(commitErr, commitSentinel) {
			t.Fatalf("commit err = %v, want %v", commitErr, commitSentinel)
		}
		assertBoundedRollback(t, commitTx, commitStart, time.Now())

		rbBoom := errors.New("rollback wire failure")
		joinTx := &fakeSnapshotTx{rollbackErr: rbBoom}
		joinErr := runTx(t.Context(), newFakeRWBeginner(joinTx, nil, nil), nil, func(*sqlcgen.Queries) error {
			return cbSentinel
		})
		if !errors.Is(joinErr, cbSentinel) || !errors.Is(joinErr, rbBoom) {
			t.Fatalf("joined err = %v, want (%v, %v)", joinErr, cbSentinel, rbBoom)
		}

		beginSentinel := errors.New("begin pool exhausted")
		beginTx := &fakeSnapshotTx{}
		if bErr := runTx(t.Context(), newFakeRWBeginner(beginTx, beginSentinel, nil), nil, func(*sqlcgen.Queries) error {
			return nil
		}); !errors.Is(bErr, beginSentinel) || beginTx.rollbacks != 0 {
			t.Fatalf("begin err=%v rollbacks=%d, want %v and 0", bErr, beginTx.rollbacks, beginSentinel)
		}
	})
}
