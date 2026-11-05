// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package prowlarr

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// deadlineTracer observes the store's context and cancels before any connection.
type deadlineTracer struct {
	deadline time.Time
	seen     bool
	bounded  bool
}

func (d *deadlineTracer) TraceAcquireStart(ctx context.Context, _ *pgxpool.Pool, _ pgxpool.TraceAcquireStartData) context.Context {
	d.deadline, d.bounded = ctx.Deadline()
	d.seen = true
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	return ctx
}

func (*deadlineTracer) TraceAcquireEnd(context.Context, *pgxpool.Pool, pgxpool.TraceAcquireEndData) {
}

func (*deadlineTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return ctx
}

func (*deadlineTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func newDeadlineService(t *testing.T, tracer *deadlineTracer) *Service {
	t.Helper()
	cfg, err := pgxpool.ParseConfig("postgres://unused:unused@127.0.0.1/unused?pool_min_conns=0")
	if err != nil {
		t.Fatalf("pool config: %v", err)
	}
	cfg.ConnConfig.Tracer = tracer
	// Independent guard: a test failure must still never open a network connection.
	cfg.BeforeConnect = func(context.Context, *pgx.ConnConfig) error {
		return errors.New("connection forbidden in deadline test")
	}
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	opts := config.Default().Prowlarr
	opts.SearchDBTimeout = 2 * time.Second
	return &Service{st: store.New(pool), cfg: opts}
}

func TestSearchDBDeadline(t *testing.T) {
	t.Parallel()
	for _, call := range []struct {
		name string
		run  func(*Service, context.Context) error
	}{
		{name: "lookup", run: func(s *Service, ctx context.Context) error {
			_, err := s.searchLink(ctx, uuid.New())
			return err
		}},
		{name: "transaction", run: func(s *Service, ctx context.Context) error {
			_, err := s.acquireGrabBudget(ctx, uuid.New(), 1)
			return err
		}},
	} {
		for _, parent := range []string{"no deadline", "earlier deadline", "expired"} {
			t.Run(call.name+"/"+parent, func(t *testing.T) {
				t.Parallel()
				ctx := context.Background() // Exercise callers that supply no deadline.
				if parent != "no deadline" {
					timeout := time.Second
					if parent == "expired" {
						timeout = -time.Second
					}
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, timeout)
					defer cancel()
				}
				var tracer deadlineTracer
				svc := newDeadlineService(t, &tracer)
				wantErr := context.Canceled
				if parent == "expired" {
					wantErr = context.DeadlineExceeded
				}
				if err := call.run(svc, ctx); !errors.Is(err, wantErr) {
					t.Fatalf("store error = %v, want %v", err, wantErr)
				}
				if !tracer.seen || !tracer.bounded {
					t.Fatal("store received no deadline")
				}
				if deadline, ok := ctx.Deadline(); ok {
					if !tracer.deadline.Equal(deadline) {
						t.Fatalf("parent deadline changed: %v -> %v", deadline, tracer.deadline)
					}
				} else if remaining := time.Until(tracer.deadline); remaining <= 0 || remaining > svc.cfg.SearchDBTimeout {
					t.Fatalf("DB timeout remaining = %v, want (0, %v]", remaining, svc.cfg.SearchDBTimeout)
				}
			})
		}
	}
}

type budgetRow func(...any) error

func (r budgetRow) Scan(dest ...any) error { return r(dest...) }

// budgetDB supplies current link state through the real generated queries.
type budgetDB struct {
	sqlcgen.DBTX
	capacity int32
	used     int32
	debited  int32
	locked   bool
}

func (d *budgetDB) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	switch {
	case strings.HasPrefix(query, "-- name: LockProwlarrLinkBudget "):
		d.locked = true
	case strings.HasPrefix(query, "-- name: AddProwlarrBucketUsed "):
		d.debited += args[2].(int32)
	default:
		return pgconn.CommandTag{}, errors.New("unexpected budget exec")
	}
	return pgconn.CommandTag{}, nil
}

func (d *budgetDB) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	return budgetRow(func(dest ...any) error {
		switch {
		case strings.HasPrefix(query, "-- name: GetProwlarrLink "):
			if !d.locked {
				return errors.New("cap read before link lock")
			}
			// GetProwlarrLink scans hourly_cap as its final column.
			*dest[len(dest)-1].(*int32) = d.capacity
		case strings.HasPrefix(query, "-- name: ProwlarrBucketUsage "):
			*dest[0].(*int32) = d.used
		case strings.HasPrefix(query, "-- name: GetSettings "):
			// Zero global_hourly_cap disables the global bucket in this fixture.
		default:
			return errors.New("unexpected budget query")
		}
		return nil
	})
}

func TestSearchBudgetCurrentCap(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		capacity int32
		cost     int
		refused  bool
	}{
		{name: "variant lowered below usage plus cost", capacity: 9, cost: 2, refused: true},
		{name: "variant exactly current cap", capacity: 10, cost: 2},
		{name: "variant below current cap", capacity: 11, cost: 2},
		{name: "grab lowered to usage", capacity: 8, cost: 1, refused: true},
		{name: "grab exactly current cap", capacity: 9, cost: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			db := &budgetDB{capacity: tc.capacity, used: 8}
			svc := &Service{clk: clock.NewFake()}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			budget, err := svc.debitSearchBudget(ctx, sqlcgen.New(db), uuid.New(), tc.cost)
			if tc.refused {
				if !errors.Is(err, domain.ErrTooManyRequests) || db.debited != 0 || budget != (SearchBudget{}) {
					t.Fatalf("refused search = %+v, %v, debit %d", budget, err, db.debited)
				}
				return
			}
			if err != nil || budget.LinkCap != int(tc.capacity) || budget.LinkUsed != 8+tc.cost || int(db.debited) != tc.cost {
				t.Fatalf("current cap %d: budget %+v, error %v, debit %d", tc.capacity, budget, err, db.debited)
			}
		})
	}
}
