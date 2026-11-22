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

	"github.com/lusoris/SnatchArr/api/internal/domain"
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

// Heartbeat answers by lease ownership: a worker whose expired lease another worker took gets
// CANCEL, the holder of the lease gets it extended (also after it expired with nobody taking
// it), and a heartbeat without a worker id gets CONTINUE and leaves the lease alone.
func TestHeartbeatLeaseOwnership(t *testing.T) {
	t.Parallel()
	cancelled := snatcharrv1.HeartbeatDirective_HEARTBEAT_DIRECTIVE_CANCEL
	carryOn := snatcharrv1.HeartbeatDirective_HEARTBEAT_DIRECTIVE_CONTINUE
	cases := []struct {
		name       string
		expire     bool
		reassign   bool
		worker     string
		want       snatcharrv1.HeartbeatDirective
		extends    bool
		wantHolder string
	}{
		{"stale holder of a reassigned lease", true, true, "worker", cancelled, false, "worker-2"},
		{"new holder of a reassigned lease", true, true, "worker-2", carryOn, true, "worker-2"},
		{"holder of an expired lease nobody took", true, false, "worker", carryOn, true, "worker"},
		{"heartbeat without a worker id", false, false, "", carryOn, false, "worker"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, _, runID := completeServer(t)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			heartbeatFixture(ctx, t, srv, runID, tc.expire, tc.reassign)
			before := heartbeatRun(ctx, t, srv, runID)
			resp, err := srv.Heartbeat(ctx, &snatcharrv1.HeartbeatRequest{RunId: runID, WorkerId: tc.worker})
			if err != nil {
				t.Fatalf("Heartbeat(%q) = %v", tc.worker, err)
			}
			if resp.GetDirective() != tc.want {
				t.Errorf("directive = %v, want %v", resp.GetDirective(), tc.want)
			}
			after := heartbeatRun(ctx, t, srv, runID)
			if after.LeasedBy != tc.wantHolder {
				t.Errorf("leased_by = %q, want %q", after.LeasedBy, tc.wantHolder)
			}
			checkLeaseExpiry(t, srv, resp, before, after, tc.extends)
		})
	}
}

// heartbeatFixture takes the run completeServer leased to "worker", lets that lease expire when
// expire is set, hands the run to "worker-2" when reassign is set, and then moves the clock 10 s on.
func heartbeatFixture(ctx context.Context, t *testing.T, srv *Server, runID string, expire, reassign bool) {
	t.Helper()
	fakeClk, ok := srv.clk.(*clockwork.FakeClock)
	if !ok {
		t.Fatalf("clk is %T, want *clockwork.FakeClock", srv.clk)
	}
	if expire {
		fakeClk.Advance(snatch.LeaseTTL + time.Second)
	}
	if reassign {
		leased, err := srv.runs.Lease(ctx, "worker-2")
		if err != nil {
			t.Fatalf("Lease(worker-2) = %v", err)
		}
		if leased.ID.String() != runID {
			t.Fatalf("leased run = %s, want %s", leased.ID, runID)
		}
	}
	fakeClk.Advance(10 * time.Second)
}

// heartbeatRun reads the run row back for the lease checks.
func heartbeatRun(ctx context.Context, t *testing.T, srv *Server, runID string) domain.Run {
	t.Helper()
	run, err := srv.runs.Get(ctx, uuid.MustParse(runID))
	if err != nil {
		t.Fatalf("runs.Get() = %v", err)
	}
	return run
}

// checkLeaseExpiry wants the lease moved to now + LeaseTTL in the answer and in the row when
// extends is set, and the row's lease left as it was otherwise.
func checkLeaseExpiry(t *testing.T, srv *Server, resp *snatcharrv1.HeartbeatResponse, before, after domain.Run, extends bool) {
	t.Helper()
	if !extends {
		if before.LeaseExpiresAt == nil || after.LeaseExpiresAt == nil || !after.LeaseExpiresAt.Equal(*before.LeaseExpiresAt) {
			t.Errorf("lease_expires_at = %v after, want unchanged %v", after.LeaseExpiresAt, before.LeaseExpiresAt)
		}
		return
	}
	wantExpires := srv.clk.Now().Add(snatch.LeaseTTL)
	if resp.GetLeaseExpiresUnix() != wantExpires.Unix() {
		t.Errorf("lease_expires_unix = %v, want %v", resp.GetLeaseExpiresUnix(), wantExpires.Unix())
	}
	wantAt := wantExpires.Truncate(time.Microsecond)
	if after.LeaseExpiresAt == nil || !after.LeaseExpiresAt.Equal(wantAt) {
		t.Errorf("run lease_expires_at = %v, want %v", after.LeaseExpiresAt, wantAt)
	}
}
