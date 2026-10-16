// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package workergrpc serves snatcharr.v1.WorkerService: the lease/pull contract the Rust
// snatch-worker executes against (ADR-0002).
package workergrpc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/fx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/dlclients"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	snatcharrv1 "github.com/lusoris/SnatchArr/api/internal/gen/snatcharr/v1"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/metrics"
	"github.com/lusoris/SnatchArr/api/internal/policies"
	"github.com/lusoris/SnatchArr/api/internal/seerr"
	"github.com/lusoris/SnatchArr/api/internal/settings"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
)

// Bounds (HISS-02).
const (
	maxLeaseWait       = 30 * time.Second
	leasePollInterval  = time.Second
	maxEventsPerStream = 10_000
	maxSearchedPerRun  = 5_000
	maxBudgetItems     = 512
	userAgent          = "SnatchArr/1.0 (https://github.com/lusoris/SnatchArr)"
	// stoppingMessage goes out with Unavailable once the control plane shuts down.
	stoppingMessage = "control plane is shutting down"
)

// Pacer scales a leased run's per-cycle count by download-client bandwidth (ADR-0006).
type Pacer interface {
	PaceFactor(ctx context.Context, instanceID uuid.UUID) (float64, error)
}

// Options are the optional collaborators.
type Options struct {
	fx.In
	Pacer Pacer `optional:"true"`
}

// Server implements snatcharrv1.WorkerServiceServer.
type Server struct {
	snatcharrv1.UnimplementedWorkerServiceServer
	runs      *snatch.Runs
	workers   *snatch.Workers
	budget    *snatch.Budget
	memory    *snatch.Memory
	rec       *snatch.Recorder
	planner   *snatch.Planner
	instances *instances.Service
	policies  *policies.Service
	settings  *settings.Service
	seerr     *seerr.Service
	pacer     Pacer
	clk       clock.Clock
	logger    *slog.Logger
	m         *metrics.Metrics
	// stopping is closed by Shutdown.
	stopping chan struct{}
	stopOnce sync.Once
}

// New wires the server.
func New(runs *snatch.Runs, workers *snatch.Workers, budget *snatch.Budget, memory *snatch.Memory, rec *snatch.Recorder, planner *snatch.Planner,
	inst *instances.Service, pol *policies.Service, cfg *settings.Service, requests *seerr.Service, clk clock.Clock, logger *slog.Logger, m *metrics.Metrics,
	opts Options,
) *Server {
	m.WorkerEventTypes(eventTypes()...)
	return &Server{
		runs: runs, workers: workers, budget: budget, memory: memory, rec: rec, planner: planner, instances: inst, policies: pol,
		settings: cfg, seerr: requests, pacer: opts.Pacer, clk: clk, logger: logger, m: m,
		stopping: make(chan struct{}),
	}
}

// Shutdown ends every waiting LeaseRun with Unavailable and has later ones answer the same,
// so the gRPC server's graceful stop does not wait for a long-poll and a worker reads a
// planned stop as retryable (#146). Calling it again is a no-op.
func (s *Server) Shutdown() {
	s.stopOnce.Do(func() { close(s.stopping) })
}

func (s *Server) isStopping() bool {
	select {
	case <-s.stopping:
		return true
	default:
		return false
	}
}

// pause waits one poll interval; the caller's context or a shutdown ends it early.
func (s *Server) pause(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return status.FromContextError(ctx.Err()).Err()
	case <-s.stopping:
		return status.Error(codes.Unavailable, stoppingMessage)
	case <-s.clk.After(leasePollInterval):
		return nil
	}
}

// touch records the worker's contact for the system status. A failed write is logged and
// does not fail the call: the status would read stale, the run itself is unaffected.
func (s *Server) touch(ctx context.Context, workerID string) {
	if err := s.workers.Seen(ctx, workerID); err != nil {
		s.logger.WarnContext(ctx, "workergrpc: record worker contact", slog.String("worker_id", workerID), slog.String("error", err.Error()))
	}
}

// LeaseRun long-polls for a queued run and returns it with credentials and policy.
func (s *Server) LeaseRun(ctx context.Context, req *snatcharrv1.LeaseRunRequest) (*snatcharrv1.LeaseRunResponse, error) {
	if req.GetWorkerId() == "" {
		return nil, status.Error(codes.InvalidArgument, "worker_id is required")
	}
	s.touch(ctx, req.GetWorkerId())
	s.failExhausted(ctx)
	wait := min(time.Duration(req.GetWaitSeconds())*time.Second, maxLeaseWait)
	deadline := s.clk.Now().Add(wait)
	for attempt := 0; attempt <= int(maxLeaseWait/leasePollInterval); attempt++ {
		if s.isStopping() {
			return nil, status.Error(codes.Unavailable, stoppingMessage)
		}
		run, err := s.runs.Lease(ctx, req.GetWorkerId())
		if err == nil {
			s.recordLeased(run)
			return s.leaseResponse(ctx, run)
		}
		if !errors.Is(err, snatch.ErrNoRun) {
			return nil, toStatus(err)
		}
		if !s.clk.Now().Before(deadline) {
			break
		}
		if pauseErr := s.pause(ctx); pauseErr != nil {
			return nil, pauseErr
		}
	}
	return &snatcharrv1.LeaseRunResponse{}, nil
}

func (s *Server) recordLeased(run domain.Run) {
	s.m.RunLeased(string(run.Kind))
	if !run.QueuedAt.IsZero() {
		wait := s.clk.Now().Sub(run.QueuedAt).Seconds()
		if wait >= 0 {
			s.m.RunLeaseWait(string(run.Kind), wait)
		}
	}
}

func (s *Server) leaseResponse(ctx context.Context, run domain.Run) (*snatcharrv1.LeaseRunResponse, error) {
	inst, err := s.instances.Get(ctx, run.InstanceID)
	if err != nil {
		return nil, toStatus(err)
	}
	creds, err := s.instances.Credentials(ctx, run.InstanceID)
	if err != nil {
		return nil, toStatus(err)
	}
	policy, err := s.policies.Get(ctx, run.InstanceID)
	if err != nil {
		return nil, toStatus(err)
	}
	var expires int64
	if run.LeaseExpiresAt != nil {
		expires = run.LeaseExpiresAt.Unix()
	}
	pol := policyProto(policy, run.Kind)
	s.pace(ctx, run, pol)
	return &snatcharrv1.LeaseRunResponse{Run: &snatcharrv1.Run{
		RunId:            run.ID.String(),
		InstanceId:       run.InstanceID.String(),
		App:              appKind(inst.Kind),
		Snatch:           snatchKind(run.Kind),
		BaseUrl:          creds.BaseURL,
		ApiKey:           creds.APIKey,
		Policy:           pol,
		LeaseExpiresUnix: expires,
		UserAgent:        userAgent,
		FocusEntityId:    deref64(run.FocusEntityID),
		FocusGroupId:     deref64(run.FocusGroupID),
	}}, nil
}

func deref64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// pace applies download-client bandwidth pacing to the per-cycle count. A pacer failure
// is logged and leaves the count alone: pacing is a courtesy, not a gate.
func (s *Server) pace(ctx context.Context, run domain.Run, pol *snatcharrv1.Policy) {
	if s.pacer == nil || pol.GetPerCycle() == 0 {
		return
	}
	factor, err := s.pacer.PaceFactor(ctx, run.InstanceID)
	if err != nil {
		s.logger.WarnContext(ctx, "workergrpc: pace factor", slog.String("error", err.Error()))
		return
	}
	before := int(pol.GetPerCycle())
	scaled := dlclients.Scale(before, factor)
	if scaled == before {
		return
	}
	pol.PerCycle = uint32(max(scaled, 0)) // #nosec G115 -- scaled <= before <= 100
	err = s.rec.Record(ctx, domain.Event{
		RunID: &run.ID, InstanceID: run.InstanceID, Level: "info", Type: "snatch_paced",
		Title: fmt.Sprintf("Foreplay: per-cycle reduced from %d to %d by download-client pacing", before, scaled),
	})
	if err != nil {
		s.logger.WarnContext(ctx, "workergrpc: record pacing", slog.String("error", err.Error()))
	}
}

// Heartbeat extends a lease and tells a worker when its run was cancelled.
func (s *Server) Heartbeat(ctx context.Context, req *snatcharrv1.HeartbeatRequest) (*snatcharrv1.HeartbeatResponse, error) {
	runID, err := parseRun(req.GetRunId())
	if err != nil {
		return nil, err
	}
	if req.GetWorkerId() != "" {
		s.touch(ctx, req.GetWorkerId())
	}
	run, err := s.runs.Heartbeat(ctx, runID, req.GetWorkerId())
	if err != nil {
		return nil, toStatus(err)
	}
	directive := snatcharrv1.HeartbeatDirective_HEARTBEAT_DIRECTIVE_CONTINUE
	if run.Status != domain.RunLeased {
		directive = snatcharrv1.HeartbeatDirective_HEARTBEAT_DIRECTIVE_CANCEL
	}
	var expires int64
	if run.LeaseExpiresAt != nil {
		expires = run.LeaseExpiresAt.Unix()
	}
	return &snatcharrv1.HeartbeatResponse{Directive: directive, LeaseExpiresUnix: expires}, nil
}

// FilterCandidates strips ids that are still in processed memory.
func (s *Server) FilterCandidates(ctx context.Context, req *snatcharrv1.FilterCandidatesRequest) (*snatcharrv1.FilterCandidatesResponse, error) {
	run, err := s.leasedRun(ctx, req.GetRunId())
	if err != nil {
		return nil, err
	}
	ids, err := s.memory.FilterUnprocessed(ctx, run.InstanceID, run.Kind, req.GetEntityType(), req.GetEntityIds())
	if err != nil {
		return nil, toStatus(err)
	}
	resp := &snatcharrv1.FilterCandidatesResponse{UnprocessedIds: ids}
	s.addPriorities(ctx, run, resp)
	return resp, nil
}

// addPriorities attaches the entities open Seerr requests point at: movie ids for
// Radarr-shaped instances, series ids (groups) for Sonarr-shaped ones. A lookup failure
// only costs the priority, never the run.
func (s *Server) addPriorities(ctx context.Context, run domain.Run, resp *snatcharrv1.FilterCandidatesResponse) {
	inst, err := s.instances.Get(ctx, run.InstanceID)
	if err != nil {
		return
	}
	ids, err := s.seerr.PriorityFor(ctx, run.InstanceID)
	if err != nil {
		s.logger.WarnContext(ctx, "workergrpc: seerr priorities", slog.String("error", err.Error()))
		return
	}
	switch inst.Kind {
	case domain.KindRadarr, domain.KindWhisparrV3:
		resp.PriorityIds = ids
	case domain.KindSonarr, domain.KindWhisparrV2:
		resp.PriorityGroupIds = ids
	case domain.KindLidarr, domain.KindReadarr:
		// Seerr has no music or book requests.
	}
}

// AcquireBudget debits the instance's hourly bucket (schedule overrides applied). A request
// that names its items is granted item by item and debited only for what it can use (#142).
// Every request is counted by its result.
func (s *Server) AcquireBudget(ctx context.Context, req *snatcharrv1.AcquireBudgetRequest) (*snatcharrv1.AcquireBudgetResponse, error) {
	resp, err := s.acquireBudget(ctx, req)
	s.m.BudgetRequested(budgetResult(req, resp, err))
	return resp, err
}

// budgetResult names the outcome of a budget request for the budget counter.
func budgetResult(req *snatcharrv1.AcquireBudgetRequest, resp *snatcharrv1.AcquireBudgetResponse, err error) string {
	asked := askedStamina(req)
	switch {
	case err != nil:
		return metrics.BudgetError
	case int(resp.GetGranted()) >= asked:
		return metrics.BudgetGranted
	case resp.GetGranted() == 0:
		return metrics.BudgetDenied
	default:
		return metrics.BudgetPartial
	}
}

// askedStamina is the stamina a request asks for, each cost capped the way grantBudget caps it.
func askedStamina(req *snatcharrv1.AcquireBudgetRequest) int {
	if len(req.GetItems()) == 0 {
		return int(min(req.GetRequested(), maxSearchedPerRun))
	}
	total := 0
	for _, item := range req.GetItems() {
		total += int(min(item.GetCost(), maxSearchedPerRun))
	}
	return total
}

func (s *Server) acquireBudget(ctx context.Context, req *snatcharrv1.AcquireBudgetRequest) (*snatcharrv1.AcquireBudgetResponse, error) {
	if len(req.GetItems()) > maxBudgetItems {
		return nil, status.Errorf(codes.InvalidArgument, "items must hold at most %d entries", maxBudgetItems)
	}
	run, err := s.leasedRun(ctx, req.GetRunId())
	if err != nil {
		return nil, err
	}
	policy, err := s.policies.Get(ctx, run.InstanceID)
	if err != nil {
		return nil, toStatus(err)
	}
	capacity, err := s.planner.EffectiveCap(ctx, run.InstanceID, policy.HourlyCap)
	if err != nil {
		return nil, toStatus(err)
	}
	cfg, err := s.settings.Get(ctx)
	if err != nil {
		return nil, toStatus(err)
	}
	g, err := s.grantBudget(ctx, run.InstanceID, capacity, cfg.GlobalHourlyCap, req)
	if err != nil {
		return nil, toStatus(err)
	}
	s.checkBudgetWarning(ctx, run, g)
	grantedItems := make([]uint32, len(g.GrantedItems))
	for i, granted := range g.GrantedItems {
		grantedItems[i] = uint32(granted) // #nosec G115 -- non-negative, <= 500
	}
	return &snatcharrv1.AcquireBudgetResponse{
		Granted: uint32(g.Granted), RemainingInWindow: uint32(g.Remaining), WindowResetsUnix: g.ResetsAt.Unix(), // #nosec G115 -- non-negative, <= 500
		GrantedItems: grantedItems,
	}, nil
}

func (s *Server) checkBudgetWarning(ctx context.Context, run domain.Run, g snatch.ItemsGrant) {
	if g.Used >= int(float64(g.Cap)*snatch.WarnRatio) {
		s.logger.WarnContext(ctx, "snatch: hourly cap nearly exhausted", slog.String("instance", run.InstanceID.String()), slog.Int("used", g.Used), slog.Int("cap", g.Cap))
	}
	if g.Granted > 0 && g.Remaining == 0 && s.rec != nil {
		if rErr := s.rec.Record(ctx, domain.Event{
			InstanceID: run.InstanceID,
			RunID:      &run.ID,
			Level:      "warn",
			Type:       "stamina_exhausted",
			Title:      "Stamina exhausted",
			Detail:     fmt.Sprintf("hourly cap (%d) exhausted", g.Cap),
		}); rErr != nil {
			s.logger.WarnContext(ctx, "workergrpc: record stamina exhausted error", slog.String("error", rErr.Error()))
		}
	}
}

// grantBudget asks the budget item by item when the worker named its items, and for the
// plain total when it did not (a worker from before #142).
func (s *Server) grantBudget(ctx context.Context, instanceID uuid.UUID, capacity, globalCap int, req *snatcharrv1.AcquireBudgetRequest) (snatch.ItemsGrant, error) {
	if len(req.GetItems()) == 0 {
		g, err := s.budget.Acquire(ctx, instanceID, capacity, globalCap, int(min(req.GetRequested(), maxSearchedPerRun)))
		return snatch.ItemsGrant{Grant: g}, err
	}
	items := make([]snatch.BudgetItem, len(req.GetItems()))
	for i, item := range req.GetItems() {
		// No cap reaches maxSearchedPerRun, so a larger cost is granted the same way.
		items[i] = snatch.BudgetItem{Cost: int(min(item.GetCost(), maxSearchedPerRun)), Atomic: item.GetAtomic()}
	}
	return s.budget.AcquireItems(ctx, instanceID, capacity, globalCap, items)
}

// ReportEvents persists a bounded client stream of snatch events.
func (s *Server) ReportEvents(stream snatcharrv1.WorkerService_ReportEventsServer) error {
	ctx := stream.Context()
	accepted := uint32(0)
	names := make(map[uuid.UUID]string)
	for range maxEventsPerStream {
		req, done, err := recvReportEvent(stream)
		if done || err != nil {
			if err != nil {
				return toStatus(err)
			}
			break
		}
		if err := s.handleReportEvent(ctx, req.GetEvent(), names); err != nil {
			return toStatus(err)
		}
		accepted++
	}
	return stream.SendAndClose(&snatcharrv1.ReportEventsResponse{Accepted: accepted})
}

func recvReportEvent(stream snatcharrv1.WorkerService_ReportEventsServer) (*snatcharrv1.ReportEventsRequest, bool, error) {
	req, err := stream.Recv()
	if errors.Is(err, errEOF) || (err != nil && errors.Is(err, context.Canceled)) || isEOF(err) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	return req, false, nil
}

func (s *Server) handleReportEvent(ctx context.Context, ev *snatcharrv1.SnatchEvent, names map[uuid.UUID]string) error {
	run, err := s.recordProto(ctx, ev)
	if err != nil {
		return err
	}
	if ev == nil {
		return nil
	}
	s.m.WorkerEvent(eventType(ev.GetType()))
	if ev.GetType() == snatcharrv1.EventType_EVENT_TYPE_SEARCH_DISPATCHED && run.InstanceID != uuid.Nil {
		name := s.instanceName(ctx, names, run.InstanceID)
		s.m.SearchesDispatched(name, string(run.Kind), 1)
		if s.seerr != nil && s.rec != nil {
			s.recordSeerrSnatched(ctx, run, ev)
		}
	}
	return nil
}

func (s *Server) recordSeerrSnatched(ctx context.Context, run domain.Run, ev *snatcharrv1.SnatchEvent) {
	seerrReq, err := s.seerr.FindByEntity(ctx, run.InstanceID, ev.GetEntityId())
	if err != nil {
		return
	}
	if rErr := s.rec.Record(ctx, domain.Event{
		RunID:      &run.ID,
		InstanceID: run.InstanceID,
		Timestamp:  s.clk.Now(),
		Level:      "info",
		Type:       "seerr_request_snatched",
		EntityType: ev.GetEntityType(),
		EntityID:   ev.GetEntityId(),
		Title:      "Seerr request snatched: " + seerrReq.Title,
		Detail:     "Requested by " + seerrReq.RequestedBy,
	}); rErr != nil {
		s.logger.WarnContext(ctx, "workergrpc: record seerr snatched error", slog.String("error", rErr.Error()))
	}
}

func (s *Server) instanceName(ctx context.Context, names map[uuid.UUID]string, id uuid.UUID) string {
	if name, ok := names[id]; ok {
		return name
	}
	inst, err := s.instances.Get(ctx, id)
	if err != nil {
		s.logger.WarnContext(ctx, "workergrpc: resolve instance name",
			slog.String("instance_id", id.String()),
			slog.String("error", fmt.Errorf("instances get: %w", err).Error()),
		)
		names[id] = "unknown"
		return "unknown"
	}
	if inst.Name == "" {
		names[id] = "unknown"
		return "unknown"
	}
	names[id] = inst.Name
	return inst.Name
}

func (s *Server) recordProto(ctx context.Context, ev *snatcharrv1.SnatchEvent) (domain.Run, error) {
	if ev == nil {
		return domain.Run{}, nil
	}
	run, err := s.leasedRunDomain(ctx, ev.GetRunId())
	if err != nil {
		return domain.Run{}, err
	}
	ts := s.clk.Now()
	if ev.GetTsUnixMs() > 0 {
		ts = time.UnixMilli(ev.GetTsUnixMs())
	}
	if err := s.rec.Record(ctx, domain.Event{
		RunID: &run.ID, InstanceID: run.InstanceID, Timestamp: ts,
		Level: levelName(ev.GetLevel()), Type: ev.GetType().String(),
		EntityType: ev.GetEntityType(), EntityID: ev.GetEntityId(), Title: ev.GetTitle(), Detail: ev.GetDetail(),
	}); err != nil {
		return domain.Run{}, err
	}
	return run, nil
}

// CompleteRun closes the run, records searched items into memory and saves the cursor.
func (s *Server) CompleteRun(ctx context.Context, req *snatcharrv1.CompleteRunRequest) (*snatcharrv1.CompleteRunResponse, error) {
	run, err := s.leasedRun(ctx, req.GetRunId())
	if err != nil {
		return nil, err
	}
	policy, err := s.policies.Get(ctx, run.InstanceID)
	if err != nil {
		return nil, toStatus(err)
	}
	searched := req.GetSearched()
	if len(searched) > maxSearchedPerRun {
		searched = searched[:maxSearchedPerRun]
	}
	if err := s.remember(ctx, run, policy, searched); err != nil {
		return nil, toStatus(err)
	}
	if req.GetCursor() != "" {
		if err := s.policies.SaveCursor(ctx, run.InstanceID, run.Kind, req.GetCursor()); err != nil {
			return nil, toStatus(err)
		}
	}
	if _, err := s.runs.Complete(ctx, run.ID, req.GetWorkerId(), outcome(req.GetOutcome()), len(searched), req.GetError()); err != nil {
		return nil, toStatus(err)
	}
	s.m.RunFinished(string(outcome(req.GetOutcome())))
	if outcome(req.GetOutcome()) == domain.RunFailed {
		s.recordBackoff(ctx, run, policy)
	}
	s.recordUnconfirmed(ctx, run, req.GetEventsUnconfirmed())
	level := runOutcomeLevel(outcome(req.GetOutcome()))
	// Event is recorded after runs.Complete so trailing status queries see the updated run state.
	return &snatcharrv1.CompleteRunResponse{}, s.rec.Record(ctx, domain.Event{
		RunID: &run.ID, InstanceID: run.InstanceID, Level: level, Type: "run_finished",
		Title: fmt.Sprintf("%s snatch %s: %d item(s) searched", run.Kind, outcome(req.GetOutcome()), len(searched)), Detail: req.GetError(),
	})
}

func runOutcomeLevel(status domain.RunStatus) string {
	switch status {
	case domain.RunFailed:
		return "error"
	case domain.RunCancelled:
		return "warn"
	case domain.RunDone, domain.RunQueued, domain.RunLeased:
		return "info"
	}
	return "info"
}

// failExhausted ends the runs whose lease expired snatch.MaxLeases times and records each
// as a failed run, so history, the SSE stream and the planner's backoff see it like any
// other failure. A failure here is logged, never surfaced to the worker asking for a lease.
func (s *Server) failExhausted(ctx context.Context) {
	runs, err := s.runs.FailExhausted(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "workergrpc: fail exhausted leases", slog.String("error", err.Error()))
		return
	}
	for _, run := range runs {
		s.m.RunFinished(metrics.OutcomeLeaseExhausted)
		if err := s.rec.Record(ctx, domain.Event{
			RunID: &run.ID, InstanceID: run.InstanceID, Level: "error", Type: "run_finished",
			Title: fmt.Sprintf("%s snatch failed", run.Kind), Detail: snatch.ExhaustedReason,
		}); err != nil {
			s.logger.WarnContext(ctx, "workergrpc: record exhausted run", slog.String("error", err.Error()))
		}
		policy, err := s.policies.Get(ctx, run.InstanceID)
		if err != nil {
			s.logger.WarnContext(ctx, "workergrpc: policy of exhausted run", slog.String("error", err.Error()))
			continue
		}
		s.recordBackoff(ctx, run, policy)
	}
}

// recordUnconfirmed counts the n events the worker emitted for run but could not confirm and,
// when there are any, warns on the run that its history may be incomplete. A recording
// failure is logged, never surfaced to the worker.
func (s *Server) recordUnconfirmed(ctx context.Context, run domain.Run, n uint32) {
	if n == 0 {
		return
	}
	s.m.EventsUnconfirmed(n)
	err := s.rec.Record(ctx, domain.Event{
		RunID: &run.ID, InstanceID: run.InstanceID, Level: "warn", Type: "events_unconfirmed",
		Title: fmt.Sprintf("%d event(s) of this run were not confirmed; its history may be incomplete", n),
	})
	if err != nil {
		s.logger.WarnContext(ctx, "workergrpc: record unconfirmed events", slog.String("error", err.Error()))
	}
}

// recordBackoff writes one history entry per failed run saying how long the circuit
// stays open; a recording failure is logged, never surfaced to the worker.
func (s *Server) recordBackoff(ctx context.Context, run domain.Run, policy domain.Policy) {
	failures, err := s.runs.TrailingFailures(ctx, run.InstanceID, run.Kind)
	if err != nil {
		s.logger.WarnContext(ctx, "workergrpc: trailing failures", slog.String("error", err.Error()))
		return
	}
	wait := snatch.Backoff(policy.CycleInterval, failures)
	err = s.rec.Record(ctx, domain.Event{
		RunID: &run.ID, InstanceID: run.InstanceID, Level: "warn", Type: "snatch_backoff",
		Title: fmt.Sprintf("Circuit open: %d failure(s) in a row; next %s snatch not before %s", failures, run.Kind, s.clk.Now().Add(wait).UTC().Format(time.RFC3339)),
	})
	if err != nil {
		s.logger.WarnContext(ctx, "workergrpc: record backoff", slog.String("error", err.Error()))
	}
}

func (s *Server) remember(ctx context.Context, run domain.Run, policy domain.Policy, searched []*snatcharrv1.SearchedItem) error {
	byType := map[string][]int64{}
	requested := make([]int64, 0, len(searched))
	for _, item := range searched {
		byType[item.GetEntityType()] = append(byType[item.GetEntityType()], item.GetEntityId())
		// Seerr requests point at movies (the entity) or series (the group).
		if item.GetEntityType() == "movie" {
			requested = append(requested, item.GetEntityId())
		} else if item.GetGroupId() > 0 {
			requested = append(requested, item.GetGroupId())
		}
	}
	for entityType, ids := range byType {
		if err := s.memory.Mark(ctx, run.InstanceID, run.Kind, entityType, ids, policy.ProcessedTTL, policy.AfterglowMax); err != nil {
			return err
		}
	}
	if err := s.seerr.MarkSnatched(ctx, run.InstanceID, requested); err != nil {
		s.logger.WarnContext(ctx, "workergrpc: mark seerr requests", slog.String("error", err.Error()))
	}
	return nil
}

func (s *Server) leasedRun(ctx context.Context, rawID string) (domain.Run, error) {
	run, err := s.leasedRunDomain(ctx, rawID)
	if err != nil {
		return domain.Run{}, err
	}
	return run, nil
}

func (s *Server) leasedRunDomain(ctx context.Context, rawID string) (domain.Run, error) {
	runID, err := parseRun(rawID)
	if err != nil {
		return domain.Run{}, err
	}
	run, err := s.runs.Get(ctx, runID)
	if err != nil {
		return domain.Run{}, toStatus(err)
	}
	if run.Status != domain.RunLeased && run.Status != domain.RunCancelled {
		return domain.Run{}, status.Errorf(codes.FailedPrecondition, "run %s is %s", run.ID, run.Status)
	}
	return run, nil
}

func parseRun(raw string) (uuid.UUID, error) {
	runID, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, status.Error(codes.InvalidArgument, "run_id must be a UUID")
	}
	return runID, nil
}

func toStatus(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, domain.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, domain.ErrInvalid):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, domain.ErrConflict):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}
