// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package workergrpc

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"io"
	"log/slog"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/events"
	snatcharrv1 "github.com/lusoris/SnatchArr/api/internal/gen/snatcharr/v1"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/metrics"
	"github.com/lusoris/SnatchArr/api/internal/metrics/metricstest"
	"github.com/lusoris/SnatchArr/api/internal/policies"
	"github.com/lusoris/SnatchArr/api/internal/seerr"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

// The worker-protocol series (gap audit A1-02).
const (
	runsLeased     = "snatcharr_runs_leased_total"
	runsFinished   = "snatcharr_runs_finished_total"
	workerEvents   = "snatcharr_worker_events_total"
	budgetRequests = "snatcharr_budget_requests_total"
)

// withMetrics gives s Metrics on a registry of the test's own and returns that registry.
func withMetrics(t *testing.T, s *Server) *prometheus.Registry {
	t.Helper()
	m, reg := metricstest.New(t)
	s.m = m
	return reg
}

func TestBudgetResult(t *testing.T) {
	t.Parallel()
	asked := func(n uint32) *snatcharrv1.AcquireBudgetRequest {
		return &snatcharrv1.AcquireBudgetRequest{Requested: n}
	}
	got := func(n uint32) *snatcharrv1.AcquireBudgetResponse {
		return &snatcharrv1.AcquireBudgetResponse{Granted: n}
	}
	items := &snatcharrv1.AcquireBudgetRequest{Requested: 1, Items: []*snatcharrv1.BudgetItem{whole(3), part(4)}}
	cases := []struct {
		name string
		req  *snatcharrv1.AcquireBudgetRequest
		resp *snatcharrv1.AcquireBudgetResponse
		err  error
		want string
	}{
		// Positive.
		{"everything asked", asked(5), got(5), nil, metrics.BudgetGranted},
		{"items count by their sum", items, got(7), nil, metrics.BudgetGranted},
		// Negative.
		{"an error", asked(5), nil, errors.New("schedule"), metrics.BudgetError},
		{"nothing", asked(5), got(0), nil, metrics.BudgetDenied},
		{"a part", asked(5), got(4), nil, metrics.BudgetPartial},
		{"a part of the items' sum", items, got(3), nil, metrics.BudgetPartial},
		// Boundary: asking for nothing gets all of it, and a cost above the per-run bound asks
		// for the bound only.
		{"zero asked", asked(0), got(0), nil, metrics.BudgetGranted},
		{"one less than asked", asked(2), got(1), nil, metrics.BudgetPartial},
		{"above the per-run bound", asked(maxSearchedPerRun + 1), got(maxSearchedPerRun), nil, metrics.BudgetGranted},
		{
			"an item above the per-run bound", &snatcharrv1.AcquireBudgetRequest{Items: []*snatcharrv1.BudgetItem{part(maxSearchedPerRun + 1)}},
			got(maxSearchedPerRun), nil, metrics.BudgetGranted,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := budgetResult(tc.req, tc.resp, tc.err); got != tc.want {
				t.Errorf("budgetResult() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEventType(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   snatcharrv1.EventType
		want string
	}{
		// Positive.
		{"a search failed", snatcharrv1.EventType_EVENT_TYPE_SEARCH_FAILED, "search_failed"},
		{"a run finished", snatcharrv1.EventType_EVENT_TYPE_RUN_FINISHED, "run_finished"},
		// Negative: a value this release does not define cannot grow the label set.
		{"an undefined value", snatcharrv1.EventType(99), "unknown"},
		{"a negative value", snatcharrv1.EventType(-1), "unknown"},
		// Boundary.
		{"unspecified", snatcharrv1.EventType_EVENT_TYPE_UNSPECIFIED, "unspecified"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := eventType(tc.in); got != tc.want {
				t.Errorf("eventType(%d) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// eventTypes names every defined type once and never "unknown".
func TestEventTypes(t *testing.T) {
	t.Parallel()
	got := eventTypes()
	if len(got) != len(snatcharrv1.EventType_name) {
		t.Fatalf("eventTypes() has %d names, want %d", len(got), len(snatcharrv1.EventType_name))
	}
	slices.Sort(got)
	if len(slices.Compact(slices.Clone(got))) != len(got) || slices.Contains(got, "unknown") || !slices.Contains(got, "search_dispatched") {
		t.Errorf("eventTypes() = %v, want each defined name once", got)
	}
}

// New creates every worker event series at zero, so an alert on an increase has a base.
func TestNewCreatesTheEventSeries(t *testing.T) {
	t.Parallel()
	m, reg := metricstest.New(t)
	New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, m, Options{})
	for _, name := range eventTypes() {
		if got, ok := metricstest.Value(t, reg, workerEvents, "type", name); !ok || got != 0 {
			t.Errorf("%s{%s} = %v (present %t), want 0", workerEvents, name, got, ok)
		}
	}
	if got, ok := metricstest.Value(t, reg, workerEvents, "type", "unknown"); ok {
		t.Errorf("%s{unknown} = %v, want no series before an unknown event", workerEvents, got)
	}
}

// Every AcquireBudget is counted by its result (cap 20 per hour).
func TestAcquireBudgetIsCounted(t *testing.T) {
	t.Parallel()
	srv, _, runID := budgetServer(t)
	reg := withMetrics(t, srv)
	for _, req := range []*snatcharrv1.AcquireBudgetRequest{
		{RunId: runID, Requested: 5},  // granted: 5 of 20
		{RunId: runID, Requested: 30}, // partial: the 15 left
		{RunId: runID, Requested: 1},  // denied: the bucket is empty
		{RunId: "not a run", Requested: 1},
	} {
		_, _ = srv.AcquireBudget(t.Context(), req)
	}
	for _, result := range []string{metrics.BudgetGranted, metrics.BudgetPartial, metrics.BudgetDenied, metrics.BudgetError} {
		if got, _ := metricstest.Value(t, reg, budgetRequests, "result", result); got != 1 {
			t.Errorf("%s{%s} = %v, want 1", budgetRequests, result, got)
		}
	}
}

// A leased run is counted by its kind; a LeaseRun that finds none counts nothing.
func TestLeaseRunIsCounted(t *testing.T) {
	t.Parallel()
	srv, _, st := leaseServer(t)
	reg := withMetrics(t, srv)
	if _, err := srv.LeaseRun(t.Context(), &snatcharrv1.LeaseRunRequest{WorkerId: "worker"}); err != nil {
		t.Fatalf("LeaseRun() without a run = %v", err)
	}
	if got, ok := metricstest.Value(t, reg, runsLeased, "kind", string(domain.SnatchMissing)); ok {
		t.Fatalf("%s{missing} = %v without a run, want no series", runsLeased, got)
	}
	enc, err := crypto.NewEncryptor(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("encryptor: %v", err)
	}
	srv.instances = instances.New(st, enc, srv.clk, id.New(), nil, srv.logger)
	inst, err := srv.instances.Create(t.Context(), instances.Input{
		Kind: domain.KindSonarr, Name: "sonarr", BaseURL: "http://sonarr.invalid:8989", APIKey: "a-key-for-the-lease-test", Enabled: true,
	})
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}
	if _, _, err = srv.runs.Enqueue(t.Context(), inst.ID, domain.SnatchMissing); err != nil {
		t.Fatalf("Enqueue() = %v", err)
	}
	if _, err := srv.LeaseRun(t.Context(), &snatcharrv1.LeaseRunRequest{WorkerId: "worker"}); err != nil {
		t.Fatalf("LeaseRun() = %v", err)
	}
	if got, _ := metricstest.Value(t, reg, runsLeased, "kind", string(domain.SnatchMissing)); got != 1 {
		t.Errorf("%s{missing} = %v, want 1", runsLeased, got)
	}
}

// completeFixture is a Server with what WorkerService reaches, its store and
// registry, and one leased run.
type completeFixture struct {
	srv *Server
	reg *prometheus.Registry
	st  *store.Store
	clk *clockwork.FakeClock
	run domain.Run
}

// completeServer is completeFixture's server, registry and run id.
func completeServer(t *testing.T) (*Server, *prometheus.Registry, string) {
	t.Helper()
	f := newCompleteFixture(t)
	return f.srv, f.reg, f.run.ID.String()
}

func newCompleteFixture(t *testing.T) completeFixture {
	t.Helper()
	st := storetest.New(t)
	clk := clock.NewFake()
	logger := slog.New(slog.DiscardHandler)
	runs := snatch.NewRuns(st, clk, id.New())
	rec := snatch.NewRecorder(st, clk, events.New(logger))
	enc, encErr := crypto.NewEncryptor(bytes.Repeat([]byte{0x42}, 32))
	if encErr != nil {
		t.Fatalf("encryptor: %v", encErr)
	}
	inst := instances.New(st, enc, clk, id.New(), nil, logger)
	srv := &Server{
		runs: runs, workers: snatch.NewWorkers(st, clk), memory: snatch.NewMemory(st, clk), rec: rec,
		instances: inst, policies: policies.New(st, clk), seerr: seerr.New(st, enc, clk, id.New(), nil, nil, inst, runs, rec, logger),
		clk: clk, logger: logger, stopping: make(chan struct{}),
	}
	reg := withMetrics(t, srv)
	instanceID := storetest.Instance(t, st, clk.Now())
	run, _, err := srv.runs.Enqueue(t.Context(), instanceID, domain.SnatchMissing)
	if err != nil {
		t.Fatalf("Enqueue() = %v", err)
	}
	if _, err = srv.runs.Lease(t.Context(), "worker"); err != nil {
		t.Fatalf("Lease() = %v", err)
	}
	return completeFixture{srv: srv, reg: reg, st: st, clk: clk, run: run}
}

// A completed run is counted by its outcome; a refused completion counts nothing.
func TestCompleteRunIsCounted(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		outcome snatcharrv1.RunOutcome
		want    domain.RunStatus
	}{
		// Positive.
		{"done", snatcharrv1.RunOutcome_RUN_OUTCOME_DONE, domain.RunDone},
		// Negative.
		{"failed", snatcharrv1.RunOutcome_RUN_OUTCOME_FAILED, domain.RunFailed},
		{"cancelled", snatcharrv1.RunOutcome_RUN_OUTCOME_CANCELLED, domain.RunCancelled},
		// Boundary: an unspecified outcome counts as done.
		{"unspecified", snatcharrv1.RunOutcome_RUN_OUTCOME_UNSPECIFIED, domain.RunDone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, reg, runID := completeServer(t)
			if _, err := srv.CompleteRun(t.Context(), &snatcharrv1.CompleteRunRequest{RunId: uuidOther(runID), WorkerId: "worker", Outcome: tc.outcome}); err == nil {
				t.Fatal("CompleteRun() of an unknown run = nil, want an error")
			}
			if _, err := srv.CompleteRun(t.Context(), &snatcharrv1.CompleteRunRequest{RunId: runID, WorkerId: "worker", Outcome: tc.outcome}); err != nil {
				t.Fatalf("CompleteRun() = %v", err)
			}
			for _, outcome := range []domain.RunStatus{domain.RunDone, domain.RunFailed, domain.RunCancelled} {
				want := 0.0
				if outcome == tc.want {
					want = 1
				}
				if got, ok := metricstest.Value(t, reg, runsFinished, "outcome", string(outcome)); !ok || got != want {
					t.Errorf("%s{%s} = %v (present %t), want %v", runsFinished, outcome, got, ok, want)
				}
			}
		})
	}
}

// A completion with unconfirmed events counts them and warns on the run before run_finished;
// a completion without counts nothing and warns nothing.
func TestCompleteRunCountsUnconfirmedEvents(t *testing.T) {
	t.Parallel()
	const series = "snatcharr_worker_events_unconfirmed_total"
	cases := []struct {
		name string
		n    uint32
	}{
		// Positive.
		{"three unconfirmed", 3},
		// Boundary.
		{"one unconfirmed", 1},
		{"the largest count", math.MaxUint32},
		// Negative: every event confirmed.
		{"none unconfirmed", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newCompleteFixture(t)
			req := &snatcharrv1.CompleteRunRequest{RunId: f.run.ID.String(), WorkerId: "worker", EventsUnconfirmed: tc.n}
			if _, err := f.srv.CompleteRun(t.Context(), req); err != nil {
				t.Fatalf("CompleteRun() = %v", err)
			}
			if got, ok := metricstest.Value(t, f.reg, series, "", ""); !ok || got != float64(tc.n) {
				t.Errorf("%s = %v (present %t), want %d", series, got, ok, tc.n)
			}
			rows, err := f.st.Q().ListEvents(t.Context(), sqlcgen.ListEventsParams{InstanceID: &f.run.InstanceID, PageSize: 10})
			if err != nil {
				t.Fatalf("ListEvents() = %v", err)
			}
			want := unconfirmedHistory(tc.n)
			if got := historyTypes(rows); !slices.Equal(got, want) {
				t.Fatalf("history = %v, want %v", got, want)
			}
			if title := rows[0].Title; tc.n > 0 && !strings.HasPrefix(title, strconv.FormatUint(uint64(tc.n), 10)+" event(s)") {
				t.Errorf("warning title = %q, want it to start with the count %d", title, tc.n)
			}
		})
	}
}

// historyTypes sorts rows oldest first and lists the level and type of each.
func historyTypes(rows []sqlcgen.SnatchEvent) []string {
	slices.SortFunc(rows, func(a, b sqlcgen.SnatchEvent) int { return cmp.Compare(a.ID, b.ID) })
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Level+" "+r.Type)
	}
	return out
}

// unconfirmedHistory is the history a completed run with n unconfirmed events leaves.
func unconfirmedHistory(n uint32) []string {
	if n == 0 {
		return []string{"info run_finished"}
	}
	return []string{"warn events_unconfirmed", "info run_finished"}
}

// uuidOther is a well-formed run id other than id.
func uuidOther(id string) string {
	if id == "00000000-0000-4000-8000-000000000001" {
		return "00000000-0000-4000-8000-000000000002"
	}
	return "00000000-0000-4000-8000-000000000001"
}

// eventStream is a ReportEvents stream that hands out reqs, then io.EOF.
type eventStream struct {
	grpc.ServerStream
	ctx  context.Context
	reqs []*snatcharrv1.ReportEventsRequest
	resp *snatcharrv1.ReportEventsResponse
}

func (s *eventStream) Context() context.Context { return s.ctx }

func (s *eventStream) Recv() (*snatcharrv1.ReportEventsRequest, error) {
	if len(s.reqs) == 0 {
		return nil, io.EOF
	}
	req := s.reqs[0]
	s.reqs = s.reqs[1:]
	return req, nil
}

func (s *eventStream) SendAndClose(resp *snatcharrv1.ReportEventsResponse) error {
	s.resp = resp
	return nil
}

// A recorded worker event is counted by its type, an undefined type as "unknown"; an empty
// request and a refused event count nothing.
func TestReportEventsIsCounted(t *testing.T) {
	t.Parallel()
	srv, reg, runID := completeServer(t)
	event := func(runID string, typ snatcharrv1.EventType) *snatcharrv1.ReportEventsRequest {
		return &snatcharrv1.ReportEventsRequest{Event: &snatcharrv1.SnatchEvent{RunId: runID, Type: typ, Title: "t"}}
	}
	stream := &eventStream{ctx: t.Context(), reqs: []*snatcharrv1.ReportEventsRequest{
		event(runID, snatcharrv1.EventType_EVENT_TYPE_SEARCH_FAILED),
		event(runID, snatcharrv1.EventType_EVENT_TYPE_SEARCH_FAILED),
		event(runID, snatcharrv1.EventType(99)),
		{},
	}}
	if err := srv.ReportEvents(stream); err != nil || stream.resp.GetAccepted() != 4 {
		t.Fatalf("ReportEvents() = %v, accepted %d; want nil, 4", err, stream.resp.GetAccepted())
	}
	refused := &eventStream{ctx: t.Context(), reqs: []*snatcharrv1.ReportEventsRequest{event(uuidOther(runID), snatcharrv1.EventType_EVENT_TYPE_RUN_STARTED)}}
	if err := srv.ReportEvents(refused); err == nil {
		t.Fatal("ReportEvents() of an unknown run = nil, want an error")
	}
	for name, want := range map[string]float64{"search_failed": 2, "unknown": 1, "run_started": 0, "unspecified": 0} {
		if got, _ := metricstest.Value(t, reg, workerEvents, "type", name); got != want {
			t.Errorf("%s{%s} = %v, want %v", workerEvents, name, got, want)
		}
	}
}

func TestLeaseRun_RecordsWaitTime(t *testing.T) {
	t.Parallel()
	f := newCompleteFixture(t)
	inst, err := f.srv.instances.Create(t.Context(), instances.Input{
		Kind: domain.KindSonarr, Name: "sonarr-wait", BaseURL: "http://sonarr.invalid:8989", APIKey: "a-key-for-the-wait-test", Enabled: true,
	})
	if err != nil {
		t.Fatalf("instances.Create() = %v", err)
	}
	if _, _, err := f.srv.runs.Enqueue(t.Context(), inst.ID, domain.SnatchUpgrade); err != nil {
		t.Fatalf("Enqueue() = %v", err)
	}
	f.clk.Advance(42 * time.Second)
	if _, err := f.srv.LeaseRun(t.Context(), &snatcharrv1.LeaseRunRequest{WorkerId: "worker-wait"}); err != nil {
		t.Fatalf("LeaseRun() = %v", err)
	}
	if got, ok := metricstest.Value(t, f.reg, "snatcharr_run_lease_wait_seconds", "kind", string(domain.SnatchUpgrade)); !ok || got != 1 {
		t.Errorf("snatcharr_run_lease_wait_seconds{upgrade} = %v (present %t), want 1", got, ok)
	}
}

func TestReportEvents_RecordsSearchesDispatched(t *testing.T) {
	t.Parallel()
	f := newCompleteFixture(t)
	inst, err := f.srv.instances.Get(t.Context(), f.run.InstanceID)
	if err != nil {
		t.Fatalf("instances.Get() = %v", err)
	}
	event := func(typ snatcharrv1.EventType) *snatcharrv1.ReportEventsRequest {
		return &snatcharrv1.ReportEventsRequest{Event: &snatcharrv1.SnatchEvent{
			RunId: f.run.ID.String(), Type: typ, Title: "Item 1", EntityType: "movie", EntityId: 101,
		}}
	}
	stream := &eventStream{ctx: t.Context(), reqs: []*snatcharrv1.ReportEventsRequest{
		event(snatcharrv1.EventType_EVENT_TYPE_SEARCH_DISPATCHED),
		event(snatcharrv1.EventType_EVENT_TYPE_SEARCH_DISPATCHED),
	}}
	if err := f.srv.ReportEvents(stream); err != nil || stream.resp.GetAccepted() != 2 {
		t.Fatalf("ReportEvents() = %v, accepted %d; want nil, 2", err, stream.resp.GetAccepted())
	}
	const series = "snatcharr_searches_dispatched_total"
	if got, ok := metricstest.ValueLabels(t, f.reg, series, map[string]string{
		"instance": inst.Name,
		"kind":     string(f.run.Kind),
	}); !ok || got != 2 {
		t.Errorf("%s{instance=%s,kind=%s} = %v (present %t), want 2", series, inst.Name, f.run.Kind, got, ok)
	}
}

func TestInstanceName_FallbackUnknown(t *testing.T) {
	t.Parallel()
	f := newCompleteFixture(t)
	names := make(map[uuid.UUID]string)
	unknownID := uuid.New()
	got := f.srv.instanceName(t.Context(), names, unknownID)
	if got != "unknown" {
		t.Errorf("instanceName(%s) = %q, want %q", unknownID, got, "unknown")
	}
	if cached, ok := names[unknownID]; !ok || cached != "unknown" {
		t.Errorf("names[%s] = %q (present %t), want %q", unknownID, cached, ok, "unknown")
	}
}
