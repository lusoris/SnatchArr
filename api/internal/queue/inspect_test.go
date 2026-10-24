// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package queue_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/queue"
)

func TestInspectTableDriven(t *testing.T) {
	t.Parallel()
	clk := clock.NewFake()
	instID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	disabledID := uuid.MustParse("00000000-0000-0000-0000-000000000002")

	inst := queue.Instance{
		ID:      instID,
		Name:    "Sonarr Main",
		Kind:    domain.KindSonarr,
		BaseURL: "http://sonarr.local",
		APIKey:  "secret-key",
	}

	stalledItem := queue.Item{
		ID:                    101,
		DownloadID:            "dl-stalled",
		Title:                 "Stalled Show S01E01",
		Status:                "warning",
		TrackedDownloadStatus: "warning",
		TrackedDownloadState:  "downloading",
		Protocol:              "torrent",
		Size:                  104857600,
		SizeLeft:              52428800,
		Added:                 clk.Now().Add(-2 * time.Hour),
		StatusLines:           []string{"the download is stalling"},
		Monitored:             true,
	}

	cleanItem := queue.Item{
		ID:                    102,
		DownloadID:            "dl-clean",
		Title:                 "Clean Show S01E02",
		Status:                "ok",
		TrackedDownloadStatus: "ok",
		TrackedDownloadState:  "downloading",
		Protocol:              "usenet",
		Size:                  104857600,
		SizeLeft:              2000,
		Added:                 clk.Now().Add(-10 * time.Minute),
		Monitored:             true,
	}

	blocklistTrue := true
	cfg := queue.Settings{
		Enabled: true,
		Rules: map[queue.Rule]queue.RuleSettings{
			queue.RuleStalled: {
				Mode:      queue.ModeObserve,
				Threshold: 2,
				Blocklist: &blocklistTrue,
			},
		},
		Grace:               0,
		MaxRemovalsPerSweep: 10,
	}

	strikes := map[string]queue.Strike{
		"dl-stalled": {
			Rule:         queue.RuleStalled,
			Count:        1,
			LastSizeLeft: 52428800,
			LastSeen:     clk.Now().Add(-15 * time.Minute),
		},
	}

	cases := []struct {
		name          string
		id            uuid.UUID
		items         []queue.Item
		queueErr      error
		disabled      bool
		wantNotFound  bool
		wantLiveErr   bool
		wantItemCount int
		wantTruncated bool
	}{
		{
			name:          "positive stalled and clean evaluated",
			id:            instID,
			items:         []queue.Item{stalledItem, cleanItem},
			wantItemCount: 2,
			wantTruncated: false,
		},
		{
			name:         "negative unknown id returns not found",
			id:           uuid.MustParse("00000000-0000-0000-0000-000000000099"),
			wantNotFound: true,
		},
		{
			name:         "negative disabled instance returns not found",
			id:           disabledID,
			disabled:     true,
			wantNotFound: true,
		},
		{
			name:        "negative arr error wrapped",
			id:          instID,
			queueErr:    errors.New("dial tcp connection refused"),
			wantLiveErr: true,
		},
		{
			name:          "boundary empty queue",
			id:            instID,
			items:         []queue.Item{},
			wantItemCount: 0,
			wantTruncated: false,
		},
		{
			name:          "boundary inspectPageSize minus 1 items",
			id:            instID,
			items:         makeItems(99, clk.Now()),
			wantItemCount: 99,
			wantTruncated: false,
		},
		{
			name:          "boundary exactly inspectPageSize items",
			id:            instID,
			items:         makeItems(100, clk.Now()),
			wantItemCount: 100,
			wantTruncated: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := setupInspectEnv(t, clk, inst, cfg, strikes, tc.disabled, tc.queueErr, tc.items)
			got, err := env.svc.Inspect(context.Background(), tc.id)

			if tc.wantNotFound {
				if !errors.Is(err, domain.ErrNotFound) {
					t.Fatalf("Inspect() err = %v, want ErrNotFound", err)
				}
				return
			}
			if tc.wantLiveErr {
				if err == nil || !errors.Is(err, tc.queueErr) {
					t.Fatalf("Inspect() err = %v, want wrapped %v", err, tc.queueErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Inspect() unexpected err: %v", err)
			}
			if len(got.Items) != tc.wantItemCount {
				t.Errorf("len(Items) = %d, want %d", len(got.Items), tc.wantItemCount)
			}
			if got.Truncated != tc.wantTruncated {
				t.Errorf("Truncated = %v, want %v", got.Truncated, tc.wantTruncated)
			}

			if tc.name == "positive stalled and clean evaluated" {
				assertInspectedFields(t, got.Items)
			}
			env.assertNoMutations(t)
		})
	}
}

type inspectEnv struct {
	svc *queue.Service
	st  *inspectStore
	arr *inspectArr
}

func (e *inspectEnv) assertNoMutations(t *testing.T) {
	t.Helper()
	if e.st.recordedCalls > 0 {
		t.Errorf("store.Record called %d times; want 0 (read-only)", e.st.recordedCalls)
	}
	if e.st.forgetCalls > 0 {
		t.Errorf("store.Forget called %d times; want 0 (read-only)", e.st.forgetCalls)
	}
	if e.arr.removeCalls > 0 {
		t.Errorf("arr.RemoveQueueItem called %d times; want 0 (read-only)", e.arr.removeCalls)
	}
	if e.arr.forceImportCalls > 0 {
		t.Errorf("arr.ForceImport called %d times; want 0 (read-only)", e.arr.forceImportCalls)
	}
}

func setupInspectEnv(
	t *testing.T,
	clk clock.Clock,
	inst queue.Instance,
	cfg queue.Settings,
	strikes map[string]queue.Strike,
	disabled bool,
	queueErr error,
	items []queue.Item,
) *inspectEnv {
	t.Helper()
	st := &inspectStore{settings: cfg, strikes: strikes}
	arr := &inspectArr{items: items, err: queueErr}
	disabledMap := make(map[uuid.UUID]bool)
	disabledID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	if disabled {
		disabledMap[disabledID] = true
	}
	is := &inspectInstanceSource{
		instances: []queue.Instance{inst, {ID: disabledID, Name: "Disabled", Kind: domain.KindSonarr}},
		disabled:  disabledMap,
	}
	svc := queue.NewService(st, is, arr, clk, nil, nil, nil)
	return &inspectEnv{svc: svc, st: st, arr: arr}
}

func assertInspectedFields(t *testing.T, items []queue.InspectedItem) {
	t.Helper()
	stalled := items[0]
	if stalled.Rule != queue.RuleStalled {
		t.Errorf("stalled.Rule = %q, want %q", stalled.Rule, queue.RuleStalled)
	}
	if stalled.StrikeCount != 2 {
		t.Errorf("stalled.StrikeCount = %d, want 2", stalled.StrikeCount)
	}
	if stalled.Threshold != 2 {
		t.Errorf("stalled.Threshold = %d, want 2", stalled.Threshold)
	}
	if !stalled.WouldRemove {
		t.Errorf("stalled.WouldRemove = false, want true")
	}
	if stalled.Remove {
		t.Errorf("stalled.Remove = true, want false in observe mode")
	}
	if !stalled.BlocklistOnRemoval {
		t.Errorf("stalled.BlocklistOnRemoval = false, want true")
	}

	clean := items[1]
	if clean.Rule != "" {
		t.Errorf("clean.Rule = %q, want empty", clean.Rule)
	}
	if clean.StrikeCount != 0 {
		t.Errorf("clean.StrikeCount = %d, want 0", clean.StrikeCount)
	}
	if clean.Threshold != 0 {
		t.Errorf("clean.Threshold = %d, want 0", clean.Threshold)
	}
	if clean.BlocklistOnRemoval {
		t.Errorf("clean.BlocklistOnRemoval = true, want false")
	}
}

func makeItems(count int, now time.Time) []queue.Item {
	items := make([]queue.Item, count)
	for i := range count {
		items[i] = queue.Item{
			ID:         int32(i + 1),
			DownloadID: fmt.Sprintf("dl-%d", i+1),
			Title:      fmt.Sprintf("Item %d", i+1),
			Added:      now,
		}
	}
	return items
}

type inspectStore struct {
	settings      queue.Settings
	strikes       map[string]queue.Strike
	recordedCalls int
	forgetCalls   int
}

func (s *inspectStore) Settings(_ context.Context, _ uuid.UUID) (queue.Settings, error) {
	return s.settings, nil
}

func (s *inspectStore) Strikes(_ context.Context, _ uuid.UUID) (map[string]queue.Strike, error) {
	return s.strikes, nil
}

func (s *inspectStore) Record(_ context.Context, _ uuid.UUID, _ []queue.Decision) error {
	s.recordedCalls++
	return nil
}

func (s *inspectStore) Forget(_ context.Context, _ uuid.UUID, _ string) error {
	s.forgetCalls++
	return nil
}

func (s *inspectStore) Purge(_ context.Context, _ int) (int64, error) {
	return 0, nil
}

func (s *inspectStore) DownloadPriorityPreset(_ context.Context) (domain.PriorityPreset, error) {
	return domain.MediaFirstPreset(), nil
}

type inspectInstanceSource struct {
	instances []queue.Instance
	disabled  map[uuid.UUID]bool
}

func (s *inspectInstanceSource) ListEnabledInstances(_ context.Context) ([]queue.Instance, error) {
	return s.instances, nil
}

func (s *inspectInstanceSource) EnabledInstance(_ context.Context, id uuid.UUID) (queue.Instance, error) {
	if s.disabled != nil && s.disabled[id] {
		return queue.Instance{}, domain.ErrNotFound
	}
	for _, inst := range s.instances {
		if inst.ID == id {
			return inst, nil
		}
	}
	return queue.Instance{}, domain.ErrNotFound
}

type inspectArr struct {
	items            []queue.Item
	err              error
	removeCalls      int
	forceImportCalls int
}

func (a *inspectArr) Queue(_ context.Context, _ domain.AppKind, _, _ string, _, _ int) ([]queue.Item, error) {
	if a.err != nil {
		return nil, a.err
	}
	return a.items, nil
}

func (a *inspectArr) RemoveQueueItem(_ context.Context, _ domain.AppKind, _, _ string, _ int32, _, _ bool) error {
	a.removeCalls++
	return nil
}

func (a *inspectArr) ForceImport(_ context.Context, _ domain.AppKind, _, _, _ string, _ queue.ImportMode) (int, error) {
	a.forceImportCalls++
	return 0, nil
}
