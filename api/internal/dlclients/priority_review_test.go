// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestReviewT1AmbiguousCategories(t *testing.T) {
	t.Parallel()
	for _, count := range []int{0, 1, 2, 3, 4} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			t.Parallel()
			inputs := actTestInputs(domain.ClientQBittorrent, "act")
			rows := make([]domain.DownloadClient, count)
			for i := range count {
				rows[i] = inputs.Clients[0]
				rows[i].ID = uuid.New()
			}
			clients := categoryClients(rows)
			_, hasClient := clients["movies"]
			if hasClient != (count == 1) {
				t.Fatalf("%d rows: category attributed = %v", count, hasClient)
			}
			items, unknown, _ := attributePriorityItems([]PriorityItem{{Category: "movies", ID: "one", State: domain.DownloadItemQueued}}, rows, inputs.Instances, inputs.Preset, nil)
			want := 0
			if count == 1 {
				want = 1
			}
			if len(items) != want || unknown != 1-want {
				t.Fatalf("items=%+v unknown=%d", items, unknown)
			}
		})
	}
}

func TestReviewT2OwnershipResumeCandidates(t *testing.T) {
	t.Parallel()
	owner, remapped := uuid.New(), uuid.New()
	tests := []struct {
		name          string
		current       uuid.UUID
		owned, paused bool
		want          int
	}{
		{"empty category", uuid.Nil, true, true, 1},
		{"ambiguous category", uuid.Nil, true, true, 1},
		{"different row", remapped, true, true, 1},
		{"still attributed", owner, true, true, 0},
		{"user paused", uuid.Nil, false, true, 0},
		{"already active", uuid.Nil, true, false, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			state := domain.DownloadItemActive
			if tc.paused {
				state = domain.DownloadItemPaused
			}
			own := []PriorityOwnership{{ClientID: owner, ItemID: "low", PausedBySnatchArr: tc.owned}}
			got := ownershipResumeCandidates([]PriorityItem{{ID: "low", State: state}}, own, map[string]uuid.UUID{"low": tc.current}, nil, "act")
			if len(got) != tc.want {
				t.Fatalf("resume=%v want %d", got, tc.want)
			}
			if tc.want > 0 && ownershipClients(nil, own)["low"] != owner {
				t.Fatal("lost persisted owner")
			}
		})
	}
}

func TestReviewT3ItemBoundary(t *testing.T) {
	t.Parallel()
	for _, count := range []int{0, maxPriorityItemsPerGroup, maxPriorityItemsPerGroup + 1} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			t.Parallel()
			inputs := actTestInputs(domain.ClientQBittorrent, "act")
			obs := PriorityObservation{Items: make([]PriorityItem, count)}
			for i := range count {
				obs.Items[i] = PriorityItem{ID: strconv.Itoa(i), Category: "movies", State: domain.DownloadItemQueued, Priority: i + 1}
			}
			writer := &fakePriorityWriter{}
			r := newActTestReconciler(&prioritySourceFake{inputs: inputs}, &obs, writer)
			var logs bytes.Buffer
			r.logger = slog.New(slog.NewTextHandler(&logs, nil))
			if err := r.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			_, err := r.Latest(inputs.Clients[0].ID)
			if (err != nil) != (count > maxPriorityItemsPerGroup) {
				t.Fatalf("count=%d plan err=%v", count, err)
			}
			if count > maxPriorityItemsPerGroup && !strings.Contains(logs.String(), "items truncated") {
				t.Fatal("missing truncation log")
			}
		})
	}
}

func TestReviewT3MutationBoundary(t *testing.T) {
	t.Parallel()
	for _, count := range []int{0, maxPriorityMutationsPerGroup, maxPriorityMutationsPerGroup + 1} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			t.Parallel()
			inputs := actTestInputs(domain.ClientSABnzbd, "act")
			writer := &fakePriorityWriter{}
			r := newActTestReconciler(&prioritySourceFake{inputs: inputs}, &PriorityObservation{}, writer)
			plan := domain.DownloadClientPlan{}
			clients := make(map[string]uuid.UUID)
			for i := range count {
				id := strconv.Itoa(i)
				plan.Priority = append(plan.Priority, domain.PriorityWrite{ID: id, To: 1})
				clients[id] = inputs.Clients[0].ID
			}
			ctx, cancel := context.WithTimeout(t.Context(), ObserveTimeout)
			defer cancel()
			r.executeActPlan(ctx, inputs.Clients[0], "", plan, false, nil, clients, nil)
			want := count
			if count > maxPriorityMutationsPerGroup {
				want = 0
			}
			if len(writer.calls) != want {
				t.Fatalf("calls=%d want=%d", len(writer.calls), want)
			}
		})
	}
}

func TestReviewT5ModeRecheck(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"none", "observe", "new observe row", "disabled", "read error"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			inputs := actTestInputs(domain.ClientQBittorrent, "act")
			source := &prioritySourceFake{inputs: inputs}
			writer := &fakePriorityWriter{}
			obs := PriorityObservation{Items: []PriorityItem{
				{ID: "high", Category: "movies", State: domain.DownloadItemQueued, Priority: 1},
				{ID: "low", Category: "tv", State: domain.DownloadItemQueued, Priority: 2},
			}}
			r := newActTestReconciler(source, &obs, writer)
			r.observer = priorityObserverFunc(func(context.Context, domain.DownloadClient, string) (PriorityObservation, error) {
				mutateModeSource(source, change)
				return obs, nil
			})
			if err := r.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			if (len(writer.calls) > 0) != (change == "none") {
				t.Fatalf("change=%s calls=%+v", change, writer.calls)
			}
		})
	}
}

func mutateModeSource(source *prioritySourceFake, change string) {
	switch change {
	case "observe":
		source.inputs.Clients[0].PriorityMode = "observe"
	case "disabled":
		source.inputs.Clients[0].Enabled = false
	case "new observe row":
		row := source.inputs.Clients[0]
		row.ID = uuid.New()
		row.PriorityMode = "observe"
		source.inputs.Clients = append(slices.Clone(source.inputs.Clients), row)
	case "read error":
		source.err = errors.New("configuration unavailable")
	}
}

func TestReviewT6WithdrawnPauseCleanup(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, mode, category string
		owned                bool
		want                 int
	}{
		{"mode withdrawn", "observe", "tv", true, 1},
		{"attribution withdrawn", "act", "", true, 1},
		{"user paused observe", "observe", "tv", false, 0},
		{"user paused unattributed", "act", "", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			inputs := actTestInputs(domain.ClientQBittorrent, tc.mode)
			owner := inputs.Clients[1].ID
			inputs.Clients[1].Category = tc.category
			key := owner.String() + "\nlow"
			source := &prioritySourceFake{inputs: inputs, ownership: map[string]PriorityOwnership{key: {ClientID: owner, ItemID: "low", PausedBySnatchArr: tc.owned}}}
			obs := PriorityObservation{Items: []PriorityItem{
				{ID: "low", Category: "tv", State: domain.DownloadItemPaused},
				{ID: "high", Category: "movies", State: domain.DownloadItemQueued, Priority: 1},
			}}
			writer := &fakePriorityWriter{}
			r := newActTestReconciler(source, &obs, writer)
			if err := r.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(writer.calls) != tc.want {
				t.Fatalf("calls=%+v want=%d", writer.calls, tc.want)
			}
			if tc.want > 0 {
				assertCall(t, writer.calls, "ResumeItems", []string{"low"})
			}
			if source.ownership[key].PausedBySnatchArr {
				t.Fatal("ownership not cleared")
			}
		})
	}
}

// TestReviewManualResumeRepairFailure verifies that failed ownership repair preserves the durable record
// and blocks writes and plan publication in act and observe modes.
func TestReviewManualResumeRepairFailure(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"act", "observe"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			inputs := actTestInputs(domain.ClientQBittorrent, mode)
			owner := inputs.Clients[1].ID
			key := owner.String() + "\nlow"
			base := &prioritySourceFake{inputs: inputs, ownership: map[string]PriorityOwnership{
				key:                         {ClientID: owner, ItemID: "low", PausedBySnatchArr: true},
				owner.String() + "\npaused": {ClientID: owner, ItemID: "paused", PausedBySnatchArr: true},
			}}
			checked := false
			source := ownershipFaultSource{prioritySourceFake: base, check: func(context.Context, string) error {
				checked = true
				return errors.New("ownership database unavailable")
			}}
			obs := PriorityObservation{Items: []PriorityItem{
				{ID: "low", Category: "tv", State: domain.DownloadItemActive},
				{ID: "high", Category: "movies", State: domain.DownloadItemQueued, Priority: 1},
				{ID: "paused", Category: "tv", State: domain.DownloadItemPaused},
			}}
			writer := &fakePriorityWriter{}
			r := newActTestReconciler(source, &obs, writer)
			var logs bytes.Buffer
			r.logger = slog.New(slog.NewTextHandler(&logs, nil))
			if err := r.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			if !checked || len(writer.calls) != 0 || !base.ownership[key].PausedBySnatchArr {
				t.Fatalf("repair failure not contained: checked=%v calls=%+v ownership=%+v", checked, writer.calls, base.ownership[key])
			}
			if !strings.Contains(logs.String(), "stage=repair_ownership") {
				t.Fatalf("missing repair failure diagnostic: %s", logs.String())
			}
			if _, err := r.Latest(owner); err == nil {
				t.Fatal("failed ownership repair published a plan")
			}
		})
	}
}

func TestReviewT7SingleRankNoOrdering(t *testing.T) {
	t.Parallel()
	for _, count := range []int{0, 1, 2} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			t.Parallel()
			inputs := actTestInputs(domain.ClientQBittorrent, "act")
			obs := PriorityObservation{Items: []PriorityItem{{ID: "unattributed-first", Category: "other", State: domain.DownloadItemQueued, Priority: 1}}}
			for i := range count {
				obs.Items = append(obs.Items, PriorityItem{ID: strconv.Itoa(i), Category: "movies", State: domain.DownloadItemQueued, Priority: i + 2})
			}
			writer := &fakePriorityWriter{}
			r := newActTestReconciler(&prioritySourceFake{inputs: inputs}, &obs, writer)
			if err := r.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(writer.calls) != 0 {
				t.Fatalf("single rank must not reorder: %+v", writer.calls)
			}
		})
	}
}

func TestReviewT3GroupBoundary(t *testing.T) {
	t.Parallel()
	for _, count := range []int{0, maxPriorityGroupsPerTick, maxPriorityGroupsPerTick + 1} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			t.Parallel()
			inputs := actTestInputs(domain.ClientQBittorrent, "act")
			row := inputs.Clients[0]
			inputs.Clients = make([]domain.DownloadClient, count)
			for i := range count {
				row.ID = uuid.New()
				row.BaseURL = "http://client" + strconv.Itoa(i)
				inputs.Clients[i] = row
			}
			r := newActTestReconciler(&prioritySourceFake{inputs: inputs}, &PriorityObservation{}, &fakePriorityWriter{})
			if err := r.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(r.plans) != min(count, maxPriorityGroupsPerTick) {
				t.Fatalf("observed groups=%d count=%d", len(r.plans), count)
			}
		})
	}
}

func TestReviewT3TruncatedObservationRetainsOwnership(t *testing.T) {
	t.Parallel()
	inputs := actTestInputs(domain.ClientQBittorrent, "act")
	owner := inputs.Clients[1].ID
	key := owner.String() + "\nlow"
	source := &prioritySourceFake{inputs: inputs, ownership: map[string]PriorityOwnership{
		key: {ClientID: owner, ItemID: "low", PausedBySnatchArr: true},
	}}
	obs := PriorityObservation{ItemsTruncated: true}
	writer := &fakePriorityWriter{}
	r := newActTestReconciler(source, &obs, writer)
	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Latest(owner); err == nil {
		t.Fatal("truncated observation produced a plan")
	}
	if !source.ownership[key].PausedBySnatchArr || len(writer.calls) != 0 {
		t.Fatal("truncated observation changed ownership or client")
	}
}

func TestReviewT6CleanupSurvivesActFailure(t *testing.T) {
	t.Parallel()
	inputs := actTestInputs(domain.ClientSABnzbd, "act")
	owner := inputs.Clients[1].ID
	key := owner.String() + "\nlow"
	source := &prioritySourceFake{inputs: inputs, ownership: map[string]PriorityOwnership{
		key: {ClientID: owner, ItemID: "low", PausedBySnatchArr: true},
	}}
	obs := PriorityObservation{Items: []PriorityItem{
		{ID: "low", Category: "other", State: domain.DownloadItemPaused},
		{ID: "high", Category: "movies", State: domain.DownloadItemQueued},
		{ID: "other-low", Category: "tv", State: domain.DownloadItemQueued},
	}}
	writer := &fakePriorityWriter{failMethod: "SetItemPriority", failErr: errors.New("priority unavailable")}
	r := newActTestReconciler(source, &obs, writer)
	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertCall(t, writer.calls, "ResumeItems", []string{"low"})
	if source.ownership[key].PausedBySnatchArr {
		t.Fatal("withdrawn ownership survived unrelated scheduling failure")
	}
	if len(writer.calls) != 2 || writer.calls[0].Method != "ResumeItems" {
		t.Fatalf("cleanup must precede scheduling: %+v", writer.calls)
	}
}

func TestReviewT5ModeWithdrawnDuringCleanup(t *testing.T) {
	t.Parallel()
	inputs := actTestInputs(domain.ClientSABnzbd, "act")
	owner := inputs.Clients[1].ID
	key := owner.String() + "\nlow"
	source := &prioritySourceFake{inputs: inputs, ownership: map[string]PriorityOwnership{
		key: {ClientID: owner, ItemID: "low", PausedBySnatchArr: true},
	}}
	obs := PriorityObservation{Items: []PriorityItem{
		{ID: "low", Category: "other", State: domain.DownloadItemPaused},
		{ID: "high", Category: "movies", State: domain.DownloadItemQueued},
		{ID: "other-low", Category: "tv", State: domain.DownloadItemQueued},
	}}
	writer := ownershipCompletionWriter{fakePriorityWriter: &fakePriorityWriter{}, after: func() { source.inputs.Clients[0].PriorityMode = "observe" }}
	r := newActTestReconciler(source, &obs, writer)
	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(writer.calls) != 1 || writer.calls[0].Method != "ResumeItems" {
		t.Fatalf("act writes after mode withdrawal: %+v", writer.calls)
	}
	plan, err := r.Latest(owner)
	if err != nil || plan.Mode != "observe" {
		t.Fatalf("stale mode: %+v err=%v", plan, err)
	}
}
