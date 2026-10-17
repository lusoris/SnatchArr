// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package queue_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/events"
	"github.com/lusoris/SnatchArr/api/internal/queue"
)

type fakeStore struct {
	settings    map[uuid.UUID]queue.Settings
	strikes     map[uuid.UUID]map[string]queue.Strike
	recorded    map[uuid.UUID][]queue.Decision
	forgotten   map[uuid.UUID][]string
	purgeCalled int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		settings:  make(map[uuid.UUID]queue.Settings),
		strikes:   make(map[uuid.UUID]map[string]queue.Strike),
		recorded:  make(map[uuid.UUID][]queue.Decision),
		forgotten: make(map[uuid.UUID][]string),
	}
}

func (f *fakeStore) Settings(_ context.Context, id uuid.UUID) (queue.Settings, error) {
	if s, ok := f.settings[id]; ok {
		return s, nil
	}
	return queue.DefaultSettings(), nil
}

func (f *fakeStore) Strikes(_ context.Context, id uuid.UUID) (map[string]queue.Strike, error) {
	out := make(map[string]queue.Strike)
	maps.Copy(out, f.strikes[id])
	return out, nil
}

func (f *fakeStore) Record(_ context.Context, id uuid.UUID, decisions []queue.Decision) error {
	f.recorded[id] = append(f.recorded[id], decisions...)
	if f.strikes[id] == nil {
		f.strikes[id] = make(map[string]queue.Strike)
	}
	for _, d := range decisions {
		if d.DownloadID != "" {
			f.strikes[id][d.DownloadID] = d.Next
		}
	}
	return nil
}

func (f *fakeStore) Forget(_ context.Context, id uuid.UUID, downloadID string) error {
	f.forgotten[id] = append(f.forgotten[id], downloadID)
	if f.strikes[id] != nil {
		delete(f.strikes[id], downloadID)
	}
	return nil
}

func (f *fakeStore) Purge(_ context.Context, _ int) (int64, error) {
	f.purgeCalled++
	return 0, nil
}

type fakeInstanceSource struct {
	instances []queue.Instance
	err       error
}

func (f *fakeInstanceSource) ListEnabledInstances(_ context.Context) ([]queue.Instance, error) {
	return f.instances, f.err
}

type fakeRemoval struct {
	kind      domain.AppKind
	id        int32
	blocklist bool
}

type fakeArrClient struct {
	items       map[string][]queue.Item
	queueErr    map[string]error
	removeErr   error
	removals    []fakeRemoval
	queueCalls  int
	removeCalls int
}

func newFakeArrClient() *fakeArrClient {
	return &fakeArrClient{
		items:    make(map[string][]queue.Item),
		queueErr: make(map[string]error),
	}
}

func (f *fakeArrClient) Queue(_ context.Context, kind domain.AppKind, baseURL, _ string, _, _ int) ([]queue.Item, error) {
	f.queueCalls++
	key := string(kind) + ":" + baseURL
	if err, ok := f.queueErr[key]; ok {
		return nil, err
	}
	return f.items[key], nil
}

func (f *fakeArrClient) RemoveQueueItem(_ context.Context, kind domain.AppKind, _, _ string, id int32, blocklist bool) error {
	f.removeCalls++
	f.removals = append(f.removals, fakeRemoval{kind: kind, id: id, blocklist: blocklist})
	return f.removeErr
}

type fakeEventPublisher struct {
	frames []events.Frame
}

func (f *fakeEventPublisher) Publish(_ context.Context, frame events.Frame) {
	f.frames = append(f.frames, frame)
}

type fakeMetricsRecorder struct {
	strikes  []string
	removals []string
	skipped  []string
}

func (f *fakeMetricsRecorder) QueueStruck(instance, rule, mode string) {
	f.strikes = append(f.strikes, instance+":"+rule+":"+mode)
}

func (f *fakeMetricsRecorder) QueueRemoval(instance, rule, result string) {
	f.removals = append(f.removals, instance+":"+rule+":"+result)
}

func (f *fakeMetricsRecorder) QueueSkipped(instance, reason string) {
	f.skipped = append(f.skipped, instance+":"+reason)
}

func newTestService(st *fakeStore, is *fakeInstanceSource, arr *fakeArrClient, clk clock.Clock, ev *fakeEventPublisher, m *fakeMetricsRecorder) *queue.Service {
	logger := slog.New(slog.DiscardHandler)
	return queue.NewService(st, is, arr, clk, ev, m, logger)
}

func TestActModeRemovesWithCorrectBlocklist(t *testing.T) {
	t.Parallel()

	instID := uuid.New()
	inst := queue.Instance{
		ID:      instID,
		Name:    "sonarr-main",
		Kind:    domain.KindSonarr,
		BaseURL: "http://sonarr.local",
		APIKey:  "secret-key",
	}

	st := newFakeStore()
	st.settings[instID] = queue.Settings{
		Enabled: true,
		Rules: map[queue.Rule]queue.RuleSettings{
			queue.RuleStalled:             {Mode: queue.ModeAct, Threshold: 2},
			queue.RuleUnwantedUnmonitored: {Mode: queue.ModeAct, Threshold: 1},
		},
		Grace:               0,
		MaxRemovalsPerSweep: 10,
	}
	// Item 1 already has 1 strike, next makes it 2 == threshold
	st.strikes[instID] = map[string]queue.Strike{
		"dl-stalled": {Rule: queue.RuleStalled, Count: 1, LastSizeLeft: 100, LastSeen: time.Now().Add(-10 * time.Minute)},
	}

	arr := newFakeArrClient()
	arrKey := string(domain.KindSonarr) + ":http://sonarr.local"
	arr.items[arrKey] = []queue.Item{
		{
			ID:                    101,
			DownloadID:            "dl-stalled",
			Added:                 time.Now().Add(-1 * time.Hour),
			Status:                "warning",
			TrackedDownloadStatus: "warning",
			TrackedDownloadState:  "downloading",
			SizeLeft:              100,
			Monitored:             true,
		},
		{
			ID:         102,
			DownloadID: "dl-unmonitored",
			Added:      time.Now().Add(-1 * time.Hour),
			SizeLeft:   200,
			Monitored:  false, // should match unwanted_unmonitored, blocklist=false
		},
	}

	is := &fakeInstanceSource{instances: []queue.Instance{inst}}
	ev := &fakeEventPublisher{}
	m := &fakeMetricsRecorder{}
	clk := clock.NewFake()

	svc := newTestService(st, is, arr, clk, ev, m)
	if err := svc.SweepAll(t.Context()); err != nil {
		t.Fatalf("SweepAll() error: %v", err)
	}

	if len(arr.removals) != 2 {
		t.Fatalf("got %d removals, want 2", len(arr.removals))
	}

	// Removal 1: stalled -> blocklist=true
	if arr.removals[0].id != 101 || !arr.removals[0].blocklist {
		t.Errorf("removal 0: got id=%d blocklist=%v, want id=101 blocklist=true", arr.removals[0].id, arr.removals[0].blocklist)
	}
	// Removal 2: unwanted_unmonitored -> blocklist=false
	if arr.removals[1].id != 102 || arr.removals[1].blocklist {
		t.Errorf("removal 1: got id=%d blocklist=%v, want id=102 blocklist=false", arr.removals[1].id, arr.removals[1].blocklist)
	}

	// Forgotten downloads
	if len(st.forgotten[instID]) != 2 {
		t.Errorf("got %d forgotten, want 2", len(st.forgotten[instID]))
	}

	// Metrics
	foundOk := 0
	for _, r := range m.removals {
		if r == "sonarr-main:stalled:ok" || r == "sonarr-main:unwanted_unmonitored:ok" {
			foundOk++
		}
	}
	if foundOk != 2 {
		t.Errorf("expected 2 ok removal metrics, got %v", m.removals)
	}

	// Purge called
	if st.purgeCalled != 1 {
		t.Errorf("purge called %d times, want 1", st.purgeCalled)
	}
}

func TestObserveModeNeverRemovesAndEmitsWouldRemove(t *testing.T) {
	t.Parallel()

	instID := uuid.New()
	inst := queue.Instance{
		ID:      instID,
		Name:    "radarr-obs",
		Kind:    domain.KindRadarr,
		BaseURL: "http://radarr.local",
		APIKey:  "key",
	}

	st := newFakeStore()
	st.settings[instID] = queue.Settings{
		Enabled: true,
		Rules: map[queue.Rule]queue.RuleSettings{
			queue.RuleStalled: {Mode: queue.ModeObserve, Threshold: 2},
		},
		Grace:               0,
		MaxRemovalsPerSweep: 10,
	}
	st.strikes[instID] = map[string]queue.Strike{
		"dl-obs": {Rule: queue.RuleStalled, Count: 1, LastSizeLeft: 50, LastSeen: time.Now().Add(-10 * time.Minute)},
	}

	arr := newFakeArrClient()
	arrKey := string(domain.KindRadarr) + ":http://radarr.local"
	arr.items[arrKey] = []queue.Item{
		{
			ID:                    201,
			DownloadID:            "dl-obs",
			Added:                 time.Now().Add(-1 * time.Hour),
			Status:                "warning",
			TrackedDownloadStatus: "warning",
			TrackedDownloadState:  "downloading",
			SizeLeft:              50,
			Monitored:             true,
		},
	}

	is := &fakeInstanceSource{instances: []queue.Instance{inst}}
	ev := &fakeEventPublisher{}
	m := &fakeMetricsRecorder{}
	clk := clock.NewFake()

	svc := newTestService(st, is, arr, clk, ev, m)
	if err := svc.SweepAll(t.Context()); err != nil {
		t.Fatalf("SweepAll() error: %v", err)
	}

	if len(arr.removals) != 0 {
		t.Fatalf("expected 0 removals in observe mode, got %d", len(arr.removals))
	}
	if len(st.forgotten[instID]) != 0 {
		t.Fatalf("expected 0 forgotten in observe mode, got %d", len(st.forgotten[instID]))
	}

	// Verify would_remove in event payload
	foundStruck := false
	for _, f := range ev.frames {
		if f.Type == "queue_item_struck" {
			foundStruck = true
			var payload map[string]any
			if err := json.Unmarshal([]byte(f.Detail), &payload); err != nil {
				t.Fatalf("unmarshal detail error: %v", err)
			}
			if payload["mode"] != "observe" {
				t.Errorf("payload mode = %v, want observe", payload["mode"])
			}
			if payload["would_remove"] != true {
				t.Errorf("payload would_remove = %v, want true", payload["would_remove"])
			}
		}
	}
	if !foundStruck {
		t.Errorf("did not find queue_item_struck event")
	}
}

func TestFailedDeleteKeepsRowAndRetries(t *testing.T) {
	t.Parallel()

	instID := uuid.New()
	inst := queue.Instance{
		ID:      instID,
		Name:    "lidarr-fail",
		Kind:    domain.KindLidarr,
		BaseURL: "http://lidarr.local",
		APIKey:  "key",
	}

	st := newFakeStore()
	st.settings[instID] = queue.Settings{
		Enabled: true,
		Rules: map[queue.Rule]queue.RuleSettings{
			queue.RuleStalled: {Mode: queue.ModeAct, Threshold: 2},
		},
		Grace:               0,
		MaxRemovalsPerSweep: 10,
	}
	st.strikes[instID] = map[string]queue.Strike{
		"dl-fail": {Rule: queue.RuleStalled, Count: 1, LastSizeLeft: 80, LastSeen: time.Now().Add(-10 * time.Minute)},
	}

	arr := newFakeArrClient()
	arrKey := string(domain.KindLidarr) + ":http://lidarr.local"
	arr.items[arrKey] = []queue.Item{
		{
			ID:                    301,
			DownloadID:            "dl-fail",
			Added:                 time.Now().Add(-1 * time.Hour),
			Status:                "warning",
			TrackedDownloadStatus: "warning",
			TrackedDownloadState:  "downloading",
			SizeLeft:              80,
			Monitored:             true,
		},
	}
	arr.removeErr = errors.New("upstream connection reset")

	is := &fakeInstanceSource{instances: []queue.Instance{inst}}
	ev := &fakeEventPublisher{}
	m := &fakeMetricsRecorder{}
	clk := clock.NewFake()

	svc := newTestService(st, is, arr, clk, ev, m)

	// Sweep 1: Remove fails
	if err := svc.SweepAll(t.Context()); err != nil {
		t.Fatalf("SweepAll() 1 error: %v", err)
	}

	if len(st.forgotten[instID]) != 0 {
		t.Errorf("should not forget row on failed delete")
	}
	// Strike must be kept in store with Count=2
	if st.strikes[instID]["dl-fail"].Count != 2 {
		t.Errorf("strike count = %d, want 2", st.strikes[instID]["dl-fail"].Count)
	}

	// Sweep 2: Remove succeeds
	arr.removeErr = nil
	if err := svc.SweepAll(t.Context()); err != nil {
		t.Fatalf("SweepAll() 2 error: %v", err)
	}

	if len(st.forgotten[instID]) != 1 {
		t.Errorf("should forget row on successful delete")
	}
}

func TestMaxRemovalsBound(t *testing.T) {
	t.Parallel()

	instID := uuid.New()
	inst := queue.Instance{
		ID:      instID,
		Name:    "readarr-bound",
		Kind:    domain.KindReadarr,
		BaseURL: "http://readarr.local",
		APIKey:  "key",
	}

	st := newFakeStore()
	st.settings[instID] = queue.Settings{
		Enabled: true,
		Rules: map[queue.Rule]queue.RuleSettings{
			queue.RuleFailed: {Mode: queue.ModeAct, Threshold: 1},
		},
		Grace:               0,
		MaxRemovalsPerSweep: 1, // bound to 1 removal
	}

	arr := newFakeArrClient()
	arrKey := string(domain.KindReadarr) + ":http://readarr.local"
	arr.items[arrKey] = []queue.Item{
		{ID: 1, DownloadID: "dl-1", Added: time.Now().Add(-1 * time.Hour), TrackedDownloadState: "failed"},
		{ID: 2, DownloadID: "dl-2", Added: time.Now().Add(-1 * time.Hour), TrackedDownloadState: "failed"},
		{ID: 3, DownloadID: "dl-3", Added: time.Now().Add(-1 * time.Hour), TrackedDownloadState: "failed"},
	}

	is := &fakeInstanceSource{instances: []queue.Instance{inst}}
	ev := &fakeEventPublisher{}
	m := &fakeMetricsRecorder{}
	clk := clock.NewFake()

	svc := newTestService(st, is, arr, clk, ev, m)
	if err := svc.SweepAll(t.Context()); err != nil {
		t.Fatalf("SweepAll() error: %v", err)
	}

	if len(arr.removals) != 1 {
		t.Fatalf("expected exactly 1 removal due to MaxRemovalsPerSweep=1, got %d", len(arr.removals))
	}
	if len(st.forgotten[instID]) != 1 {
		t.Fatalf("expected 1 forgotten, got %d", len(st.forgotten[instID]))
	}

	// 2 non-removed items should be recorded in store
	if len(st.strikes[instID]) != 2 {
		t.Errorf("expected 2 deferred strikes in store, got %d", len(st.strikes[instID]))
	}
}

func TestWhisparrSkipped(t *testing.T) {
	t.Parallel()

	inst1 := queue.Instance{ID: uuid.New(), Name: "whisparr-v2", Kind: domain.KindWhisparrV2, BaseURL: "http://w2.local"}
	inst2 := queue.Instance{ID: uuid.New(), Name: "whisparr-v3", Kind: domain.KindWhisparrV3, BaseURL: "http://w3.local"}

	st := newFakeStore()
	st.settings[inst1.ID] = queue.Settings{Enabled: true}
	st.settings[inst2.ID] = queue.Settings{Enabled: true}

	// The adapter, not the service, decides that a kind is unsupported.
	arr := newFakeArrClient()
	arr.queueErr[string(inst1.Kind)+":"+inst1.BaseURL] = queue.ErrQueueUnsupported
	arr.queueErr[string(inst2.Kind)+":"+inst2.BaseURL] = queue.ErrQueueUnsupported
	is := &fakeInstanceSource{instances: []queue.Instance{inst1, inst2}}
	ev := &fakeEventPublisher{}
	m := &fakeMetricsRecorder{}
	clk := clock.NewFake()

	svc := newTestService(st, is, arr, clk, ev, m)
	if err := svc.SweepAll(t.Context()); err != nil {
		t.Fatalf("SweepAll() error: %v", err)
	}

	if arr.removeCalls != 0 {
		t.Errorf("expected 0 remove calls for whisparr, got %d", arr.removeCalls)
	}

	skippedCount := 0
	for _, s := range m.skipped {
		if s == "whisparr-v2:unsupported" || s == "whisparr-v3:unsupported" {
			skippedCount++
		}
	}
	if skippedCount != 2 {
		t.Errorf("expected 2 unsupported skipped metrics, got %v", m.skipped)
	}
}

func TestOneBrokenInstanceDoesNotStopAnother(t *testing.T) {
	t.Parallel()

	instBroken := queue.Instance{ID: uuid.New(), Name: "broken", Kind: domain.KindSonarr, BaseURL: "http://broken.local"}
	instWorking := queue.Instance{ID: uuid.New(), Name: "working", Kind: domain.KindRadarr, BaseURL: "http://working.local"}

	st := newFakeStore()
	st.settings[instBroken.ID] = queue.Settings{Enabled: true}
	st.settings[instWorking.ID] = queue.Settings{
		Enabled:             true,
		Rules:               map[queue.Rule]queue.RuleSettings{queue.RuleFailed: {Mode: queue.ModeAct, Threshold: 1}},
		MaxRemovalsPerSweep: 10,
	}

	arr := newFakeArrClient()
	arr.queueErr[string(domain.KindSonarr)+":http://broken.local"] = errors.New("upstream timeout")
	arr.items[string(domain.KindRadarr)+":http://working.local"] = []queue.Item{
		{ID: 55, DownloadID: "dl-working", Added: time.Now().Add(-1 * time.Hour), TrackedDownloadState: "failed"},
	}

	is := &fakeInstanceSource{instances: []queue.Instance{instBroken, instWorking}}
	ev := &fakeEventPublisher{}
	m := &fakeMetricsRecorder{}
	clk := clock.NewFake()

	svc := newTestService(st, is, arr, clk, ev, m)
	err := svc.SweepAll(t.Context())
	if err == nil {
		t.Fatalf("expected joined error containing broken instance error, got nil")
	}

	// Working instance must have proceeded and removed its item
	if len(arr.removals) != 1 || arr.removals[0].id != 55 {
		t.Errorf("expected removal of working instance item 55, got removals: %+v", arr.removals)
	}
	// Purge must have run
	if st.purgeCalled != 1 {
		t.Errorf("expected purge to run even with broken instance, purgeCalled=%d", st.purgeCalled)
	}
}

func TestPartialInstanceListStillSwept(t *testing.T) {
	t.Parallel()

	good := queue.Instance{ID: uuid.New(), Name: "sonarr-good", Kind: domain.KindSonarr, BaseURL: "http://good.local"}
	st := newFakeStore()
	st.settings[good.ID] = queue.Settings{Enabled: true}
	arr := newFakeArrClient()
	keyErr := errors.New("open api key of sonarr-bad: cipher: message authentication failed")
	is := &fakeInstanceSource{instances: []queue.Instance{good}, err: keyErr}

	svc := newTestService(st, is, arr, clock.NewFake(), &fakeEventPublisher{}, &fakeMetricsRecorder{})
	err := svc.SweepAll(t.Context())
	if !errors.Is(err, keyErr) {
		t.Fatalf("SweepAll() error = %v, want it to carry the key error", err)
	}
	if arr.queueCalls != 1 {
		t.Fatalf("queue calls = %d, want 1 (the loaded instance is still swept)", arr.queueCalls)
	}
}
