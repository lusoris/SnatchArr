// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package workergrpc

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	snatcharrv1 "github.com/lusoris/SnatchArr/api/internal/gen/snatcharr/v1"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
)

// LeaseRun and Heartbeat record the worker's contact for the system status (gap audit
// A1-01), also when the call itself fails later; a call without a worker id records none.
func TestCallsRecordWorkerContact(t *testing.T) {
	t.Parallel()
	unknownRun := uuid.NewString()
	cases := []struct {
		name     string
		call     func(context.Context, *Server) error
		wantCode codes.Code
		wantSeen bool
	}{
		{"lease run", func(ctx context.Context, s *Server) error {
			_, err := s.LeaseRun(ctx, &snatcharrv1.LeaseRunRequest{WorkerId: "w1"})
			return err
		}, codes.OK, true},
		{"lease run without worker id", func(ctx context.Context, s *Server) error {
			_, err := s.LeaseRun(ctx, &snatcharrv1.LeaseRunRequest{})
			return err
		}, codes.InvalidArgument, false},
		{"heartbeat on an unknown run", func(ctx context.Context, s *Server) error {
			_, err := s.Heartbeat(ctx, &snatcharrv1.HeartbeatRequest{RunId: unknownRun, WorkerId: "w1"})
			return err
		}, codes.NotFound, true},
		{"heartbeat without worker id", func(ctx context.Context, s *Server) error {
			_, err := s.Heartbeat(ctx, &snatcharrv1.HeartbeatRequest{RunId: unknownRun})
			return err
		}, codes.NotFound, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, clk, _ := leaseServer(t)
			if code := status.Code(tc.call(t.Context(), srv)); code != tc.wantCode {
				t.Fatalf("call = %v, want %v", code, tc.wantCode)
			}
			at, ok, err := srv.workers.LastContact(t.Context())
			if err != nil {
				t.Fatalf("LastContact() = %v", err)
			}
			// Postgres keeps microseconds.
			if got := ok && at.Equal(clk.Now().Truncate(time.Microsecond)); got != tc.wantSeen {
				t.Fatalf("contact recorded at now = %v (%v), want %v", got, at, tc.wantSeen)
			}
		})
	}
}

// Heartbeat extends the lease of a leased run when called by the right worker.
func TestHeartbeatExtendsLease(t *testing.T) {
	t.Parallel()
	srv, _, runID := completeServer(t)
	fakeClk, ok := srv.clk.(*clockwork.FakeClock)
	if !ok {
		t.Fatalf("clk is %T, want *clockwork.FakeClock", srv.clk)
	}
	fakeClk.Advance(2 * time.Minute)
	resp, err := srv.Heartbeat(t.Context(), &snatcharrv1.HeartbeatRequest{
		RunId:    runID,
		WorkerId: "worker",
	})
	if err != nil {
		t.Fatalf("Heartbeat() = %v", err)
	}
	if resp.GetDirective() != snatcharrv1.HeartbeatDirective_HEARTBEAT_DIRECTIVE_CONTINUE {
		t.Errorf("directive = %v, want CONTINUE", resp.GetDirective())
	}
	wantExpires := srv.clk.Now().Add(snatch.LeaseTTL).Unix()
	if resp.GetLeaseExpiresUnix() != wantExpires {
		t.Errorf("lease_expires_unix = %v, want %v", resp.GetLeaseExpiresUnix(), wantExpires)
	}
	run, getErr := srv.runs.Get(t.Context(), uuid.MustParse(runID))
	if getErr != nil {
		t.Fatalf("runs.Get() = %v", getErr)
	}
	wantAt := srv.clk.Now().Add(snatch.LeaseTTL).Truncate(time.Microsecond)
	if run.LeaseExpiresAt == nil || !run.LeaseExpiresAt.Equal(wantAt) {
		t.Errorf("run lease_expires_at = %v, want %v", run.LeaseExpiresAt, wantAt)
	}
}

// Heartbeat cancels a worker whose expired lease was handed to another worker, extends the new
// holder's lease, and still extends an expired lease that nobody else took.
func TestHeartbeatCancelsAStaleHolder(t *testing.T) {
	t.Parallel()
	t.Run("reassigned lease cancels stale holder and extends new holder", func(t *testing.T) {
		t.Parallel()
		srv, _, runID := completeServer(t)
		fakeClk, ok := srv.clk.(*clockwork.FakeClock)
		if !ok {
			t.Fatalf("clk is %T, want *clockwork.FakeClock", srv.clk)
		}
		fakeClk.Advance(snatch.LeaseTTL + time.Second)
		leased, leaseErr := srv.runs.Lease(t.Context(), "worker-2")
		if leaseErr != nil {
			t.Fatalf("Lease(worker-2) = %v", leaseErr)
		}
		if leased.ID.String() != runID {
			t.Fatalf("leased run = %s, want %s", leased.ID, runID)
		}
		fakeClk.Advance(10 * time.Second)
		staleResp, staleErr := srv.Heartbeat(t.Context(), &snatcharrv1.HeartbeatRequest{
			RunId:    runID,
			WorkerId: "worker",
		})
		if staleErr != nil {
			t.Fatalf("Heartbeat(worker) = %v", staleErr)
		}
		if staleResp.GetDirective() != snatcharrv1.HeartbeatDirective_HEARTBEAT_DIRECTIVE_CANCEL {
			t.Errorf("stale directive = %v, want CANCEL", staleResp.GetDirective())
		}
		curResp, curErr := srv.Heartbeat(t.Context(), &snatcharrv1.HeartbeatRequest{
			RunId:    runID,
			WorkerId: "worker-2",
		})
		if curErr != nil {
			t.Fatalf("Heartbeat(worker-2) = %v", curErr)
		}
		if curResp.GetDirective() != snatcharrv1.HeartbeatDirective_HEARTBEAT_DIRECTIVE_CONTINUE {
			t.Errorf("current directive = %v, want CONTINUE", curResp.GetDirective())
		}
		wantExpires := srv.clk.Now().Add(snatch.LeaseTTL).Unix()
		if curResp.GetLeaseExpiresUnix() != wantExpires {
			t.Errorf("lease_expires_unix = %v, want %v", curResp.GetLeaseExpiresUnix(), wantExpires)
		}
		run, getErr := srv.runs.Get(t.Context(), uuid.MustParse(runID))
		if getErr != nil {
			t.Fatalf("runs.Get() = %v", getErr)
		}
		wantAt := srv.clk.Now().Add(snatch.LeaseTTL).Truncate(time.Microsecond)
		if run.LeaseExpiresAt == nil || !run.LeaseExpiresAt.Equal(wantAt) {
			t.Errorf("run lease_expires_at = %v, want %v", run.LeaseExpiresAt, wantAt)
		}
	})
	t.Run("expired lease not taken again extends for original holder", func(t *testing.T) {
		t.Parallel()
		srv, _, runID := completeServer(t)
		fakeClk, ok := srv.clk.(*clockwork.FakeClock)
		if !ok {
			t.Fatalf("clk is %T, want *clockwork.FakeClock", srv.clk)
		}
		fakeClk.Advance(snatch.LeaseTTL + time.Second)
		resp, err := srv.Heartbeat(t.Context(), &snatcharrv1.HeartbeatRequest{
			RunId:    runID,
			WorkerId: "worker",
		})
		if err != nil {
			t.Fatalf("Heartbeat(worker) = %v", err)
		}
		if resp.GetDirective() != snatcharrv1.HeartbeatDirective_HEARTBEAT_DIRECTIVE_CONTINUE {
			t.Errorf("directive = %v, want CONTINUE", resp.GetDirective())
		}
		wantExpires := srv.clk.Now().Add(snatch.LeaseTTL).Unix()
		if resp.GetLeaseExpiresUnix() != wantExpires {
			t.Errorf("lease_expires_unix = %v, want %v", resp.GetLeaseExpiresUnix(), wantExpires)
		}
		run, getErr := srv.runs.Get(t.Context(), uuid.MustParse(runID))
		if getErr != nil {
			t.Fatalf("runs.Get() = %v", getErr)
		}
		wantAt := srv.clk.Now().Add(snatch.LeaseTTL).Truncate(time.Microsecond)
		if run.LeaseExpiresAt == nil || !run.LeaseExpiresAt.Equal(wantAt) {
			t.Errorf("run lease_expires_at = %v, want %v", run.LeaseExpiresAt, wantAt)
		}
	})
}
