// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain_test

import (
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestChannelKindValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   domain.ChannelKind
		want bool
	}{
		// Positive.
		{"discord", domain.ChannelDiscord, true},
		{"slack", domain.ChannelSlack, true},
		{"telegram", domain.ChannelTelegram, true},
		{"ntfy", domain.ChannelNtfy, true},
		{"gotify", domain.ChannelGotify, true},
		{"teams", domain.ChannelTeams, true},
		{"webhook", domain.ChannelWebhook, true},
		// Negative.
		{"unknown kind", domain.ChannelKind("email"), false},
		{"empty string", domain.ChannelKind(""), false},
		// Boundary.
		{"case sensitive", domain.ChannelKind("Discord"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.in.Valid(); got != tc.want {
				t.Errorf("ChannelKind(%q).Valid() = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestRuleEventKindValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   domain.RuleEventKind
		want bool
	}{
		// Positive.
		{"run_failure_streak", domain.RuleRunFailureStreak, true},
		{"circuit_opened", domain.RuleCircuitOpened, true},
		{"instance_unreachable", domain.RuleInstanceUnreachable, true},
		{"stamina_exhausted", domain.RuleStaminaExhausted, true},
		{"seerr_request_snatched", domain.RuleSeerrRequestSnatched, true},
		// Negative.
		{"unknown event", domain.RuleEventKind("search_failed"), false},
		{"empty string", domain.RuleEventKind(""), false},
		// Boundary.
		{"case sensitive", domain.RuleEventKind("Circuit_Opened"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.in.Valid(); got != tc.want {
				t.Errorf("RuleEventKind(%q).Valid() = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestChannelConfigSecrets(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		cfg       domain.ChannelConfig
		hasSecret bool
	}{
		{"empty", domain.ChannelConfig{}, false},
		{"webhook_url", domain.ChannelConfig{WebhookURL: "https://example.com/hook"}, true},
		{"token", domain.ChannelConfig{Token: "secret-token"}, true},
		{"bot_token", domain.ChannelConfig{BotToken: "bot:token"}, true},
		{"app_token", domain.ChannelConfig{AppToken: "app:token"}, true},
		{"password", domain.ChannelConfig{Password: "secret-pass"}, true},
		{"secret", domain.ChannelConfig{Secret: "signing-secret"}, true},
		{"public only", domain.ChannelConfig{ServerURL: "https://ntfy.sh", Topic: "alerts"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.cfg.HasSecret(); got != tc.hasSecret {
				t.Errorf("HasSecret() = %v, want %v", got, tc.hasSecret)
			}
			redacted := tc.cfg.Redacted()
			if redacted.HasSecret() {
				t.Errorf("Redacted() still has secrets: %+v", redacted)
			}
		})
	}
}

func TestChannelConfigMergeSecrets(t *testing.T) {
	t.Parallel()
	existing := domain.ChannelConfig{
		WebhookURL: "https://old.example.com",
		Token:      "old-token",
		BotToken:   "old-bot",
	}
	incoming := domain.ChannelConfig{
		ServerURL: "https://new.example.com",
		Token:     "new-token",
	}
	merged := incoming.MergeSecrets(existing)
	if merged.WebhookURL != "https://old.example.com" {
		t.Errorf("MergeSecrets WebhookURL = %q, want https://old.example.com", merged.WebhookURL)
	}
	if merged.Token != "new-token" {
		t.Errorf("MergeSecrets Token = %q, want new-token", merged.Token)
	}
	if merged.BotToken != "old-bot" {
		t.Errorf("MergeSecrets BotToken = %q, want old-bot", merged.BotToken)
	}
}
