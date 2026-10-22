// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	snatcharrv1 "github.com/lusoris/SnatchArr/api/internal/gen/snatcharr/v1"
	"github.com/lusoris/SnatchArr/api/internal/metrics/metricstest"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
	"github.com/lusoris/SnatchArr/api/internal/testdiag"
	"github.com/lusoris/SnatchArr/api/internal/workergrpc"
)

// TestNotificationDeliveryThroughAPI covers deliver requirement 1:
// chat channel and webhook deliver a test event via POST /notifications/channels/{channelId}/test.
func TestNotificationDeliveryThroughAPI(t *testing.T) {
	t.Parallel()
	f := newIntegrationFixture(t)
	ctx := withUser(t.Context(), f.admin)

	// 1. Chat channel (ntfy).
	var chatReceived atomic.Bool
	var chatToken atomic.Value
	chatSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chatReceived.Store(true)
		chatToken.Store(r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(chatSrv.Close)

	chatCh, err := f.h.CreateNotificationChannel(ctx, &oas.NotificationChannelInput{
		Name: "test-ntfy",
		Kind: oas.NotificationChannelKindNtfy,
		Config: oas.NotificationChannelConfig{
			ServerURL: oas.NewOptString(chatSrv.URL),
			Topic:     oas.NewOptString("alerts"),
			Token:     oas.NewOptString("ntfy-secret-token"),
		},
	})
	if err != nil {
		t.Fatalf("CreateNotificationChannel(ntfy) = %v", err)
	}

	if tErr := f.h.TestNotificationChannel(ctx, oas.TestNotificationChannelParams{ChannelId: chatCh.ID}); tErr != nil {
		t.Fatalf("TestNotificationChannel(ntfy) = %v", tErr)
	}
	if !chatReceived.Load() {
		t.Fatal("chat server did not receive test notification")
	}
	if got := chatToken.Load().(string); got != "Bearer ntfy-secret-token" {
		t.Fatalf("chat server auth = %q, want Bearer ntfy-secret-token", got)
	}

	// 2. Webhook channel verifying HMAC signature.
	const webhookSecret = "webhook-hmac-secret-test-32bytes"
	var webhookReceived atomic.Bool
	var signatureValid atomic.Bool
	webhookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webhookReceived.Store(true)
		body, _ := io.ReadAll(r.Body)
		sigHeader := r.Header.Get("X-Webhook-Signature")
		mac := hmac.New(sha256.New, []byte(webhookSecret))
		mac.Write(body)
		expectedSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if hmac.Equal([]byte(sigHeader), []byte(expectedSig)) {
			signatureValid.Store(true)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(webhookSrv.Close)

	webhookCh, err := f.h.CreateNotificationChannel(ctx, &oas.NotificationChannelInput{
		Name: "test-webhook",
		Kind: oas.NotificationChannelKindWebhook,
		Config: oas.NotificationChannelConfig{
			URL:                 oas.NewOptString(webhookSrv.URL),
			Secret:              oas.NewOptString(webhookSecret),
			AllowPrivateNetwork: oas.NewOptBool(true),
			AllowInsecureHTTP:   oas.NewOptBool(true),
		},
	})
	if err != nil {
		t.Fatalf("CreateNotificationChannel(webhook) = %v", err)
	}

	if err := f.h.TestNotificationChannel(ctx, oas.TestNotificationChannelParams{ChannelId: webhookCh.ID}); err != nil {
		t.Fatalf("TestNotificationChannel(webhook) = %v", err)
	}
	if !webhookReceived.Load() {
		t.Fatal("webhook server did not receive test payload")
	}
	if !signatureValid.Load() {
		t.Fatal("webhook HMAC signature in X-Webhook-Signature is invalid")
	}
}

// TestNotificationChannelFailureAndFanout covers requirement 2:
// A failing channel (500) records notification_failed event without secrets,
// and other channels in the same fan-out still deliver.
func TestNotificationChannelFailureAndFanout(t *testing.T) {
	t.Parallel()
	f := newIntegrationFixture(t)
	ctx := withUser(t.Context(), f.admin)

	var failingReceived atomic.Bool
	failingSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		failingReceived.Store(true)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("error: internal failure with secret-api-key-12345"))
	}))
	t.Cleanup(failingSrv.Close)

	var healthyReceived atomic.Bool
	healthySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		healthyReceived.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(healthySrv.Close)

	chFail, err := f.h.CreateNotificationChannel(ctx, &oas.NotificationChannelInput{
		Name: "failing-chan",
		Kind: oas.NotificationChannelKindNtfy,
		Config: oas.NotificationChannelConfig{
			ServerURL: oas.NewOptString(failingSrv.URL),
			Topic:     oas.NewOptString("fail-topic"),
			Token:     oas.NewOptString("secret-token-fail"),
		},
	})
	if err != nil {
		t.Fatalf("CreateNotificationChannel(fail) = %v", err)
	}

	chOk, err := f.h.CreateNotificationChannel(ctx, &oas.NotificationChannelInput{
		Name: "healthy-chan",
		Kind: oas.NotificationChannelKindNtfy,
		Config: oas.NotificationChannelConfig{
			ServerURL: oas.NewOptString(healthySrv.URL),
			Topic:     oas.NewOptString("ok-topic"),
		},
	})
	if err != nil {
		t.Fatalf("CreateNotificationChannel(ok) = %v", err)
	}

	_, err = f.h.CreateNotificationRule(ctx, &oas.NotificationRuleInput{
		Name:       "circuit-rule",
		EventKind:  oas.NotificationRuleEventKindCircuitOpened,
		ChannelIds: []uuid.UUID{chFail.ID, chOk.ID},
	})
	if err != nil {
		t.Fatalf("CreateNotificationRule = %v", err)
	}

	instID := seedTestInstance(t, f)
	ev := domain.Event{
		InstanceID: instID,
		Level:      "warn",
		Type:       "circuit_opened",
		Title:      "Circuit opened for instance",
		Detail:     "consecutive search failures",
	}
	f.notifyEngine.ProcessEvent(ctx, ev)

	if !failingReceived.Load() {
		t.Fatal("failing channel was not contacted")
	}
	if !healthyReceived.Load() {
		t.Fatal("healthy channel was not contacted despite fan-out")
	}

	// Verify notification_failed event was stored.
	events, err := f.rec.List(ctx, snatch.ListParams{
		Type:     "notification_failed",
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("List events = %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected notification_failed event to be recorded, found none")
	}
	found := false
	for _, e := range events {
		if strings.Contains(e.Title, "failing-chan") {
			found = true
			if strings.Contains(e.Detail, "secret-token-fail") {
				t.Fatalf("notification_failed event contains secret token: %s", e.Detail)
			}
		}
	}
	if !found {
		t.Fatal("notification_failed event for failing-chan not found")
	}
}

// TestNotificationRulesTriggerAndDedupe covers requirement 3:
// Each rule kind fires from its real trigger in a test, plus stamina dedupe boundary test.
func TestNotificationRulesTriggerAndDedupe(t *testing.T) {
	t.Parallel()
	f := newIntegrationFixture(t)
	ctx := withUser(t.Context(), f.admin)
	instID := seedTestInstance(t, f)

	var receivedCount atomic.Int64
	var lastEvent atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedCount.Add(1)
		body, _ := io.ReadAll(r.Body)
		lastEvent.Store(string(body))
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	ch, err := f.h.CreateNotificationChannel(ctx, &oas.NotificationChannelInput{
		Name: "alert-sink",
		Kind: oas.NotificationChannelKindNtfy,
		Config: oas.NotificationChannelConfig{
			ServerURL: oas.NewOptString(srv.URL),
			Topic:     oas.NewOptString("sink"),
		},
	})
	if err != nil {
		t.Fatalf("CreateNotificationChannel = %v", err)
	}

	if err := f.notifyEngine.Start(ctx); err != nil {
		t.Fatalf("Start notify engine: %v", err)
	}
	t.Cleanup(func() { _ = f.notifyEngine.Stop(ctx) })

	testRuleKinds(t, f, ctx, instID, ch.ID, &receivedCount)
	testStaminaDedupeBoundary(t, f, ctx, instID, ch.ID, &receivedCount)
}

func testRuleKinds(t *testing.T, f *integrationFixture, ctx context.Context, instID, chID uuid.UUID, counter *atomic.Int64) {
	t.Helper()
	ws := newWorkerServer(t, f)

	_, _, err := f.h.runs.Enqueue(ctx, instID, domain.SnatchMissing)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	leased, err := f.h.runs.Lease(ctx, "worker-1")
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}

	testCircuitOpenedTrigger(t, f, ws, ctx, leased.ID.String(), chID, counter, 1)
	testInstanceUnreachableTrigger(t, f, ws, ctx, leased.ID.String(), chID, counter, 2)
	testStaminaExhaustedTrigger(t, f, ws, ctx, leased.ID.String(), chID, counter, 3)
	testSeerrSnatchedTrigger(t, f, ws, ctx, leased.ID.String(), instID, chID, counter, 4)
	testRunFailureStreakTrigger(t, f, ws, ctx, leased.ID.String(), instID, chID, counter, 5)
}

func testCircuitOpenedTrigger(t *testing.T, f *integrationFixture, ws *workergrpc.Server, ctx context.Context, runID string, chID uuid.UUID, counter *atomic.Int64, want int64) {
	t.Helper()
	rule, err := f.h.CreateNotificationRule(ctx, &oas.NotificationRuleInput{
		Name:       "circuit-rule",
		EventKind:  oas.NotificationRuleEventKindCircuitOpened,
		ChannelIds: []uuid.UUID{chID},
	})
	if err != nil {
		t.Fatalf("CreateNotificationRule(circuit): %v", err)
	}
	defer func() { _ = f.h.DeleteNotificationRule(ctx, oas.DeleteNotificationRuleParams{RuleId: rule.ID}) }()

	stream := &eventStream{
		ctx: ctx,
		reqs: []*snatcharrv1.ReportEventsRequest{
			{
				Event: &snatcharrv1.SnatchEvent{
					RunId: runID,
					Type:  snatcharrv1.EventType_EVENT_TYPE_CIRCUIT_OPENED,
					Title: "Circuit opened on indexer",
				},
			},
		},
	}
	//nolint:contextcheck // workergrpc.Server.ReportEvents receives context via stream.Context()
	if err := ws.ReportEvents(stream); err != nil {
		t.Fatalf("ReportEvents(circuit_opened): %v", err)
	}
	waitCounter(t, counter, want)
}

func testInstanceUnreachableTrigger(t *testing.T, f *integrationFixture, ws *workergrpc.Server, ctx context.Context, runID string, chID uuid.UUID, counter *atomic.Int64, want int64) {
	t.Helper()
	rule, err := f.h.CreateNotificationRule(ctx, &oas.NotificationRuleInput{
		Name:       "unreachable-rule",
		EventKind:  oas.NotificationRuleEventKindInstanceUnreachable,
		ChannelIds: []uuid.UUID{chID},
	})
	if err != nil {
		t.Fatalf("CreateNotificationRule(unreachable): %v", err)
	}
	defer func() { _ = f.h.DeleteNotificationRule(ctx, oas.DeleteNotificationRuleParams{RuleId: rule.ID}) }()

	stream := &eventStream{
		ctx: ctx,
		reqs: []*snatcharrv1.ReportEventsRequest{
			{
				Event: &snatcharrv1.SnatchEvent{
					RunId: runID,
					Type:  snatcharrv1.EventType_EVENT_TYPE_INSTANCE_UNREACHABLE,
					Title: "Instance unreachable",
				},
			},
		},
	}
	//nolint:contextcheck // workergrpc.Server.ReportEvents receives context via stream.Context()
	if err := ws.ReportEvents(stream); err != nil {
		t.Fatalf("ReportEvents(instance_unreachable): %v", err)
	}
	waitCounter(t, counter, want)
}

func testStaminaExhaustedTrigger(t *testing.T, f *integrationFixture, ws *workergrpc.Server, ctx context.Context, runID string, chID uuid.UUID, counter *atomic.Int64, want int64) {
	t.Helper()
	rule, err := f.h.CreateNotificationRule(ctx, &oas.NotificationRuleInput{
		Name:       "stamina-rule",
		EventKind:  oas.NotificationRuleEventKindStaminaExhausted,
		ChannelIds: []uuid.UUID{chID},
	})
	if err != nil {
		t.Fatalf("CreateNotificationRule(stamina): %v", err)
	}
	defer func() { _ = f.h.DeleteNotificationRule(ctx, oas.DeleteNotificationRuleParams{RuleId: rule.ID}) }()

	resp, bErr := ws.AcquireBudget(ctx, &snatcharrv1.AcquireBudgetRequest{
		RunId:     runID,
		Requested: 20,
	})
	if bErr != nil || resp.GetRemainingInWindow() != 0 {
		t.Fatalf("AcquireBudget: err=%v, resp=%v", bErr, resp)
	}
	waitCounter(t, counter, want)
}

func testSeerrSnatchedTrigger(t *testing.T, f *integrationFixture, ws *workergrpc.Server, ctx context.Context, runID string, instID, chID uuid.UUID, counter *atomic.Int64, want int64) {
	t.Helper()
	rule, err := f.h.CreateNotificationRule(ctx, &oas.NotificationRuleInput{
		Name:       "seerr-rule",
		EventKind:  oas.NotificationRuleEventKindSeerrRequestSnatched,
		ChannelIds: []uuid.UUID{chID},
	})
	if err != nil {
		t.Fatalf("CreateNotificationRule(seerr): %v", err)
	}
	defer func() { _ = f.h.DeleteNotificationRule(ctx, oas.DeleteNotificationRuleParams{RuleId: rule.ID}) }()

	linkID := uuid.New()
	if _, err := f.st.Pool().Exec(ctx, `
		INSERT INTO seerr_links (id, name, base_url, api_key_enc, enabled, created_at, updated_at)
		VALUES ($1, 'test-seerr', 'http://seerr.local', $2, true, now(), now())`,
		linkID, []byte("sealed-key")); err != nil {
		t.Fatalf("insert seerr_links: %v", err)
	}
	const seerrEntityID = 42
	if _, err := f.st.Pool().Exec(ctx, `
		INSERT INTO seerr_requests (link_id, request_id, media_type, title, request_status, media_status, requested_by, instance_id, entity_id, last_seen_at, updated_at)
		VALUES ($1, 101, 'movie', 'Test Movie', 1, 1, 'Alice', $2, $3, now(), now())`,
		linkID, instID, seerrEntityID); err != nil {
		t.Fatalf("insert seerr_requests: %v", err)
	}

	stream := &eventStream{
		ctx: ctx,
		reqs: []*snatcharrv1.ReportEventsRequest{
			{
				Event: &snatcharrv1.SnatchEvent{
					RunId:      runID,
					Type:       snatcharrv1.EventType_EVENT_TYPE_SEARCH_DISPATCHED,
					EntityType: "movie",
					EntityId:   seerrEntityID,
					Title:      "Test Movie",
				},
			},
		},
	}
	//nolint:contextcheck // workergrpc.Server.ReportEvents receives context via stream.Context()
	if err := ws.ReportEvents(stream); err != nil {
		t.Fatalf("ReportEvents(seerr): %v", err)
	}
	waitCounter(t, counter, want)
}

func testRunFailureStreakTrigger(t *testing.T, f *integrationFixture, ws *workergrpc.Server, ctx context.Context, leasedRunID string, instID, chID uuid.UUID, counter *atomic.Int64, wantAfterStreak2 int64) {
	t.Helper()
	rule, err := f.h.CreateNotificationRule(ctx, &oas.NotificationRuleInput{
		Name:       "streak-rule",
		EventKind:  oas.NotificationRuleEventKindRunFailureStreak,
		Threshold:  2,
		ChannelIds: []uuid.UUID{chID},
	})
	if err != nil {
		t.Fatalf("CreateNotificationRule(streak) = %v", err)
	}
	defer func() { _ = f.h.DeleteNotificationRule(ctx, oas.DeleteNotificationRuleParams{RuleId: rule.ID}) }()

	initial := counter.Load()

	// Failure 1: Complete existing leased run as failed -> streak = 1 (threshold 2, must NOT fire).
	if _, err = ws.CompleteRun(ctx, &snatcharrv1.CompleteRunRequest{
		RunId:    leasedRunID,
		WorkerId: "worker-1",
		Outcome:  snatcharrv1.RunOutcome_RUN_OUTCOME_FAILED,
		Error:    "failure 1",
	}); err != nil {
		t.Fatalf("CompleteRun 1: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if got := counter.Load(); got != initial {
		t.Fatalf("after streak=1, counter = %d, want %d", got, initial)
	}

	// Failure 2: streak = 2 == threshold -> MUST fire!
	_, _, err = f.h.runs.Enqueue(ctx, instID, domain.SnatchMissing)
	if err != nil {
		t.Fatalf("Enqueue run 2: %v", err)
	}
	leased2, err := f.h.runs.Lease(ctx, "worker-1")
	if err != nil {
		t.Fatalf("Lease run 2: %v", err)
	}
	if _, err = ws.CompleteRun(ctx, &snatcharrv1.CompleteRunRequest{
		RunId:    leased2.ID.String(),
		WorkerId: "worker-1",
		Outcome:  snatcharrv1.RunOutcome_RUN_OUTCOME_FAILED,
		Error:    "failure 2",
	}); err != nil {
		t.Fatalf("CompleteRun 2: %v", err)
	}
	waitCounter(t, counter, wantAfterStreak2)

	// Failure 3: streak = 3 != threshold -> must NOT fire again!
	_, _, err = f.h.runs.Enqueue(ctx, instID, domain.SnatchMissing)
	if err != nil {
		t.Fatalf("Enqueue run 3: %v", err)
	}
	leased3, err := f.h.runs.Lease(ctx, "worker-1")
	if err != nil {
		t.Fatalf("Lease run 3: %v", err)
	}
	if _, err := ws.CompleteRun(ctx, &snatcharrv1.CompleteRunRequest{
		RunId:    leased3.ID.String(),
		WorkerId: "worker-1",
		Outcome:  snatcharrv1.RunOutcome_RUN_OUTCOME_FAILED,
		Error:    "failure 3",
	}); err != nil {
		t.Fatalf("CompleteRun 3: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if got := counter.Load(); got != wantAfterStreak2 {
		t.Fatalf("after streak=3, counter = %d, want %d (must not re-fire while broken)", got, wantAfterStreak2)
	}
}

func waitCounter(t *testing.T, counter *atomic.Int64, want int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if counter.Load() == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := counter.Load(); got != want {
		t.Fatalf("counter = %d, want %d", got, want)
	}
}

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

func newWorkerServer(t *testing.T, f *integrationFixture) *workergrpc.Server {
	t.Helper()
	m, _ := metricstest.New(t)
	logger := slog.New(slog.DiscardHandler)
	return workergrpc.New(
		f.h.runs, f.h.workers, f.h.budget, f.h.memory, f.rec, f.h.planner,
		f.h.instances, f.h.policies, f.h.settings, f.h.seerr, f.clk, logger, m,
		workergrpc.Options{},
	)
}

func testStaminaDedupeBoundary(t *testing.T, f *integrationFixture, ctx context.Context, instID, chID uuid.UUID, counter *atomic.Int64) {
	t.Helper()
	f.notifyEngine.ResetStaminaDedupe()
	baseCount := counter.Load()

	rule, err := f.h.CreateNotificationRule(ctx, &oas.NotificationRuleInput{
		Name:       "stamina-rule",
		EventKind:  oas.NotificationRuleEventKindStaminaExhausted,
		ChannelIds: []uuid.UUID{chID},
	})
	if err != nil {
		t.Fatalf("CreateNotificationRule(stamina) = %v", err)
	}
	defer func() { _ = f.h.DeleteNotificationRule(ctx, oas.DeleteNotificationRuleParams{RuleId: rule.ID}) }()

	ev := domain.Event{InstanceID: instID, Type: "stamina_exhausted", Title: "Stamina exhausted", Level: "warn"}

	// t = 0m -> should fire.
	f.notifyEngine.ProcessEvent(ctx, ev)
	if got := counter.Load(); got != baseCount+1 {
		t.Fatalf("t=0m stamina counter = %d, want %d", got, baseCount+1)
	}

	// t = 30m -> dedupe window active, must NOT fire.
	f.clk.Advance(30 * time.Minute)
	f.notifyEngine.ProcessEvent(ctx, ev)
	if got := counter.Load(); got != baseCount+1 {
		t.Fatalf("t=30m stamina counter = %d, want %d", got, baseCount+1)
	}

	// t = 59m -> boundary just before 1h, must NOT fire.
	f.clk.Advance(29 * time.Minute)
	f.notifyEngine.ProcessEvent(ctx, ev)
	if got := counter.Load(); got != baseCount+1 {
		t.Fatalf("t=59m stamina counter = %d, want %d", got, baseCount+1)
	}

	// t = 60m -> boundary hits 1h window, MUST fire!
	f.clk.Advance(1 * time.Minute)
	f.notifyEngine.ProcessEvent(ctx, ev)
	if got := counter.Load(); got != baseCount+2 {
		t.Fatalf("t=60m stamina counter = %d, want %d", got, baseCount+2)
	}
}

// TestNotificationSecretsEncryptionAndRedaction covers requirement 4:
// Secret never appears in any API response and is stored encrypted in DB.
func TestNotificationSecretsEncryptionAndRedaction(t *testing.T) {
	t.Parallel()
	f := newIntegrationFixture(t)
	ctx := withUser(t.Context(), f.admin)

	const plainWebhookURL = "https://discord.com/api/webhooks/123/super-secret-token"
	const plainBotToken = "my-bot-secret-token-xyz"

	// 1. Create channel.
	created, err := f.h.CreateNotificationChannel(ctx, &oas.NotificationChannelInput{
		Name: "secure-discord",
		Kind: oas.NotificationChannelKindDiscord,
		Config: oas.NotificationChannelConfig{
			WebhookURL: oas.NewOptString(plainWebhookURL),
			BotToken:   oas.NewOptString(plainBotToken),
			Username:   oas.NewOptString("SnatchBot"),
		},
	})
	if err != nil {
		t.Fatalf("CreateNotificationChannel = %v", err)
	}

	// 2. Verify API response redaction.
	if !created.HasSecret {
		t.Fatal("expected created.HasSecret = true")
	}
	if created.Config.WebhookURL.IsSet() && created.Config.WebhookURL.Value != "" {
		t.Fatalf("WebhookURL leaked in create response: %q", created.Config.WebhookURL.Value)
	}
	if created.Config.BotToken.IsSet() && created.Config.BotToken.Value != "" {
		t.Fatalf("BotToken leaked in create response: %q", created.Config.BotToken.Value)
	}
	if created.Config.Username.Value != "SnatchBot" {
		t.Fatalf("Username = %q, want SnatchBot", created.Config.Username.Value)
	}

	// 3. Verify GetChannel API response redaction.
	fetched, err := f.h.GetNotificationChannel(ctx, oas.GetNotificationChannelParams{ChannelId: created.ID})
	if err != nil {
		t.Fatalf("GetNotificationChannel = %v", err)
	}
	if !fetched.HasSecret {
		t.Fatal("expected fetched.HasSecret = true")
	}
	if fetched.Config.WebhookURL.Value != "" {
		t.Fatalf("WebhookURL leaked in get response: %q", fetched.Config.WebhookURL.Value)
	}
	if fetched.Config.BotToken.Value != "" {
		t.Fatalf("BotToken leaked in get response: %q", fetched.Config.BotToken.Value)
	}

	// 4. Verify ListChannels API response redaction.
	list, err := f.h.ListNotificationChannels(ctx)
	if err != nil {
		t.Fatalf("ListNotificationChannels = %v", err)
	}
	for _, item := range list {
		if item.ID == created.ID {
			if item.Config.WebhookURL.Value != "" || item.Config.BotToken.Value != "" {
				t.Fatalf("secrets leaked in list response: %+v", item.Config)
			}
		}
	}

	// 5. Verify database row is encrypted at rest.
	row, err := f.st.Q().GetNotificationChannel(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetNotificationChannel DB = %v", err)
	}
	rawEncrypted := string(row.ConfigEnc)
	if strings.Contains(rawEncrypted, plainWebhookURL) {
		t.Fatal("plaintext webhook URL found in database row config_enc!")
	}
	if strings.Contains(rawEncrypted, plainBotToken) {
		t.Fatal("plaintext bot token found in database row config_enc!")
	}

	// Decrypt with encryptor to prove it is correctly encrypted.
	decryptedBytes, err := f.enc.Open(row.ConfigEnc)
	if err != nil {
		t.Fatalf("enc.Open(row.ConfigEnc) = %v", err)
	}
	var storedCfg domain.ChannelConfig
	if err := json.Unmarshal(decryptedBytes, &storedCfg); err != nil {
		t.Fatalf("json.Unmarshal = %v", err)
	}
	if storedCfg.WebhookURL != plainWebhookURL {
		t.Fatalf("stored WebhookURL = %q, want %q", storedCfg.WebhookURL, plainWebhookURL)
	}
	if storedCfg.BotToken != plainBotToken {
		t.Fatalf("stored BotToken = %q, want %q", storedCfg.BotToken, plainBotToken)
	}
}

func TestEngineLifecycleCancelledStartCtx(t *testing.T) {
	t.Parallel()
	f := newIntegrationFixture(t)
	ctx := withUser(t.Context(), f.admin)

	var counter atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	ch, err := f.h.CreateNotificationChannel(ctx, &oas.NotificationChannelInput{
		Name:    "webhook-f1",
		Kind:    oas.NotificationChannelKindWebhook,
		Enabled: oas.NewOptBool(true),
		Config: oas.NotificationChannelConfig{
			URL:                 oas.NewOptString(srv.URL),
			AllowPrivateNetwork: oas.NewOptBool(true),
			AllowInsecureHTTP:   oas.NewOptBool(true),
		},
	})
	if err != nil {
		t.Fatalf("CreateNotificationChannel = %v", err)
	}
	defer func() { _ = f.h.DeleteNotificationChannel(ctx, oas.DeleteNotificationChannelParams{ChannelId: ch.ID}) }()

	rule, err := f.h.CreateNotificationRule(ctx, &oas.NotificationRuleInput{
		Name:       "rule-f1",
		EventKind:  oas.NotificationRuleEventKindCircuitOpened,
		ChannelIds: []uuid.UUID{ch.ID},
	})
	if err != nil {
		t.Fatalf("CreateNotificationRule = %v", err)
	}
	defer func() { _ = f.h.DeleteNotificationRule(ctx, oas.DeleteNotificationRuleParams{RuleId: rule.ID}) }()

	startCtx, cancelStart := context.WithCancel(ctx)
	if err := f.notifyEngine.Start(startCtx); err != nil {
		t.Fatalf("Start = %v", err)
	}
	cancelStart() // Cancel fx OnStart ctx immediately!

	time.Sleep(50 * time.Millisecond)

	f.notifyEngine.OnEvent(ctx, domain.Event{
		Type:  "circuit_opened",
		Title: "Circuit opened",
		Level: "warn",
	})

	if got := waitDeliveries(t, f, &counter, 1); got != 1 {
		t.Fatalf("counter after cancelled startCtx = %d, want 1 (engine workers died prematurely)", got)
	}

	stopCtx, cancelStop := context.WithTimeout(ctx, 2*time.Second)
	defer cancelStop()
	if err := f.notifyEngine.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
}

func TestEngineWorkerBoundRestart(t *testing.T) {
	t.Parallel()
	f := newIntegrationFixture(t)
	ctx := withUser(t.Context(), f.admin)

	var counter atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	ch, err := f.h.CreateNotificationChannel(ctx, &oas.NotificationChannelInput{
		Name:    "webhook-f9",
		Kind:    oas.NotificationChannelKindWebhook,
		Enabled: oas.NewOptBool(true),
		Config: oas.NotificationChannelConfig{
			URL:                 oas.NewOptString(srv.URL),
			AllowPrivateNetwork: oas.NewOptBool(true),
			AllowInsecureHTTP:   oas.NewOptBool(true),
		},
	})
	if err != nil {
		t.Fatalf("CreateNotificationChannel = %v", err)
	}
	defer func() { _ = f.h.DeleteNotificationChannel(ctx, oas.DeleteNotificationChannelParams{ChannelId: ch.ID}) }()

	rule, err := f.h.CreateNotificationRule(ctx, &oas.NotificationRuleInput{
		Name:       "rule-f9",
		EventKind:  oas.NotificationRuleEventKindCircuitOpened,
		ChannelIds: []uuid.UUID{ch.ID},
	})
	if err != nil {
		t.Fatalf("CreateNotificationRule = %v", err)
	}
	defer func() { _ = f.h.DeleteNotificationRule(ctx, oas.DeleteNotificationRuleParams{RuleId: rule.ID}) }()

	f.notifyEngine.MaxWorkerEvents = 1
	if err := f.notifyEngine.Start(ctx); err != nil {
		t.Fatalf("Start = %v", err)
	}
	defer func() { _ = f.notifyEngine.Stop(ctx) }()

	// Emit 5 events. With 2 workers and bound=1, each worker only processes 1 before dying without restart.
	for range 5 {
		f.notifyEngine.OnEvent(ctx, domain.Event{
			Type:  "circuit_opened",
			Title: "Circuit opened",
			Level: "warn",
		})
	}

	if got := waitDeliveries(t, f, &counter, 5); got != 5 {
		t.Fatalf("counter after inner bound = %d, want 5 (workers exited silently)", got)
	}
}

func TestUpdateChannelDeletesWebhookEndpointOnKindChange(t *testing.T) {
	t.Parallel()
	f := newIntegrationFixture(t)
	ctx := withUser(t.Context(), f.admin)

	ch, err := f.h.CreateNotificationChannel(ctx, &oas.NotificationChannelInput{
		Name:    "webhook-to-discord",
		Kind:    oas.NotificationChannelKindWebhook,
		Enabled: oas.NewOptBool(true),
		Config: oas.NotificationChannelConfig{
			URL: oas.NewOptString("https://example.com/webhook"),
		},
	})
	if err != nil {
		t.Fatalf("CreateNotificationChannel(webhook) = %v", err)
	}
	defer func() { _ = f.h.DeleteNotificationChannel(ctx, oas.DeleteNotificationChannelParams{ChannelId: ch.ID}) }()

	// Verify endpoint row exists.
	if _, err = f.webhookStore.FindEndpoint(ctx, ch.ID.String()); err != nil {
		t.Fatalf("FindEndpoint before update = %v", err)
	}

	// Update channel kind to Discord.
	_, err = f.h.UpdateNotificationChannel(ctx, &oas.NotificationChannelUpdate{
		Name:    "webhook-to-discord",
		Kind:    oas.NotificationChannelKindDiscord,
		Enabled: oas.NewOptBool(true),
		Config: oas.NewOptNotificationChannelConfig(oas.NotificationChannelConfig{
			WebhookURL: oas.NewOptString("https://discord.com/api/webhooks/123/token"),
		}),
	}, oas.UpdateNotificationChannelParams{ChannelId: ch.ID})
	if err != nil {
		t.Fatalf("UpdateNotificationChannel(discord) = %v", err)
	}

	// Verify endpoint row was deleted.
	if _, err := f.webhookStore.FindEndpoint(ctx, ch.ID.String()); err == nil {
		t.Fatal("FindEndpoint after kind change succeeded, want not found / error")
	}
}

func waitDeliveries(t *testing.T, f *integrationFixture, counter *atomic.Int64, want int64) int64 {
	t.Helper()
	w := testdiag.Watch(2*time.Second, f.st.Pool())
	deadline := time.Now().Add(2 * time.Second)
	for range 100 {
		if time.Now().After(deadline) || counter.Load() >= want {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	w.Stop()

	got := counter.Load()
	if got >= want {
		return got
	}

	w.Report(t, context.DeadlineExceeded)
	pollGraceDeliveries(t, counter, want)
	logBufferOutput(t, f.logs)
	return got
}

func pollGraceDeliveries(t *testing.T, counter *atomic.Int64, want int64) {
	t.Helper()
	graceStart := time.Now()
	graceDeadline := graceStart.Add(20 * time.Second)
	for range 1000 {
		if time.Now().After(graceDeadline) || counter.Load() >= want {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	late := counter.Load()
	graceStr := "exhausted"
	if late >= want {
		graceStr = time.Since(graceStart).Round(time.Millisecond).String()
	}
	t.Logf("notifydiag: delivered=%d/%d after deadline, grace=%s", late, want, graceStr)
}

func logBufferOutput(t *testing.T, logs *testdiag.LogBuffer) {
	t.Helper()
	if logs == nil {
		t.Log("notifydiag: no warn records")
		return
	}
	out := logs.String()
	if out == "" {
		t.Log("notifydiag: no warn records")
		return
	}
	t.Logf("%s", out)
}
