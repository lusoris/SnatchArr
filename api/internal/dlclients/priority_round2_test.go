// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestReviewR1OversizedSchedulingCleanup(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		queued          int
		owned, schedule bool
	}{
		{0, true, false},
		{127, true, true},
		{128, true, false},
		{129, true, false},
		{129, false, false},
	} {
		t.Run(strconv.Itoa(tc.queued)+"/owned="+strconv.FormatBool(tc.owned), func(t *testing.T) {
			t.Parallel()
			inputs := actTestInputs(domain.ClientQBittorrent, "act")
			owner := inputs.Clients[0].ID
			key := owner.String() + "\nwithdrawn"
			source := &prioritySourceFake{inputs: inputs, ownership: map[string]PriorityOwnership{
				key: {ClientID: owner, ItemID: "withdrawn", PausedBySnatchArr: tc.owned},
			}}
			obs := oversizedCleanupObservation(tc.queued)
			writer := &fakePriorityWriter{}
			r := newActTestReconciler(source, &obs, writer)
			if err := r.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			want := 0
			if tc.owned {
				want++
				assertCall(t, writer.calls, "ResumeItems", []string{"withdrawn"})
			}
			if tc.schedule {
				want += tc.queued + 1
			}
			if len(writer.calls) != want || source.ownership[key].PausedBySnatchArr {
				t.Fatalf("calls=%+v want=%d ownership=%+v", writer.calls, want, source.ownership[key])
			}
		})
	}
}

func oversizedCleanupObservation(queued int) PriorityObservation {
	obs := PriorityObservation{Items: []PriorityItem{
		{ID: "withdrawn", Category: "other", State: domain.DownloadItemPaused},
		{ID: "tv", Category: "tv", State: domain.DownloadItemQueued, Priority: queued + 1},
	}}
	for i := range queued {
		obs.Items = append(obs.Items, PriorityItem{
			ID: strconv.Itoa(i), Category: "movies", State: domain.DownloadItemQueued, Priority: i + 1,
		})
	}
	return obs
}

func TestReviewR2GroupRotation(t *testing.T) {
	t.Parallel()
	for _, count := range []int{0, 16, 17, 40} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			t.Parallel()
			inputs := rotationTestInputs(count)
			r := newActTestReconciler(&prioritySourceFake{inputs: inputs}, &PriorityObservation{}, nil)
			clk := clock.NewFake()
			r.clk = clk
			var mu sync.Mutex
			var observed []uuid.UUID
			r.observer = priorityObserverFunc(func(_ context.Context, row domain.DownloadClient, _ string) (PriorityObservation, error) {
				mu.Lock()
				defer mu.Unlock()
				observed = append(observed, row.ID)
				return PriorityObservation{}, nil
			})
			ticks := max(1, (count+15)/16)
			for tick := range ticks {
				observed = nil
				if err := r.Tick(t.Context()); err != nil {
					t.Fatal(err)
				}
				assertRotationBatch(t, inputs.Clients, observed, tick)
				assertRotationPlans(t, r, inputs.Clients, tick, clk.Now())
				clk.Advance(time.Minute)
			}
			// After a full pass, wrap to the first batch without changing its order.
			observed = nil
			if err := r.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			assertRotationBatch(t, inputs.Clients, observed, 0)
		})
	}
}

func rotationTestInputs(count int) priorityInputs {
	inputs := actTestInputs(domain.ClientQBittorrent, "act")
	row := inputs.Clients[0]
	inputs.Clients = make([]domain.DownloadClient, count)
	for i := range count {
		inputs.Clients[i] = row
		inputs.Clients[i].ID = uuid.New()
		inputs.Clients[i].BaseURL = "http://client" + strconv.Itoa(i)
	}
	return inputs
}

func assertRotationBatch(t *testing.T, rows []domain.DownloadClient, observed []uuid.UUID, tick int) {
	t.Helper()
	start := min(tick*16, len(rows))
	end := min(start+16, len(rows))
	if len(observed) != end-start {
		t.Fatalf("tick=%d observed=%d want=%d", tick+1, len(observed), end-start)
	}
	for _, row := range rows[start:end] {
		if !slices.Contains(observed, row.ID) {
			t.Errorf("tick=%d missing group=%s", tick+1, row.BaseURL)
		}
	}
}

func assertRotationPlans(t *testing.T, r *Reconciler, rows []domain.DownloadClient, tick int, now time.Time) {
	t.Helper()
	for i, row := range rows {
		plan, err := r.Latest(row.ID)
		if i >= (tick+1)*16 {
			if !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("unvisited group=%d plan err=%v", i, err)
			}
			continue
		}
		wantTime := now.Add(-time.Duration(tick-i/16) * time.Minute)
		if err != nil || !plan.ObservedAt.Equal(wantTime) {
			t.Errorf("group=%d observed=%v want=%v err=%v", i, plan.ObservedAt, wantTime, err)
		}
	}
}

func TestReviewR2LaterGroupCleanup(t *testing.T) {
	t.Parallel()
	inputs := rotationTestInputs(17)
	owner := inputs.Clients[16].ID
	key := owner.String() + "\nwithdrawn"
	source := &prioritySourceFake{inputs: inputs, ownership: map[string]PriorityOwnership{
		key: {ClientID: owner, ItemID: "withdrawn", PausedBySnatchArr: true},
	}}
	obs := PriorityObservation{Items: []PriorityItem{{ID: "withdrawn", Category: "other", State: domain.DownloadItemPaused}}}
	writer := &fakePriorityWriter{}
	r := newActTestReconciler(source, &obs, writer)
	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(writer.calls) != 0 || !source.ownership[key].PausedBySnatchArr {
		t.Fatal("skipped group's ownership changed before its turn")
	}
	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertCall(t, writer.calls, "ResumeItems", []string{"withdrawn"})
	if len(writer.calls) != 1 || source.ownership[key].PausedBySnatchArr {
		t.Fatalf("cleanup calls=%+v ownership=%+v", writer.calls, source.ownership[key])
	}
}

func TestReviewR3SupportedPlanningKinds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind      domain.DownloadClientKind
		supported bool
	}{
		{domain.ClientQBittorrent, true},
		{domain.ClientNZBGet, true},
		{domain.ClientSABnzbd, true},
		{domain.ClientTransmission, false},
		{domain.ClientDeluge, false},
		{domain.ClientRTorrent, false},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			t.Parallel()
			inputs := actTestInputs(tc.kind, "observe")
			r := newActTestReconciler(&prioritySourceFake{inputs: inputs}, &PriorityObservation{}, nil)
			if err := r.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			_, err := r.Latest(inputs.Clients[0].ID)
			if tc.supported && err != nil {
				t.Fatalf("supported client missing plan: %v", err)
			}
			if !tc.supported && !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("excluded client produced plan: %v", err)
			}
		})
	}
}

func TestReviewR4DisabledObserveRow(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{false, true} {
		t.Run(strconv.FormatBool(enabled), func(t *testing.T) {
			t.Parallel()
			r, writer, inputs := actGroupFixture("act")
			source := r.source.(*prioritySourceFake)
			row := inputs.Clients[0]
			row.ID = uuid.New()
			row.Enabled = enabled
			row.PriorityMode = "observe"
			source.inputs.Clients = append(slices.Clone(inputs.Clients), row)
			if err := r.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			if (len(writer.calls) > 0) == enabled {
				t.Fatalf("observe row enabled=%v calls=%+v", enabled, writer.calls)
			}
			_, err := r.Latest(row.ID)
			if !enabled && !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("disabled row produced plan: %v", err)
			}
		})
	}
}
