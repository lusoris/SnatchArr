// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Command fakeworker is a lightweight test worker that connects to SnatchArr's gRPC WorkerService,
// leases runs, and completes them with a designated outcome (failed or done).
// It is intended for end-to-end integration and smoke tests.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	snatcharrv1 "github.com/lusoris/SnatchArr/api/internal/gen/snatcharr/v1"
)

type config struct {
	grpcAddr string
	workerID string
	outcome  string
	count    int
	timeout  time.Duration
	token    string
	instance string
}

const (
	defaultGRPCAddr    = "127.0.0.1:19090"
	defaultWorkerID    = "fakeworker-1"
	defaultOutcome     = "failed"
	defaultCount       = 1
	defaultTimeout     = 10 * time.Second
	defaultWorkerToken = "smoke-worker-token"
	leaseWaitSeconds   = 1
	leaseRetryInterval = 100 * time.Millisecond
	maxLeaseAttempts   = 200
)

var (
	errNoRunLeased = errors.New("no run leased within timeout")
	errBadOutcome  = errors.New("outcome must be 'failed' or 'done'")
	errBadCount    = errors.New("count must be > 0")
	errBadTimeout  = errors.New("timeout must be > 0")
)

func parseFlags(args []string) (config, error) {
	fs := flag.NewFlagSet("fakeworker", flag.ContinueOnError)
	var cfg config
	defaultToken := os.Getenv("SNATCHARR_E2E_WORKER_TOKEN")
	if defaultToken == "" {
		defaultToken = os.Getenv("SNATCHARR_WORKER_TOKEN")
	}
	if defaultToken == "" {
		defaultToken = defaultWorkerToken
	}

	fs.StringVar(&cfg.grpcAddr, "grpc", defaultGRPCAddr, "gRPC server address")
	fs.StringVar(&cfg.workerID, "worker-id", defaultWorkerID, "worker identifier")
	fs.StringVar(&cfg.outcome, "outcome", defaultOutcome, "run outcome (failed|done)")
	fs.IntVar(&cfg.count, "count", defaultCount, "number of runs to process")
	fs.DurationVar(&cfg.timeout, "timeout", defaultTimeout, "timeout duration")
	fs.StringVar(&cfg.token, "token", defaultToken, "worker bearer token")
	fs.StringVar(&cfg.instance, "instance", "", "complete only runs of this instance id; a run of another instance is held until its lease expires")

	if err := fs.Parse(args); err != nil {
		return config{}, fmt.Errorf("parse flags: %w", err)
	}
	if cfg.outcome != "failed" && cfg.outcome != "done" {
		return config{}, fmt.Errorf("%w: got %q", errBadOutcome, cfg.outcome)
	}
	if cfg.count <= 0 {
		return config{}, fmt.Errorf("%w: got %d", errBadCount, cfg.count)
	}
	if cfg.timeout <= 0 {
		return config{}, fmt.Errorf("%w: got %v", errBadTimeout, cfg.timeout)
	}
	return cfg, nil
}

// leaseRun leases runs until it gets one of the wanted instance (any instance when empty).
// A run of another instance stays leased and goes back to the queue when its lease expires,
// so a parallel test's run is never completed by this worker.
func leaseRun(ctx context.Context, client snatcharrv1.WorkerServiceClient, workerID, instance string) (string, error) {
	for range maxLeaseAttempts {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("lease run wait: %w", ctx.Err())
		default:
		}

		callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		resp, err := client.LeaseRun(callCtx, &snatcharrv1.LeaseRunRequest{
			WorkerId:    workerID,
			WaitSeconds: leaseWaitSeconds,
		})
		cancel()
		if err != nil {
			return "", fmt.Errorf("lease rpc: %w", err)
		}
		if r := resp.GetRun(); r.GetRunId() != "" && (instance == "" || r.GetInstanceId() == instance) {
			return r.GetRunId(), nil
		}

		select {
		case <-ctx.Done():
			return "", fmt.Errorf("lease retry wait: %w", ctx.Err())
		case <-time.After(leaseRetryInterval):
		}
	}
	return "", errNoRunLeased
}

func completeRun(ctx context.Context, client snatcharrv1.WorkerServiceClient, workerID, runID, outcome string) error {
	protoOutcome := snatcharrv1.RunOutcome_RUN_OUTCOME_FAILED
	errMsg := "simulated failure by fakeworker"
	if outcome == "done" {
		protoOutcome = snatcharrv1.RunOutcome_RUN_OUTCOME_DONE
		errMsg = ""
	}

	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := client.CompleteRun(callCtx, &snatcharrv1.CompleteRunRequest{
		RunId:    runID,
		WorkerId: workerID,
		Outcome:  protoOutcome,
		Error:    errMsg,
	})
	if err != nil {
		return fmt.Errorf("complete rpc for run %s: %w", runID, err)
	}
	return nil
}

func processRuns(ctx context.Context, client snatcharrv1.WorkerServiceClient, cfg config, logger *slog.Logger) error {
	for i := range cfg.count {
		logger.InfoContext(ctx, "waiting to lease run", slog.Int("run_index", i+1), slog.Int("count", cfg.count))
		runID, err := leaseRun(ctx, client, cfg.workerID, cfg.instance)
		if err != nil {
			return fmt.Errorf("run %d of %d lease failed: %w", i+1, cfg.count, err)
		}
		logger.InfoContext(ctx, "leased run", slog.String("run_id", runID), slog.String("outcome", cfg.outcome))

		if err := completeRun(ctx, client, cfg.workerID, runID, cfg.outcome); err != nil {
			return fmt.Errorf("run %d of %d complete failed: %w", i+1, cfg.count, err)
		}
		logger.InfoContext(ctx, "completed run", slog.String("run_id", runID), slog.String("outcome", cfg.outcome))
	}
	return nil
}

func run(args []string, logger *slog.Logger) int {
	cfg, err := parseFlags(args)
	if err != nil {
		logger.Error("flag parsing failed", slog.String("error", err.Error()))
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	if cfg.token != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+cfg.token)
	}

	conn, err := grpc.NewClient(cfg.grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		logger.ErrorContext(ctx, "dial gRPC failed", slog.String("error", err.Error()), slog.String("addr", cfg.grpcAddr))
		return 1
	}
	defer func() {
		if err := conn.Close(); err != nil {
			logger.WarnContext(ctx, "close gRPC connection failed", slog.String("error", err.Error()))
		}
	}()

	client := snatcharrv1.NewWorkerServiceClient(conn)
	if err := processRuns(ctx, client, cfg, logger); err != nil {
		logger.ErrorContext(ctx, "processing runs failed", slog.String("error", err.Error()))
		return 1
	}
	logger.InfoContext(ctx, "fakeworker finished successfully", slog.Int("completed", cfg.count), slog.String("outcome", cfg.outcome))
	return 0
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	os.Exit(run(os.Args[1:], logger))
}
