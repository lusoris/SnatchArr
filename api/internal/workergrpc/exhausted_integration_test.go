// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package workergrpc

import (
	"bufio"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/events"
	"github.com/lusoris/SnatchArr/api/internal/policies"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

// Bounds of the SSE read (HISS-02).
const (
	sseLines   = 200
	clientWait = 100
)

// A run ended by an exhausted lease shows up in history and on the live SSE stream (#17).
func TestExhaustedRunIsRecorded(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	clk := clock.NewFake()
	logger := slog.New(slog.DiscardHandler)
	bus := events.New(logger)
	runs := snatch.NewRuns(st, clk, id.New())
	s := &Server{runs: runs, rec: snatch.NewRecorder(st, clk, bus), policies: policies.New(st, clk), clk: clk, logger: logger}
	instanceID := storetest.Instance(t, st, clk.Now())
	run, _, err := runs.Enqueue(t.Context(), instanceID, domain.SnatchMissing)
	if err != nil {
		t.Fatalf("Enqueue() = %v", err)
	}
	for range snatch.MaxLeases {
		if _, leaseErr := runs.Lease(t.Context(), "worker"); leaseErr != nil {
			t.Fatalf("Lease() = %v", leaseErr)
		}
		clk.Advance(snatch.LeaseTTL + time.Second)
	}

	stream := openStream(t, bus)
	s.failExhausted(t.Context())

	if !streamHas(stream, "event: run_finished", snatch.ExhaustedReason) {
		t.Errorf("SSE stream has no run_finished frame naming %q", snatch.ExhaustedReason)
	}
	kind := "run_finished"
	rows, err := st.Q().ListEvents(t.Context(), sqlcgen.ListEventsParams{InstanceID: &instanceID, Type: &kind, PageSize: 10})
	if err != nil || len(rows) != 1 || rows[0].Level != "error" || rows[0].RunID == nil || *rows[0].RunID != run.ID {
		t.Fatalf("history run_finished rows = %+v, %v; want one error row for run %s", rows, err, run.ID)
	}
}

// openStream connects one SSE client to the bus and waits until the hub has it.
func openStream(t *testing.T, bus *events.Bus) *bufio.Scanner {
	t.Helper()
	srv := httptest.NewServer(bus.Hub().Handler())
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("SSE request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("SSE connect: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	for range clientWait {
		if bus.Hub().ClientCount() > 0 {
			return bufio.NewScanner(resp.Body)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("SSE client never registered with the hub")
	return nil
}

// streamHas reads up to sseLines lines and reports whether the event line is followed by
// a data line containing want.
func streamHas(sc *bufio.Scanner, event, want string) bool {
	seen := false
	for i := 0; i < sseLines && sc.Scan(); i++ {
		line := sc.Text()
		if line == event {
			seen = true
			continue
		}
		if seen && strings.HasPrefix(line, "data:") && strings.Contains(line, want) {
			return true
		}
	}
	return false
}
