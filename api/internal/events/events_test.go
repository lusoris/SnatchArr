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
		{"EVENT_TYPE_CIRCUIT_OPENED", "circuit_opened"},
		{"EVENT_TYPE_QUEUE_ITEM_STRUCK", "queue_item_struck"},
		{"EVENT_TYPE_QUEUE_ITEM_REMOVED", "queue_item_removed"},
		{"EVENT_TYPE_QUEUE_ITEM_IMPORTED", "queue_item_imported"},
		{"EVENT_TYPE_QUEUE_ITEM_IMPORT_FAILED", "queue_item_import_failed"},
		{"already_lower", "already_lower"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := events.EventName(tc.in); got != tc.want {
			t.Fatalf("EventName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
