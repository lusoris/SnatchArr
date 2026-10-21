// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package queue_test

import (
	"errors"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/events"
	"github.com/lusoris/SnatchArr/api/internal/queue"
	"github.com/lusoris/SnatchArr/api/internal/settings"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

type queueFixture struct {
	q        *queue.Store
	st       *store.Store
	clk      *clockwork.FakeClock
	instance uuid.UUID
}

func newQueueFixture(t *testing.T) queueFixture {
	t.Helper()
	st := storetest.New(t)
	clk := clock.NewFake()
	return queueFixture{
		q:        queue.NewStore(st, clk),
		st:       st,
		clk:      clk,
		instance: storetest.Instance(t, st, clk.Now()),
	}
}

func TestSettingsDefaultsWhenNoRow(t *testing.T) {
	t.Parallel()
	f := newQueueFixture(t)
	got, err := f.q.Settings(t.Context(), f.instance)
	if err != nil {
		t.Fatalf("Settings() = %v", err)
	}
	want := queue.DefaultSettings()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Settings() = %+v, want defaults %+v", got, want)
	}
}

func TestSettingsSaveLoadRoundTrip(t *testing.T) {
	t.Parallel()
	f := newQueueFixture(t)

	// Round trip 1: with nil score and custom rule settings
	s1 := queue.DefaultSettings()
	s1.Enabled = true
	s1.Grace = 30 * time.Minute
	s1.MinBytesPerSecond = 1048576
	s1.MinCustomFormatScore = nil
	s1.MaxRemovalsPerSweep = 25
	s1.MaxImportsPerSweep = 40
	s1.Rules[queue.RuleStalled] = queue.RuleSettings{Mode: queue.ModeAct, Threshold: 5}
	noBlocklist, blocklist := false, true
	s1.Rules[queue.RuleFailed] = queue.RuleSettings{Mode: queue.ModeObserve, Threshold: 2, Blocklist: &noBlocklist}
	s1.Rules[queue.RuleUnwantedUnmonitored] = queue.RuleSettings{Mode: queue.ModeObserve, Threshold: 1, Blocklist: &blocklist}

	if err := f.q.SaveSettings(t.Context(), f.instance, s1); err != nil {
		t.Fatalf("SaveSettings(s1) = %v", err)
	}
	got1, err := f.q.Settings(t.Context(), f.instance)
	if err != nil {
		t.Fatalf("Settings(1) = %v", err)
	}
	if !reflect.DeepEqual(got1, s1) {
		t.Fatalf("Settings(1) = %+v, want %+v", got1, s1)
	}

	// Round trip 2: with non-nil score
	score := int32(150)
	s2 := s1
	s2.MinCustomFormatScore = &score
	s2.Enabled = false
	if err = f.q.SaveSettings(t.Context(), f.instance, s2); err != nil {
		t.Fatalf("SaveSettings(s2) = %v", err)
	}
	got2, err := f.q.Settings(t.Context(), f.instance)
	if err != nil {
		t.Fatalf("Settings(2) = %v", err)
	}
	if !reflect.DeepEqual(got2, s2) {
		t.Fatalf("Settings(2) = %+v, want %+v", got2, s2)
	}
}

func TestSettingsDropsUnknownRuleKeys(t *testing.T) {
	t.Parallel()
	f := newQueueFixture(t)

	// Save policy directly with an unknown rule key in JSON
	rawJSON := `{"stalled":{"mode":"act","threshold":4},"unknown_alien":{"mode":"act","threshold":99}}`
	_, err := f.st.Pool().Exec(t.Context(), `
		INSERT INTO queue_policies (instance_id, enabled, rules, grace_s, min_bytes_per_second, max_removals_per_sweep, updated_at)
		VALUES ($1, true, $2::jsonb, 900, 0, 10, $3)
	`, f.instance, rawJSON, f.clk.Now())
	if err != nil {
		t.Fatalf("direct insert: %v", err)
	}

	got, err := f.q.Settings(t.Context(), f.instance)
	if err != nil {
		t.Fatalf("Settings() = %v", err)
	}
	if _, ok := got.Rules[queue.Rule("unknown_alien")]; ok {
		t.Fatalf("Settings() kept unknown rule: %+v", got.Rules)
	}
	if !reflect.DeepEqual(got.Rules[queue.RuleStalled], queue.RuleSettings{Mode: queue.ModeAct, Threshold: 4}) {
		t.Fatalf("Settings() stalled rule = %+v, want mode act threshold 4", got.Rules[queue.RuleStalled])
	}
	// A row written before the blocklist field existed keeps the rule's default.
	if rs := queue.ResolveRule(got, queue.RuleStalled); rs.Blocklist == nil || !*rs.Blocklist {
		t.Fatalf("ResolveRule(stalled) blocklist = %v, want default true", rs.Blocklist)
	}
	if want := queue.DefaultSettings().Rules[queue.RuleFailed]; !reflect.DeepEqual(got.Rules[queue.RuleFailed], want) {
		t.Fatalf("Settings() failed rule = %+v, want default %+v", got.Rules[queue.RuleFailed], want)
	}
}

func TestSaveSettingsRefusesInvalidAndWritesNothing(t *testing.T) {
	t.Parallel()
	f := newQueueFixture(t)

	// Case 1: Threshold 0
	invalid1 := queue.DefaultSettings()
	invalid1.Rules[queue.RuleStalled] = queue.RuleSettings{Mode: queue.ModeObserve, Threshold: 0}
	if err := f.q.SaveSettings(t.Context(), f.instance, invalid1); !errors.Is(err, queue.ErrInvalid) {
		t.Fatalf("SaveSettings(threshold 0) = %v, want ErrInvalid", err)
	}

	// Verify nothing was written
	got1, err := f.q.Settings(t.Context(), f.instance)
	if err != nil {
		t.Fatalf("Settings() = %v", err)
	}
	if !reflect.DeepEqual(got1, queue.DefaultSettings()) {
		t.Fatalf("Settings() wrote row on invalid threshold: %+v", got1)
	}

	// Case 2: Grace 25h (exceeds 24h)
	invalid2 := queue.DefaultSettings()
	invalid2.Grace = 25 * time.Hour
	if err = f.q.SaveSettings(t.Context(), f.instance, invalid2); !errors.Is(err, queue.ErrInvalid) {
		t.Fatalf("SaveSettings(grace 25h) = %v, want ErrInvalid", err)
	}

	// Verify nothing was written
	got2, err := f.q.Settings(t.Context(), f.instance)
	if err != nil {
		t.Fatalf("Settings() = %v", err)
	}
	if !reflect.DeepEqual(got2, queue.DefaultSettings()) {
		t.Fatalf("Settings() wrote row on invalid grace: %+v", got2)
	}
}

func TestRecordAndStrikesRoundTrip(t *testing.T) {
	t.Parallel()
	f := newQueueFixture(t)
	now := f.clk.Now().UTC().Truncate(time.Microsecond)

	decisions := []queue.Decision{
		{
			ItemID:     101,
			DownloadID: "dl-101",
			Rule:       queue.RuleStalled,
			Next: queue.Strike{
				Rule:         queue.RuleStalled,
				Count:        3,
				LastSizeLeft: 1048576,
				LastSeen:     now,
			},
		},
		{
			ItemID:     102,
			DownloadID: "dl-102",
			Rule:       queue.RuleSlow,
			Next: queue.Strike{
				Rule:         queue.RuleSlow,
				Count:        1,
				LastSizeLeft: 2097152,
				LastSeen:     now,
			},
		},
	}

	if err := f.q.Record(t.Context(), f.instance, decisions); err != nil {
		t.Fatalf("Record() = %v", err)
	}

	strikes, err := f.q.Strikes(t.Context(), f.instance)
	if err != nil {
		t.Fatalf("Strikes() = %v", err)
	}
	if len(strikes) != 2 {
		t.Fatalf("Strikes() count = %d, want 2", len(strikes))
	}

	s1 := strikes["dl-101"]
	if s1.Rule != queue.RuleStalled || s1.Count != 3 || s1.LastSizeLeft != 1048576 || !s1.LastSeen.Equal(now) {
		t.Fatalf("strike dl-101 = %+v, want rule stalled count 3 size 1048576", s1)
	}

	s2 := strikes["dl-102"]
	if s2.Rule != queue.RuleSlow || s2.Count != 1 || s2.LastSizeLeft != 2097152 || !s2.LastSeen.Equal(now) {
		t.Fatalf("strike dl-102 = %+v, want rule slow count 1 size 2097152", s2)
	}

	// Update in place
	later := now.Add(10 * time.Minute)
	decisions[0].Next.Count = 4
	decisions[0].Next.LastSeen = later
	if err = f.q.Record(t.Context(), f.instance, decisions[:1]); err != nil {
		t.Fatalf("Record update = %v", err)
	}

	updated, err := f.q.Strikes(t.Context(), f.instance)
	if err != nil {
		t.Fatalf("Strikes() after update = %v", err)
	}
	if updated["dl-101"].Count != 4 || !updated["dl-101"].LastSeen.Equal(later) {
		t.Fatalf("strike dl-101 after update = %+v, want count 4 time %v", updated["dl-101"], later)
	}
}

// TestRecordImportStampRoundTrip pins the id_matched read-back columns: an
// unstamped row reads back zero, a stamp and a refusal survive, and a later
// unstamped write clears them again (the epoch sentinel becomes NULL).
func TestRecordImportStampRoundTrip(t *testing.T) {
	t.Parallel()
	f := newQueueFixture(t)
	now := f.clk.Now().UTC().Truncate(time.Microsecond)
	d := queue.Decision{ItemID: 7, DownloadID: "dl-7", Rule: queue.RuleIDMatched, Next: queue.Strike{Rule: queue.RuleIDMatched, Count: 1, LastSeen: now}}
	read := func(step string) queue.Strike {
		t.Helper()
		if err := f.q.Record(t.Context(), f.instance, []queue.Decision{d}); err != nil {
			t.Fatalf("%s: Record() = %v", step, err)
		}
		strikes, err := f.q.Strikes(t.Context(), f.instance)
		if err != nil {
			t.Fatalf("%s: Strikes() = %v", step, err)
		}
		return strikes["dl-7"]
	}
	if got := read("unstamped"); !got.ImportedAt.IsZero() || got.ImportRefused {
		t.Fatalf("unstamped = %+v, want zero ImportedAt", got)
	}
	d.Next.ImportedAt, d.Next.ImportRefused = now, true
	if got := read("stamped"); !got.ImportedAt.Equal(now) || !got.ImportRefused {
		t.Fatalf("stamped = %+v, want ImportedAt %v refused", got, now)
	}
	d.Next.ImportedAt, d.Next.ImportRefused = time.Time{}, false
	if got := read("cleared"); !got.ImportedAt.IsZero() || got.ImportRefused {
		t.Fatalf("cleared = %+v, want zero ImportedAt", got)
	}
}

func TestRecordSkipsEmptyDownloadID(t *testing.T) {
	t.Parallel()
	f := newQueueFixture(t)
	now := f.clk.Now().UTC()

	decisions := []queue.Decision{
		{
			ItemID:     201,
			DownloadID: "",
			Rule:       queue.RuleStalled,
			Next: queue.Strike{
				Rule:         queue.RuleStalled,
				Count:        1,
				LastSizeLeft: 100,
				LastSeen:     now,
			},
		},
	}

	if err := f.q.Record(t.Context(), f.instance, decisions); err != nil {
		t.Fatalf("Record(empty download id) = %v", err)
	}

	strikes, err := f.q.Strikes(t.Context(), f.instance)
	if err != nil {
		t.Fatalf("Strikes() = %v", err)
	}
	if len(strikes) != 0 {
		t.Fatalf("Strikes() = %+v, want empty", strikes)
	}
}

func TestForgetRemovesStrike(t *testing.T) {
	t.Parallel()
	f := newQueueFixture(t)
	now := f.clk.Now().UTC()

	decisions := []queue.Decision{
		{
			ItemID:     301,
			DownloadID: "dl-forget",
			Rule:       queue.RuleFailed,
			Next: queue.Strike{
				Rule:         queue.RuleFailed,
				Count:        2,
				LastSizeLeft: 50,
				LastSeen:     now,
			},
		},
	}
	if err := f.q.Record(t.Context(), f.instance, decisions); err != nil {
		t.Fatalf("Record() = %v", err)
	}

	strikes, err := f.q.Strikes(t.Context(), f.instance)
	if err != nil || len(strikes) != 1 {
		t.Fatalf("Strikes() before forget = %+v, %v", strikes, err)
	}

	if err = f.q.Forget(t.Context(), f.instance, "dl-forget"); err != nil {
		t.Fatalf("Forget() = %v", err)
	}

	strikesAfter, err := f.q.Strikes(t.Context(), f.instance)
	if err != nil || len(strikesAfter) != 0 {
		t.Fatalf("Strikes() after forget = %+v, %v; want empty", strikesAfter, err)
	}
}

func TestPurgeRemovesOnlyExpiredRows(t *testing.T) {
	t.Parallel()
	f := newQueueFixture(t)
	now := f.clk.Now().UTC()

	decisions := []queue.Decision{
		{
			ItemID:     401,
			DownloadID: "dl-purge-target",
			Rule:       queue.RuleStalled,
			Next: queue.Strike{
				Rule:         queue.RuleStalled,
				Count:        2,
				LastSizeLeft: 1000,
				LastSeen:     now,
			},
		},
	}
	if err := f.q.Record(t.Context(), f.instance, decisions); err != nil {
		t.Fatalf("Record() = %v", err)
	}

	// 24h - 1s: not yet expired
	f.clk.Advance(24*time.Hour - time.Second)
	purged, err := f.q.Purge(t.Context(), 100)
	if err != nil {
		t.Fatalf("Purge(24h-1s) = %v", err)
	}
	if purged != 0 {
		t.Fatalf("Purge(24h-1s) deleted %d rows, want 0", purged)
	}
	strikes, err := f.q.Strikes(t.Context(), f.instance)
	if err != nil || len(strikes) != 1 {
		t.Fatalf("Strikes(24h-1s) = %+v, want 1 strike kept", strikes)
	}

	// Advance 2s more -> 24h + 1s from insertion: now expired
	f.clk.Advance(2 * time.Second)
	purged, err = f.q.Purge(t.Context(), 100)
	if err != nil {
		t.Fatalf("Purge(24h+1s) = %v", err)
	}
	if purged != 1 {
		t.Fatalf("Purge(24h+1s) deleted %d rows, want 1", purged)
	}
	strikesAfter, err := f.q.Strikes(t.Context(), f.instance)
	if err != nil || len(strikesAfter) != 0 {
		t.Fatalf("Strikes(24h+1s) = %+v, want 0 strikes", strikesAfter)
	}
}

func TestInstanceDeleteCascades(t *testing.T) {
	t.Parallel()
	f := newQueueFixture(t)
	now := f.clk.Now().UTC()

	if err := f.q.SaveSettings(t.Context(), f.instance, queue.DefaultSettings()); err != nil {
		t.Fatalf("SaveSettings() = %v", err)
	}
	dec := []queue.Decision{
		{
			ItemID:     501,
			DownloadID: "dl-cascade",
			Rule:       queue.RuleSlow,
			Next: queue.Strike{
				Rule:         queue.RuleSlow,
				Count:        1,
				LastSizeLeft: 10,
				LastSeen:     now,
			},
		},
	}
	if err := f.q.Record(t.Context(), f.instance, dec); err != nil {
		t.Fatalf("Record() = %v", err)
	}

	// Delete instance
	tag, err := f.st.Pool().Exec(t.Context(), "DELETE FROM instances WHERE id = $1", f.instance)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("DELETE instance: %v (rows: %d)", err, tag.RowsAffected())
	}

	// Check queue_policies
	var policyCount int
	err = f.st.Pool().QueryRow(t.Context(), "SELECT count(*) FROM queue_policies WHERE instance_id = $1", f.instance).Scan(&policyCount)
	if err != nil || policyCount != 0 {
		t.Fatalf("policyCount = %d, %v; want 0", policyCount, err)
	}

	// Check queue_strikes
	var strikeCount int
	err = f.st.Pool().QueryRow(t.Context(), "SELECT count(*) FROM queue_strikes WHERE instance_id = $1", f.instance).Scan(&strikeCount)
	if err != nil || strikeCount != 0 {
		t.Fatalf("strikeCount = %d, %v; want 0", strikeCount, err)
	}
}

func TestDownloadPriorityPreset(t *testing.T) {
	t.Parallel()
	f := newQueueFixture(t)

	// Positive: freshly migrated DB returns MediaFirstPreset.
	got, err := f.q.DownloadPriorityPreset(t.Context())
	if err != nil {
		t.Fatalf("DownloadPriorityPreset() freshly migrated = %v", err)
	}
	if !reflect.DeepEqual(got, domain.MediaFirstPreset()) {
		t.Fatalf("DownloadPriorityPreset() freshly migrated = %+v, want MediaFirstPreset %+v", got, domain.MediaFirstPreset())
	}

	// Positive & boundary: returns stored preset after settings update with ranks 1 and 100.
	customPreset := domain.PriorityPreset{
		domain.KindSonarr:     1,
		domain.KindRadarr:     100,
		domain.KindLidarr:     20,
		domain.KindReadarr:    30,
		domain.KindWhisparrV2: 40,
		domain.KindWhisparrV3: 50,
	}
	settingsSvc := settings.New(f.st, f.clk)
	current, err := settingsSvc.Get(t.Context())
	if err != nil {
		t.Fatalf("settings.Get() = %v", err)
	}
	current.DownloadPriorityPreset = customPreset
	if _, err = settingsSvc.Update(t.Context(), current); err != nil {
		t.Fatalf("settings.Update() = %v", err)
	}

	gotAfter, err := f.q.DownloadPriorityPreset(t.Context())
	if err != nil {
		t.Fatalf("DownloadPriorityPreset() after update = %v", err)
	}
	if !reflect.DeepEqual(gotAfter, customPreset) {
		t.Fatalf("DownloadPriorityPreset() after update = %+v, want %+v", gotAfter, customPreset)
	}
}

func TestQueueEventPersistenceIntegration(t *testing.T) {
	t.Parallel()
	f := newQueueFixture(t)
	logger := slog.New(slog.DiscardHandler)
	bus := events.New(logger)
	rec := snatch.NewRecorder(f.st, f.clk, bus)
	adapter := queue.NewRecorderAdapter(rec, bus, logger)

	now := f.clk.Now().UTC()
	frame := events.Frame{
		InstanceID: f.instance,
		Timestamp:  now,
		Level:      "info",
		Type:       "queue_item_imported",
		EntityID:   777,
		Title:      "dl-777",
		Detail:     "imported 1 files by rule id_matched",
	}

	adapter.Publish(t.Context(), frame)

	evs, err := rec.List(t.Context(), snatch.ListParams{
		InstanceID: &f.instance,
		Type:       "queue_item_imported",
		PageSize:   10,
	})
	if err != nil {
		t.Fatalf("rec.List() = %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("rec.List() returned %d events, want 1", len(evs))
	}
	got := evs[0]
	if got.RunID != nil {
		t.Fatalf("got.RunID = %v, want nil", got.RunID)
	}
	if got.InstanceID != f.instance || got.Type != "queue_item_imported" || got.EntityID != 777 || got.Title != "dl-777" || got.Detail != "imported 1 files by rule id_matched" {
		t.Fatalf("persisted event = %+v, want matching frame fields", got)
	}
}
