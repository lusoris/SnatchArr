// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	snatcharrv1 "github.com/lusoris/SnatchArr/api/internal/gen/snatcharr/v1"
)

type mockWorkerServiceClient struct {
	leaseFunc    func(ctx context.Context, in *snatcharrv1.LeaseRunRequest) (*snatcharrv1.LeaseRunResponse, error)
	completeFunc func(ctx context.Context, in *snatcharrv1.CompleteRunRequest) (*snatcharrv1.CompleteRunResponse, error)
}

func (m *mockWorkerServiceClient) LeaseRun(ctx context.Context, in *snatcharrv1.LeaseRunRequest, _ ...grpc.CallOption) (*snatcharrv1.LeaseRunResponse, error) {
	if m.leaseFunc != nil {
		return m.leaseFunc(ctx, in)
	}
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (m *mockWorkerServiceClient) CompleteRun(ctx context.Context, in *snatcharrv1.CompleteRunRequest, _ ...grpc.CallOption) (*snatcharrv1.CompleteRunResponse, error) {
	if m.completeFunc != nil {
		return m.completeFunc(ctx, in)
	}
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (m *mockWorkerServiceClient) Heartbeat(context.Context, *snatcharrv1.HeartbeatRequest, ...grpc.CallOption) (*snatcharrv1.HeartbeatResponse, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (m *mockWorkerServiceClient) FilterCandidates(context.Context, *snatcharrv1.FilterCandidatesRequest, ...grpc.CallOption) (*snatcharrv1.FilterCandidatesResponse, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (m *mockWorkerServiceClient) AcquireBudget(context.Context, *snatcharrv1.AcquireBudgetRequest, ...grpc.CallOption) (*snatcharrv1.AcquireBudgetResponse, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (m *mockWorkerServiceClient) ReportEvents(context.Context, ...grpc.CallOption) (grpc.ClientStreamingClient[snatcharrv1.ReportEventsRequest, snatcharrv1.ReportEventsResponse], error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (m *mockWorkerServiceClient) ReportLogs(context.Context, *snatcharrv1.ReportLogsRequest, ...grpc.CallOption) (*snatcharrv1.ReportLogsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func TestParseFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		want    config
		wantErr error
	}{
		{
			name: "positive: default flags",
			args: []string{},
			want: config{
				grpcAddr: defaultGRPCAddr,
				workerID: defaultWorkerID,
				outcome:  defaultOutcome,
				count:    defaultCount,
				timeout:  defaultTimeout,
				token:    defaultWorkerToken,
				eligible: -1,
			},
			wantErr: nil,
		},
		{
			name: "positive: custom flags",
			args: []string{
				"-grpc", "127.0.0.1:29090",
				"-worker-id", "custom-worker",
				"-outcome", "done",
				"-count", "3",
				"-timeout", "15s",
				"-token", "custom-token",
				"-eligible", "7",
			},
			want: config{
				grpcAddr: "127.0.0.1:29090",
				workerID: "custom-worker",
				outcome:  "done",
				count:    3,
				timeout:  15 * time.Second,
				token:    "custom-token",
				eligible: 7,
			},
			wantErr: nil,
		},
		{
			name:    "negative: invalid outcome",
			args:    []string{"-outcome", "cancelled"},
			wantErr: errBadOutcome,
		},
		{
			name:    "negative: non-positive count",
			args:    []string{"-count", "0"},
			wantErr: errBadCount,
		},
		{
			name:    "negative: negative timeout",
			args:    []string{"-timeout", "-5s"},
			wantErr: errBadTimeout,
		},
		{
			name: "boundary: count 1 and outcome failed",
			args: []string{"-count", "1", "-outcome", "failed"},
			want: config{
				grpcAddr: defaultGRPCAddr,
				workerID: defaultWorkerID,
				outcome:  "failed",
				count:    1,
				timeout:  defaultTimeout,
				token:    defaultWorkerToken,
				eligible: -1,
			},
			wantErr: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseFlags(tc.args)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("parseFlags() error = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseFlags() unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("parseFlags() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestProcessRuns_Positive(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	var leasedIDs []string
	var completedOutcomes []snatcharrv1.RunOutcome

	client := &mockWorkerServiceClient{
		leaseFunc: func(_ context.Context, in *snatcharrv1.LeaseRunRequest) (*snatcharrv1.LeaseRunResponse, error) {
			if in.GetWorkerId() != "test-worker" {
				return nil, status.Error(codes.InvalidArgument, "wrong worker id")
			}
			id := "run-123"
			leasedIDs = append(leasedIDs, id)
			return &snatcharrv1.LeaseRunResponse{
				Run: &snatcharrv1.Run{RunId: id},
			}, nil
		},
		completeFunc: func(_ context.Context, in *snatcharrv1.CompleteRunRequest) (*snatcharrv1.CompleteRunResponse, error) {
			if in.GetRunId() != "run-123" {
				return nil, status.Error(codes.InvalidArgument, "wrong run id")
			}
			completedOutcomes = append(completedOutcomes, in.GetOutcome())
			return &snatcharrv1.CompleteRunResponse{}, nil
		},
	}

	cfg := config{
		workerID: "test-worker",
		outcome:  "failed",
		count:    2,
		timeout:  5 * time.Second,
	}

	ctx := t.Context()
	if err := processRuns(ctx, client, cfg, logger); err != nil {
		t.Fatalf("processRuns() failed: %v", err)
	}

	if len(leasedIDs) != 2 {
		t.Fatalf("leased %d runs, want 2", len(leasedIDs))
	}
	if len(completedOutcomes) != 2 || completedOutcomes[0] != snatcharrv1.RunOutcome_RUN_OUTCOME_FAILED {
		t.Fatalf("completed outcomes = %v, want 2 failed runs", completedOutcomes)
	}
}

func TestProcessRuns_InstanceFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		instance string
		leased   []*snatcharrv1.Run
		want     []string
	}{
		{
			name:     "matching run completes",
			instance: "inst-a",
			leased:   []*snatcharrv1.Run{{RunId: "run-a", InstanceId: "inst-a"}},
			want:     []string{"run-a"},
		},
		{
			name:     "foreign run is held, matching run completes",
			instance: "inst-a",
			leased: []*snatcharrv1.Run{
				{RunId: "run-b", InstanceId: "inst-b"},
				{RunId: "run-a", InstanceId: "inst-a"},
			},
			want: []string{"run-a"},
		},
		{
			name:     "empty instance completes any run",
			instance: "",
			leased:   []*snatcharrv1.Run{{RunId: "run-b", InstanceId: "inst-b"}},
			want:     []string{"run-b"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			next := 0
			var completed []string
			client := &mockWorkerServiceClient{
				leaseFunc: func(context.Context, *snatcharrv1.LeaseRunRequest) (*snatcharrv1.LeaseRunResponse, error) {
					if next >= len(tc.leased) {
						return &snatcharrv1.LeaseRunResponse{}, nil
					}
					run := tc.leased[next]
					next++
					return &snatcharrv1.LeaseRunResponse{Run: run}, nil
				},
				completeFunc: func(_ context.Context, in *snatcharrv1.CompleteRunRequest) (*snatcharrv1.CompleteRunResponse, error) {
					completed = append(completed, in.GetRunId())
					return &snatcharrv1.CompleteRunResponse{}, nil
				},
			}

			cfg := config{
				workerID: "test-worker",
				instance: tc.instance,
				outcome:  "done",
				count:    1,
				timeout:  5 * time.Second,
			}
			if err := processRuns(t.Context(), client, cfg, slog.New(slog.DiscardHandler)); err != nil {
				t.Fatalf("processRuns() failed: %v", err)
			}
			if len(completed) != len(tc.want) || completed[0] != tc.want[0] {
				t.Fatalf("completed = %v, want %v", completed, tc.want)
			}
		})
	}
}

func TestProcessRuns_NegativeLeaseError(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	client := &mockWorkerServiceClient{
		leaseFunc: func(_ context.Context, _ *snatcharrv1.LeaseRunRequest) (*snatcharrv1.LeaseRunResponse, error) {
			return nil, status.Error(codes.Internal, "rpc internal error")
		},
	}

	cfg := config{
		workerID: "test-worker",
		outcome:  "done",
		count:    1,
		timeout:  2 * time.Second,
	}

	ctx := t.Context()
	err := processRuns(ctx, client, cfg, logger)
	if err == nil {
		t.Fatal("processRuns() expected error, got nil")
	}
}

func TestProcessRuns_NegativeCompleteError(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	client := &mockWorkerServiceClient{
		leaseFunc: func(_ context.Context, _ *snatcharrv1.LeaseRunRequest) (*snatcharrv1.LeaseRunResponse, error) {
			return &snatcharrv1.LeaseRunResponse{
				Run: &snatcharrv1.Run{RunId: "run-failed-complete"},
			}, nil
		},
		completeFunc: func(_ context.Context, _ *snatcharrv1.CompleteRunRequest) (*snatcharrv1.CompleteRunResponse, error) {
			return nil, status.Error(codes.PermissionDenied, "cannot complete")
		},
	}

	cfg := config{
		workerID: "test-worker",
		outcome:  "done",
		count:    1,
		timeout:  2 * time.Second,
	}

	ctx := t.Context()
	err := processRuns(ctx, client, cfg, logger)
	if err == nil {
		t.Fatal("processRuns() expected error, got nil")
	}
}

func TestProcessRuns_BoundaryTimeout(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	client := &mockWorkerServiceClient{
		leaseFunc: func(_ context.Context, _ *snatcharrv1.LeaseRunRequest) (*snatcharrv1.LeaseRunResponse, error) {
			// Returns empty response (no run available)
			return &snatcharrv1.LeaseRunResponse{}, nil
		},
	}

	cfg := config{
		workerID: "test-worker",
		outcome:  "failed",
		count:    1,
		timeout:  150 * time.Millisecond,
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := processRuns(ctx, client, cfg, logger)
	if err == nil {
		t.Fatal("processRuns() with a cancelled context: expected an error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("processRuns() error = %v, want %v", err, context.Canceled)
	}
}

func TestEligibleFlagReachesCompleteRun(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		args []string
		want *uint32
	}{
		{"positive", []string{"-eligible", "7"}, new(uint32(7))},
		{"boundary", []string{"-eligible", "0"}, new(uint32(0))},
		{"absent", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := parseFlags(tc.args)
			if err != nil {
				t.Fatalf("parseFlags: %v", err)
			}
			called := false
			client := &mockWorkerServiceClient{
				completeFunc: func(_ context.Context, req *snatcharrv1.CompleteRunRequest) (*snatcharrv1.CompleteRunResponse, error) {
					called = true
					if (req.EligibleItems == nil) != (tc.want == nil) || (tc.want != nil && req.GetEligibleItems() != *tc.want) {
						t.Errorf("eligible_items = %v, want %v", req.EligibleItems, tc.want)
					}
					return &snatcharrv1.CompleteRunResponse{}, nil
				},
			}
			if err := completeRun(t.Context(), client, cfg.workerID, "run", cfg.outcome, cfg.eligible); err != nil {
				t.Fatalf("completeRun: %v", err)
			}
			if !called {
				t.Fatal("CompleteRun was not called")
			}
		})
	}
}

func TestEligibleFlagRejectsOverflow(t *testing.T) {
	t.Parallel()
	if _, err := parseFlags([]string{"-eligible", "4294967296"}); !errors.Is(err, errBadEligible) {
		t.Fatalf("parseFlags overflow = %v, want errBadEligible", err)
	}
}
