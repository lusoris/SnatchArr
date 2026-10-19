// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package queue_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/queue"
)

// idMatchedItem is a queue item whose only problem is the ID-match note.
func idMatchedItem(id int32, downloadID string) queue.Item {
	return queue.Item{
		ID:                    id,
		DownloadID:            downloadID,
		Added:                 fixedNow.Add(-1 * time.Hour),
		Status:                "completed",
		TrackedDownloadStatus: "warning",
		TrackedDownloadState:  "importBlocked",
		Title:                 "Show.S01E01.1080p.WEB-GRP",
		StatusLines:           []string{"Show.S01E01.1080p.WEB-GRP", idMatchNote},
		CustomFormatScore:     100,
		Monitored:             true,
	}
}

func idMatchedSettings(mode queue.Mode) queue.Settings {
	s := testSettings(mode, map[queue.Rule]int{queue.RuleIDMatched: 1, queue.RuleImportBlocked: 6})
	s.ForceImport = true
	return s
}

func sweepOne(t *testing.T, s queue.Settings, item queue.Item, prev map[string]queue.Strike, now time.Time) queue.Decision {
	t.Helper()
	ds := queue.Sweep(s, []queue.Item{item}, prev, now)
	if len(ds) != 1 {
		t.Fatalf("Sweep returned %d decisions, want 1", len(ds))
	}
	return ds[0]
}

func TestIDMatchedSweepDecisions(t *testing.T) {
	t.Parallel()
	act, observe := idMatchedSettings(queue.ModeAct), idMatchedSettings(queue.ModeObserve)
	noKind := act
	noKind.ForceImport = false
	secondLine := idMatchedItem(1, "dl-1")
	secondLine.StatusLines = append(secondLine.StatusLines, "Some unknown error line")
	noDownload := idMatchedItem(1, "")
	pending := idMatchedItem(1, "dl-1")
	pending.TrackedDownloadState = "importPending"
	tests := []struct {
		name       string
		s          queue.Settings
		item       queue.Item
		wantRule   queue.Rule
		wantImport bool
		wantWould  bool
	}{
		{"positive/act_imports", act, idMatchedItem(1, "dl-1"), queue.RuleIDMatched, true, false},
		{"positive/import_pending_imports", act, pending, queue.RuleIDMatched, true, false},
		{"positive/observe_would_import", observe, idMatchedItem(1, "dl-1"), queue.RuleIDMatched, false, true},
		{"negative/kind_without_mapping_untouched", noKind, idMatchedItem(1, "dl-1"), "", false, false},
		{"negative/second_status_line_is_import_blocked", act, secondLine, queue.RuleImportBlocked, false, false},
		{"negative/no_download_id_untouched", act, noDownload, "", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := sweepOne(t, tt.s, tt.item, nil, fixedNow)
			if d.Rule != tt.wantRule || d.Import != tt.wantImport || d.WouldImport != tt.wantWould {
				t.Fatalf("decision = rule %q import %v would %v, want %q %v %v", d.Rule, d.Import, d.WouldImport, tt.wantRule, tt.wantImport, tt.wantWould)
			}
			if d.Remove || (tt.wantRule == queue.RuleIDMatched && (d.WouldRemove || d.Blocklist)) {
				t.Fatalf("id_matched decision removes: %+v", d)
			}
		})
	}
}

func TestIDMatchedThresholdTwo(t *testing.T) {
	t.Parallel()
	s := idMatchedSettings(queue.ModeAct)
	s.Rules[queue.RuleIDMatched] = queue.RuleSettings{Mode: queue.ModeAct, Threshold: 2}
	item := idMatchedItem(1, "dl-1")
	first := sweepOne(t, s, item, nil, fixedNow)
	if first.Import || first.Next.Count != 1 {
		t.Fatalf("first sweep = %+v, want count 1 and no import", first)
	}
	second := sweepOne(t, s, item, map[string]queue.Strike{"dl-1": first.Next}, fixedNow.Add(10*time.Minute))
	if !second.Import || second.Next.Count != 2 {
		t.Fatalf("second sweep = %+v, want count 2 and import", second)
	}
}

func TestIDMatchedImportBound(t *testing.T) {
	t.Parallel()
	s := idMatchedSettings(queue.ModeAct)
	s.MaxImportsPerSweep = 2
	items := []queue.Item{idMatchedItem(1, "dl-1"), idMatchedItem(2, "dl-2"), idMatchedItem(3, "dl-3")}
	ds := queue.Sweep(s, items, nil, fixedNow)
	got := []bool{ds[0].Import, ds[1].Import, ds[2].Import}
	if !got[0] || !got[1] || got[2] || !ds[2].Deferred {
		t.Fatalf("imports = %v deferred[2] = %v, want 2 imports and the third deferred", got, ds[2].Deferred)
	}
}

func TestIDMatchedSeasonPackImportsOnce(t *testing.T) {
	t.Parallel()
	s := idMatchedSettings(queue.ModeAct)
	s.MaxImportsPerSweep = 2
	items := []queue.Item{idMatchedItem(1, "dl-pack"), idMatchedItem(2, "dl-pack"), idMatchedItem(3, "dl-pack"), idMatchedItem(4, "dl-2")}
	ds := queue.Sweep(s, items, nil, fixedNow)
	imports := 0
	for _, d := range ds {
		if d.Import {
			imports++
		}
		if d.Deferred {
			t.Fatalf("item %d deferred: a pack must spend one import", d.ItemID)
		}
	}
	if imports != 2 || !ds[0].Import || !ds[3].Import {
		t.Fatalf("imports = %d (pack %v, dl-2 %v), want the pack once and dl-2", imports, ds[0].Import, ds[3].Import)
	}
}

func TestIDMatchedReadBack(t *testing.T) {
	t.Parallel()
	s := idMatchedSettings(queue.ModeAct)
	item := idMatchedItem(1, "dl-1")
	imported := fixedNow
	sent := queue.Strike{Rule: queue.RuleIDMatched, Count: 1, LastSeen: imported, ImportedAt: imported}
	later := sent
	later.LastSeen = imported.Add(10 * time.Minute)
	refused := sent
	refused.ImportRefused = true
	tests := []struct {
		name       string
		prev       queue.Strike
		now        time.Time
		wantFailed bool
		wantImport bool
	}{
		{"positive/first_sweep_after_import_fails", sent, imported.Add(10 * time.Minute), true, false},
		{"negative/failure_reported_once", later, imported.Add(20 * time.Minute), false, false},
		{"negative/refused_is_no_failed_import", refused, imported.Add(10 * time.Minute), false, false},
		{"boundary/just_inside_ttl_waits", later, imported.Add(24*time.Hour - time.Second), false, false},
		{"boundary/at_ttl_imports_again", later, imported.Add(24 * time.Hour), false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := sweepOne(t, s, item, map[string]queue.Strike{"dl-1": tt.prev}, tt.now)
			if d.ImportFailed != tt.wantFailed || d.Import != tt.wantImport {
				t.Fatalf("decision = failed %v import %v, want %v %v", d.ImportFailed, d.Import, tt.wantFailed, tt.wantImport)
			}
			if !tt.wantImport && !d.Next.ImportedAt.Equal(tt.prev.ImportedAt) {
				t.Fatalf("Next.ImportedAt = %v, want carried %v", d.Next.ImportedAt, tt.prev.ImportedAt)
			}
		})
	}
}

func TestValidateMaxImportsPerSweep(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		n       int
		wantErr bool
	}{{0, true}, {1, false}, {100, false}, {101, true}} {
		s := queue.DefaultSettings()
		s.MaxImportsPerSweep = tt.n
		if err := s.Validate(); (err != nil) != tt.wantErr {
			t.Errorf("MaxImportsPerSweep %d: err = %v, want error %v", tt.n, err, tt.wantErr)
		}
	}
}

func TestSupportsForceImport(t *testing.T) {
	t.Parallel()
	for _, k := range domain.AllKinds {
		want := k == domain.KindSonarr || k == domain.KindRadarr
		if got := queue.SupportsForceImport(k); got != want {
			t.Errorf("SupportsForceImport(%s) = %v, want %v", k, got, want)
		}
	}
}

// fakeClock is the part of clock.NewFake the fixture drives.
type fakeClock interface {
	clock.Clock
	Advance(d time.Duration)
}

type importFixture struct {
	inst queue.Instance
	st   *fakeStore
	arr  *fakeArrClient
	ev   *fakeEventPublisher
	m    *fakeMetricsRecorder
	clk  fakeClock
	svc  *queue.Service
}

func newImportFixture(t *testing.T, kind domain.AppKind, mode queue.Mode, items ...queue.Item) *importFixture {
	t.Helper()
	f := &importFixture{
		inst: queue.Instance{ID: uuid.New(), Name: "arr-main", Kind: kind, BaseURL: "http://arr.local", APIKey: "k"},
		st:   newFakeStore(),
		arr:  newFakeArrClient(),
		ev:   &fakeEventPublisher{},
		m:    &fakeMetricsRecorder{},
		clk:  clock.NewFake(),
	}
	s := idMatchedSettings(mode)
	s.ForceImport = false // the service sets it from the kind
	s.Grace = 0
	f.st.settings[f.inst.ID] = s
	f.arr.items[string(kind)+":"+f.inst.BaseURL] = items
	f.svc = newTestService(f.st, &fakeInstanceSource{instances: []queue.Instance{f.inst}}, f.arr, f.clk, f.ev, f.m)
	return f
}

func (f *importFixture) sweep(t *testing.T) {
	t.Helper()
	if err := f.svc.SweepAll(t.Context()); err != nil {
		t.Fatalf("SweepAll() error: %v", err)
	}
}

func (f *importFixture) eventTypes() []string {
	out := make([]string, 0, len(f.ev.frames))
	for _, fr := range f.ev.frames {
		if fr.Type != "queue_item_struck" {
			out = append(out, fr.Type+":"+fr.Detail)
		}
	}
	return out
}

func TestServiceForceImportSent(t *testing.T) {
	t.Parallel()
	f := newImportFixture(t, domain.KindSonarr, queue.ModeAct, idMatchedItem(7, "dl-7"))
	f.sweep(t)
	if len(f.arr.imports) != 1 || f.arr.imports[0] != "sonarr:dl-7" {
		t.Fatalf("imports = %v, want [sonarr:dl-7]", f.arr.imports)
	}
	if len(f.arr.removals) != 0 {
		t.Fatalf("removals = %v, want none", f.arr.removals)
	}
	got := f.eventTypes()
	if len(got) != 1 || got[0] != "queue_item_imported:imported 1 files by rule id_matched" {
		t.Fatalf("events = %v, want one queue_item_imported", got)
	}
	row := f.st.strikes[f.inst.ID]["dl-7"]
	if !row.ImportedAt.Equal(f.clk.Now()) || row.ImportRefused || !row.LastSeen.Equal(row.ImportedAt) {
		t.Fatalf("strike row = %+v, want ImportedAt = LastSeen = now", row)
	}
	if len(f.m.imports) != 1 || f.m.imports[0] != "arr-main:ok" {
		t.Fatalf("metrics = %v, want [arr-main:ok]", f.m.imports)
	}

	// The item is still ID-matched one sweep later: failed read-back, no second import.
	f.clk.Advance(10 * time.Minute)
	f.ev.frames = nil
	f.sweep(t)
	if len(f.arr.imports) != 1 {
		t.Fatalf("imports after read-back = %v, want no second import", f.arr.imports)
	}
	got = f.eventTypes()
	if len(got) != 1 || got[0] != "queue_item_import_failed:still_id_matched" {
		t.Fatalf("read-back events = %v, want one still_id_matched", got)
	}

	// A third sweep reports nothing new.
	f.clk.Advance(10 * time.Minute)
	f.ev.frames = nil
	f.sweep(t)
	if got := f.eventTypes(); len(got) != 0 || len(f.arr.imports) != 1 {
		t.Fatalf("third sweep events = %v imports = %v, want none", got, f.arr.imports)
	}
}

func TestServiceForceImportRefused(t *testing.T) {
	t.Parallel()
	f := newImportFixture(t, domain.KindRadarr, queue.ModeAct, idMatchedItem(7, "dl-7"))
	f.arr.importErr["dl-7"] = queue.ErrNoCleanCandidate
	f.sweep(t)
	got := f.eventTypes()
	if len(got) != 1 || got[0] != "queue_item_import_failed:no_clean_candidate" {
		t.Fatalf("events = %v, want one no_clean_candidate", got)
	}
	if row := f.st.strikes[f.inst.ID]["dl-7"]; !row.ImportRefused || row.ImportedAt.IsZero() {
		t.Fatalf("strike row = %+v, want refused stamp", row)
	}
	f.clk.Advance(10 * time.Minute)
	f.ev.frames = nil
	f.sweep(t)
	if got := f.eventTypes(); len(got) != 0 || len(f.arr.imports) != 1 || len(f.arr.removals) != 0 {
		t.Fatalf("after refusal: events %v imports %v removals %v, want nothing", got, f.arr.imports, f.arr.removals)
	}
}

func TestServiceForceImportSendErrorRetries(t *testing.T) {
	t.Parallel()
	f := newImportFixture(t, domain.KindSonarr, queue.ModeAct, idMatchedItem(7, "dl-7"))
	f.arr.importErr["dl-7"] = errors.New("connection refused")
	f.sweep(t)
	if row := f.st.strikes[f.inst.ID]["dl-7"]; !row.ImportedAt.IsZero() {
		t.Fatalf("strike row = %+v, want no stamp after a send error", row)
	}
	if len(f.m.imports) != 1 || f.m.imports[0] != "arr-main:error" {
		t.Fatalf("metrics = %v, want [arr-main:error]", f.m.imports)
	}
	delete(f.arr.importErr, "dl-7")
	f.clk.Advance(10 * time.Minute)
	f.sweep(t)
	if len(f.arr.imports) != 2 {
		t.Fatalf("imports = %v, want a retry", f.arr.imports)
	}
}

func TestServiceForceImportObserveAndKinds(t *testing.T) {
	t.Parallel()
	observe := newImportFixture(t, domain.KindSonarr, queue.ModeObserve, idMatchedItem(7, "dl-7"))
	observe.sweep(t)
	if len(observe.arr.imports) != 0 {
		t.Fatalf("observe imports = %v, want none", observe.arr.imports)
	}
	if len(observe.ev.frames) != 1 || observe.ev.frames[0].Type != "queue_item_struck" ||
		!jsonHas(t, observe.ev.frames[0].Detail, "would_import") {
		t.Fatalf("observe events = %+v, want one strike with would_import", observe.ev.frames)
	}
	lidarr := newImportFixture(t, domain.KindLidarr, queue.ModeAct, idMatchedItem(7, "dl-7"))
	lidarr.sweep(t)
	if len(lidarr.arr.imports) != 0 || len(lidarr.ev.frames) != 0 {
		t.Fatalf("lidarr imports = %v events = %v, want untouched", lidarr.arr.imports, lidarr.ev.frames)
	}
}

func jsonHas(t *testing.T, raw, key string) bool {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("payload %q: %v", raw, err)
	}
	return m[key] == true
}

func TestServiceForceImportSeasonPackOnce(t *testing.T) {
	t.Parallel()
	f := newImportFixture(t, domain.KindSonarr, queue.ModeAct, idMatchedItem(7, "dl-pack"), idMatchedItem(8, "dl-pack"), idMatchedItem(9, "dl-pack"))
	f.sweep(t)
	if len(f.arr.imports) != 1 {
		t.Fatalf("imports = %v, want one for the pack", f.arr.imports)
	}
	f.clk.Advance(10 * time.Minute)
	f.ev.frames = nil
	f.sweep(t)
	if got := f.eventTypes(); len(got) != 1 || got[0] != "queue_item_import_failed:still_id_matched" {
		t.Fatalf("read-back events = %v, want one still_id_matched for the pack", got)
	}
}
