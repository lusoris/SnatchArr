// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package workergrpc

import (
	"context"
	"strconv"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	snatcharrv1 "github.com/lusoris/SnatchArr/api/internal/gen/snatcharr/v1"
	"github.com/lusoris/SnatchArr/api/internal/instances"
)

// Bounds of the fairness scenario (HISS-02, HISS-06).
const (
	fairRounds  = 4
	fairBurst   = 3
	fairWorkers = 2
)

// leasedBy is one answered LeaseRun: which worker got which run.
type leasedBy struct{ worker, run string }

// pollingWorker is the worker's lease loop: one long-poll at a time, the next one sent as
// soon as the run is handed over, as the Rust worker does with a run that ends at once.
func pollingWorker(ctx context.Context, srv *Server, worker string, out chan<- leasedBy) {
	for ctx.Err() == nil {
		resp, err := srv.LeaseRun(ctx, &snatcharrv1.LeaseRunRequest{WorkerId: worker, WaitSeconds: 30})
		if err != nil {
			return
		}
		if run := resp.GetRun(); run != nil {
			select {
			case out <- leasedBy{worker: worker, run: run.GetRunId()}:
			case <-ctx.Done():
				return
			}
		}
	}
}

// queueBurst queues fairBurst runs, one per new instance.
func queueBurst(ctx context.Context, t *testing.T, srv *Server, round int) {
	t.Helper()
	for i := range fairBurst {
		name := "fair-" + strconv.Itoa(round) + "-" + strconv.Itoa(i)
		inst, err := srv.instances.Create(ctx, instances.Input{
			Kind: domain.KindSonarr, Name: name, BaseURL: "http://" + name + ".invalid:8989", APIKey: "a-key-for-the-fairness-test", Enabled: true,
		})
		if err != nil {
			t.Fatalf("round %d: instances.Create() = %v", round, err)
		}
		if _, _, err = srv.runs.Enqueue(ctx, inst.ID, domain.SnatchMissing); err != nil {
			t.Fatalf("round %d: Enqueue() = %v", round, err)
		}
	}
}

// Two workers wait on a LeaseRun long-poll, w2 half a poll interval behind w1, and each round
// queues a burst of runs just before w1's turn to poll. w1 takes the whole burst with its
// repeated polls before w2's poll comes round, and so on every round: a waiting worker never
// gets a run (#343). Both must be served.
func TestWaitingWorkersAreBothServed(t *testing.T) {
	t.Parallel()
	f := newCompleteFixture(t)
	srv, clk := f.srv, f.clk
	ctx, stop := context.WithTimeout(t.Context(), leaseWait)
	defer stop()
	out := make(chan leasedBy, fairRounds*fairBurst)

	go pollingWorker(ctx, srv, "w1", out)
	if err := clk.BlockUntilContext(ctx, 1); err != nil {
		t.Fatalf("w1 never waited: %v", err)
	}
	clk.Advance(leasePollInterval / 2)
	go pollingWorker(ctx, srv, "w2", out)
	if err := clk.BlockUntilContext(ctx, fairWorkers); err != nil {
		t.Fatalf("w2 never waited: %v", err)
	}

	served := map[string]int{}
	for round := range fairRounds {
		queueBurst(ctx, t, srv, round)
		// w1's poll falls due first; w2's follows half an interval later.
		clk.Advance(leasePollInterval / 2)
		for range fairBurst {
			select {
			case got := <-out:
				served[got.worker]++
			case <-ctx.Done():
				t.Fatalf("round %d: runs left unleased after %v: %v", round, served, ctx.Err())
			}
		}
		if err := clk.BlockUntilContext(ctx, fairWorkers); err != nil {
			t.Fatalf("round %d: workers never waited again: %v", round, err)
		}
		clk.Advance(leasePollInterval / 2)
		if err := clk.BlockUntilContext(ctx, fairWorkers); err != nil {
			t.Fatalf("round %d: workers never waited again: %v", round, err)
		}
	}
	stop()
	for _, worker := range []string{"w1", "w2"} {
		if served[worker] == 0 {
			t.Errorf("worker %s leased no run of %d; leases by worker: %v", worker, fairRounds*fairBurst, served)
		}
	}
}
