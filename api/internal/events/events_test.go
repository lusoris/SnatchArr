// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package events_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

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
		{"EVENT_TYPE_WATCHAWARE_GATE_DECIDED", "watchaware_gate_decided"},
		{"already_lower", "already_lower"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := events.EventName(tc.in); got != tc.want {
			t.Fatalf("EventName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFramePublishAndID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		frame      events.Frame
		wantJSONID bool
		wantSSEID  string
	}{
		{
			name: "positive recorded event carries frame id",
			frame: events.Frame{
				ID:         42,
				InstanceID: uuid.MustParse("00000000-0000-0000-0000-000000000001"),
				Timestamp:  time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC),
				Level:      "info",
				Type:       "run_queued",
				Title:      "missing snatch queued",
			},
			wantJSONID: true,
			wantSSEID:  "42",
		},
		{
			name: "negative unrecorded event omits frame id",
			frame: events.Frame{
				ID:         0,
				InstanceID: uuid.MustParse("00000000-0000-0000-0000-000000000001"),
				Timestamp:  time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC),
				Level:      "info",
				Type:       "queue_item_struck",
				Title:      "queue strike",
			},
			wantJSONID: false,
			wantSSEID:  "",
		},
		{
			name: "boundary max int64 id preserved",
			frame: events.Frame{
				ID:         math.MaxInt64,
				InstanceID: uuid.MustParse("00000000-0000-0000-0000-000000000001"),
				Timestamp:  time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC),
				Level:      "debug",
				Type:       "search_completed",
				Title:      "search finished",
			},
			wantJSONID: true,
			wantSSEID:  strconv.FormatInt(math.MaxInt64, 10),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Check JSON serialization
			data, err := json.Marshal(tc.frame)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			hasJSONID := strings.Contains(string(data), `"id":`)
			if hasJSONID != tc.wantJSONID {
				t.Fatalf("json %s: hasJSONID=%v, want %v", string(data), hasJSONID, tc.wantJSONID)
			}

			// Check SSE hub publish delivers expected id
			bus := events.New(slog.New(slog.DiscardHandler))
			srv := httptest.NewServer(bus.Hub().Handler())
			t.Cleanup(srv.Close)

			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			t.Cleanup(cancel)

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
			if err != nil {
				t.Fatalf("NewRequestWithContext: %v", err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("client.Do: %v", err)
			}
			t.Cleanup(func() { _ = resp.Body.Close() })

			// Wait for client to register in hub
			for range 50 {
				if bus.Hub().ClientCount() > 0 {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}

			bus.Publish(ctx, tc.frame)

			reader := bufio.NewReader(resp.Body)
			var gotID string
			foundEvent := false
			for range 20 {
				line, readErr := reader.ReadString('\n')
				if readErr != nil {
					if errorsIsEOF(readErr) {
						break
					}
					t.Fatalf("ReadString: %v", readErr)
				}
				line = strings.TrimSpace(line)
				if after, ok := strings.CutPrefix(line, "id: "); ok {
					gotID = after
				}
				if strings.HasPrefix(line, "event: ") {
					foundEvent = true
				}
				if line == "" && foundEvent {
					break
				}
			}

			if gotID != tc.wantSSEID {
				t.Fatalf("SSE id = %q, want %q", gotID, tc.wantSSEID)
			}
		})
	}
}

func errorsIsEOF(err error) bool {
	return errors.Is(err, io.EOF)
}
