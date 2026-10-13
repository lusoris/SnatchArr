// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package workergrpc

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	snatcharrv1 "github.com/lusoris/SnatchArr/api/internal/gen/snatcharr/v1"
)

func testFilterCandidatesProcessedDropped(t *testing.T) {
	t.Helper()
	srv, _, runID := completeServer(t)
	run, err := srv.runs.Get(t.Context(), uuid.MustParse(runID))
	if err != nil {
		t.Fatalf("runs.Get() = %v", err)
	}
	err = srv.memory.Mark(t.Context(), run.InstanceID, run.Kind, "episode", []int64{100}, 24*time.Hour, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("memory.Mark() = %v", err)
	}
	resp, err := srv.FilterCandidates(t.Context(), &snatcharrv1.FilterCandidatesRequest{
		RunId:      runID,
		EntityType: "episode",
		EntityIds:  []int64{100, 200, 300},
	})
	if err != nil {
		t.Fatalf("FilterCandidates() = %v", err)
	}
	want := []int64{200, 300}
	if !slices.Equal(resp.GetUnprocessedIds(), want) {
		t.Errorf("unprocessed_ids = %v, want %v", resp.GetUnprocessedIds(), want)
	}
}

func testFilterCandidatesUnknownRun(t *testing.T) {
	t.Helper()
	srv, _, runID := completeServer(t)
	unknownRun := uuidOther(runID)
	_, err := srv.FilterCandidates(t.Context(), &snatcharrv1.FilterCandidatesRequest{
		RunId:      unknownRun,
		EntityType: "episode",
		EntityIds:  []int64{100},
	})
	if code := status.Code(err); code != codes.NotFound {
		t.Errorf("FilterCandidates() code = %v, want %v", code, codes.NotFound)
	}
}

func testFilterCandidatesEmptyList(t *testing.T) {
	t.Helper()
	srv, _, runID := completeServer(t)
	resp, err := srv.FilterCandidates(t.Context(), &snatcharrv1.FilterCandidatesRequest{
		RunId:      runID,
		EntityType: "episode",
		EntityIds:  []int64{},
	})
	if err != nil {
		t.Fatalf("FilterCandidates() = %v", err)
	}
	if len(resp.GetUnprocessedIds()) != 0 {
		t.Errorf("unprocessed_ids = %v, want empty", resp.GetUnprocessedIds())
	}
}

func TestFilterCandidates(t *testing.T) {
	t.Parallel()
	t.Run("dropped in memory and new kept", func(t *testing.T) {
		t.Parallel()
		testFilterCandidatesProcessedDropped(t)
	})
	t.Run("unknown run id", func(t *testing.T) {
		t.Parallel()
		testFilterCandidatesUnknownRun(t)
	})
	t.Run("empty candidate list", func(t *testing.T) {
		t.Parallel()
		testFilterCandidatesEmptyList(t)
	})
}
