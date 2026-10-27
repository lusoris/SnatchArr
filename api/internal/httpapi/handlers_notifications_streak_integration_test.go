// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	snatcharrv1 "github.com/lusoris/SnatchArr/api/internal/gen/snatcharr/v1"
	"github.com/lusoris/SnatchArr/api/internal/workergrpc"
)

type streakTestHarness struct {
	f       *integrationFixture
	ws      *workergrpc.Server
	instID  uuid.UUID
	counter *atomic.Int64
}

func setupStreakHarness(t *testing.T) (*streakTestHarness, context.Context) {
	t.Helper()
	f := newIntegrationFixture(t)
	ctx := withUser(t.Context(), f.admin)
	instID := seedTestInstance(t, f)
	ws := newWorkerServer(t, f)

	counter := &atomic.Int64{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	ch, err := f.h.CreateNotificationChannel(ctx, &oas.NotificationChannelInput{
		Name: "streak-sink",
		Kind: oas.NotificationChannelKindNtfy,
		Config: oas.NotificationChannelConfig{
			ServerURL: oas.NewOptString(srv.URL),
			Topic:     oas.NewOptString("streak"),
		},
	})
	if err != nil {
		t.Fatalf("CreateNotificationChannel: %v", err)
	}

	rule, err := f.h.CreateNotificationRule(ctx, &oas.NotificationRuleInput{
		Name:       "streak-rule",
		EventKind:  oas.NotificationRuleEventKindRunFailureStreak,
		Threshold:  2,
		ChannelIds: []uuid.UUID{ch.ID},
	})
	if err != nil {
		t.Fatalf("CreateNotificationRule: %v", err)
	}
	t.Cleanup(func() {
		_ = f.h.DeleteNotificationRule(ctx, oas.DeleteNotificationRuleParams{RuleId: rule.ID})
	})

	return &streakTestHarness{
		f:       f,
		ws:      ws,
		instID:  instID,
		counter: counter,
	}, ctx
}

func completeRunHelper(t *testing.T, h *streakTestHarness, ctx context.Context, outcome snatcharrv1.RunOutcome, errMsg string) (uuid.UUID, domain.Event) {
	t.Helper()
	_, _, err := h.f.h.runs.Enqueue(ctx, h.instID, domain.SnatchMissing)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	leased, err := h.f.h.runs.Lease(ctx, "worker-1")
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	level := "info"
	if outcome == snatcharrv1.RunOutcome_RUN_OUTCOME_FAILED {
		level = "error"
	}
	if _, err := h.ws.CompleteRun(ctx, &snatcharrv1.CompleteRunRequest{
		RunId:    leased.ID.String(),
		WorkerId: "worker-1",
		Outcome:  outcome,
		Error:    errMsg,
	}); err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}
	ev := domain.Event{
		RunID:      &leased.ID,
		InstanceID: h.instID,
		Level:      level,
		Type:       "run_finished",
		Detail:     errMsg,
	}
	return leased.ID, ev
}

// TestNotificationStreak_DeterministicDedupe proves that completing run1 and run2 before
// processing events fires exactly once. Under the old code (processing-time query),
// both events see streak 2 and fire twice.
func TestNotificationStreak_DeterministicDedupe(t *testing.T) {
	t.Parallel()
	h, ctx := setupStreakHarness(t)

	// Complete run1 failed and run2 failed BEFORE calling ProcessEvent.
	_, ev1 := completeRunHelper(t, h, ctx, snatcharrv1.RunOutcome_RUN_OUTCOME_FAILED, "fail 1")
	_, ev2 := completeRunHelper(t, h, ctx, snatcharrv1.RunOutcome_RUN_OUTCOME_FAILED, "fail 2")

	h.f.notifyEngine.ProcessEvent(ctx, ev1)
	h.f.notifyEngine.ProcessEvent(ctx, ev2)

	if got := h.counter.Load(); got != 1 {
		t.Fatalf("counter after run1 and run2 = %d, want 1 (must fire exactly once)", got)
	}
}

// TestNotificationStreak_BoundaryReset verifies run3 does not re-fire and done resets streak.
func TestNotificationStreak_BoundaryReset(t *testing.T) {
	t.Parallel()
	h, ctx := setupStreakHarness(t)

	_, ev1 := completeRunHelper(t, h, ctx, snatcharrv1.RunOutcome_RUN_OUTCOME_FAILED, "fail 1")
	h.f.notifyEngine.ProcessEvent(ctx, ev1)
	_, ev2 := completeRunHelper(t, h, ctx, snatcharrv1.RunOutcome_RUN_OUTCOME_FAILED, "fail 2")
	h.f.notifyEngine.ProcessEvent(ctx, ev2)
	if got := h.counter.Load(); got != 1 {
		t.Fatalf("after streak=2, counter = %d, want 1", got)
	}

	// Boundary 1: run3 fails -> streak = 3 != threshold 2 -> must not fire.
	_, ev3 := completeRunHelper(t, h, ctx, snatcharrv1.RunOutcome_RUN_OUTCOME_FAILED, "fail 3")
	h.f.notifyEngine.ProcessEvent(ctx, ev3)
	if got := h.counter.Load(); got != 1 {
		t.Fatalf("after streak=3, counter = %d, want 1", got)
	}

	// Boundary 2: done run resets streak.
	_, evDone := completeRunHelper(t, h, ctx, snatcharrv1.RunOutcome_RUN_OUTCOME_DONE, "")
	h.f.notifyEngine.ProcessEvent(ctx, evDone)
	if got := h.counter.Load(); got != 1 {
		t.Fatalf("after done run, counter = %d, want 1", got)
	}

	// Boundary 3: single failure after reset has streak = 1 -> must not fire.
	_, ev4 := completeRunHelper(t, h, ctx, snatcharrv1.RunOutcome_RUN_OUTCOME_FAILED, "fail 4")
	h.f.notifyEngine.ProcessEvent(ctx, ev4)
	if got := h.counter.Load(); got != 1 {
		t.Fatalf("after streak=1 post-reset, counter = %d, want 1", got)
	}

	// Boundary 4: second failure post-reset reaches threshold 2 again -> fires.
	_, ev5 := completeRunHelper(t, h, ctx, snatcharrv1.RunOutcome_RUN_OUTCOME_FAILED, "fail 5")
	h.f.notifyEngine.ProcessEvent(ctx, ev5)
	if got := h.counter.Load(); got != 2 {
		t.Fatalf("after streak=2 post-reset, counter = %d, want 2", got)
	}
}

// TestNotificationStreak_EqualFinishedAtTie verifies two runs with identical finished_at fire once.
func TestNotificationStreak_EqualFinishedAtTie(t *testing.T) {
	t.Parallel()
	h, ctx := setupStreakHarness(t)

	id1, ev1 := completeRunHelper(t, h, ctx, snatcharrv1.RunOutcome_RUN_OUTCOME_FAILED, "fail tie 1")
	id2, ev2 := completeRunHelper(t, h, ctx, snatcharrv1.RunOutcome_RUN_OUTCOME_FAILED, "fail tie 2")

	sameTime := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	_, err := h.f.st.Pool().Exec(ctx,
		"UPDATE snatch_runs SET finished_at = $1 WHERE id IN ($2, $3)",
		sameTime, id1, id2,
	)
	if err != nil {
		t.Fatalf("UPDATE snatch_runs finished_at: %v", err)
	}

	h.f.notifyEngine.ProcessEvent(ctx, ev1)
	h.f.notifyEngine.ProcessEvent(ctx, ev2)

	if got := h.counter.Load(); got != 1 {
		t.Fatalf("counter after tie runs = %d, want 1", got)
	}
}
