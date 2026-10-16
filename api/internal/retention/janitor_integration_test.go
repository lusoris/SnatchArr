// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package retention

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/id"
	"github.com/golusoris/golusoris/leader"

	"github.com/lusoris/SnatchArr/api/internal/auth"
	"github.com/lusoris/SnatchArr/api/internal/events"
	"github.com/lusoris/SnatchArr/api/internal/leaderx"
	"github.com/lusoris/SnatchArr/api/internal/metrics/metricstest"
	"github.com/lusoris/SnatchArr/api/internal/settings"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

const (
	day = 24 * time.Hour
	// defaultRetention is history_retention_days on a fresh settings row.
	defaultRetention = 90 * day
	// Bounds of the leader polling (HISS-02).
	pollTries = 100
	pollEvery = 50 * time.Millisecond
)

type fixture struct {
	j        *Janitor
	st       *store.Store
	clk      *clockwork.FakeClock
	settings *settings.Service
	instance uuid.UUID
	reg      *prometheus.Registry
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	st := storetest.New(t)
	clk := clock.NewFake()
	logger := slog.New(slog.DiscardHandler)
	set := settings.New(st, clk)
	m, reg := metricstest.New(t)
	j := New(Deps{
		Settings: set, Recorder: snatch.NewRecorder(st, clk, events.New(logger)), Runs: snatch.NewRuns(st, clk, id.New()),
		Workers: snatch.NewWorkers(st, clk), Memory: snatch.NewMemory(st, clk), Budget: snatch.NewBudget(st, clk), Sessions: auth.NewPgSessionStore(st, clk),
		Lockouts: auth.NewPgLockoutStore(st),
		Clock:    clk, Logger: logger, Metrics: m,
	})
	return fixture{j: j, st: st, clk: clk, settings: set, instance: storetest.Instance(t, st, clk.Now()), reg: reg}
}

func (f fixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.st.Pool().Exec(t.Context(), sql, args...); err != nil {
		t.Fatalf("seed %q: %v", sql, err)
	}
}

func (f fixture) event(t *testing.T, ts time.Time) {
	t.Helper()
	_, err := f.st.Q().InsertEvent(t.Context(), sqlcgen.InsertEventParams{InstanceID: f.instance, Ts: ts, Level: "info", Type: "run_queued"})
	if err != nil {
		t.Fatalf("seed event: %v", err)
	}
}

func (f fixture) run(t *testing.T, status string, queued time.Time, finished *time.Time) {
	t.Helper()
	f.exec(t, `INSERT INTO snatch_runs (id, instance_id, kind, status, queued_at, finished_at) VALUES ($1, $2, 'missing', $3, $4, $5)`,
		uuid.New(), f.instance, status, queued, finished)
}

func (f fixture) lockout(t *testing.T, key string, fails int32, firstFailAt time.Time, lockedUntil *time.Time) {
	t.Helper()
	f.exec(t, `INSERT INTO login_lockouts (key, fails, first_fail_at, locked_until) VALUES ($1, $2, $3, $4)`,
		key, fails, firstFailAt, lockedUntil)
}

// remaining counts the rows left in every table the janitor trims.
func (f fixture) remaining(t *testing.T) Counts {
	t.Helper()
	var c Counts
	err := f.st.Pool().QueryRow(t.Context(), `SELECT
		(SELECT count(*) FROM snatch_events), (SELECT count(*) FROM snatch_runs), (SELECT count(*) FROM processed_items),
		(SELECT count(*) FROM rate_buckets) + (SELECT count(*) FROM global_rate_buckets), (SELECT count(*) FROM sessions), (SELECT count(*) FROM workers),
		(SELECT count(*) FROM login_lockouts)`,
	).Scan(&c.Events, &c.Runs, &c.Afterglow, &c.Buckets, &c.Sessions, &c.Workers, &c.Lockouts)
	if err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return c
}

// seed puts one row just past and one row exactly at (or just inside) each cutoff. The
// cutoffs are strict (<) for events, runs, buckets and workers, inclusive (<=) for afterglow and
// sessions, matching the queries.
func (f fixture) seed(t *testing.T) {
	t.Helper()
	ctx := t.Context()
	now := f.clk.Now()
	cutoff := now.Add(-defaultRetention)
	f.event(t, cutoff.Add(-time.Second)) // purged
	f.event(t, cutoff)                   // kept: not before the cutoff
	f.event(t, now)                      // kept
	old, atCutoff := cutoff.Add(-time.Second), cutoff
	f.run(t, "done", old, &old)               // purged
	f.run(t, "failed", atCutoff, &atCutoff)   // kept: not before the cutoff
	f.run(t, "queued", old.Add(-10*day), nil) // kept: never finished
	grace := now.Add(-snatch.PurgeGrace)
	f.exec(t, `INSERT INTO processed_items (instance_id, kind, entity_type, entity_id, expires_at) VALUES
		($1, 'missing', 'episode', 1, $2), ($1, 'missing', 'episode', 2, $3)`, f.instance, grace, grace.Add(time.Second))
	bucket := snatch.Minute(now).Add(-snatch.BucketKeep)
	for _, w := range []time.Time{bucket.Add(-time.Minute), bucket} { // first purged, second kept
		if err := f.st.Q().AddBucketUsed(ctx, sqlcgen.AddBucketUsedParams{InstanceID: f.instance, WindowStart: w, Used: 1}); err != nil {
			t.Fatalf("seed bucket: %v", err)
		}
		if err := f.st.Q().AddGlobalBucketUsed(ctx, sqlcgen.AddGlobalBucketUsedParams{WindowStart: w, Used: 1}); err != nil {
			t.Fatalf("seed global bucket: %v", err)
		}
	}
	for i, exp := range []time.Time{now, now.Add(time.Second)} { // first purged, second kept
		err := f.st.Q().SaveSession(ctx, sqlcgen.SaveSessionParams{ID: uuid.NewString(), Data: []byte(`{}`), ExpiresAt: exp})
		if err != nil {
			t.Fatalf("seed session %d: %v", i, err)
		}
	}
	f.exec(t, `INSERT INTO workers (id, last_contact_at) VALUES ('gone', $1), ('at-cutoff', $2)`, old, atCutoff)
}

// Rows past retention go, rows at or inside it stay, and a second pass finds nothing.
func TestPassDeletesOnlyPastRetention(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.seed(t)
	got, err := f.j.Pass(t.Context())
	want := Counts{Events: 1, Runs: 1, Afterglow: 1, Buckets: 2, Sessions: 1, Workers: 1}
	if err != nil || got != want {
		t.Fatalf("Pass() = %+v, %v; want %+v", got, err, want)
	}
	if left := f.remaining(t); left != (Counts{Events: 2, Runs: 2, Afterglow: 1, Buckets: 2, Sessions: 1, Workers: 1}) {
		t.Fatalf("rows left = %+v", left)
	}
	if again, err := f.j.Pass(t.Context()); err != nil || again != (Counts{}) {
		t.Fatalf("second Pass() = %+v, %v; want nothing deleted", again, err)
	}
}

// Lowering history_retention_days makes the next pass delete what the old value kept.
func TestRetentionSettingChangesNextPass(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.seed(t)
	if _, err := f.j.Pass(t.Context()); err != nil {
		t.Fatalf("Pass() = %v", err)
	}
	cfg, err := f.settings.Get(t.Context())
	if err != nil {
		t.Fatalf("settings.Get() = %v", err)
	}
	cfg.HistoryRetentionDays = 1
	if _, updateErr := f.settings.Update(t.Context(), cfg); updateErr != nil {
		t.Fatalf("settings.Update() = %v", updateErr)
	}
	got, err := f.j.Pass(t.Context())
	if want := (Counts{Events: 1, Runs: 1, Workers: 1}); err != nil || got != want {
		t.Fatalf("Pass() after retention 1 day = %+v, %v; want %+v", got, err, want)
	}
}

// One pass drains a table in several batches.
func TestPassDrainsInBatches(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.j.batch = 2
	old := f.clk.Now().Add(-defaultRetention - time.Hour)
	for range 5 {
		f.event(t, old)
	}
	if got, err := f.j.Pass(t.Context()); err != nil || got.Events != 5 {
		t.Fatalf("Pass() = %+v, %v; want 5 events in batches of 2", got, err)
	}
}

// The janitor does not run while another replica leads, and runs once this one does.
func TestJanitorRunsOnLeaderOnly(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.seed(t)
	follower := func(ctx context.Context, cb leader.Callbacks) error {
		cb.OnNewLeader("other-replica")
		<-ctx.Done()
		return nil
	}
	runGate(t, f, follower, 10*pollEvery)
	if left := f.remaining(t); left.Sessions != 2 {
		t.Fatalf("a follower purged: rows left %+v", left)
	}

	leading := func(ctx context.Context, cb leader.Callbacks) error {
		cb.OnNewLeader("me")
		cb.OnStartedLeading(ctx)
		<-ctx.Done()
		cb.OnStoppedLeading()
		return nil
	}
	ctx, stop := startGate(t, f, leading)
	defer stop()
	for range pollTries {
		if f.remaining(t).Sessions == 1 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("gate stopped early")
		case <-time.After(pollEvery):
		}
	}
	t.Fatal("the leader never purged")
}

// runGate runs the leader gate with elect for d, then stops it.
func runGate(t *testing.T, f fixture, elect leaderx.Elect, d time.Duration) {
	t.Helper()
	_, stop := startGate(t, f, elect)
	time.Sleep(d)
	stop()
}

func startGate(t *testing.T, f fixture, elect leaderx.Elect) (context.Context, func()) {
	t.Helper()
	g := leaderx.New(f.clk, slog.New(slog.DiscardHandler), f.j)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := g.Run(ctx, elect, time.Second); err != nil {
			t.Errorf("gate Run: %v", err)
		}
	}()
	return ctx, func() { cancel(); <-done }
}

// startJanitor runs the janitor's loop directly, without the gate, until the test ends.
func startJanitor(t *testing.T, f fixture) {
	t.Helper()
	if err := f.j.Start(t.Context()); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), time.Duration(pollTries)*pollEvery)
		defer cancel()
		if err := f.j.Stop(ctx); err != nil {
			t.Errorf("Stop() = %v", err)
		}
	})
}

// Positive: a pass that purges every table reaches the loop metrics as the last success.
func TestJanitorRecordsTheLastSuccess(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.seed(t)
	startJanitor(t, f)
	metricstest.Await(t, f.reg, "snatcharr_loop_last_success_timestamp_seconds", "loop", "retention",
		float64(f.clk.Now().UnixNano())/float64(time.Second))
	metricstest.Await(t, f.reg, "snatcharr_loop_failures_total", "loop", "retention", 0)
}

// Negative: a table that cannot be purged fails the pass and counts as a failed tick, while
// the other tables are still purged (gap audit A1-07).
func TestJanitorCountsAFailedPass(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.seed(t)
	f.exec(t, `DROP TABLE snatch_events`)
	startJanitor(t, f)
	metricstest.Await(t, f.reg, "snatcharr_loop_failures_total", "loop", "retention", 1)
	var sessions int
	if err := f.st.Pool().QueryRow(t.Context(), `SELECT count(*) FROM sessions`).Scan(&sessions); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessions != 1 {
		t.Errorf("sessions left %d after the failed pass, want 1: the other tables are still purged", sessions)
	}
	if got, ok := metricstest.Value(t, f.reg, "snatcharr_loop_last_success_timestamp_seconds", "loop", "retention"); ok {
		t.Errorf("last success = %v after a failed pass only, want no series", got)
	}
}

// Positive, negative and boundary cases for pruning stale login lockouts:
// - Positive: stale unlocked row (first_fail_at < now - window) purged.
// - Positive: stale row whose lock expired (locked_until < now and first_fail_at < now - window) purged.
// - Negative: row inside window (first_fail_at >= now - window) kept.
// - Negative: row still locked in the future (locked_until > now) kept.
// - Boundary: row exactly at window edge (first_fail_at == now - window) kept (strict < window).
// - Boundary: row just past window edge (first_fail_at == now - window - 1ms) purged.
// - Boundary: row with locked_until == now kept (strict < now).
// - Boundary: row with locked_until == now - 1ms purged (strict < now).
func TestPassPrunesStaleLoginLockouts(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	now := f.clk.Now()
	window := auth.LockoutWindow

	// Positive: stale unlocked row
	old := now.Add(-window - time.Minute)
	f.lockout(t, "stale-unlocked", 2, old, nil)

	// Positive: stale row with expired lock
	lockExpired := now.Add(-time.Minute)
	f.lockout(t, "stale-locked-expired", 5, old, &lockExpired)

	// Negative 1: row inside window
	insideWindow := now.Add(-5 * time.Minute)
	f.lockout(t, "active-inside-window", 1, insideWindow, nil)

	// Negative 2: row still locked in future
	lockedFuture := now.Add(5 * time.Minute)
	f.lockout(t, "active-still-locked", 5, old, &lockedFuture)

	// Boundary 1: row exactly at window edge (kept)
	atEdge := now.Add(-window)
	f.lockout(t, "boundary-at-edge", 1, atEdge, nil)

	// Boundary 2: row just past window edge (purged)
	justPastEdge := now.Add(-window - time.Millisecond)
	f.lockout(t, "boundary-just-past", 1, justPastEdge, nil)

	// Boundary 3: row whose locked_until == now (kept)
	atLockNow := now
	f.lockout(t, "boundary-lock-at-now", 5, old, &atLockNow)

	// Boundary 4: row whose locked_until == now - 1ms (purged)
	justExpiredLock := now.Add(-time.Millisecond)
	f.lockout(t, "boundary-lock-just-expired", 5, old, &justExpiredLock)

	if _, err := f.j.Pass(t.Context()); err != nil {
		t.Fatalf("Pass() err = %v", err)
	}

	surviving := []string{
		"active-inside-window",
		"active-still-locked",
		"boundary-at-edge",
		"boundary-lock-at-now",
	}
	for _, key := range surviving {
		var count int
		err := f.st.Pool().QueryRow(t.Context(), `SELECT count(*) FROM login_lockouts WHERE key = $1`, key).Scan(&count)
		if err != nil || count != 1 {
			t.Errorf("expected row %q to survive, count=%d, err=%v", key, count, err)
		}
	}

	purged := []string{
		"stale-unlocked",
		"stale-locked-expired",
		"boundary-just-past",
		"boundary-lock-just-expired",
	}
	for _, key := range purged {
		var count int
		err := f.st.Pool().QueryRow(t.Context(), `SELECT count(*) FROM login_lockouts WHERE key = $1`, key).Scan(&count)
		if err != nil || count != 0 {
			t.Errorf("expected row %q to be purged, count=%d, err=%v", key, count, err)
		}
	}
}
