// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain_test

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/quick"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestDownloadClientPriorityTables(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		get  func() domain.PriorityLevels
		want []int
	}{
		{"NZBGet", domain.NZBGetPriorityLevels, []int{100, 50, 0, -50, -100}},
		{"SABnzbd", domain.SABnzbdPriorityLevels, []int{1, 0, -1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := tc.get()
			if got.Default != 0 || !slices.Equal(got.Best2Worst, tc.want) {
				t.Fatalf("levels = %+v, want %v with default 0", got, tc.want)
			}
			got.Best2Worst[0] = 900
			if !slices.Equal(tc.get().Best2Worst, tc.want) {
				t.Fatal("table shares mutable storage")
			}
		})
	}
}

func TestDownloadClientSplitRankLevels(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		ranks []int
		want  map[int]int
	}{
		{"empty", nil, map[int]int{}},
		{"single", []int{50}, map[int]int{50: 0}},
		{"ties", []int{100, 1, 1, 100}, map[int]int{1: 100, 100: -100}},
		{"one distinct", []int{1, 1, 1}, map[int]int{1: 0}},
		{"two endpoints", []int{1, 100}, map[int]int{1: 100, 100: -100}},
		{"one rank per level", []int{90, 30, 10, 70, 50}, map[int]int{10: 100, 30: 50, 50: 0, 70: -50, 90: -100}},
		{"more ranks than levels", []int{1, 2, 3, 4, 5, 6, 7}, map[int]int{1: 100, 2: 50, 3: 50, 4: 0, 5: -50, 6: -50, 7: -100}},
		{"half up", []int{1, 2, 3, 4, 5, 6, 7, 8, 9}, map[int]int{1: 100, 2: 50, 3: 50, 4: 0, 5: 0, 6: -50, 7: -50, 8: -100, 9: -100}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			before := slices.Clone(tc.ranks)
			got, err := domain.SplitRankLevels(tc.ranks, domain.NZBGetPriorityLevels())
			if err != nil || !maps.Equal(got, tc.want) {
				t.Fatalf("split = %v, %v; want %v", got, err, tc.want)
			}
			if !slices.Equal(before, tc.ranks) {
				t.Fatal("split mutated ranks")
			}
		})
	}
}

func TestDownloadClientSplitInvalid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		ranks  []int
		levels domain.PriorityLevels
	}{
		{"rank zero", []int{0}, domain.NZBGetPriorityLevels()},
		{"rank 101", []int{1, 101}, domain.NZBGetPriorityLevels()},
		{"no levels", []int{1}, domain.PriorityLevels{}},
		{"one level", []int{1, 100}, domain.PriorityLevels{Best2Worst: []int{0}}},
		{"empty still validates levels", nil, domain.PriorityLevels{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := domain.SplitRankLevels(tc.ranks, tc.levels)
			if !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("error = %v, want wrapped ErrInvalid", err)
			}
		})
	}
}

func TestDownloadClientSplitCustomLevels(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		ranks  []int
		levels domain.PriorityLevels
		want   map[int]int
	}{
		{"minimum table half up", []int{1, 50, 100}, domain.PriorityLevels{Best2Worst: []int{10, -10}}, map[int]int{1: 10, 50: -10, 100: -10}},
		{"custom default", []int{50, 50}, domain.PriorityLevels{Best2Worst: []int{10, -10}, Default: 7}, map[int]int{50: 7}},
		{"SABnzbd endpoints", []int{1, 100}, domain.SABnzbdPriorityLevels(), map[int]int{1: 1, 100: -1}},
		{"SABnzbd one per level", []int{100, 50, 1}, domain.SABnzbdPriorityLevels(), map[int]int{1: 1, 50: 0, 100: -1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := domain.SplitRankLevels(tc.ranks, tc.levels)
			if err != nil || !maps.Equal(got, tc.want) {
				t.Fatalf("split = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

func TestDownloadClientPriorityOwnership(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		priority int
		last     *int
		want     []domain.PriorityWrite
	}{
		{"default", 0, nil, []domain.PriorityWrite{{ID: "a", From: 0, To: 100}}},
		{"unrecorded hand set", 50, nil, nil},
		{"changed after write", 50, new(-50), nil},
		{"unchanged after write", 50, new(50), []domain.PriorityWrite{{ID: "a", From: 50, To: 100}}},
		{"reset to default", 0, new(-50), []domain.PriorityWrite{{ID: "a", From: 0, To: 100}}},
		{"already target", 100, new(100), nil},
		{"force hand set", 900, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			items := []domain.DownloadQueueItem{
				{ID: "z", Rank: 100, State: domain.DownloadItemFinished},
				{ID: "a", Rank: 1, State: domain.DownloadItemQueued, Priority: tc.priority, LastWrittenPriority: tc.last},
			}
			got, err := domain.PlanLeveledClient(items, domain.NZBGetPriorityLevels())
			assertDownloadPlan(t, got, err, domain.DownloadClientPlan{Priority: tc.want})
		})
	}
}

func TestDownloadClientPriorityScope(t *testing.T) {
	t.Parallel()
	states := []domain.DownloadItemState{domain.DownloadItemQueued, domain.DownloadItemActive, domain.DownloadItemPaused, domain.DownloadItemFinished}
	for _, state := range states {
		t.Run(string(state), func(t *testing.T) {
			t.Parallel()
			items := []domain.DownloadQueueItem{
				{ID: "z", Rank: 100, State: domain.DownloadItemFinished},
				{ID: "a", Rank: 1, State: state},
			}
			var want domain.DownloadClientPlan
			if state == domain.DownloadItemQueued {
				want.Priority = []domain.PriorityWrite{{ID: "a", From: 0, To: 1}}
			}
			got, err := domain.PlanLeveledClient(items, domain.SABnzbdPriorityLevels())
			assertDownloadPlan(t, got, err, want)
		})
	}
}

func TestDownloadClientOneRankNoWrites(t *testing.T) {
	t.Parallel()
	items := []domain.DownloadQueueItem{
		{ID: "z", Rank: 1, State: domain.DownloadItemQueued, Priority: 100, LastWrittenPriority: new(100)},
		{ID: "a", Rank: 1, State: domain.DownloadItemQueued},
	}
	got, err := domain.PlanLeveledClient(items, domain.NZBGetPriorityLevels())
	assertDownloadPlan(t, got, err, domain.DownloadClientPlan{})
}

type downloadPauseCase struct {
	name   string
	other  domain.DownloadItemState
	self   domain.DownloadItemState
	rank   int
	owned  bool
	pause  []string
	resume []string
}

func downloadPauseCases() []downloadPauseCase {
	return []downloadPauseCase{
		{"better queued", domain.DownloadItemQueued, domain.DownloadItemActive, 100, false, []string{"z"}, nil},
		{"better active no waiting", domain.DownloadItemActive, domain.DownloadItemActive, 100, false, nil, nil},
		{"equal queued", domain.DownloadItemQueued, domain.DownloadItemActive, 1, false, nil, nil},
		{"better queued prevents resume", domain.DownloadItemQueued, domain.DownloadItemPaused, 100, true, nil, nil},
		{"better active prevents flap", domain.DownloadItemActive, domain.DownloadItemPaused, 100, true, nil, nil},
		{"finished permits resume", domain.DownloadItemFinished, domain.DownloadItemPaused, 100, true, nil, []string{"z"}},
		{"paused does not block resume", domain.DownloadItemPaused, domain.DownloadItemPaused, 100, true, nil, []string{"z"}},
		{"user paused", domain.DownloadItemFinished, domain.DownloadItemPaused, 100, false, nil, nil},
		{"equal queued permits resume", domain.DownloadItemQueued, domain.DownloadItemPaused, 1, true, nil, []string{"z"}},
		{"finished ownership flag ignored", domain.DownloadItemFinished, domain.DownloadItemFinished, 100, true, nil, nil},
		{"queued ownership flag ignored", domain.DownloadItemFinished, domain.DownloadItemQueued, 100, true, nil, nil},
	}
}

func TestDownloadClientPauseResume(t *testing.T) {
	t.Parallel()
	for _, tc := range downloadPauseCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			items := []domain.DownloadQueueItem{
				{ID: "z", Rank: tc.rank, State: tc.self, Priority: 900, PausedBySnatchArr: tc.owned},
				{ID: "a", Rank: 1, State: tc.other, Priority: 900},
			}
			want := domain.DownloadClientPlan{Pause: tc.pause, Resume: tc.resume}
			got, err := domain.PlanLeveledClient(items, domain.NZBGetPriorityLevels())
			assertDownloadPlan(t, got, err, want)
			want.Order = []string{"a", "z"}
			got, err = domain.PlanQueueOrderClient(items)
			assertDownloadPlan(t, got, err, want)
		})
	}
}

func TestDownloadClientQueueOrder(t *testing.T) {
	t.Parallel()
	old := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	items := []domain.DownloadQueueItem{
		{ID: "c", Rank: 1, Added: old.Add(time.Second), State: domain.DownloadItemFinished},
		{ID: "z", Rank: 100, Added: old, State: domain.DownloadItemPaused},
		{ID: "b", Rank: 1, Added: old, State: domain.DownloadItemActive, Priority: 900},
		{ID: "a", Rank: 1, Added: old.In(time.FixedZone("plus-one", 3600)), State: domain.DownloadItemQueued},
	}
	before := slices.Clone(items)
	got, err := domain.PlanQueueOrderClient(items)
	assertDownloadPlan(t, got, err, domain.DownloadClientPlan{Order: []string{"a", "b", "c", "z"}})
	if !reflect.DeepEqual(items, before) {
		t.Fatal("planner mutated caller snapshot")
	}
}

func TestDownloadClientDeterministicPlan(t *testing.T) {
	t.Parallel()
	items := []domain.DownloadQueueItem{
		{ID: "z", Rank: 100, State: domain.DownloadItemActive},
		{ID: "b", Rank: 50, State: domain.DownloadItemQueued},
		{ID: "a", Rank: 25, State: domain.DownloadItemQueued},
		{ID: "y", Rank: 100, State: domain.DownloadItemActive},
		{ID: "r", Rank: 1, State: domain.DownloadItemPaused, PausedBySnatchArr: true},
		{ID: "p", Rank: 1, State: domain.DownloadItemPaused, PausedBySnatchArr: true},
	}
	reversed := slices.Clone(items)
	slices.Reverse(reversed)
	cases := []struct {
		name  string
		items []domain.DownloadQueueItem
	}{
		{"original", items},
		{"reversed", reversed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			before := slices.Clone(tc.items)
			want := domain.DownloadClientPlan{
				Priority: []domain.PriorityWrite{{ID: "a", From: 0, To: 50}, {ID: "b", From: 0, To: -50}},
				Pause:    []string{"y", "z"}, Resume: []string{"p", "r"},
			}
			got, err := domain.PlanLeveledClient(tc.items, domain.NZBGetPriorityLevels())
			assertDownloadPlan(t, got, err, want)
			if !reflect.DeepEqual(before, tc.items) {
				t.Fatal("leveled planner mutated caller snapshot")
			}
			want.Priority = nil
			want.Order = []string{"p", "r", "a", "b", "y", "z"}
			got, err = domain.PlanQueueOrderClient(tc.items)
			assertDownloadPlan(t, got, err, want)
		})
	}
}

func TestDownloadClientInvalidItems(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		items   []domain.DownloadQueueItem
		context string
	}{
		{"empty ID", []domain.DownloadQueueItem{{Rank: 1, State: domain.DownloadItemQueued}}, "ID"},
		{"duplicate ID", []domain.DownloadQueueItem{{ID: "x", Rank: 1, State: domain.DownloadItemQueued}, {ID: "x", Rank: 1, State: domain.DownloadItemQueued}}, "x"},
		{"rank zero", []domain.DownloadQueueItem{{ID: "x", Rank: 0, State: domain.DownloadItemQueued}}, "rank"},
		{"rank 101", []domain.DownloadQueueItem{{ID: "x", Rank: 101, State: domain.DownloadItemQueued}}, "rank"},
		{"bad state", []domain.DownloadQueueItem{{ID: "x", Rank: 1, State: "unknown"}}, "state"},
		{"empty state", []domain.DownloadQueueItem{{ID: "x", Rank: 1}}, "state"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			leveled, levelErr := domain.PlanLeveledClient(tc.items, domain.NZBGetPriorityLevels())
			ordered, orderErr := domain.PlanQueueOrderClient(tc.items)
			for _, err := range []error{levelErr, orderErr} {
				if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), tc.context) {
					t.Fatalf("error = %v, want ErrInvalid with %q", err, tc.context)
				}
			}
			assertDownloadPlan(t, leveled, nil, domain.DownloadClientPlan{})
			assertDownloadPlan(t, ordered, nil, domain.DownloadClientPlan{})
		})
	}
}

func TestDownloadClientEmptyAndInvalidLevels(t *testing.T) {
	t.Parallel()
	got, err := domain.PlanQueueOrderClient(nil)
	assertDownloadPlan(t, got, err, domain.DownloadClientPlan{})
	got, err = domain.PlanLeveledClient(nil, domain.NZBGetPriorityLevels())
	assertDownloadPlan(t, got, err, domain.DownloadClientPlan{})
	for _, levels := range []domain.PriorityLevels{{}, {Best2Worst: []int{0}}} {
		got, err = domain.PlanLeveledClient(nil, levels)
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("error = %v, want ErrInvalid", err)
		}
		assertDownloadPlan(t, got, nil, domain.DownloadClientPlan{})
	}
}

func assertDownloadPlan(t *testing.T, got domain.DownloadClientPlan, err error, want domain.DownloadClientPlan) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Priority, want.Priority) || !slices.Equal(got.Order, want.Order) ||
		!slices.Equal(got.Pause, want.Pause) || !slices.Equal(got.Resume, want.Resume) {
		t.Fatalf("plan = %+v, want %+v", got, want)
	}
}

func TestDownloadClientSplitProperty(t *testing.T) {
	t.Parallel()
	for _, levels := range []domain.PriorityLevels{domain.NZBGetPriorityLevels(), domain.SABnzbdPriorityLevels()} {
		property := func(raw []uint8) bool {
			ranks := make([]int, len(raw))
			for i, value := range raw {
				ranks[i] = int(value%100) + 1
			}
			got, err := domain.SplitRankLevels(ranks, levels)
			if err != nil {
				return false
			}
			slices.Sort(ranks)
			distinct := slices.Compact(ranks)
			if len(got) != len(distinct) {
				return false
			}
			for i, rank := range distinct {
				level, ok := got[rank]
				if !ok || !slices.Contains(levels.Best2Worst, level) || level == 900 || level == 2 {
					return false
				}
				if i > 0 && got[distinct[i-1]] < level {
					return false
				}
			}
			return true
		}
		if err := quick.Check(property, &quick.Config{MaxCount: 2000}); err != nil {
			t.Fatal(err)
		}
	}
}

func downloadRandomItems(raw []uint32) []domain.DownloadQueueItem {
	states := []domain.DownloadItemState{domain.DownloadItemQueued, domain.DownloadItemActive, domain.DownloadItemPaused, domain.DownloadItemFinished}
	items := make([]domain.DownloadQueueItem, len(raw))
	for i, value := range raw {
		items[i] = domain.DownloadQueueItem{
			ID: fmt.Sprintf("item-%03d", len(raw)-i), Rank: int(value%100) + 1,
			State: states[(value>>8)%4], Priority: int((value>>10)%5)*50 - 100,
			PausedBySnatchArr: value&(1<<15) != 0,
			Added:             time.Unix(int64(value>>16), 0),
		}
		switch (value >> 13) % 3 {
		case 1:
			items[i].LastWrittenPriority = new(items[i].Priority)
		case 2:
			items[i].LastWrittenPriority = new(items[i].Priority + 1)
		}
	}
	return items
}

func applyDownloadPlan(items []domain.DownloadQueueItem, plan domain.DownloadClientPlan) []domain.DownloadQueueItem {
	out := slices.Clone(items)
	for i := range out {
		for _, write := range plan.Priority {
			if write.ID == out[i].ID {
				out[i].Priority = write.To
				out[i].LastWrittenPriority = new(write.To)
			}
		}
		if slices.Contains(plan.Pause, out[i].ID) {
			out[i].State = domain.DownloadItemPaused
			out[i].PausedBySnatchArr = true
		}
		if slices.Contains(plan.Resume, out[i].ID) {
			out[i].State = domain.DownloadItemActive
			out[i].PausedBySnatchArr = false
		}
	}
	return out
}

func TestDownloadClientPlanIdempotentProperty(t *testing.T) {
	t.Parallel()
	planners := []func([]domain.DownloadQueueItem) (domain.DownloadClientPlan, error){
		func(items []domain.DownloadQueueItem) (domain.DownloadClientPlan, error) {
			return domain.PlanLeveledClient(items, domain.NZBGetPriorityLevels())
		},
		func(items []domain.DownloadQueueItem) (domain.DownloadClientPlan, error) {
			return domain.PlanLeveledClient(items, domain.SABnzbdPriorityLevels())
		},
		domain.PlanQueueOrderClient,
	}
	for _, planner := range planners {
		property := func(raw []uint32) bool {
			items := downloadRandomItems(raw)
			before := slices.Clone(items)
			plan, err := planner(items)
			if err != nil || !reflect.DeepEqual(items, before) || !downloadPlanSorted(plan) {
				return false
			}
			next, err := planner(applyDownloadPlan(items, plan))
			return err == nil && len(next.Priority)+len(next.Pause)+len(next.Resume) == 0 && slices.Equal(next.Order, plan.Order)
		}
		if err := quick.Check(property, &quick.Config{MaxCount: 2000}); err != nil {
			t.Fatal(err)
		}
	}
}

func downloadPlanSorted(plan domain.DownloadClientPlan) bool {
	ids := make([]string, len(plan.Priority))
	for i, write := range plan.Priority {
		ids[i] = write.ID
	}
	return slices.IsSorted(ids) && slices.IsSorted(plan.Pause) && slices.IsSorted(plan.Resume)
}
