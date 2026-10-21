// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package queue_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/queue"
)

func TestMatchMetadataStuck(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		item queue.Item
		want bool
	}{
		{
			name: "positive/metadata message (queued)",
			item: queue.Item{
				Protocol:    "torrent",
				Status:      "queued",
				StatusLines: []string{"qBittorrent is downloading metadata"},
				Size:        0,
			},
			want: true,
		},
		{
			name: "positive/DHT-disabled message (warning)",
			item: queue.Item{
				Protocol:    "torrent",
				Status:      "warning",
				StatusLines: []string{"qBittorrent cannot resolve magnet link with DHT disabled"},
				Size:        0,
			},
			want: true,
		},
		{
			name: "positive/torrent size 0 queued",
			item: queue.Item{
				Protocol: "torrent",
				Status:   "queued",
				Size:     0,
			},
			want: true,
		},
		{
			name: "negative/usenet item size 0",
			item: queue.Item{
				Protocol: "usenet",
				Status:   "queued",
				Size:     0,
			},
			want: false,
		},
		{
			name: "negative/torrent downloading with size>0",
			item: queue.Item{
				Protocol:             "torrent",
				Status:               "downloading",
				TrackedDownloadState: "downloading",
				Size:                 1048576,
				SizeLeft:             1048576,
			},
			want: false,
		},
		{
			name: "positive/localized message with size 0",
			item: queue.Item{
				Protocol:    "torrent",
				Status:      "queued",
				Size:        0,
				StatusLines: []string{"qBittorrent lädt Metadaten herunter"},
			},
			want: true,
		},
		{
			name: "boundary/negative torrent downloading with size 0",
			item: queue.Item{
				Protocol:             "torrent",
				Status:               "downloading",
				TrackedDownloadState: "downloading",
				Size:                 0,
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := queue.MatchMetadataStuck(tt.item)
			if got != tt.want {
				t.Errorf("MatchMetadataStuck() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMetadataStuckThreshold(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	thresholdMin := 180

	tests := []struct {
		name        string
		mode        queue.Mode
		age         time.Duration
		wantStrike  bool
		wantRemove  bool
		wantWouldRm bool
		wantReached bool
	}{
		{
			name:        "boundary/age = 179m (below threshold)",
			mode:        queue.ModeAct,
			age:         179 * time.Minute,
			wantStrike:  true,
			wantRemove:  false,
			wantWouldRm: false,
			wantReached: false,
		},
		{
			name:        "boundary/age = 180m (at threshold)",
			mode:        queue.ModeAct,
			age:         180 * time.Minute,
			wantStrike:  true,
			wantRemove:  true,
			wantWouldRm: false,
			wantReached: true,
		},
		{
			name:        "positive/age = 181m (above threshold)",
			mode:        queue.ModeAct,
			age:         181 * time.Minute,
			wantStrike:  true,
			wantRemove:  true,
			wantWouldRm: false,
			wantReached: true,
		},
		{
			name:        "negative/observe mode at 180m never removes",
			mode:        queue.ModeObserve,
			age:         180 * time.Minute,
			wantStrike:  true,
			wantRemove:  false,
			wantWouldRm: true,
			wantReached: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := queue.DefaultSettings()
			s.Enabled = true
			blocklist := true
			s.Rules[queue.RuleMetadataStuck] = queue.RuleSettings{
				Mode:      tt.mode,
				Threshold: thresholdMin,
				Blocklist: &blocklist,
			}
			item := queue.Item{
				ID:          42,
				DownloadID:  "dl-meta-42",
				Added:       now.Add(-tt.age),
				Protocol:    "torrent",
				Status:      "queued",
				Size:        0,
				StatusLines: []string{"qBittorrent is downloading metadata"},
				Monitored:   true,
			}
			decisions := queue.Sweep(s, []queue.Item{item}, nil, now)
			if len(decisions) != 1 {
				t.Fatalf("Sweep returned %d decisions, want 1", len(decisions))
			}
			d := decisions[0]
			if d.Rule != queue.RuleMetadataStuck {
				t.Errorf("Rule = %q, want %q", d.Rule, queue.RuleMetadataStuck)
			}
			if d.Strike != tt.wantStrike {
				t.Errorf("Strike = %v, want %v", d.Strike, tt.wantStrike)
			}
			if d.Remove != tt.wantRemove {
				t.Errorf("Remove = %v, want %v", d.Remove, tt.wantRemove)
			}
			if d.WouldRemove != tt.wantWouldRm {
				t.Errorf("WouldRemove = %v, want %v", d.WouldRemove, tt.wantWouldRm)
			}
			if d.Reached != tt.wantReached {
				t.Errorf("Reached = %v, want %v", d.Reached, tt.wantReached)
			}
		})
	}
}

func TestMetadataStuckAbsentAddedFallback(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	s := queue.DefaultSettings()
	s.Enabled = true
	blocklist := true
	s.Rules[queue.RuleMetadataStuck] = queue.RuleSettings{
		Mode:      queue.ModeAct,
		Threshold: 180,
		Blocklist: &blocklist,
	}
	item := queue.Item{
		ID:          42,
		DownloadID:  "dl-no-added",
		Added:       time.Time{},
		Protocol:    "torrent",
		Status:      "queued",
		Size:        0,
		StatusLines: []string{"qBittorrent is downloading metadata"},
		Monitored:   true,
	}

	d1 := queue.Sweep(s, []queue.Item{item}, nil, now)[0]
	if !d1.Strike || d1.Remove || d1.WouldRemove {
		t.Fatalf("sweep 1: expected strike without removal, got %+v", d1)
	}

	prev := map[string]queue.Strike{item.DownloadID: d1.Next}
	later := now.Add(24 * time.Hour)
	d2 := queue.Sweep(s, []queue.Item{item}, prev, later)[0]
	if !d2.Strike || d2.Remove || d2.WouldRemove {
		t.Fatalf("sweep 2 (+24h): expected strike without removal, got %+v", d2)
	}
}

func TestMetadataStuckReachedEdgeCrossing(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	s := queue.DefaultSettings()
	s.Enabled = true
	blocklist := true
	s.Rules[queue.RuleMetadataStuck] = queue.RuleSettings{
		Mode:      queue.ModeAct,
		Threshold: 180,
		Blocklist: &blocklist,
	}
	item := queue.Item{
		ID:          42,
		DownloadID:  "dl-crossing",
		Added:       now,
		Protocol:    "torrent",
		Status:      "queued",
		Size:        0,
		StatusLines: []string{"qBittorrent is downloading metadata"},
		Monitored:   true,
	}

	// Sweep 1: at Added + 179m -> threshold not met, reached false.
	t1 := now.Add(179 * time.Minute)
	d1 := queue.Sweep(s, []queue.Item{item}, nil, t1)[0]
	if !d1.Strike || d1.Remove || d1.Reached {
		t.Fatalf("sweep 1 (179m): got strike=%v, remove=%v, reached=%v; want true/false/false", d1.Strike, d1.Remove, d1.Reached)
	}

	// Sweep 2: at Added + 180m -> crossing edge, reached true.
	t2 := now.Add(180 * time.Minute)
	prev := map[string]queue.Strike{item.DownloadID: d1.Next}
	d2 := queue.Sweep(s, []queue.Item{item}, prev, t2)[0]
	if !d2.Strike || !d2.Remove || !d2.Reached {
		t.Fatalf("sweep 2 (180m crossing): got strike=%v, remove=%v, reached=%v; want true/true/true", d2.Strike, d2.Remove, d2.Reached)
	}

	// Sweep 3: at Added + 181m -> still met, but reached false (edge already crossed).
	t3 := now.Add(181 * time.Minute)
	prev = map[string]queue.Strike{item.DownloadID: d2.Next}
	d3 := queue.Sweep(s, []queue.Item{item}, prev, t3)[0]
	if !d3.Strike || !d3.Remove || d3.Reached {
		t.Fatalf("sweep 3 (181m past crossing): got strike=%v, remove=%v, reached=%v; want true/true/false", d3.Strike, d3.Remove, d3.Reached)
	}
}

func TestMetadataStuckPolicyValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		threshold int
		wantErr   bool
	}{
		{29, true},
		{30, false},
		{1440, false},
		{1441, true},
	}
	for _, tt := range tests {
		s := queue.DefaultSettings()
		s.Rules[queue.RuleMetadataStuck] = queue.RuleSettings{
			Mode:      queue.ModeObserve,
			Threshold: tt.threshold,
		}
		err := s.Validate()
		if (err != nil) != tt.wantErr {
			t.Errorf("Validate() for threshold %d: err=%v, wantErr=%v", tt.threshold, err, tt.wantErr)
		}
	}
}

func TestMetadataStuckSweepIntegration(t *testing.T) {
	t.Parallel()
	instID := uuid.New()
	inst := queue.Instance{
		ID:      instID,
		Name:    "sonarr-meta",
		Kind:    domain.KindSonarr,
		BaseURL: "http://sonarr.local",
		APIKey:  "secret-key",
	}

	st := newFakeStore()
	blocklist := true
	st.settings[instID] = queue.Settings{
		Enabled: true,
		Rules: map[queue.Rule]queue.RuleSettings{
			queue.RuleMetadataStuck: {Mode: queue.ModeAct, Threshold: 180, Blocklist: &blocklist},
		},
		Grace:               0,
		MaxRemovalsPerSweep: 10,
	}

	clk := clockwork.NewFakeClockAt(fixedNow)

	arr := newFakeArrClient()
	arrKey := string(domain.KindSonarr) + ":http://sonarr.local"
	arr.items[arrKey] = []queue.Item{
		{
			ID:          201,
			DownloadID:  "dl-stuck-meta",
			Added:       fixedNow.Add(-181 * time.Minute),
			Protocol:    "torrent",
			Status:      "queued",
			Size:        0,
			StatusLines: []string{"qBittorrent is downloading metadata"},
			Monitored:   true,
		},
	}

	is := &fakeInstanceSource{instances: []queue.Instance{inst}}
	ev := &fakeEventPublisher{}
	m := &fakeMetricsRecorder{}

	svc := newTestService(st, is, arr, clk, ev, m)
	if err := svc.SweepAll(t.Context()); err != nil {
		t.Fatalf("SweepAll() error: %v", err)
	}

	if len(arr.removals) != 1 {
		t.Fatalf("got %d removals, want 1", len(arr.removals))
	}
	rem := arr.removals[0]
	if rem.id != 201 || !rem.blocklist {
		t.Errorf("removal: got id=%d blocklist=%v, want id=201 blocklist=true", rem.id, rem.blocklist)
	}
	if len(st.forgotten[instID]) != 1 || st.forgotten[instID][0] != "dl-stuck-meta" {
		t.Errorf("forgotten downloads = %v, want [dl-stuck-meta]", st.forgotten[instID])
	}
}
