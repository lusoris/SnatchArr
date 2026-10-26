// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func seedTestUser(t *testing.T, f *integrationFixture, u domain.User) {
	t.Helper()
	_, err := f.st.Pool().Exec(t.Context(),
		"INSERT INTO users (id, username, password_hash, role, created_at, updated_at) VALUES ($1, $2, 'hash', $3, NOW(), NOW()) ON CONFLICT (id) DO NOTHING",
		u.ID, u.Username, string(u.Role),
	)
	if err != nil {
		t.Fatalf("seedTestUser: %v", err)
	}
}

func TestUserNotifyTargetsCRUDAndSecurity(t *testing.T) {
	t.Parallel()
	f := newIntegrationFixture(t)

	userA := domain.User{ID: uuid.New(), Username: "alice", Role: domain.RoleViewer}
	userB := domain.User{ID: uuid.New(), Username: "bob", Role: domain.RoleRequester}
	seedTestUser(t, f, userA)
	seedTestUser(t, f, userB)

	ctxA := withUser(t.Context(), userA)
	ctxB := withUser(t.Context(), userB)

	const plantedSecret = "super-secret-discord-token-88888"

	// 1. Positive creation of target with secret.
	in := &oas.UserNotifyTargetInput{
		Kind:    oas.NotificationChannelKindDiscord,
		Enabled: oas.NewOptBool(true),
		Config: oas.NotificationChannelConfig{
			WebhookURL: oas.NewOptString("https://discord.com/api/webhooks/123/" + plantedSecret),
		},
	}
	created, err := f.h.CreateMyNotifyTarget(ctxA, in)
	if err != nil {
		t.Fatalf("CreateMyNotifyTarget: %v", err)
	}
	if created.ID == uuid.Nil {
		t.Fatal("created target ID is nil")
	}
	if created.Kind != oas.NotificationChannelKindDiscord {
		t.Fatalf("created.Kind = %q, want %q", created.Kind, oas.NotificationChannelKindDiscord)
	}
	if !created.Enabled {
		t.Fatalf("created.Enabled = false, want true")
	}

	// 2. Security check: responses NEVER contain secret string.
	list, err := f.h.ListMyNotifyTargets(ctxA)
	if err != nil {
		t.Fatalf("ListMyNotifyTargets: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("ListMyNotifyTargets len = %d, want 1", len(list))
	}
	listJSON, err := json.Marshal(list)
	if err != nil {
		t.Fatalf("marshal list: %v", err)
	}
	if strings.Contains(string(listJSON), plantedSecret) {
		t.Fatalf("security violation: ListMyNotifyTargets exposed secret in JSON: %s", string(listJSON))
	}

	got, err := f.h.GetMyNotifyTarget(ctxA, oas.GetMyNotifyTargetParams{TargetId: created.ID})
	if err != nil {
		t.Fatalf("GetMyNotifyTarget: %v", err)
	}
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal got: %v", err)
	}
	if strings.Contains(string(gotJSON), plantedSecret) {
		t.Fatalf("security violation: GetMyNotifyTarget exposed secret in JSON: %s", string(gotJSON))
	}

	// 3. Update without config keeps the old sealed value.
	upNoConfig := &oas.UserNotifyTargetUpdate{
		Kind:    oas.NotificationChannelKindDiscord,
		Enabled: oas.NewOptBool(false),
	}
	updated, err := f.h.UpdateMyNotifyTarget(ctxA, upNoConfig, oas.UpdateMyNotifyTargetParams{TargetId: created.ID})
	if err != nil {
		t.Fatalf("UpdateMyNotifyTarget(no config): %v", err)
	}
	if updated.Enabled {
		t.Fatalf("updated.Enabled = true, want false")
	}
	// Verify through internal notify service that the stored secret is intact.
	storedWithSecrets, err := f.notifySvc.GetUserTargetWithSecrets(ctxA, userA.ID, created.ID)
	if err != nil {
		t.Fatalf("GetUserTargetWithSecrets: %v", err)
	}
	if !strings.Contains(storedWithSecrets.Config.WebhookURL, plantedSecret) {
		t.Fatalf("old sealed config was lost on update without config: %s", storedWithSecrets.Config.WebhookURL)
	}

	// 4. Cross-user isolation: User B accessing User A's target gets 404 (ErrNotFound).
	_, err = f.h.GetMyNotifyTarget(ctxB, oas.GetMyNotifyTargetParams{TargetId: created.ID})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetMyNotifyTarget(other user) err = %v, want domain.ErrNotFound", err)
	}
	_, err = f.h.UpdateMyNotifyTarget(ctxB, upNoConfig, oas.UpdateMyNotifyTargetParams{TargetId: created.ID})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("UpdateMyNotifyTarget(other user) err = %v, want domain.ErrNotFound", err)
	}
	err = f.h.DeleteMyNotifyTarget(ctxB, oas.DeleteMyNotifyTargetParams{TargetId: created.ID})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("DeleteMyNotifyTarget(other user) err = %v, want domain.ErrNotFound", err)
	}
	err = f.h.TestMyNotifyTarget(ctxB, oas.TestMyNotifyTargetParams{TargetId: created.ID})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("TestMyNotifyTarget(other user) err = %v, want domain.ErrNotFound", err)
	}

	// 5. Validation (Negative / Boundary): invalid kind or invalid config -> 400 (ErrBadRequest).
	badKind := &oas.UserNotifyTargetInput{
		Kind: "carrier_pigeon",
	}
	_, err = f.h.CreateMyNotifyTarget(ctxA, badKind)
	if !errors.Is(err, domain.ErrBadRequest) {
		t.Fatalf("CreateMyNotifyTarget(bad kind) err = %v, want domain.ErrBadRequest", err)
	}

	missingWebhookURL := &oas.UserNotifyTargetInput{
		Kind:   oas.NotificationChannelKindDiscord,
		Config: oas.NotificationChannelConfig{},
	}
	_, err = f.h.CreateMyNotifyTarget(ctxA, missingWebhookURL)
	if !errors.Is(err, domain.ErrBadRequest) {
		t.Fatalf("CreateMyNotifyTarget(missing webhook_url) err = %v, want domain.ErrBadRequest", err)
	}

	missingWebhookField := &oas.UserNotifyTargetInput{
		Kind:   oas.NotificationChannelKindWebhook,
		Config: oas.NotificationChannelConfig{},
	}
	_, err = f.h.CreateMyNotifyTarget(ctxA, missingWebhookField)
	if !errors.Is(err, domain.ErrBadRequest) {
		t.Fatalf("CreateMyNotifyTarget(missing webhook url) err = %v, want domain.ErrBadRequest", err)
	}

	// 6. TestMyNotifyTarget delivery.
	var testCalled atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		testCalled.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	ntfyTarget, err := f.h.CreateMyNotifyTarget(ctxA, &oas.UserNotifyTargetInput{
		Kind:    oas.NotificationChannelKindNtfy,
		Enabled: oas.NewOptBool(true),
		Config: oas.NotificationChannelConfig{
			ServerURL: oas.NewOptString(srv.URL),
			Topic:     oas.NewOptString("test-topic"),
		},
	})
	if err != nil {
		t.Fatalf("CreateMyNotifyTarget(ntfy): %v", err)
	}

	if testErr := f.h.TestMyNotifyTarget(ctxA, oas.TestMyNotifyTargetParams{TargetId: ntfyTarget.ID}); testErr != nil {
		t.Fatalf("TestMyNotifyTarget: %v", testErr)
	}
	if !testCalled.Load() {
		t.Fatal("test target did not deliver notification")
	}

	// 7. Delete target.
	if delErr := f.h.DeleteMyNotifyTarget(ctxA, oas.DeleteMyNotifyTargetParams{TargetId: created.ID}); delErr != nil {
		t.Fatalf("DeleteMyNotifyTarget: %v", delErr)
	}
	_, err = f.h.GetMyNotifyTarget(ctxA, oas.GetMyNotifyTargetParams{TargetId: created.ID})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetMyNotifyTarget after delete err = %v, want domain.ErrNotFound", err)
	}
}

func TestUserNotifyTargetsDeliveryFencing(t *testing.T) {
	t.Parallel()
	f := newIntegrationFixture(t)

	userA := domain.User{ID: uuid.New(), Username: "alice-fence", Role: domain.RoleViewer}
	userB := domain.User{ID: uuid.New(), Username: "bob-fence", Role: domain.RoleRequester}
	seedTestUser(t, f, userA)
	seedTestUser(t, f, userB)

	ctxA := withUser(t.Context(), userA)

	var deliverCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deliverCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	_, err := f.h.CreateMyNotifyTarget(ctxA, &oas.UserNotifyTargetInput{
		Kind:    oas.NotificationChannelKindNtfy,
		Enabled: oas.NewOptBool(true),
		Config: oas.NotificationChannelConfig{
			ServerURL: oas.NewOptString(srv.URL),
			Topic:     oas.NewOptString("fence-topic"),
		},
	})
	if err != nil {
		t.Fatalf("CreateMyNotifyTarget: %v", err)
	}

	// Case 1: Adult event for user A is dropped (fails closed).
	evAdult := domain.Event{
		ID:         201,
		UserID:     &userA.ID,
		IsAdult:    true,
		EntityType: "adult_scene",
		Type:       "seerr_request_snatched",
		Title:      "Adult Item",
	}
	f.notifyEngine.ProcessEvent(t.Context(), evAdult)
	if got := deliverCount.Load(); got != 0 {
		t.Fatalf("deliverCount after adult event = %d, want 0 (adult event must be dropped)", got)
	}

	// Case 2: Adult event by EntityType prefix is also dropped (fails closed).
	evAdultPrefix := domain.Event{
		ID:         202,
		UserID:     &userA.ID,
		EntityType: "adult_movie",
		Type:       "seerr_request_snatched",
		Title:      "Adult Movie",
	}
	f.notifyEngine.ProcessEvent(t.Context(), evAdultPrefix)
	if got := deliverCount.Load(); got != 0 {
		t.Fatalf("deliverCount after adult_movie prefix event = %d, want 0", got)
	}

	// Case 3: Non-adult event for user A is sent.
	evNonAdult := domain.Event{
		ID:         203,
		UserID:     &userA.ID,
		IsAdult:    false,
		EntityType: "movie",
		Type:       "seerr_request_snatched",
		Title:      "Regular Movie",
	}
	f.notifyEngine.ProcessEvent(t.Context(), evNonAdult)
	if got := deliverCount.Load(); got != 1 {
		t.Fatalf("deliverCount after non-adult event = %d, want 1", got)
	}

	// Case 4: Non-adult event for user B is NOT sent to user A's target.
	evUserB := domain.Event{
		ID:         204,
		UserID:     &userB.ID,
		IsAdult:    false,
		EntityType: "movie",
		Type:       "seerr_request_snatched",
		Title:      "Bob's Movie",
	}
	f.notifyEngine.ProcessEvent(t.Context(), evUserB)
	if got := deliverCount.Load(); got != 1 {
		t.Fatalf("deliverCount after user B event = %d, want 1 (user B event must not reach user A)", got)
	}
}
