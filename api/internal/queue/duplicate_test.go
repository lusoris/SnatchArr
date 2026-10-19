// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package queue_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/queue"
)

// dupItem is a healthy download for media scoring score; download names the
// download id, so items sharing it are one season pack.
func dupItem(id, media, score int32, download string) queue.Item {
	item := cleanProgressItem(queue.RuleDuplicate, fixedNow.Add(-1*time.Hour))
	item.ID = id
	item.DownloadID = download
	item.MediaID = media
	item.CustomFormatScore = score
	return item
}

func TestStateDuplicate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		items []queue.Item
		// struck lists the item ids that get a duplicate strike.
		struck []int32
	}{
		{
			name:   "positive/lower_score_struck",
			items:  []queue.Item{dupItem(1, 7, 200, "dl-a"), dupItem(2, 7, 100, "dl-b")},
			struck: []int32{2},
		},
		{
			name:   "positive/every_loser_of_three_struck",
			items:  []queue.Item{dupItem(1, 7, 50, "dl-a"), dupItem(2, 7, 300, "dl-b"), dupItem(3, 7, 100, "dl-c")},
			struck: []int32{1, 3},
		},
		{
			name: "positive/season_pack_losing_one_episode_is_struck",
			items: []queue.Item{
				dupItem(1, 7, 100, "dl-pack"), dupItem(2, 8, 100, "dl-pack"),
				dupItem(3, 8, 200, "dl-single"),
			},
			struck: []int32{1, 2},
		},
		{
			name:  "negative/one_item",
			items: []queue.Item{dupItem(1, 7, 100, "dl-a")},
		},
		{
			name:  "negative/different_media",
			items: []queue.Item{dupItem(1, 7, 100, "dl-a"), dupItem(2, 8, 50, "dl-b")},
		},
		{
			name:  "negative/season_pack_counts_once",
			items: []queue.Item{dupItem(1, 7, 100, "dl-pack"), dupItem(2, 8, 100, "dl-pack"), dupItem(3, 9, 100, "dl-pack")},
		},
		{
			name:  "negative/one_download_twice_on_one_media",
			items: []queue.Item{dupItem(1, 7, 100, "dl-pack"), dupItem(2, 7, 100, "dl-pack")},
		},
		{
			name:  "negative/unknown_media_never_grouped",
			items: []queue.Item{dupItem(1, 0, 100, "dl-a"), dupItem(2, 0, 50, "dl-b")},
		},
		{
			name:   "boundary/equal_scores_keep_lowest_queue_id",
			items:  []queue.Item{dupItem(5, 7, 100, "dl-a"), dupItem(3, 7, 100, "dl-b")},
			struck: []int32{5},
		},
		{
			name:   "boundary/one_point_lower_struck",
			items:  []queue.Item{dupItem(1, 7, 100, "dl-a"), dupItem(2, 7, 101, "dl-b")},
			struck: []int32{1},
		},
	}
	s := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleDuplicate: 1})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assertDuplicates(t, queue.Sweep(s, tt.items, nil, fixedNow), tt.struck)
		})
	}
}

func assertDuplicates(t *testing.T, decisions []queue.Decision, struck []int32) {
	t.Helper()
	want := make(map[int32]bool, len(struck))
	for _, id := range struck {
		want[id] = true
	}
	for _, d := range decisions {
		got := d.Rule == queue.RuleDuplicate
		if got != want[d.ItemID] {
			t.Errorf("item %d rule = %q, want duplicate %v", d.ItemID, d.Rule, want[d.ItemID])
		}
		if got && (d.Blocklist || !d.Remove) {
			t.Errorf("item %d remove = %v blocklist = %v, want removal without blocklist", d.ItemID, d.Remove, d.Blocklist)
		}
	}
}

// TestDuplicateDeterministic pins the tie-break to the queue id, not the
// input order: reversing the page keeps the same item.
func TestDuplicateDeterministic(t *testing.T) {
	t.Parallel()
	s := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleDuplicate: 1})
	items := make([]queue.Item, 0, 4)
	for id := int32(4); id >= 1; id-- {
		items = append(items, dupItem(id, 7, 100, "dl-"+strconv.Itoa(int(id))))
	}
	for range 2 {
		assertDuplicates(t, queue.Sweep(s, items, nil, fixedNow), []int32{2, 3, 4})
		for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
			items[i], items[j] = items[j], items[i]
		}
	}
}

// TestDuplicateObserveDefault checks the default: observe, one strike, no blocklist.
func TestDuplicateObserveDefault(t *testing.T) {
	t.Parallel()
	s := queue.DefaultSettings()
	s.Enabled = true
	decisions := queue.Sweep(s, []queue.Item{dupItem(1, 7, 200, "dl-a"), dupItem(2, 7, 100, "dl-b")}, nil, fixedNow)
	d := decisions[1]
	if d.Rule != queue.RuleDuplicate || d.Remove || !d.WouldRemove || d.Blocklist {
		t.Fatalf("decision = %+v, want observed duplicate that would be removed without blocklist", d)
	}
}

func dupItemFull(id, score int32, quality int, added time.Time, download string) queue.Item {
	item := dupItem(id, 7, score, download)
	item.QualityRank = quality
	item.Added = added
	return item
}

func TestDuplicateTieHierarchy(t *testing.T) {
	t.Parallel()
	s := testSettings(queue.ModeAct, map[queue.Rule]int{queue.RuleDuplicate: 1})

	t.Run("score_beats_quality", func(t *testing.T) {
		t.Parallel()
		t1 := fixedNow.Add(-2 * time.Hour)
		t2 := fixedNow.Add(-1 * time.Hour)
		item1 := dupItemFull(10, 200, 1, t2, "dl-a")
		item2 := dupItemFull(2, 100, 10, t1, "dl-b")
		assertDuplicates(t, queue.Sweep(s, []queue.Item{item1, item2}, nil, fixedNow), []int32{2})
	})

	t.Run("quality_beats_added_on_score_tie", func(t *testing.T) {
		t.Parallel()
		t1 := fixedNow.Add(-2 * time.Hour)
		t2 := fixedNow.Add(-1 * time.Hour)
		item1 := dupItemFull(10, 100, 5, t2, "dl-a")
		item2 := dupItemFull(2, 100, 2, t1, "dl-b")
		assertDuplicates(t, queue.Sweep(s, []queue.Item{item1, item2}, nil, fixedNow), []int32{2})
	})

	t.Run("added_beats_id_on_score_and_quality_tie", func(t *testing.T) {
		t.Parallel()
		t1 := fixedNow.Add(-2 * time.Hour)
		t2 := fixedNow.Add(-1 * time.Hour)
		item1 := dupItemFull(10, 100, 5, t1, "dl-a")
		item2 := dupItemFull(2, 100, 5, t2, "dl-b")
		assertDuplicates(t, queue.Sweep(s, []queue.Item{item1, item2}, nil, fixedNow), []int32{2})
	})

	t.Run("lower_id_wins_on_full_tie", func(t *testing.T) {
		t.Parallel()
		t1 := fixedNow.Add(-1 * time.Hour)
		item1 := dupItemFull(2, 100, 5, t1, "dl-a")
		item2 := dupItemFull(10, 100, 5, t1, "dl-b")
		assertDuplicates(t, queue.Sweep(s, []queue.Item{item1, item2}, nil, fixedNow), []int32{10})
	})
}
