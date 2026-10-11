// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package workergrpc

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/events"
	snatcharrv1 "github.com/lusoris/SnatchArr/api/internal/gen/snatcharr/v1"
	"github.com/lusoris/SnatchArr/api/internal/policies"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

// leaseWait bounds how long a test waits for a handler to block or to answer (HISS-02).
const leaseWait = 30 * time.Second

// leaseServer is a Server over a fresh database with what LeaseRun reaches as long as it
// hands out no run.
func leaseServer(t *testing.T) (*Server, *clockwork.FakeClock, *store.Store) {
	t.Helper()
	st := storetest.New(t)
	clk := clock.NewFake()
	logger := slog.New(slog.DiscardHandler)
	srv := &Server{
		runs: snatch.NewRuns(st, clk, id.New()), rec: snatch.NewRecorder(st, clk, events.New(logger)),
		policies: policies.New(st, clk), clk: clk, logger: logger, stopping: make(chan struct{}),
	}
	return srv, clk, st
}

// A LeaseRun that waits for a run ends with the stop of the control plane, with the caller's
// context, or with the wait it asked for (#146).
func TestWaitingLeaseRunEnds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		end  func(srv *Server, clk *clockwork.FakeClock, cancel context.CancelFunc)
		want codes.Code
	}{
		{"the control plane stops", func(srv *Server, _ *clockwork.FakeClock, _ context.CancelFunc) { srv.Shutdown() }, codes.Unavailable},
		{"the caller gives up", func(_ *Server, _ *clockwork.FakeClock, cancel context.CancelFunc) { cancel() }, codes.Canceled},
		{"the wait is over", func(_ *Server, clk *clockwork.FakeClock, _ context.CancelFunc) { clk.Advance(maxLeaseWait) }, codes.OK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, clk, _ := leaseServer(t)
			bounded, stop := context.WithTimeout(t.Context(), leaseWait)
			defer stop()
			ctx, cancel := context.WithCancel(bounded)
			defer cancel()
			answer := make(chan error, 1)
			go func() {
				_, err := srv.LeaseRun(ctx, &snatcharrv1.LeaseRunRequest{WorkerId: "worker", WaitSeconds: 30})
				answer <- err
			}()
			// The handler found no run and sleeps on the clock.
			if err := clk.BlockUntilContext(bounded, 1); err != nil {
				t.Fatalf("LeaseRun never waited: %v", err)
			}
			tc.end(srv, clk, cancel)
			select {
			case err := <-answer:
				if got := status.Code(err); got != tc.want {
					t.Fatalf("LeaseRun() = %v (%v), want %v", got, err, tc.want)
				}
			case <-bounded.Done():
				t.Fatalf("LeaseRun still waits: %v", bounded.Err())
			}
		})
	}
}

// Before the stop an empty queue is the plain "no run" answer. After it LeaseRun answers
// Unavailable and hands out nothing: the queued run is still there for the next control plane.
func TestLeaseRunAroundShutdown(t *testing.T) {
	t.Parallel()
	srv, clk, st := leaseServer(t)
	req := &snatcharrv1.LeaseRunRequest{WorkerId: "worker"}

	resp, err := srv.LeaseRun(t.Context(), req)
	if err != nil || resp.GetRun() != nil {
		t.Fatalf("serving, empty queue: LeaseRun() = %v, %v; want an empty answer", resp, err)
	}

	queued, _, err := srv.runs.Enqueue(t.Context(), storetest.Instance(t, st, clk.Now()), domain.SnatchMissing)
	if err != nil {
		t.Fatalf("Enqueue() = %v", err)
	}
	srv.Shutdown()
	srv.Shutdown() // a second stop is a no-op
	_, err = srv.LeaseRun(t.Context(), req)
	if st := status.Convert(err); st.Code() != codes.Unavailable || st.Message() != stoppingMessage {
		t.Fatalf("stopping: LeaseRun() = %v, want Unavailable %q", err, stoppingMessage)
	}
	run, err := srv.runs.Lease(t.Context(), "worker")
	if err != nil || run.ID != queued.ID {
		t.Fatalf("the queued run was handed out while stopping: Lease() = %v, %v; want run %s", run.ID, err, queued.ID)
	}
}
