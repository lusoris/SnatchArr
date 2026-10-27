// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package notify

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestRuleMatches_WatchawareGateDecided(t *testing.T) {
	t.Parallel()
	e := &Engine{}
	ctx := context.Background()
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		ruleKind  domain.RuleEventKind
		eventType string
		want      bool
	}{
		{
			name:      "positive: watchaware_gate_decided rule matches gate decided event",
			ruleKind:  domain.RuleWatchawareGateDecided,
			eventType: "watchaware_gate_decided",
			want:      true,
		},
		{
			name:      "negative: watchaware_gate_decided rule does not match seerr_request_snatched",
			ruleKind:  domain.RuleWatchawareGateDecided,
			eventType: "seerr_request_snatched",
			want:      false,
		},
		{
			name:      "negative: seerr rule does not match watchaware_gate_decided",
			ruleKind:  domain.RuleSeerrRequestSnatched,
			eventType: "watchaware_gate_decided",
			want:      false,
		},
		{
			name:      "boundary: empty event type does not match",
			ruleKind:  domain.RuleWatchawareGateDecided,
			eventType: "",
			want:      false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rule := domain.NotificationRule{EventKind: tc.ruleKind}
			ev := domain.Event{Type: tc.eventType}
			got := e.ruleMatches(ctx, rule, ev, now)
			if got != tc.want {
				t.Errorf("ruleMatches() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRuleMatches_RunFailureStreak_InvalidOrNilRunID(t *testing.T) {
	t.Parallel()
	e := &Engine{}
	ctx := context.Background()
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	rule := domain.NotificationRule{
		EventKind: domain.RuleRunFailureStreak,
		Threshold: 2,
	}
	validRunID := uuid.New()
	nilUUID := uuid.Nil

	tests := []struct {
		name string
		ev   domain.Event
		want bool
	}{
		{
			name: "negative: nil RunID returns false",
			ev: domain.Event{
				Type:       "run_finished",
				Level:      "error",
				InstanceID: uuid.New(),
				RunID:      nil,
			},
			want: false,
		},
		{
			name: "negative: empty uuid.Nil RunID returns false",
			ev: domain.Event{
				Type:       "run_finished",
				Level:      "error",
				InstanceID: uuid.New(),
				RunID:      &nilUUID,
			},
			want: false,
		},
		{
			name: "negative: wrong event type",
			ev: domain.Event{
				Type:       "search_dispatched",
				Level:      "error",
				InstanceID: uuid.New(),
				RunID:      &validRunID,
			},
			want: false,
		},
		{
			name: "negative: non-error level",
			ev: domain.Event{
				Type:       "run_finished",
				Level:      "info",
				InstanceID: uuid.New(),
				RunID:      &validRunID,
			},
			want: false,
		},
		{
			name: "negative: empty InstanceID",
			ev: domain.Event{
				Type:       "run_finished",
				Level:      "error",
				InstanceID: uuid.Nil,
				RunID:      &validRunID,
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := e.ruleMatches(ctx, rule, tc.ev, now)
			if got != tc.want {
				t.Errorf("ruleMatches() = %v, want %v", got, tc.want)
			}
		})
	}
}
