// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package store_test

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	dbmigrate "github.com/golusoris/golusoris/db/migrate"

	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

// TestMain shares one Postgres container across the package's integration tests.
func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

// leftovers counts what the schema still holds besides golang-migrate's bookkeeping:
// relations (tables, views, sequences, indexes), standalone types and functions.
const leftovers = `SELECT
	(SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
	 WHERE n.nspname = 'public' AND c.relname NOT LIKE 'schema_migrations%') +
	(SELECT count(*) FROM pg_type t JOIN pg_namespace n ON n.oid = t.typnamespace
	 WHERE n.nspname = 'public' AND t.typrelid = 0 AND t.typelem = 0) +
	(SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace WHERE n.nspname = 'public')`

func version(t *testing.T, m *dbmigrate.Migrator) uint {
	t.Helper()
	v, dirty, err := m.Version()
	if err != nil || dirty {
		t.Fatalf("Version() = %d, dirty %v, %v", v, dirty, err)
	}
	return v
}

func leftoverCount(t *testing.T, st *store.Store) int64 {
	t.Helper()
	var n int64
	if err := st.Pool().QueryRow(t.Context(), leftovers).Scan(&n); err != nil {
		t.Fatalf("count schema objects: %v", err)
	}
	return n
}

// Every migration reverts completely and applies again: down to nothing leaves no schema
// object behind, and a second up over that clean slate reaches the same version (a down that
// forgot something makes it fail on "already exists").
func TestMigrationsRoundTrip(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	m := storetest.Migrator(t, st)
	latest := version(t, m)
	if latest == 0 || leftoverCount(t, st) == 0 {
		t.Fatal("the cloned database has no migrations applied")
	}
	for round := 1; round <= 2; round++ {
		if err := m.Down(); err != nil {
			t.Fatalf("round %d: Down() = %v", round, err)
		}
		if v := version(t, m); v != 0 {
			t.Fatalf("round %d: version after Down() = %d, want 0", round, v)
		}
		if n := leftoverCount(t, st); n != 0 {
			t.Fatalf("round %d: %d schema objects survive a full Down()", round, n)
		}
		if err := m.Up(); err != nil {
			t.Fatalf("round %d: Up() = %v", round, err)
		}
		if v := version(t, m); v != latest {
			t.Fatalf("round %d: version after Up() = %d, want %d", round, v, latest)
		}
	}
}

// Boundary: each migration on its own goes down and up again from the version before it,
// walking from the latest down to the first.
func TestEachMigrationStepsDownAndUp(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	m := storetest.Migrator(t, st)
	for v := version(t, m); v > 0; v = version(t, m) {
		for _, step := range []int{-1, 1, -1} {
			if err := m.Steps(step); err != nil {
				t.Fatalf("Steps(%d) at version %d = %v", step, v, err)
			}
		}
		if got := version(t, m); got >= v {
			t.Fatalf("version after stepping down from %d = %d", v, got)
		}
	}
	if n := leftoverCount(t, st); n != 0 {
		t.Fatalf("%d schema objects survive stepping every migration down", n)
	}
}

// previousRelease is the schema version shipped by the last tagged release (v0.1.8).
// When cutting a release that ships new migrations, bump this to the new release's latest
// schema version, add any new tables to testdata/upgrade/v<version>.sql, and update the
// queries in TestMigrationsUpgradePopulated.
const (
	previousRelease = 9
	fixturePath     = "testdata/upgrade/v0.1.8.sql"
)

var (
	fixtureSonarrID    = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	fixtureRunID       = uuid.MustParse("a1111111-1111-1111-1111-111111111111")
	fixtureScheduleID  = uuid.MustParse("b1111111-1111-1111-1111-111111111111")
	fixtureUserID      = uuid.MustParse("c1111111-1111-1111-1111-111111111111")
	fixtureClientID    = uuid.MustParse("d1111111-1111-1111-1111-111111111111")
	fixtureSeerrLinkID = uuid.MustParse("e1111111-1111-1111-1111-111111111111")
	reInsertTable      = regexp.MustCompile(`(?i)\bINSERT\s+INTO\s+([a-zA-Z0-9_]+)`)
)

func stepDownToVersion(t *testing.T, m *dbmigrate.Migrator, target uint) {
	t.Helper()
	for v := version(t, m); v > target; v = version(t, m) {
		if err := m.Steps(-1); err != nil {
			t.Fatalf("step down from %d: %v", v, err)
		}
	}
	if got := version(t, m); got != target {
		t.Fatalf("version after step down = %d, want %d", got, target)
	}
}

func applyFixture(t *testing.T, st *store.Store, path string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	if _, err := st.Pool().Exec(t.Context(), string(content)); err != nil {
		t.Fatalf("exec fixture %s: %v", path, err)
	}
}

func parseFixtureTables(t *testing.T, path string) map[string]bool {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	matches := reInsertTable.FindAllStringSubmatch(string(content), -1)
	tables := make(map[string]bool, len(matches))
	for _, m := range matches {
		tables[strings.ToLower(m[1])] = true
	}
	return tables
}

// TestMigrationsUpgradeFixtureCoversAllTables guards against unmigrated new tables by verifying
// that the upgrade fixture inserts into every table present in the previous release database schema.
func TestMigrationsUpgradeFixtureCoversAllTables(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	m := storetest.Migrator(t, st)
	stepDownToVersion(t, m, previousRelease)
	fixtureTables := parseFixtureTables(t, fixturePath)

	const query = `SELECT table_name FROM information_schema.tables ` +
		`WHERE table_schema = 'public' AND table_type = 'BASE TABLE' ` +
		`AND table_name <> 'schema_migrations' ORDER BY table_name`

	rows, err := st.Pool().Query(t.Context(), query)
	if err != nil {
		t.Fatalf("query tables: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var tbl string
		if err := rows.Scan(&tbl); err != nil {
			t.Fatalf("scan table: %v", err)
		}
		if !fixtureTables[tbl] {
			t.Errorf("fixture missing table: %s", tbl)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}
}

// TestMigrationsUpgradePopulated steps down to the previous release, inserts a full v9 dataset,
// migrates up to latest, and confirms all rows are intact and queryable via sqlc queries.
func TestMigrationsUpgradePopulated(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	m := storetest.Migrator(t, st)

	latest := version(t, m)
	if latest < previousRelease {
		t.Fatalf("current version %d is below previousRelease %d", latest, previousRelease)
	}

	stepDownToVersion(t, m, previousRelease)
	// Migration 000025 seeds the settings singleton and its down step keeps the row; a real
	// previous-release database holds only the fixture's own row.
	if _, err := st.Pool().Exec(t.Context(), "DELETE FROM settings"); err != nil {
		t.Fatalf("clear seeded settings row: %v", err)
	}
	applyFixture(t, st, fixturePath)

	if err := m.Up(); err != nil {
		t.Fatalf("Up() after fixture: %v", err)
	}
	if v := version(t, m); v != latest {
		t.Fatalf("version after Up() = %d, want %d", v, latest)
	}

	ctx := t.Context()
	q := sqlcgen.New(st.Pool())

	assertInstancesAndPolicies(t, q, ctx)
	assertRuns(t, q, ctx, fixtureRunID)
	assertEventsAndProcessed(t, q, ctx, fixtureSonarrID)
	assertBuckets(t, q, ctx, fixtureSonarrID)
	assertSchedulesAndSettings(t, q, ctx, fixtureScheduleID)
	assertUsersAndSessions(t, q, ctx, fixtureUserID)
	assertKeysAndWorkers(t, q, ctx, fixtureUserID)
	assertClients(t, q, ctx, fixtureClientID)
	assertSeerr(t, q, ctx, fixtureSeerrLinkID)
}

func checkInstances(t *testing.T, insts []sqlcgen.Instance) {
	t.Helper()
	byKind := make(map[string]sqlcgen.Instance, len(insts))
	for _, inst := range insts {
		byKind[inst.Kind] = inst
	}
	checkInstanceFields(t, byKind["sonarr"], byKind["radarr"], byKind["readarr"])
}

func checkInstanceFields(t *testing.T, sonarr, radarr, readarr sqlcgen.Instance) {
	t.Helper()
	if sonarr.Name != "sonarr-main" || sonarr.ConfigarrOrphanedAt != nil {
		t.Errorf("unexpected sonarr: %+v", sonarr)
	}
	if radarr.Source != "configarr" || radarr.ConfigarrOrphanedAt != nil {
		t.Errorf("unexpected radarr: %+v", radarr)
	}
	if radarr.ConfigarrKey == nil || *radarr.ConfigarrKey != "radarr_4k" {
		t.Errorf("unexpected radarr configarr_key: %+v", radarr.ConfigarrKey)
	}
	if readarr.LastSeenProduct == nil || *readarr.LastSeenProduct != "readarr" {
		t.Errorf("unexpected readarr last_seen_product: %+v", readarr.LastSeenProduct)
	}
}

func checkPolicy(t *testing.T, q *sqlcgen.Queries, ctx context.Context, id uuid.UUID) {
	t.Helper()
	pol, err := q.GetPolicy(ctx, id)
	if err != nil {
		t.Fatalf("GetPolicy: %v", err)
	}
	if pol.MissingPerCycle != 5 || pol.HourlyCap != 25 {
		t.Errorf("unexpected policy rates: %+v", pol)
	}
	if pol.Selection != "recent" || pol.RecentGrabWindowH != 24 || pol.AfterglowMaxH != 720 {
		t.Errorf("unexpected policy refinements: %+v", pol)
	}
	// A policy from before the filters keeps every item: empty lists and no release delay.
	if len(pol.IncludeTags)+len(pol.ExcludeTags)+len(pol.IncludeQualityProfiles)+len(pol.ExcludeQualityProfiles) != 0 ||
		pol.IncludeTags == nil || pol.ReleaseDelayH != 0 {
		t.Errorf("unexpected policy filters after the upgrade: %+v", pol)
	}
}

func assertInstancesAndPolicies(t *testing.T, q *sqlcgen.Queries, ctx context.Context) {
	t.Helper()
	insts, err := q.ListInstances(ctx)
	if err != nil {
		t.Fatalf("ListInstances: %v", err)
	}
	if len(insts) != 6 {
		t.Fatalf("ListInstances count = %d, want 6", len(insts))
	}
	checkInstances(t, insts)
	checkPolicy(t, q, ctx, fixtureSonarrID)
}

func assertRuns(t *testing.T, q *sqlcgen.Queries, ctx context.Context, runID uuid.UUID) {
	t.Helper()
	run, err := q.GetRun(ctx, runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if run.Kind != "missing" || run.Status != "queued" || run.LeaseCount != 0 {
		t.Errorf("unexpected run state: %+v", run)
	}
	if run.FocusEntityID == nil || *run.FocusEntityID != 42 {
		t.Errorf("unexpected run focus_entity_id: %+v", run.FocusEntityID)
	}
}

func assertEventsAndProcessed(t *testing.T, q *sqlcgen.Queries, ctx context.Context, instID uuid.UUID) {
	t.Helper()
	events, err := q.ListEvents(ctx, sqlcgen.ListEventsParams{InstanceID: &instID, PageSize: 10})
	if err != nil || len(events) != 1 || events[0].Title != "Test Series S01E01" {
		t.Errorf("unexpected events: %+v, err %v", events, err)
	}

	attempts, err := q.ProcessedAttempts(ctx, sqlcgen.ProcessedAttemptsParams{
		InstanceID: instID, Kind: "missing", EntityType: "episode", EntityID: 1001,
	})
	if err != nil || attempts != 1 {
		t.Errorf("ProcessedAttempts = %d, err %v", attempts, err)
	}
}

func assertBuckets(t *testing.T, q *sqlcgen.Queries, ctx context.Context, instID uuid.UUID) {
	t.Helper()
	since := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	usage, err := q.BucketUsage(ctx, sqlcgen.BucketUsageParams{InstanceID: instID, Since: since})
	if err != nil || usage.Used != 5 {
		t.Errorf("BucketUsage used = %d, err %v", usage.Used, err)
	}

	globalUsage, err := q.GlobalBucketUsage(ctx, since)
	if err != nil || globalUsage.Used != 12 {
		t.Errorf("GlobalBucketUsage used = %d, err %v", globalUsage.Used, err)
	}
}

func assertSchedulesAndSettings(t *testing.T, q *sqlcgen.Queries, ctx context.Context, schedID uuid.UUID) {
	t.Helper()
	sched, err := q.GetSchedule(ctx, schedID)
	if err != nil || sched.Name != "Nightly Pause" || sched.Action != "pause" {
		t.Errorf("GetSchedule = %+v, err %v", sched, err)
	}

	settings, err := q.GetSettings(ctx)
	if err != nil || settings.HistoryRetentionDays != 90 || settings.GlobalHourlyCap != 100 {
		t.Errorf("GetSettings = %+v, err %v", settings, err)
	}
}

func assertUsersAndSessions(t *testing.T, q *sqlcgen.Queries, ctx context.Context, userID uuid.UUID) {
	t.Helper()
	user, err := q.GetUser(ctx, userID)
	if err != nil || user.Username != "admin" || user.Role != "admin" {
		t.Errorf("GetUser = %+v, err %v", user, err)
	}

	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	data, err := q.LoadSession(ctx, sqlcgen.LoadSessionParams{ID: "sess-token-12345", ExpiresAt: now})
	if err != nil || !strings.Contains(string(data), userID.String()) {
		t.Errorf("LoadSession data = %s, err %v", string(data), err)
	}
}

func assertKeysAndWorkers(t *testing.T, q *sqlcgen.Queries, ctx context.Context, userID uuid.UUID) {
	t.Helper()
	key, err := q.GetAPIKey(ctx, "snatcharr_key_abc123")
	if err != nil || key.OwnerID != userID || len(key.Scopes) != 2 {
		t.Errorf("GetAPIKey = %+v, err %v", key, err)
	}

	workerContact, err := q.LastWorkerContact(ctx)
	if err != nil || workerContact.IsZero() {
		t.Errorf("LastWorkerContact = %v, err %v", workerContact, err)
	}
}

func assertClients(t *testing.T, q *sqlcgen.Queries, ctx context.Context, clientID uuid.UUID) {
	t.Helper()
	client, err := q.GetDownloadClient(ctx, clientID)
	if err != nil || client.Name != "qbit-local" || client.Kind != "qbittorrent" {
		t.Errorf("GetDownloadClient = %+v, err %v", client, err)
	}
}

func assertSeerr(t *testing.T, q *sqlcgen.Queries, ctx context.Context, seerrID uuid.UUID) {
	t.Helper()
	seerr, err := q.GetSeerrLink(ctx, seerrID)
	if err != nil || seerr.Name != "overseerr-main" || !seerr.Enabled {
		t.Errorf("GetSeerrLink = %+v, err %v", seerr, err)
	}

	req, err := q.GetSeerrRequest(ctx, sqlcgen.GetSeerrRequestParams{LinkID: seerrID, RequestID: 101})
	if err != nil || req.Title != "Game of Thrones" || req.MediaType != "tv" {
		t.Errorf("GetSeerrRequest = %+v, err %v", req, err)
	}
}

// TestMigration000028Modules verifies that migration 000028 copies llm_enabled to the modules table,
// and that stepping down restores the value to settings.llm_enabled and drops the modules table.
func TestMigration000028Modules(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	m := storetest.Migrator(t, st)

	stepDownToVersion(t, m, 27)

	ctx := t.Context()
	_, err := st.Pool().Exec(ctx, "UPDATE settings SET llm_enabled = TRUE WHERE singleton")
	if err != nil {
		t.Fatalf("set settings.llm_enabled = true: %v", err)
	}

	if err = m.Steps(1); err != nil {
		t.Fatalf("Steps(1) up to 28: %v", err)
	}

	var enabled bool
	err = st.Pool().QueryRow(ctx, "SELECT enabled FROM modules WHERE id = 'llm'").Scan(&enabled)
	if err != nil {
		t.Fatalf("query modules llm: %v", err)
	}
	if !enabled {
		t.Fatalf("modules llm enabled = false, want true")
	}

	if err = m.Steps(-1); err != nil {
		t.Fatalf("Steps(-1) down to 27: %v", err)
	}

	var restoredLLM bool
	err = st.Pool().QueryRow(ctx, "SELECT llm_enabled FROM settings WHERE singleton").Scan(&restoredLLM)
	if err != nil {
		t.Fatalf("query settings.llm_enabled after down: %v", err)
	}
	if !restoredLLM {
		t.Fatalf("settings.llm_enabled = false after down migration, want true")
	}

	var tableExists bool
	err = st.Pool().QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name='modules')").Scan(&tableExists)
	if err != nil {
		t.Fatalf("check modules table exists: %v", err)
	}
	if tableExists {
		t.Fatalf("modules table still exists after down migration")
	}
}
