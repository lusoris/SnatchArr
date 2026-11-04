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
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

type writerCall struct {
	Method   string
	ClientID uuid.UUID
	ItemID   string
	IDs      []string
	Level    int
}

type fakePriorityWriter struct {
	mu          sync.Mutex
	calls       []writerCall
	failMethod  string
	failErr     error
	partialDone []string
}

// SetItemPriority records a priority mutation call and optional injected error.
func (w *fakePriorityWriter) SetItemPriority(_ context.Context, c domain.DownloadClient, _, itemID string, level int) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, writerCall{Method: "SetItemPriority", ClientID: c.ID, ItemID: itemID, Level: level})
	if w.failMethod == "SetItemPriority" {
		return w.failErr
	}
	return nil
}

// MoveToTop records a MoveToTop call and optional injected error.
func (w *fakePriorityWriter) MoveToTop(_ context.Context, c domain.DownloadClient, _ string, ids []string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, writerCall{Method: "MoveToTop", ClientID: c.ID, IDs: slices.Clone(ids)})
	if w.failMethod == "MoveToTop" {
		return w.failErr
	}
	return nil
}

// MoveToBottom records a MoveToBottom call and optional injected error.
func (w *fakePriorityWriter) MoveToBottom(_ context.Context, c domain.DownloadClient, _ string, ids []string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, writerCall{Method: "MoveToBottom", ClientID: c.ID, IDs: slices.Clone(ids)})
	if w.failMethod == "MoveToBottom" {
		return w.failErr
	}
	return nil
}

// PauseItems records a PauseItems call and optional partial write or error.
func (w *fakePriorityWriter) PauseItems(_ context.Context, c domain.DownloadClient, _ string, ids []string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, writerCall{Method: "PauseItems", ClientID: c.ID, IDs: slices.Clone(ids)})
	if w.failMethod == "PauseItems" {
		if len(w.partialDone) > 0 {
			return &PartialWriteError{Done: slices.Clone(w.partialDone), Err: w.failErr}
		}
		return w.failErr
	}
	return nil
}

// ResumeItems records a ResumeItems call and optional partial write or error.
func (w *fakePriorityWriter) ResumeItems(_ context.Context, c domain.DownloadClient, _ string, ids []string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, writerCall{Method: "ResumeItems", ClientID: c.ID, IDs: slices.Clone(ids)})
	if w.failMethod == "ResumeItems" {
		if len(w.partialDone) > 0 {
			return &PartialWriteError{Done: slices.Clone(w.partialDone), Err: w.failErr}
		}
		return w.failErr
	}
	return nil
}

func assertAllowListedCallsOnly(t *testing.T, calls []writerCall) {
	t.Helper()
	allow := map[string]bool{
		"SetItemPriority": true,
		"MoveToTop":       true,
		"PauseItems":      true,
		"ResumeItems":     true,
	}
	for _, c := range calls {
		if !allow[c.Method] || c.Method == "MoveToBottom" {
			t.Fatalf("disallowed writer method called: %s", c.Method)
		}
	}
}

func assertCall(t *testing.T, calls []writerCall, method string, ids []string) {
	t.Helper()
	for _, c := range calls {
		if c.Method == method && slices.Equal(c.IDs, ids) {
			return
		}
	}
	t.Fatalf("call %s with IDs %v not found in %+v", method, ids, calls)
}

// TestPriorityReconcilerGroupModeRule_Mixed verifies that any row on observe puts the whole group on observe with zero writes.
func TestPriorityReconcilerGroupModeRule_Mixed(t *testing.T) {
	t.Parallel()
	r, writer, inputs := actGroupFixture("observe")
	c1 := inputs.Clients[0]
	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(writer.calls) != 0 {
		t.Fatalf("expected 0 writer calls for mixed group, got %d: %+v", len(writer.calls), writer.calls)
	}
	p1, err := r.Latest(c1.ID)
	if err != nil || p1.Mode != "observe" || !slices.Equal(p1.ObserveRowNames, []string{"tv"}) {
		t.Fatalf("p1 = %+v, err = %v", p1, err)
	}
}

// TestPriorityReconcilerGroupModeRule_AllAct verifies that when all rows are act, allow-listed writes execute and ObserveRowNames is empty.
func TestPriorityReconcilerGroupModeRule_AllAct(t *testing.T) {
	t.Parallel()
	r, writer, inputs := actGroupFixture("act")
	c1 := inputs.Clients[0]
	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(writer.calls) == 0 {
		t.Fatal("expected writer calls for all-act group")
	}
	assertAllowListedCallsOnly(t, writer.calls)
	p1, err := r.Latest(c1.ID)
	if err != nil || p1.Mode != "act" || len(p1.ObserveRowNames) != 0 {
		t.Fatalf("p1 = %+v, err = %v", p1, err)
	}
}

// TestPriorityReconcilerObserveZeroWrites verifies that observe mode never invokes the writer for any client kind.
func TestPriorityReconcilerObserveZeroWrites(t *testing.T) {
	t.Parallel()
	for _, kind := range []domain.DownloadClientKind{domain.ClientQBittorrent, domain.ClientNZBGet, domain.ClientSABnzbd} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			firstInst, secondInst := uuid.New(), uuid.New()
			client := domain.DownloadClient{ID: uuid.New(), Name: "client1", InstanceID: &firstInst, Category: "movies", PriorityMode: "observe", Kind: kind, BaseURL: "http://client:8080", Enabled: true}
			client2 := domain.DownloadClient{ID: uuid.New(), Name: "client2", InstanceID: &secondInst, Category: "tv", PriorityMode: "observe", Kind: kind, BaseURL: "http://client:8080", Enabled: true}
			inputs := priorityInputs{
				Clients:   []domain.DownloadClient{client, client2},
				Instances: []domain.Instance{{ID: firstInst, Kind: domain.KindRadarr}, {ID: secondInst, Kind: domain.KindSonarr, DownloadPriority: new(100)}},
				Preset:    domain.PriorityPreset{domain.KindRadarr: 1},
			}
			obs := PriorityObservation{
				Items: []PriorityItem{
					{Category: "movies", ID: "item1", State: domain.DownloadItemQueued, Priority: 1},
					{Category: "tv", ID: "item2", State: domain.DownloadItemQueued, Priority: 2},
				},
			}
			writer := &fakePriorityWriter{}
			clk := clock.NewFake()
			observer := priorityObserverFunc(func(context.Context, domain.DownloadClient, string) (PriorityObservation, error) {
				return obs, nil
			})
			r := newPriorityReconciler(&prioritySourceFake{inputs: inputs}, observer, writer, clk, slog.New(slog.DiscardHandler))
			if err := r.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(writer.calls) != 0 {
				t.Fatalf("observe mode produced %d calls: %+v", len(writer.calls), writer.calls)
			}
		})
	}
}

// TestPriorityReconcilerActQBit verifies qBittorrent calls MoveToTop in reverse plan order of queued attributed items and skips unattributed items or when queueing is disabled.
func TestPriorityReconcilerActQBit(t *testing.T) {
	t.Parallel()
	inputs := actTestInputs(domain.ClientQBittorrent, "act")
	obs := PriorityObservation{
		Items: []PriorityItem{
			{Category: "movies", ID: "movie-queued", State: domain.DownloadItemQueued, Priority: 1},
			{Category: "tv", ID: "tv-queued", State: domain.DownloadItemQueued, Priority: 2},
			{Category: "other", ID: "unattributed-queued", State: domain.DownloadItemQueued, Priority: 3},
			{Category: "movies", ID: "movie-active", State: domain.DownloadItemActive, Priority: 0},
		},
	}
	writer := &fakePriorityWriter{}
	r := newActTestReconciler(&prioritySourceFake{inputs: inputs}, &obs, writer)
	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(writer.calls) != 2 {
		t.Fatalf("expected 2 MoveToTop calls, got %d: %+v", len(writer.calls), writer.calls)
	}
	if writer.calls[0].Method != "MoveToTop" || !slices.Equal(writer.calls[0].IDs, []string{"tv-queued"}) {
		t.Fatalf("call 0 = %+v, want MoveToTop with tv-queued", writer.calls[0])
	}
	if writer.calls[1].Method != "MoveToTop" || !slices.Equal(writer.calls[1].IDs, []string{"movie-queued"}) {
		t.Fatalf("call 1 = %+v, want MoveToTop with movie-queued", writer.calls[1])
	}
	for _, call := range writer.calls {
		if slices.Contains(call.IDs, "unattributed-queued") {
			t.Fatalf("unattributed hash in writer call: %+v", call)
		}
		if slices.Contains(call.IDs, "movie-active") {
			t.Fatalf("active hash in MoveToTop call: %+v", call)
		}
	}
	assertAllowListedCallsOnly(t, writer.calls)

	assertQueueingDisabled(t, inputs, obs)
}

// TestPriorityReconcilerActNZBGetSAB verifies priority mutations, hand-set retention and rewrites for leveled clients.
func TestPriorityReconcilerActNZBGetSAB(t *testing.T) {
	t.Parallel()
	kinds := []struct {
		kind domain.DownloadClientKind
		def  int
		hand int
	}{
		{domain.ClientNZBGet, 0, 50},
		{domain.ClientSABnzbd, 0, 1},
	}
	for _, tc := range kinds {
		t.Run(string(tc.kind), func(t *testing.T) {
			t.Parallel()
			inputs := actTestInputs(tc.kind, "act")
			client := inputs.Clients[0]
			lastWritten := -50
			source := &prioritySourceFake{
				inputs: inputs,
				ownership: map[string]PriorityOwnership{
					client.ID.String() + "\nitem3": {ClientID: client.ID, ItemID: "item3", LastWrittenPriority: &lastWritten},
				},
			}
			obs := PriorityObservation{
				Items: []PriorityItem{
					{Category: "movies", ID: "item1", State: domain.DownloadItemQueued, Priority: tc.def},
					{Category: "movies", ID: "item2", State: domain.DownloadItemQueued, Priority: tc.hand},
					{Category: "movies", ID: "item3", State: domain.DownloadItemQueued, Priority: lastWritten},
					{Category: "tv", ID: "item4", State: domain.DownloadItemQueued, Priority: tc.def},
				},
			}
			writer := &fakePriorityWriter{}
			r := newActTestReconciler(source, &obs, writer)
			if err := r.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			assertHandSetUntouched(t, writer.calls, "item2")
			assertPlannedPriorityCalls(t, r, client.ID, writer.calls, []string{"item1", "item3"})
			assertAllowListedCallsOnly(t, writer.calls)

			writer.calls = nil
			if err := r.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			assertHandSetUntouched(t, writer.calls, "item2")
		})
	}
}

// TestPriorityReconcilerPauseResumeLifecycle tests pause when higher-rank waits, pause retention during download, and resume when higher-rank finishes.
func TestPriorityReconcilerPauseResumeLifecycle(t *testing.T) {
	t.Parallel()
	inputs := actTestInputs(domain.ClientQBittorrent, "act")
	c2 := inputs.Clients[1]
	source := &prioritySourceFake{inputs: inputs}
	clk := clock.NewFake()
	writer := &fakePriorityWriter{}

	currentItems := []PriorityItem{
		{Category: "tv", ID: "low", State: domain.DownloadItemActive},
		{Category: "movies", ID: "high", State: domain.DownloadItemQueued},
		{Category: "tv", ID: "user-paused", State: domain.DownloadItemPaused},
	}
	obsFn := priorityObserverFunc(func(context.Context, domain.DownloadClient, string) (PriorityObservation, error) {
		return PriorityObservation{Items: currentItems}, nil
	})
	r := newPriorityReconciler(source, obsFn, writer, clk, slog.New(slog.DiscardHandler))

	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertCall(t, writer.calls, "PauseItems", []string{"low"})
	if !source.ownership[c2.ID.String()+"\nlow"].PausedBySnatchArr {
		t.Fatal("expected low to be flagged as paused by SnatchArr in store")
	}

	writer.calls = nil
	currentItems = []PriorityItem{
		{Category: "tv", ID: "low", State: domain.DownloadItemPaused},
		{Category: "movies", ID: "high", State: domain.DownloadItemActive},
		{Category: "tv", ID: "user-paused", State: domain.DownloadItemPaused},
	}
	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertNoWriterMethod(t, writer.calls, "ResumeItems")

	writer.calls = nil
	currentItems = []PriorityItem{
		{Category: "tv", ID: "low", State: domain.DownloadItemPaused},
		{Category: "tv", ID: "user-paused", State: domain.DownloadItemPaused},
	}
	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertCall(t, writer.calls, "ResumeItems", []string{"low"})
	for _, call := range writer.calls {
		if call.Method == "ResumeItems" && slices.Contains(call.IDs, "user-paused") {
			t.Fatalf("user-paused item was resumed: %+v", call)
		}
	}
	if source.ownership[c2.ID.String()+"\nlow"].PausedBySnatchArr {
		t.Fatal("expected low paused_by_snatcharr flag to be cleared in store")
	}
}

// TestPriorityReconcilerWriteErrorMidPlan verifies that an error halts subsequent writes for the group and retains earlier writes.
func TestPriorityReconcilerWriteErrorMidPlan(t *testing.T) {
	t.Parallel()
	firstInst, secondInst := uuid.New(), uuid.New()
	goodURL := "http://sab:8080"
	c1 := domain.DownloadClient{ID: uuid.New(), Name: "sab1", InstanceID: &firstInst, Category: "movies", PriorityMode: "act", Kind: domain.ClientSABnzbd, BaseURL: goodURL, Enabled: true}
	c2 := domain.DownloadClient{ID: uuid.New(), Name: "sab2", InstanceID: &secondInst, Category: "tv", PriorityMode: "act", Kind: domain.ClientSABnzbd, BaseURL: goodURL, Enabled: true}
	inputs := priorityInputs{
		Clients:   []domain.DownloadClient{c1, c2},
		Instances: []domain.Instance{{ID: firstInst, Kind: domain.KindRadarr}, {ID: secondInst, Kind: domain.KindSonarr, DownloadPriority: new(100)}},
		Preset:    domain.PriorityPreset{domain.KindRadarr: 1},
	}
	obs := PriorityObservation{
		Items: []PriorityItem{
			{Category: "movies", ID: "m1", State: domain.DownloadItemQueued, Priority: 0},
			{Category: "movies", ID: "m2", State: domain.DownloadItemActive, Priority: 0},
			{Category: "tv", ID: "t1", State: domain.DownloadItemQueued, Priority: 0},
		},
	}
	source := &prioritySourceFake{inputs: inputs}
	writer := &fakePriorityWriter{
		failMethod: "SetItemPriority",
		failErr:    errors.New("connection failed"),
	}
	clk := clock.NewFake()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	observer := priorityObserverFunc(func(context.Context, domain.DownloadClient, string) (PriorityObservation, error) {
		return obs, nil
	})
	r := newPriorityReconciler(source, observer, writer, clk, logger)
	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, call := range writer.calls {
		if call.Method == "PauseItems" {
			t.Fatalf("expected PauseItems to be skipped after priority error, got call: %+v", call)
		}
	}
	count := 0
	for _, call := range writer.calls {
		if call.Method == "SetItemPriority" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("priority calls = %d, want exactly one", count)
	}
	logStr := logs.String()
	if !strings.Contains(logStr, `"msg":"download priority write failed"`) || !strings.Contains(logStr, `"stage":"priority"`) {
		t.Fatalf("expected write failed log with stage priority, got: %s", logStr)
	}
	if strings.Contains(logStr, "connection failed") {
		t.Fatalf("remote error text leaked in logs: %s", logStr)
	}
}

// TestPriorityReconcilerOwnershipPrune keeps paused-by-SnatchArr markers while the client still lists the item,
// even when the row's category turned empty or ambiguous, and drops them once the item is gone.
func TestPriorityReconcilerOwnershipPrune(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		c1Category string
		c2Category string
		listed     bool
		wantKept   bool
	}{
		{"positive: category unchanged", "movies", "tv", true, true},
		{"negative: row category emptied", "movies", "", true, true},
		{"negative: row category now ambiguous", "tv", "tv", true, true},
		{"boundary: item no longer listed", "movies", "tv", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			firstInst, secondInst := uuid.New(), uuid.New()
			c1 := domain.DownloadClient{ID: uuid.New(), Name: "c1", InstanceID: &firstInst, Category: tc.c1Category, PriorityMode: "observe", Kind: domain.ClientQBittorrent, BaseURL: "http://qbit:8080", Enabled: true}
			c2 := domain.DownloadClient{ID: uuid.New(), Name: "c2", InstanceID: &secondInst, Category: tc.c2Category, PriorityMode: "observe", Kind: domain.ClientQBittorrent, BaseURL: "http://qbit:8080", Enabled: true}
			key := c2.ID.String() + "\nlow"
			source := &prioritySourceFake{
				inputs:    priorityInputs{Clients: []domain.DownloadClient{c1, c2}, Instances: []domain.Instance{{ID: firstInst, Kind: domain.KindRadarr}, {ID: secondInst, Kind: domain.KindSonarr}}},
				ownership: map[string]PriorityOwnership{key: {ClientID: c2.ID, ItemID: "low", PausedBySnatchArr: true}},
			}
			var items []PriorityItem
			if tc.listed {
				items = []PriorityItem{{Category: "tv", ID: "low", State: domain.DownloadItemPaused}}
			}
			obsFn := priorityObserverFunc(func(context.Context, domain.DownloadClient, string) (PriorityObservation, error) {
				return PriorityObservation{Items: items}, nil
			})
			r := newPriorityReconciler(source, obsFn, &fakePriorityWriter{}, clock.NewFake(), slog.New(slog.DiscardHandler))
			if err := r.Tick(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, kept := source.ownership[key]; kept != tc.wantKept {
				t.Fatalf("ownership kept = %v, want %v", kept, tc.wantKept)
			}
		})
	}
}

func newActTestReconciler(source prioritySource, obs *PriorityObservation, writer PriorityWriter) *Reconciler {
	observer := priorityObserverFunc(func(context.Context, domain.DownloadClient, string) (PriorityObservation, error) { return *obs, nil })
	return newPriorityReconciler(source, observer, writer, clock.NewFake(), slog.New(slog.DiscardHandler))
}

func actTestInputs(kind domain.DownloadClientKind, mode string) priorityInputs {
	first, second := uuid.New(), uuid.New()
	return priorityInputs{
		Clients: []domain.DownloadClient{
			{ID: uuid.New(), Name: "movies", InstanceID: &first, Category: "movies", PriorityMode: mode, Kind: kind, BaseURL: "http://client:8080", Enabled: true},
			{ID: uuid.New(), Name: "tv", InstanceID: &second, Category: "tv", PriorityMode: mode, Kind: kind, BaseURL: "http://client:8080", Enabled: true},
		},
		Instances: []domain.Instance{{ID: first, Kind: domain.KindRadarr, DownloadPriority: new(1)}, {ID: second, Kind: domain.KindSonarr, DownloadPriority: new(100)}},
	}
}

func actGroupFixture(secondMode string) (*Reconciler, *fakePriorityWriter, priorityInputs) {
	inputs := actTestInputs(domain.ClientQBittorrent, "act")
	inputs.Clients[1].PriorityMode = secondMode
	obs := PriorityObservation{Items: []PriorityItem{
		{Category: "movies", ID: "m1", State: domain.DownloadItemQueued, Priority: 1},
		{Category: "tv", ID: "t1", State: domain.DownloadItemQueued, Priority: 2},
	}}
	writer := &fakePriorityWriter{}
	r := newActTestReconciler(&prioritySourceFake{inputs: inputs}, &obs, writer)
	return r, writer, inputs
}

func assertQueueingDisabled(t *testing.T, inputs priorityInputs, obs PriorityObservation) {
	t.Helper()
	writerDisabled := &fakePriorityWriter{}
	obsDisabled := obs
	obsDisabled.QueueingDisabled = true
	obsFn := priorityObserverFunc(func(context.Context, domain.DownloadClient, string) (PriorityObservation, error) {
		return obsDisabled, nil
	})
	rDisabled := newPriorityReconciler(&prioritySourceFake{inputs: inputs}, obsFn, writerDisabled, clock.NewFake(), slog.New(slog.DiscardHandler))
	if err := rDisabled.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, call := range writerDisabled.calls {
		if call.Method == "MoveToTop" {
			t.Fatalf("expected 0 MoveToTop calls when queueing disabled, got: %+v", call)
		}
	}
}

func assertHandSetUntouched(t *testing.T, calls []writerCall, id string) {
	t.Helper()
	for _, call := range calls {
		if call.Method == "SetItemPriority" && call.ItemID == id {
			t.Fatalf("hand-set %s mutated: %+v", id, call)
		}
	}
}

func assertPlannedPriorityCalls(t *testing.T, r *Reconciler, clientID uuid.UUID, calls []writerCall, required []string) {
	t.Helper()
	plan, err := r.Latest(clientID)
	if err != nil {
		t.Fatal(err)
	}
	levels := make(map[string]int)
	for _, call := range calls {
		if call.Method == "SetItemPriority" {
			levels[call.ItemID] = call.Level
		}
	}
	for _, move := range plan.Plan.Priority {
		level, found := levels[move.ID]
		if !found || level != move.To {
			t.Fatalf("%s recorded level=%d found=%v planned To=%d", move.ID, level, found, move.To)
		}
	}
	for _, id := range required {
		if _, found := levels[id]; !found {
			t.Fatalf("missing priority call for %s", id)
		}
	}
}

func assertNoWriterMethod(t *testing.T, calls []writerCall, method string) {
	t.Helper()
	for _, call := range calls {
		if call.Method == method {
			t.Fatalf("unexpected %s: %+v", method, call)
		}
	}
}
