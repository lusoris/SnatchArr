// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package notify_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/webhooks/out"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/notify"
)

func TestSanitizeError(t *testing.T) {
	t.Parallel()
	cfg := domain.ChannelConfig{
		WebhookURL: "https://discord.com/api/webhooks/123/token-secret",
		URL:        "https://webhook.site/api/v1/hook?token=webhook-secret-token",
		ServerURL:  "https://admin:userinfo-secret-pass@ntfy.example.com/alerts",
		Token:      "auth-token-12345",
		BotToken:   "bot-token-67890",
		AppToken:   "app-token-abcde",
		Password:   "top-secret-pass",
		Secret:     "hmac-key-secret",
	}

	cases := []struct {
		name       string
		err        error
		secretText string
	}{
		{name: "nil error", err: nil, secretText: ""},
		{name: "webhook url", err: errors.New("post https://discord.com/api/webhooks/123/token-secret failed: 500"), secretText: "https://discord.com/api/webhooks/123/token-secret"},
		{name: "url with token", err: errors.New("dial error to https://webhook.site/api/v1/hook?token=webhook-secret-token: eof"), secretText: "https://webhook.site/api/v1/hook?token=webhook-secret-token"},
		{name: "server url userinfo password", err: errors.New("basic auth failed with password userinfo-secret-pass"), secretText: "userinfo-secret-pass"},
		{name: "token", err: errors.New("invalid auth with auth-token-12345: rejected"), secretText: "auth-token-12345"},
		{name: "bot token", err: errors.New("telegram error with bot-token-67890: unauthorized"), secretText: "bot-token-67890"},
		{name: "app token", err: errors.New("gotify error with app-token-abcde: bad request"), secretText: "app-token-abcde"},
		{name: "password", err: errors.New("authentication failed for top-secret-pass"), secretText: "top-secret-pass"},
		{name: "secret", err: errors.New("signature mismatch using hmac-key-secret"), secretText: "hmac-key-secret"},
		{name: "clean error untouched", err: errors.New("connection timeout to remote host"), secretText: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := notify.SanitizeError(tc.err, cfg)
			if tc.err == nil {
				if got != "" {
					t.Errorf("SanitizeError(nil) = %q, want empty", got)
				}
				return
			}
			if tc.secretText != "" {
				if strings.Contains(got, tc.secretText) {
					t.Errorf("SanitizeError() failed to redact secret %q, got: %q", tc.secretText, got)
				}
				if !strings.Contains(got, "[REDACTED]") {
					t.Errorf("SanitizeError() missing [REDACTED] in %q", got)
				}
			} else if got != tc.err.Error() {
				t.Errorf("SanitizeError() altered clean error: got %q, want %q", got, tc.err.Error())
			}
		})
	}
}

type memStore struct {
	endpoints  map[string]out.Endpoint
	deliveries map[string]out.Delivery
}

func newMemStore() *memStore {
	return &memStore{
		endpoints:  make(map[string]out.Endpoint),
		deliveries: make(map[string]out.Delivery),
	}
}

func (m *memStore) SaveEndpoint(_ context.Context, e out.Endpoint) error {
	m.endpoints[e.ID] = e
	return nil
}

func (m *memStore) FindEndpoint(_ context.Context, id string) (out.Endpoint, error) {
	e, ok := m.endpoints[id]
	if !ok {
		return out.Endpoint{}, errors.New("not found")
	}
	return e, nil
}

func (m *memStore) ListEndpoints(_ context.Context, _ string) ([]out.Endpoint, error) {
	outList := make([]out.Endpoint, 0, len(m.endpoints))
	for _, e := range m.endpoints {
		outList = append(outList, e)
	}
	return outList, nil
}

func (m *memStore) DeleteEndpoint(_ context.Context, id string) error {
	delete(m.endpoints, id)
	return nil
}

func (m *memStore) SaveDelivery(_ context.Context, d out.Delivery) error {
	m.deliveries[d.ID] = d
	return nil
}

func (m *memStore) FindDelivery(_ context.Context, id string) (out.Delivery, error) {
	d, ok := m.deliveries[id]
	if !ok {
		return out.Delivery{}, errors.New("not found")
	}
	return d, nil
}

func (m *memStore) ListDeadLetters(_ context.Context) ([]out.Delivery, error) {
	return nil, nil
}

func TestDeliverWebhook(t *testing.T) {
	t.Parallel()
	const secret = "webhook-test-signing-key"
	var receivedBody []byte
	var receivedSig string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSig = r.Header.Get("X-Webhook-Signature")
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	ch := domain.NotificationChannel{
		ID:   uuid.New(),
		Name: "test-webhook",
		Kind: domain.ChannelWebhook,
		Config: domain.ChannelConfig{
			URL:                 srv.URL,
			Secret:              secret,
			AllowPrivateNetwork: true,
			AllowInsecureHTTP:   true,
		},
	}

	payload := notify.Payload{
		Event:     "circuit_opened",
		Timestamp: time.Now(),
		Title:     "Circuit Opened",
		Detail:    "3 consecutive failures",
		Level:     "warn",
	}

	store := newMemStore()
	logger := slog.New(slog.DiscardHandler)
	clk := clock.NewFake()

	if err := notify.Deliver(t.Context(), ch, payload, store, logger, clk); err != nil {
		t.Fatalf("Deliver webhook: %v", err)
	}

	if len(receivedBody) == 0 {
		t.Fatal("webhook server received empty body")
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(receivedBody)
	wantSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if receivedSig != wantSig {
		t.Fatalf("X-Webhook-Signature = %q, want %q", receivedSig, wantSig)
	}
}
