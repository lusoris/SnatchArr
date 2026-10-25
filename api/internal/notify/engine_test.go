// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package notify

import (
	"context"
	"testing"
	"time"

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
