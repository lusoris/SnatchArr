// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package queue_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/queue"
)

// TestShadowReachedOnce pins Reached and NewStrike: each is true in exactly one
// sweep of a strike run, the one that reaches the threshold and the first one.
func TestShadowReachedOnce(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		threshold   int
		wantReached []bool
		wantNew     []bool
	}{
		{"positive/threshold_one_first_sweep", 1, []bool{true, false, false}, []bool{true, false, false}},
		{"boundary/threshold_two_second_sweep", 2, []bool{false, true, false}, []bool{true, false, false}},
		{"negative/threshold_never_reached", 4, []bool{false, false, false}, []bool{true, false, false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := idMatchedSettings(queue.ModeObserve)
			s.Rules[queue.RuleIDMatched] = queue.RuleSettings{Mode: queue.ModeObserve, Threshold: tt.threshold}
			item := idMatchedItem(1, "dl-1")
			var prev map[string]queue.Strike
			for i := range tt.wantReached {
				d := sweepOne(t, s, item, prev, fixedNow.Add(time.Duration(i)*10*time.Minute))
				if d.Reached != tt.wantReached[i] || d.NewStrike != tt.wantNew[i] {
					t.Fatalf("sweep %d: reached %v new %v, want %v %v", i, d.Reached, d.NewStrike, tt.wantReached[i], tt.wantNew[i])
				}
				prev = map[string]queue.Strike{"dl-1": d.Next}
			}
		})
	}
}

// TestShadowRuleChangeIsNewStrike: a strike by another rule starts a new run.
func TestShadowRuleChangeIsNewStrike(t *testing.T) {
	t.Parallel()
	s := idMatchedSettings(queue.ModeObserve)
	prev := map[string]queue.Strike{"dl-1": {Rule: queue.RuleImportBlocked, Count: 3, LastSeen: fixedNow.Add(-10 * time.Minute)}}
	d := sweepOne(t, s, idMatchedItem(1, "dl-1"), prev, fixedNow)
	if !d.NewStrike || !d.Reached || !d.WouldImport {
		t.Fatalf("decision = %+v, want a new id_matched strike that would import", d)
	}
}

type shadowLine struct {
	Msg        string `json:"msg"`
	Instance   string `json:"instance"`
	Rule       string `json:"rule"`
	DownloadID string `json:"download_id"`
}

func shadowService(t *testing.T, mode queue.Mode, items ...queue.Item) (*importFixture, *bytes.Buffer) {
	t.Helper()
	f := newImportFixture(t, domain.KindSonarr, mode, items...)
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	f.svc = queue.NewService(f.st, &fakeInstanceSource{instances: []queue.Instance{f.inst}}, f.arr, f.clk, f.ev, f.m, logger)
	return f, &buf
}

func shadowLines(t *testing.T, buf *bytes.Buffer, msg string) []string {
	t.Helper()
	var out []string
	for line := range bytes.Lines(buf.Bytes()) {
		var l shadowLine
		if err := json.Unmarshal(line, &l); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		if l.Msg == msg {
			out = append(out, l.Instance+":"+l.Rule+":"+l.DownloadID)
		}
	}
	return out
}

// TestShadowLogsOncePerDownload: over three sweeps in observe mode, a season pack
// and a single release give one would-import line and metric each, and one strike
// line per queue item.
func TestShadowLogsOncePerDownload(t *testing.T) {
	t.Parallel()
	f, buf := shadowService(t, queue.ModeObserve,
		idMatchedItem(7, "dl-pack"), idMatchedItem(8, "dl-pack"), idMatchedItem(9, "dl-one"))
	for range 3 {
		f.sweep(t)
		f.clk.Advance(10 * time.Minute)
	}
	would := shadowLines(t, buf, "queue: would force import")
	if !slices.Equal(would, []string{"arr-main::dl-pack", "arr-main::dl-one"}) {
		t.Fatalf("would-import lines = %v, want one per download", would)
	}
	struck := shadowLines(t, buf, "queue: item struck")
	want := []string{"arr-main:id_matched:dl-pack", "arr-main:id_matched:dl-pack", "arr-main:id_matched:dl-one"}
	if !slices.Equal(struck, want) {
		t.Fatalf("strike lines = %v, want %v", struck, want)
	}
	if !slices.Equal(f.m.imports, []string{"arr-main:would_import", "arr-main:would_import"}) {
		t.Fatalf("import metrics = %v, want two would_import", f.m.imports)
	}
	if len(f.arr.imports) != 0 {
		t.Fatalf("observe sent imports %v", f.arr.imports)
	}
}

// TestShadowActModeLogsNoWouldImport: act mode imports and counts ok, never would_import.
func TestShadowActModeLogsNoWouldImport(t *testing.T) {
	t.Parallel()
	f, buf := shadowService(t, queue.ModeAct, idMatchedItem(7, "dl-7"))
	f.sweep(t)
	if got := shadowLines(t, buf, "queue: would force import"); len(got) != 0 {
		t.Fatalf("act mode would-import lines = %v, want none", got)
	}
	if !slices.Equal(f.m.imports, []string{"arr-main:ok"}) {
		t.Fatalf("import metrics = %v, want [arr-main:ok]", f.m.imports)
	}
}
