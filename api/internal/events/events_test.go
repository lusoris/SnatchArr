// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package events_test

import (
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/events"
)

func TestEventName(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		{"EVENT_TYPE_SEARCH_DISPATCHED", "search_dispatched"},
		{"EVENT_TYPE_RUN_FINISHED", "run_finished"},
		{"already_lower", "already_lower"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := events.EventName(tc.in); got != tc.want {
			t.Fatalf("EventName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
