// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package snatch

import (
	"testing"
	"time"

	"github.com/jonboulle/clockwork"

	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

var contactStart = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)

func workersAt(st *store.Store, now time.Time) *Workers {
	return NewWorkers(st, clockwork.NewFakeClockAt(now))
}

// lastContact is the newest contact, nil when none is recorded.
func lastContact(t *testing.T, w *Workers) *time.Time {
	t.Helper()
	at, ok, err := w.LastContact(t.Context())
	if err != nil {
		t.Fatalf("LastContact() = %v", err)
	}
	if !ok {
		return nil
	}
	return &at
}

func seen(t *testing.T, w *Workers, workerID string) {
	t.Helper()
	if err := w.Seen(t.Context(), workerID); err != nil {
		t.Fatalf("Seen(%q) = %v", workerID, err)
	}
}

// No contact reads as nil; the newest contact over all workers wins; a late write with an
// older time does not move a worker's contact back.
func TestWorkersLastContact(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	if at := lastContact(t, workersAt(st, contactStart)); at != nil {
		t.Fatalf("LastContact() with no worker = %v, want nil", at)
	}
	newest := contactStart.Add(time.Minute)
	seen(t, workersAt(st, contactStart), "w1")
	seen(t, workersAt(st, newest), "w2")
	seen(t, workersAt(st, contactStart.Add(-time.Hour)), "w2")
	if at := lastContact(t, workersAt(st, newest)); at == nil || !at.Equal(newest) {
		t.Fatalf("LastContact() = %v, want %v", at, newest)
	}
}

// An empty worker id is refused by the table.
func TestWorkersSeenRefusesEmptyID(t *testing.T) {
	t.Parallel()
	w := workersAt(storetest.New(t), contactStart)
	if err := w.Seen(t.Context(), ""); err == nil {
		t.Fatal(`Seen("") = nil, want an error`)
	}
	if at := lastContact(t, w); at != nil {
		t.Fatalf("LastContact() after a refused write = %v, want nil", at)
	}
}

// Purge deletes workers silent strictly before the cutoff, at most limit per call.
func TestWorkersPurge(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		limit int32
		want  int64
	}{
		{"before the cutoff only", 10, 2},
		{"bounded by the limit", 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			st := storetest.New(t)
			seen(t, workersAt(st, contactStart.Add(-2*time.Second)), "old1")
			seen(t, workersAt(st, contactStart.Add(-time.Second)), "old2")
			seen(t, workersAt(st, contactStart), "at-cutoff")
			n, err := workersAt(st, contactStart).Purge(t.Context(), contactStart, tc.limit)
			if err != nil || n != tc.want {
				t.Fatalf("Purge() = %d, %v; want %d", n, err, tc.want)
			}
			if at := lastContact(t, workersAt(st, contactStart)); at == nil || !at.Equal(contactStart) {
				t.Fatalf("LastContact() after Purge = %v, want the worker at the cutoff", at)
			}
		})
	}
}
