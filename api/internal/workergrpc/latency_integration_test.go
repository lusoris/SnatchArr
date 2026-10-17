// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package workergrpc

import (
	"bytes"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"

	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/events"
	snatcharrv1 "github.com/lusoris/SnatchArr/api/internal/gen/snatcharr/v1"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/policies"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

// The load the lease timings are measured under (gap audit C12-03): this many workers
// long-poll LeaseRun at once, on the wall clock and one Postgres.
const (
	loadWorkers = 64
	loadRuns    = 8
	// idleWait is the wait an idle long-poll asks for; leaseWaitLoad the one a poll asks for
	// while runs arrive. Both are short stand-ins for the 30 s a worker asks for.
	idleWait      = 3 * time.Second
	leaseWaitLoad = 10 * time.Second
	// idleSlack: an empty long-poll answers no later than its wait plus this.
	idleSlack = 2 * time.Second
	// leaseSlack: a queued run is handed to a waiting worker no later than this after it was
	// queued: the 1 s poll interval plus margin. Fixed, not derived from leasePollInterval, so a
	// slower poll fails the test instead of widening its bound.
	leaseSlack = 3 * time.Second
)

// poll is what one long-poll returned and when.
type poll struct {
	runID string
	err   error
	took  time.Duration
	done  time.Time
}

// latencyServer is a Server with what LeaseRun reaches, on the wall clock.
func latencyServer(t *testing.T) *Server {
	t.Helper()
	st := storetest.New(t)
	clk := clockwork.NewRealClock()
	logger := slog.New(slog.DiscardHandler)
	enc, err := crypto.NewEncryptor(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("encryptor: %v", err)
	}
	srv := &Server{
		runs: snatch.NewRuns(st, clk, id.New()), workers: snatch.NewWorkers(st, clk), rec: snatch.NewRecorder(st, clk, events.New(logger)),
		instances: instances.New(st, enc, clk, id.New(), nil, logger), policies: policies.New(st, clk),
		clk: clk, logger: logger, stopping: make(chan struct{}),
	}
	withMetrics(t, srv)
	return srv
}

// pollAll runs loadWorkers long-polls at once, each asking for wait, and returns what each
// one got.
func pollAll(t *testing.T, srv *Server, wait time.Duration) []poll {
	t.Helper()
	polls := make([]poll, loadWorkers)
	var wg sync.WaitGroup
	for i := range loadWorkers {
		wg.Go(func() {
			start := time.Now()
			resp, err := srv.LeaseRun(t.Context(), &snatcharrv1.LeaseRunRequest{
				WorkerId: fmt.Sprintf("load-%02d", i), WaitSeconds: uint32(wait / time.Second),
			})
			end := time.Now()
			polls[i] = poll{runID: resp.GetRun().GetRunId(), err: err, took: end.Sub(start), done: end}
		})
	}
	wg.Wait()
	return polls
}

// Under loadWorkers concurrent long-polls an empty poll keeps the wait it asked for and
// answers within idleSlack after it, so a worker neither hammers the API nor overruns the
// 35 s connection grace (APP_GRPC_KEEPALIVE_MAX_CONNECTION_AGE_GRACE).
func TestIdleLongPollLatencyUnderLoad(t *testing.T) {
	t.Parallel()
	srv := latencyServer(t)
	for i, p := range pollAll(t, srv, idleWait) {
		if p.err != nil || p.runID != "" {
			t.Fatalf("poll %d = run %q, %v; want an empty answer", i, p.runID, p.err)
		}
		if p.took < idleWait || p.took > idleWait+idleSlack {
			t.Errorf("poll %d answered after %v, want between %v and %v", i, p.took, idleWait, idleWait+idleSlack)
		}
	}
}

// Runs queued while loadWorkers long-polls wait are each handed to exactly one worker within
// leaseSlack after they were queued.
func TestLeaseLatencyUnderLoad(t *testing.T) {
	t.Parallel()
	srv := latencyServer(t)
	ids := make([]uuid.UUID, loadRuns)
	for i := range ids {
		inst, err := srv.instances.Create(t.Context(), instances.Input{
			Kind: domain.KindSonarr, Name: fmt.Sprintf("load-%d", i), BaseURL: fmt.Sprintf("http://sonarr-%d.invalid:8989", i),
			APIKey: "a-key-for-the-latency-test", Enabled: true,
		})
		if err != nil {
			t.Fatalf("instances.Create() = %v", err)
		}
		ids[i] = inst.ID
	}
	queued := make(chan time.Time, 1)
	go func() {
		// Every poll has started and found the queue empty by now.
		time.Sleep(leasePollInterval)
		at := time.Now()
		for _, instanceID := range ids {
			if _, _, err := srv.runs.Enqueue(t.Context(), instanceID, domain.SnatchMissing); err != nil {
				t.Errorf("Enqueue() = %v", err)
			}
		}
		queued <- at
	}()
	polls := pollAll(t, srv, leaseWaitLoad)
	at := <-queued
	leased := map[string]int{}
	for i, p := range polls {
		if p.err != nil {
			t.Fatalf("poll %d = %v", i, p.err)
		}
		if p.runID == "" {
			continue
		}
		leased[p.runID]++
		if lag := p.done.Sub(at); lag > leaseSlack {
			t.Errorf("run %s reached a worker %v after it was queued, want at most %v", p.runID, lag, leaseSlack)
		}
	}
	if len(leased) != loadRuns {
		t.Errorf("%d distinct runs leased, want %d", len(leased), loadRuns)
	}
	for runID, n := range leased {
		if n != 1 {
			t.Errorf("run %s leased %d times, want once", runID, n)
		}
	}
}
