// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package workergrpc

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	snatcharrv1 "github.com/lusoris/SnatchArr/api/internal/gen/snatcharr/v1"
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
