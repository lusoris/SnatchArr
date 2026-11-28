// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

type deadlineProbeTx struct {
	pgx.Tx
	commit func(context.Context) error
}

func (tx deadlineProbeTx) Commit(ctx context.Context) error { return tx.commit(ctx) }

func (deadlineProbeTx) Rollback(context.Context) error { return pgx.ErrTxClosed }

func TestRunTxStepDeadlines(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"fallback", "caller deadline", "caller cancellation"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			parent, cancelParent := context.WithCancel(t.Context())
			defer cancelParent()
			if name == "caller deadline" {
				var cancelDeadline context.CancelFunc
				parent, cancelDeadline = context.WithTimeout(parent, time.Minute)
				defer cancelDeadline()
			}
			checkDeadline := func(step string, ctx context.Context, before time.Time) {
				after := time.Now()
				t.Helper()
				deadline, ok := ctx.Deadline()
				if !ok {
					t.Fatalf("%s context has no deadline", step)
				}
				if want, callerHasDeadline := parent.Deadline(); callerHasDeadline && !deadline.Equal(want) {
					t.Fatalf("%s deadline = %v, want caller deadline %v", step, deadline, want)
				}
				if name != "caller deadline" {
					earliest, latest := before.Add(txStepTimeout), after.Add(txStepTimeout)
					if deadline.Before(earliest) || deadline.After(latest) {
						t.Fatalf("%s fallback deadline = %v, want between %v and %v", step, deadline, earliest, latest)
					}
				}
			}
			var beginErr func() error
			var committed bool
			var commitBefore time.Time
			beginBefore := time.Now()
			err := runTx(parent, func(ctx context.Context) (pgx.Tx, error) {
				beginErr = ctx.Err
				checkDeadline("begin", ctx, beginBefore)
				return deadlineProbeTx{commit: func(ctx context.Context) error {
					committed = true
					checkDeadline("commit", ctx, commitBefore)
					return ctx.Err()
				}}, nil
			}, nil, func(*sqlcgen.Queries) error {
				if name != "caller deadline" && !errors.Is(beginErr(), context.Canceled) {
					t.Fatalf("begin context error in callback = %v, want context.Canceled", beginErr())
				}
				if err := parent.Err(); err != nil {
					t.Fatalf("caller context was canceled before callback: %v", err)
				}
				if name == "caller cancellation" {
					cancelParent()
				}
				commitBefore = time.Now()
				return nil
			})
			var wantErr error
			if name == "caller cancellation" {
				wantErr = context.Canceled
			}
			if !committed || !errors.Is(err, wantErr) {
				t.Fatalf("commit called = %v, runTx error = %v; want true, %v", committed, err, wantErr)
			}
		})
	}
}
