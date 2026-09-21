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
	"github.com/lusoris/SnatchArr/api/internal/policies"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
)

// Bounds (HISS-02).
const (
	maxLeaseWait       = 30 * time.Second
	leasePollInterval  = time.Second
	maxEventsPerStream = 10_000
	maxSearchedPerRun  = 5_000
	userAgent          = "SnatchArr/1.0 (https://github.com/lusoris/SnatchArr)"
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
	budget    *snatch.Budget
	memory    *snatch.Memory
	rec       *snatch.Recorder
	planner   *snatch.Planner
	instances *instances.Service
	policies  *policies.Service
	pacer     Pacer
	clk       clock.Clock
	logger    *slog.Logger
}

// New wires the server.
func New(runs *snatch.Runs, budget *snatch.Budget, memory *snatch.Memory, rec *snatch.Recorder, planner *snatch.Planner,
	inst *instances.Service, pol *policies.Service, clk clock.Clock, logger *slog.Logger, opts Options,
) *Server {
	return &Server{
		runs: runs, budget: budget, memory: memory, rec: rec, planner: planner, instances: inst, policies: pol,
		pacer: opts.Pacer, clk: clk, logger: logger,
	}
}

// LeaseRun long-polls for a queued run and returns it with credentials and policy.
func (s *Server) LeaseRun(ctx context.Context, req *snatcharrv1.LeaseRunRequest) (*snatcharrv1.LeaseRunResponse, error) {
	if req.GetWorkerId() == "" {
		return nil, status.Error(codes.InvalidArgument, "worker_id is required")
	}
	wait := min(time.Duration(req.GetWaitSeconds())*time.Second, maxLeaseWait)
	deadline := s.clk.Now().Add(wait)
	for attempt := 0; attempt <= int(maxLeaseWait/leasePollInterval); attempt++ {
		run, err := s.runs.Lease(ctx, req.GetWorkerId())
		if err == nil {
			return s.leaseResponse(ctx, run)
		}
		if !errors.Is(err, snatch.ErrNoRun) {
			return nil, toStatus(err)
		}
		if !s.clk.Now().Before(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return nil, status.FromContextError(ctx.Err()).Err()
		case <-s.clk.After(leasePollInterval):
		}
	}
	return &snatcharrv1.LeaseRunResponse{}, nil
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
	}}, nil
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
	return &snatcharrv1.FilterCandidatesResponse{UnprocessedIds: ids}, nil
}

// AcquireBudget debits the instance's hourly bucket (schedule overrides applied).
func (s *Server) AcquireBudget(ctx context.Context, req *snatcharrv1.AcquireBudgetRequest) (*snatcharrv1.AcquireBudgetResponse, error) {
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
	g, err := s.budget.Acquire(ctx, run.InstanceID, capacity, int(min(req.GetRequested(), maxSearchedPerRun)))
	if err != nil {
		return nil, toStatus(err)
	}
	if g.Used >= int(float64(g.Cap)*snatch.WarnRatio) {
		s.logger.WarnContext(ctx, "snatch: hourly cap nearly exhausted", slog.String("instance", run.InstanceID.String()), slog.Int("used", g.Used), slog.Int("cap", g.Cap))
	}
	return &snatcharrv1.AcquireBudgetResponse{
		Granted: uint32(g.Granted), RemainingInWindow: uint32(g.Remaining), WindowResetsUnix: g.ResetsAt.Unix(), // #nosec G115 -- non-negative, <= 500
	}, nil
}

// ReportEvents persists a bounded client stream of snatch events.
func (s *Server) ReportEvents(stream snatcharrv1.WorkerService_ReportEventsServer) error {
	ctx := stream.Context()
	accepted := uint32(0)
	for range maxEventsPerStream {
		req, err := stream.Recv()
		if errors.Is(err, errEOF) || (err != nil && errors.Is(err, context.Canceled)) {
			break
		}
		if err != nil {
			if isEOF(err) {
				break
			}
			return toStatus(err)
		}
		if err := s.recordProto(ctx, req.GetEvent()); err != nil {
			return toStatus(err)
		}
		accepted++
	}
	return stream.SendAndClose(&snatcharrv1.ReportEventsResponse{Accepted: accepted})
}

func (s *Server) recordProto(ctx context.Context, ev *snatcharrv1.SnatchEvent) error {
	if ev == nil {
		return nil
	}
	run, err := s.leasedRunDomain(ctx, ev.GetRunId())
	if err != nil {
		return err
	}
	ts := s.clk.Now()
	if ev.GetTsUnixMs() > 0 {
		ts = time.UnixMilli(ev.GetTsUnixMs())
	}
	return s.rec.Record(ctx, domain.Event{
		RunID: &run.ID, InstanceID: run.InstanceID, Timestamp: ts,
		Level: levelName(ev.GetLevel()), Type: ev.GetType().String(),
		EntityType: ev.GetEntityType(), EntityID: ev.GetEntityId(), Title: ev.GetTitle(), Detail: ev.GetDetail(),
	})
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
	return &snatcharrv1.CompleteRunResponse{}, s.rec.Record(ctx, domain.Event{
		RunID: &run.ID, InstanceID: run.InstanceID, Level: "info", Type: "run_finished",
		Title: fmt.Sprintf("%s snatch %s: %d item(s) searched", run.Kind, outcome(req.GetOutcome()), len(searched)), Detail: req.GetError(),
	})
}

func (s *Server) remember(ctx context.Context, run domain.Run, policy domain.Policy, searched []*snatcharrv1.SearchedItem) error {
	byType := map[string][]int64{}
	for _, item := range searched {
		byType[item.GetEntityType()] = append(byType[item.GetEntityType()], item.GetEntityId())
	}
	for entityType, ids := range byType {
		if err := s.memory.Mark(ctx, run.InstanceID, run.Kind, entityType, ids, policy.ProcessedTTL); err != nil {
			return err
		}
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
