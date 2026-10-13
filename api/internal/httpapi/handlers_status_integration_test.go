// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"log/slog"
	"testing"
	"time"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/auth"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

// The system status carries the newest worker contact, leaves it out while no worker has
// called, and fails when the contact cannot be read (gap audit A1-01).
func TestSystemStatusWorkerContact(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		contact bool
		broken  bool
	}{
		{"no worker yet", false, false},
		{"a worker called", true, false},
		{"contact unreadable", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			st := storetest.New(t)
			clk := clock.NewFake()
			authSvc, err := auth.NewService(st, nil, clk, id.New(), slog.New(slog.DiscardHandler))
			if err != nil {
				t.Fatalf("auth.NewService() = %v", err)
			}
			workers := snatch.NewWorkers(st, clk)
			if tc.contact {
				if seenErr := workers.Seen(t.Context(), "w1"); seenErr != nil {
					t.Fatalf("Seen() = %v", seenErr)
				}
			}
			if tc.broken {
				if _, dropErr := st.Pool().Exec(t.Context(), `DROP TABLE workers`); dropErr != nil {
					t.Fatalf("drop workers: %v", dropErr)
				}
			}
			h := &Handlers{auth: authSvc, workers: workers}
			got, err := h.GetSystemStatus(t.Context())
			if (err != nil) != tc.broken {
				t.Fatalf("GetSystemStatus() error = %v, want error %v", err, tc.broken)
			}
			if tc.broken {
				return
			}
			// Postgres keeps microseconds.
			want := clk.Now().Truncate(time.Microsecond)
			at, set := got.WorkerLastContactAt.Get()
			if set != tc.contact || (set && !at.Equal(want)) {
				t.Fatalf("worker_last_contact_at = %v (set %v), want set %v at %v", at, set, tc.contact, want)
			}
		})
	}
}
