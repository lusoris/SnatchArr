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
	if err != nil || client.Name != "qbit-local" || client.Kind != "qbittorrent" || client.MaxQueued != 0 {
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

// TestMigration000030DownloadClientsMaxQueued verifies migration 000030 adds max_queued
// with default 0 and check constraint 0..1000, and stepping down drops the column.
func TestMigration000030DownloadClientsMaxQueued(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	m := storetest.Migrator(t, st)

	stepDownToVersion(t, m, 29)

	ctx := t.Context()
	clientID := uuid.New()
	const insert29 = `INSERT INTO download_clients (
		id, kind, name, base_url, secret_enc, max_active, created_at, updated_at
	) VALUES (
		$1, 'qbittorrent', 'qbit-m30', 'http://127.0.0.1:8080', '\x0102', 5, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
	)`
	if _, err := st.Pool().Exec(ctx, insert29, clientID); err != nil {
		t.Fatalf("insert download client at v29: %v", err)
	}

	if err := m.Steps(1); err != nil {
		t.Fatalf("Steps(1) up to 30: %v", err)
	}

	var maxQueued int
	err := st.Pool().QueryRow(ctx, "SELECT max_queued FROM download_clients WHERE id = $1", clientID).Scan(&maxQueued)
	if err != nil {
		t.Fatalf("query max_queued after up: %v", err)
	}
	if maxQueued != 0 {
		t.Fatalf("max_queued = %d, want 0", maxQueued)
	}

	if _, err = st.Pool().Exec(ctx, "UPDATE download_clients SET max_queued = 1001 WHERE id = $1", clientID); err == nil {
		t.Fatalf("expected error updating max_queued = 1001, got nil")
	}
	if _, err = st.Pool().Exec(ctx, "UPDATE download_clients SET max_queued = -1 WHERE id = $1", clientID); err == nil {
		t.Fatalf("expected error updating max_queued = -1, got nil")
	}

	if err = m.Steps(-1); err != nil {
		t.Fatalf("Steps(-1) down to 29: %v", err)
	}

	var colExists bool
	err = st.Pool().QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='download_clients' AND column_name='max_queued')").Scan(&colExists)
	if err != nil {
		t.Fatalf("check column exists after down: %v", err)
	}
	if colExists {
		t.Fatalf("max_queued column still exists after down migration")
	}
}

// TestMigration000033UserRoleRequester verifies migration 000033 widens the users.role
// CHECK constraint to include 'requester', and stepping down updates 'requester' rows to
// 'viewer' and restores the old check constraint.
func TestMigration000033UserRoleRequester(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	m := storetest.Migrator(t, st)

	stepDownToVersion(t, m, 32)
	ctx := t.Context()

	reqID := uuid.New()
	const insertUser = `INSERT INTO users (
		id, username, password_hash, role, created_at, updated_at
	) VALUES (
		$1, $2, 'dummy-hash', $3, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
	)`

	// At v32, inserting role 'requester' must be rejected by check constraint.
	if _, err := st.Pool().Exec(ctx, insertUser, reqID, "req-user-m33", "requester"); err == nil {
		t.Fatalf("expected error inserting role 'requester' at v32, got nil")
	}

	// Step up to 33.
	if err := m.Steps(1); err != nil {
		t.Fatalf("Steps(1) up to 33: %v", err)
	}

	// At v33, inserting role 'requester' succeeds.
	if _, err := st.Pool().Exec(ctx, insertUser, reqID, "req-user-m33", "requester"); err != nil {
		t.Fatalf("insert user with role 'requester' at v33: %v", err)
	}

	// Step down to 32: must convert requester to viewer and restore check constraint.
	if err := m.Steps(-1); err != nil {
		t.Fatalf("Steps(-1) down to 32: %v", err)
	}

	var role string
	err := st.Pool().QueryRow(ctx, "SELECT role FROM users WHERE id = $1", reqID).Scan(&role)
	if err != nil {
		t.Fatalf("query user role after down migration: %v", err)
	}
	if role != "viewer" {
		t.Fatalf("user role after down migration = %q, want 'viewer'", role)
	}

	// At v32 again, inserting role 'requester' must fail.
	otherID := uuid.New()
	if _, err := st.Pool().Exec(ctx, insertUser, otherID, "req-user-2", "requester"); err == nil {
		t.Fatalf("expected error inserting role 'requester' after step down to v32, got nil")
	}

	// Restore to v33.
	if err := m.Steps(1); err != nil {
		t.Fatalf("Steps(1) back up to 33: %v", err)
	}
}

// TestMigration000034WatchAwareTvdb verifies migration 000034 adds provider_tvdb_id
// with an empty-string default to watchaware_item_watches, and stepping down drops the column.
func TestMigration000034WatchAwareTvdb(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	m := storetest.Migrator(t, st)

	stepDownToVersion(t, m, 33)

	ctx := t.Context()
	srcID := uuid.New()
	const insertSrc = `INSERT INTO watchaware_sources (
		id, kind, name, url, api_key_enc, enabled, created_at, updated_at
	) VALUES (
		$1, 'jellyfin', 'jf-m34', 'http://127.0.0.1:8096', '\x0102', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
	)`
	if _, err := st.Pool().Exec(ctx, insertSrc, srcID); err != nil {
		t.Fatalf("insert watchaware source at v33: %v", err)
	}

	const insert33 = `INSERT INTO watchaware_item_watches (
		source_id, user_id, item_id, user_name, item_type, series_id,
		season_number, episode_number, played, synced_at
	) VALUES (
		$1, 'u1', 'it1', 'Alice', 'Episode', 's1', 1, 1, true, CURRENT_TIMESTAMP
	)`
	if _, err := st.Pool().Exec(ctx, insert33, srcID); err != nil {
		t.Fatalf("insert watch item at v33: %v", err)
	}

	if err := m.Steps(1); err != nil {
		t.Fatalf("Steps(1) up to 34: %v", err)
	}

	var tvdbID string
	err := st.Pool().QueryRow(ctx, "SELECT provider_tvdb_id FROM watchaware_item_watches WHERE source_id = $1 AND user_id = 'u1' AND item_id = 'it1'", srcID).Scan(&tvdbID)
	if err != nil {
		t.Fatalf("query provider_tvdb_id after up: %v", err)
	}
	if tvdbID != "" {
		t.Fatalf("provider_tvdb_id = %q, want empty string default", tvdbID)
	}

	if err = m.Steps(-1); err != nil {
		t.Fatalf("Steps(-1) down to 33: %v", err)
	}

	var colExists bool
	err = st.Pool().QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='watchaware_item_watches' AND column_name='provider_tvdb_id')").Scan(&colExists)
	if err != nil {
		t.Fatalf("check column exists after down: %v", err)
	}
	if colExists {
		t.Fatalf("provider_tvdb_id column still exists after down migration")
	}
}

type adultModuleIDs struct {
	instanceID  uuid.UUID
	performerID uuid.UUID
	setID       uuid.UUID
	itemID      uuid.UUID
}

var adultModuleTables = []string{
	"adult_stashbox_instances",
	"adult_favorite_performers",
	"adult_performer_links",
	"adult_favorite_studios",
	"adult_studio_links",
	"adult_image_sets",
	"adult_image_items",
}

func assertTableExists(t *testing.T, st *store.Store, table string, want bool) {
	t.Helper()
	var exists bool
	const q = `SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema='public' AND table_name=$1)`
	if err := st.Pool().QueryRow(t.Context(), q, table).Scan(&exists); err != nil {
		t.Fatalf("check table %s exists: %v", table, err)
	}
	if exists != want {
		t.Fatalf("table %s exists = %v, want %v", table, exists, want)
	}
}

func assertAdultTables(t *testing.T, st *store.Store, want bool) {
	t.Helper()
	for _, table := range adultModuleTables {
		assertTableExists(t, st, table, want)
	}
}

func testAdultMigrationPositive(t *testing.T, st *store.Store, ids adultModuleIDs) {
	t.Helper()
	ctx := t.Context()
	const insInst = `INSERT INTO adult_stashbox_instances (
		id, name, endpoint, api_key_enc, priority, created_at, updated_at
	) VALUES ($1, 'stashbox-m36', 'https://stashdb.org/graphql', '\x0102', 10, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`
	if _, err := st.Pool().Exec(ctx, insInst, ids.instanceID); err != nil {
		t.Fatalf("insert adult_stashbox_instances: %v", err)
	}

	const insPerf = `INSERT INTO adult_favorite_performers (
		id, canonical_name, disambiguation, created_at, updated_at
	) VALUES ($1, 'Jane Doe', 'Star', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`
	if _, err := st.Pool().Exec(ctx, insPerf, ids.performerID); err != nil {
		t.Fatalf("insert adult_favorite_performers: %v", err)
	}

	const insLink = `INSERT INTO adult_performer_links (
		favorite_id, instance_id, foreign_id, is_favorite, last_synced_at
	) VALUES ($1, $2, 'foreign-p1', true, CURRENT_TIMESTAMP)`
	if _, err := st.Pool().Exec(ctx, insLink, ids.performerID, ids.instanceID); err != nil {
		t.Fatalf("insert adult_performer_links: %v", err)
	}

	const insSet = `INSERT INTO adult_image_sets (
		id, title, directory_path, whisparr_scene_foreign_id, performer_id,
		item_count, total_size_bytes, cover_image_path, created_at, updated_at
	) VALUES ($1, 'Gallery 1', '/media/imagesets/set1', 'scene-1', $2, 1, 1024, '/media/imagesets/set1/cover.jpg', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`
	if _, err := st.Pool().Exec(ctx, insSet, ids.setID, ids.performerID); err != nil {
		t.Fatalf("insert adult_image_sets: %v", err)
	}

	const insItem = `INSERT INTO adult_image_items (
		id, image_set_id, file_path, file_name, file_size_bytes, mime_type,
		width, height, sha256_checksum, sort_order, created_at
	) VALUES ($1, $2, '/media/imagesets/set1/img1.jpg', 'img1.jpg', 1024, 'image/jpeg', 1920, 1080, '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef', 0, CURRENT_TIMESTAMP)`
	if _, err := st.Pool().Exec(ctx, insItem, ids.itemID, ids.setID); err != nil {
		t.Fatalf("insert adult_image_items: %v", err)
	}

	var instName, foreignID, title, fileName string
	if err := st.Pool().QueryRow(ctx, "SELECT name FROM adult_stashbox_instances WHERE id = $1", ids.instanceID).Scan(&instName); err != nil || instName != "stashbox-m36" {
		t.Fatalf("query instance: %v (got %q)", err, instName)
	}
	if err := st.Pool().QueryRow(ctx, "SELECT foreign_id FROM adult_performer_links WHERE favorite_id = $1 AND instance_id = $2", ids.performerID, ids.instanceID).Scan(&foreignID); err != nil || foreignID != "foreign-p1" {
		t.Fatalf("query performer link: %v (got %q)", err, foreignID)
	}
	if err := st.Pool().QueryRow(ctx, "SELECT title FROM adult_image_sets WHERE id = $1", ids.setID).Scan(&title); err != nil || title != "Gallery 1" {
		t.Fatalf("query image set: %v (got %q)", err, title)
	}
	if err := st.Pool().QueryRow(ctx, "SELECT file_name FROM adult_image_items WHERE id = $1", ids.itemID).Scan(&fileName); err != nil || fileName != "img1.jpg" {
		t.Fatalf("query image item: %v (got %q)", err, fileName)
	}
}

func testAdultMigrationNegative(t *testing.T, st *store.Store, ids adultModuleIDs) {
	t.Helper()
	ctx := t.Context()

	unknownInstID := uuid.New()
	const badLink = `INSERT INTO adult_performer_links (
		favorite_id, instance_id, foreign_id, is_favorite, last_synced_at
	) VALUES ($1, $2, 'foreign-unknown', true, CURRENT_TIMESTAMP)`
	if _, err := st.Pool().Exec(ctx, badLink, ids.performerID, unknownInstID); err == nil {
		t.Fatalf("expected error inserting link with unknown instance_id, got nil")
	}

	badItem := uuid.New()
	const badCheck = `INSERT INTO adult_image_items (
		id, image_set_id, file_path, file_name, file_size_bytes, mime_type,
		width, height, sha256_checksum, sort_order, created_at
	) VALUES ($1, $2, '/media/imagesets/set1/bad.jpg', 'bad.jpg', 1024, 'image/jpeg', 1920, 1080, 'xyz', 0, CURRENT_TIMESTAMP)`
	if _, err := st.Pool().Exec(ctx, badCheck, badItem, ids.setID); err == nil {
		t.Fatalf("expected error inserting item with checksum 'xyz', got nil")
	}

	badSet := uuid.New()
	const badCount = `INSERT INTO adult_image_sets (
		id, title, directory_path, item_count, total_size_bytes, created_at, updated_at
	) VALUES ($1, 'Bad Set', '/media/imagesets/bad', -1, 0, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`
	if _, err := st.Pool().Exec(ctx, badCount, badSet); err == nil {
		t.Fatalf("expected error inserting image set with item_count -1, got nil")
	}
}

func testAdultMigrationBoundary(t *testing.T, st *store.Store, ids adultModuleIDs) {
	t.Helper()
	ctx := t.Context()

	if _, err := st.Pool().Exec(ctx, "DELETE FROM adult_stashbox_instances WHERE id = $1", ids.instanceID); err != nil {
		t.Fatalf("delete instance: %v", err)
	}
	var linkCount int
	err := st.Pool().QueryRow(ctx, "SELECT count(*) FROM adult_performer_links WHERE instance_id = $1", ids.instanceID).Scan(&linkCount)
	if err != nil {
		t.Fatalf("count performer links: %v", err)
	}
	if linkCount != 0 {
		t.Fatalf("linkCount = %d after deleting instance, want 0", linkCount)
	}

	if _, err = st.Pool().Exec(ctx, "DELETE FROM adult_favorite_performers WHERE id = $1", ids.performerID); err != nil {
		t.Fatalf("delete performer: %v", err)
	}
	var perfID *uuid.UUID
	err = st.Pool().QueryRow(ctx, "SELECT performer_id FROM adult_image_sets WHERE id = $1", ids.setID).Scan(&perfID)
	if err != nil {
		t.Fatalf("query image_set performer_id: %v", err)
	}
	if perfID != nil {
		t.Fatalf("image_set performer_id = %v after deleting performer, want NULL", perfID)
	}
}

// TestMigration000036AdultModule verifies migration 000036 creates the 7 adult module tables,
// enforces constraints, handles cascading deletes, and cleanly steps down and up.
func TestMigration000036AdultModule(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	m := storetest.Migrator(t, st)

	stepDownToVersion(t, m, 35)
	assertTableExists(t, st, "adult_stashbox_instances", false)

	if err := m.Steps(1); err != nil {
		t.Fatalf("Steps(1) up to 36: %v", err)
	}

	ids := adultModuleIDs{
		instanceID:  uuid.New(),
		performerID: uuid.New(),
		setID:       uuid.New(),
		itemID:      uuid.New(),
	}

	testAdultMigrationPositive(t, st, ids)
	testAdultMigrationNegative(t, st, ids)
	testAdultMigrationBoundary(t, st, ids)

	if err := m.Steps(-1); err != nil {
		t.Fatalf("Steps(-1) down to 35: %v", err)
	}
	assertAdultTables(t, st, false)

	if err := m.Steps(1); err != nil {
		t.Fatalf("Steps(1) back up to 36: %v", err)
	}
	assertAdultTables(t, st, true)
}

// TestMigration000037UserIdentities verifies migration 000037 creates user_identities,
// enforces provider and subject check constraints, foreign keys and cascades, and steps down/up.
func TestMigration000037UserIdentities(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	m := storetest.Migrator(t, st)

	stepDownToVersion(t, m, 36)
	assertTableExists(t, st, "user_identities", false)

	if err := m.Steps(1); err != nil {
		t.Fatalf("Steps(1) up to 37: %v", err)
	}
	assertTableExists(t, st, "user_identities", true)

	userID := uuid.New()
	testUserIdentitiesPositive(t, st, userID)
	testUserIdentitiesNegative(t, st, userID)
	testUserIdentitiesBoundary(t, st, userID)

	if err := m.Steps(-1); err != nil {
		t.Fatalf("Steps(-1) down to 36: %v", err)
	}
	assertTableExists(t, st, "user_identities", false)

	if err := m.Steps(1); err != nil {
		t.Fatalf("Steps(1) back up to 37: %v", err)
	}
	assertTableExists(t, st, "user_identities", true)
}

func testUserIdentitiesPositive(t *testing.T, st *store.Store, userID uuid.UUID) {
	t.Helper()
	now := time.Date(2026, 10, 26, 6, 0, 0, 0, time.UTC)
	const insUser = `INSERT INTO users (id, username, password_hash, role, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $5)`
	if _, err := st.Pool().Exec(t.Context(), insUser, userID, "ident-user", "unused", "requester", now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	const insIdent = `INSERT INTO user_identities (provider, subject, user_id, created_at) VALUES ($1, $2, $3, $4)`
	if _, err := st.Pool().Exec(t.Context(), insIdent, "jellyfin", "jf-sub-1", userID, now); err != nil {
		t.Fatalf("insert jellyfin identity: %v", err)
	}
	if _, err := st.Pool().Exec(t.Context(), insIdent, "proxy", "proxy-sub-1", userID, now); err != nil {
		t.Fatalf("insert proxy identity: %v", err)
	}
}

func testUserIdentitiesNegative(t *testing.T, st *store.Store, userID uuid.UUID) {
	t.Helper()
	now := time.Date(2026, 10, 26, 6, 0, 0, 0, time.UTC)
	const insIdent = `INSERT INTO user_identities (provider, subject, user_id, created_at) VALUES ($1, $2, $3, $4)`

	// Invalid provider
	if _, err := st.Pool().Exec(t.Context(), insIdent, "oidc", "sub-invalid", userID, now); err == nil {
		t.Fatal("insert oidc provider succeeded, want check violation")
	}

	// Empty subject
	if _, err := st.Pool().Exec(t.Context(), insIdent, "jellyfin", "", userID, now); err == nil {
		t.Fatal("insert empty subject succeeded, want check violation")
	}

	// Duplicate (provider, subject)
	if _, err := st.Pool().Exec(t.Context(), insIdent, "jellyfin", "jf-sub-1", userID, now); err == nil {
		t.Fatal("duplicate (provider, subject) succeeded, want primary key conflict")
	}

	// Duplicate (user_id, provider) with different subject -> unique index violation
	if _, err := st.Pool().Exec(t.Context(), insIdent, "jellyfin", "jf-sub-2", userID, now); err == nil {
		t.Fatal("second jellyfin identity for same user succeeded, want unique index violation")
	}

	// Foreign key violation
	if _, err := st.Pool().Exec(t.Context(), insIdent, "proxy", "sub-fk", uuid.New(), now); err == nil {
		t.Fatal("insert invalid user_id fk succeeded, want fk violation")
	}
}

func testUserIdentitiesBoundary(t *testing.T, st *store.Store, userID uuid.UUID) {
	t.Helper()
	// Deleting user should cascade delete user_identities
	if _, err := st.Pool().Exec(t.Context(), `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	var count int
	if err := st.Pool().QueryRow(t.Context(), `SELECT count(*) FROM user_identities WHERE user_id = $1`, userID).Scan(&count); err != nil {
		t.Fatalf("count user identities: %v", err)
	}
	if count != 0 {
		t.Fatalf("user_identities count after user cascade delete = %d, want 0", count)
	}
}

func TestMigration000041ItemSearchCooldown(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	m := storetest.Migrator(t, st)

	stepDownToVersion(t, m, 40)
	assertTableExists(t, st, "item_search_cooldowns", false)

	if err := m.Steps(1); err != nil {
		t.Fatalf("Steps(1) up to 41: %v", err)
	}
	assertTableExists(t, st, "item_search_cooldowns", true)

	userID := uuid.New()
	testItemSearchCooldownPositive(t, st, userID)
	testItemSearchCooldownNegative(t, st, userID)
	testItemSearchCooldownBoundary(t, st, userID)

	if err := m.Steps(-1); err != nil {
		t.Fatalf("Steps(-1) down to 40: %v", err)
	}
	assertTableExists(t, st, "item_search_cooldowns", false)

	if err := m.Steps(1); err != nil {
		t.Fatalf("Steps(1) back up to 41: %v", err)
	}
	assertTableExists(t, st, "item_search_cooldowns", true)
}

func testItemSearchCooldownPositive(t *testing.T, st *store.Store, userID uuid.UUID) {
	t.Helper()
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	const insUser = `INSERT INTO users (id, username, password_hash, role, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $5)`
	if _, err := st.Pool().Exec(t.Context(), insUser, userID, "cd-user", "unused", "requester", now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	const insCooldown = `INSERT INTO item_search_cooldowns (user_id, last_searched_at) VALUES ($1, $2)`
	if _, err := st.Pool().Exec(t.Context(), insCooldown, userID, now); err != nil {
		t.Fatalf("insert item_search_cooldown: %v", err)
	}
}

func testItemSearchCooldownNegative(t *testing.T, st *store.Store, userID uuid.UUID) {
	t.Helper()
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	const insCooldown = `INSERT INTO item_search_cooldowns (user_id, last_searched_at) VALUES ($1, $2)`

	// Foreign key violation
	if _, err := st.Pool().Exec(t.Context(), insCooldown, uuid.New(), now); err == nil {
		t.Fatal("insert item_search_cooldown with unknown user_id succeeded, want fk violation")
	}

	// Duplicate primary key
	if _, err := st.Pool().Exec(t.Context(), insCooldown, userID, now); err == nil {
		t.Fatal("duplicate item_search_cooldown for user succeeded, want primary key conflict")
	}
}

func testItemSearchCooldownBoundary(t *testing.T, st *store.Store, userID uuid.UUID) {
	t.Helper()
	// Deleting user must cascade delete item_search_cooldowns
	if _, err := st.Pool().Exec(t.Context(), `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	var count int
	if err := st.Pool().QueryRow(t.Context(), `SELECT count(*) FROM item_search_cooldowns WHERE user_id = $1`, userID).Scan(&count); err != nil {
		t.Fatalf("count item_search_cooldowns: %v", err)
	}
	if count != 0 {
		t.Fatalf("item_search_cooldowns count after user cascade delete = %d, want 0", count)
	}
}

func TestMigration000043ProwlarrLinkCap(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	m := storetest.Migrator(t, st)

	stepDownToVersion(t, m, 42)
	assertTableExists(t, st, "prowlarr_rate_buckets", false)

	ctx := t.Context()
	linkID := uuid.New()
	const insertV42 = `INSERT INTO prowlarr_links (
		id, name, base_url, api_key_enc, enabled, created_at, updated_at
	) VALUES (
		$1, 'prowlarr-m43', 'http://127.0.0.1:9696', '\x0102', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
	)`
	if _, err := st.Pool().Exec(ctx, insertV42, linkID); err != nil {
		t.Fatalf("insert prowlarr link at v42: %v", err)
	}

	if err := m.Steps(1); err != nil {
		t.Fatalf("Steps(1) up to 43: %v", err)
	}
	assertTableExists(t, st, "prowlarr_rate_buckets", true)

	// Check default 15 on existing row
	var capVal int
	err := st.Pool().QueryRow(ctx, "SELECT hourly_cap FROM prowlarr_links WHERE id = $1", linkID).Scan(&capVal)
	if err != nil {
		t.Fatalf("query hourly_cap after up: %v", err)
	}
	if capVal != 15 {
		t.Fatalf("hourly_cap default = %d, want 15", capVal)
	}

	// CHECK constraint rejects 0 and 501
	if _, err = st.Pool().Exec(ctx, "UPDATE prowlarr_links SET hourly_cap = 0 WHERE id = $1", linkID); err == nil {
		t.Fatal("expected error updating hourly_cap = 0, got nil")
	}
	if _, err = st.Pool().Exec(ctx, "UPDATE prowlarr_links SET hourly_cap = 501 WHERE id = $1", linkID); err == nil {
		t.Fatal("expected error updating hourly_cap = 501, got nil")
	}

	// Boundary values 1 and 500 accepted
	if _, err = st.Pool().Exec(ctx, "UPDATE prowlarr_links SET hourly_cap = 1 WHERE id = $1", linkID); err != nil {
		t.Fatalf("update hourly_cap = 1: %v", err)
	}
	if _, err = st.Pool().Exec(ctx, "UPDATE prowlarr_links SET hourly_cap = 500 WHERE id = $1", linkID); err != nil {
		t.Fatalf("update hourly_cap = 500: %v", err)
	}

	// prowlarr_rate_buckets: FK constraint and cascade delete
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	const insBucket = `INSERT INTO prowlarr_rate_buckets (link_id, window_start, used) VALUES ($1, $2, $3)`
	if _, err = st.Pool().Exec(ctx, insBucket, uuid.New(), now, 1); err == nil {
		t.Fatal("insert bucket with unknown link_id succeeded, want fk violation")
	}
	if _, err = st.Pool().Exec(ctx, insBucket, linkID, now, 1); err != nil {
		t.Fatalf("insert bucket: %v", err)
	}

	// Down migration
	if err = m.Steps(-1); err != nil {
		t.Fatalf("Steps(-1) down to 42: %v", err)
	}
	assertTableExists(t, st, "prowlarr_rate_buckets", false)

	var colExists bool
	err = st.Pool().QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='prowlarr_links' AND column_name='hourly_cap')").Scan(&colExists)
	if err != nil {
		t.Fatalf("check column exists after down: %v", err)
	}
	if colExists {
		t.Fatalf("hourly_cap column still exists after down migration")
	}

	// Back up to 43
	if err = m.Steps(1); err != nil {
		t.Fatalf("Steps(1) back up to 43: %v", err)
	}
	assertTableExists(t, st, "prowlarr_rate_buckets", true)
}

func TestMigration000044BrandMark(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	m := storetest.Migrator(t, st)

	stepDownToVersion(t, m, 43)
	assertTableExists(t, st, "users_prefs", false)

	if err := m.Steps(1); err != nil {
		t.Fatalf("Steps(1) up to 44: %v", err)
	}
	assertTableExists(t, st, "users_prefs", true)

	testBrandMarkSettingsConstraints(t, st)
	userID := uuid.New()
	testUsersPrefsConstraints(t, st, userID)

	if err := m.Steps(-1); err != nil {
		t.Fatalf("Steps(-1) down to 43: %v", err)
	}
	assertTableExists(t, st, "users_prefs", false)

	var colExists bool
	err := st.Pool().QueryRow(t.Context(), "SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='settings' AND column_name='brand_mark')").Scan(&colExists)
	if err != nil {
		t.Fatalf("check column exists after down: %v", err)
	}
	if colExists {
		t.Fatalf("brand_mark column still exists after down migration")
	}

	if err := m.Steps(1); err != nil {
		t.Fatalf("Steps(1) back up to 44: %v", err)
	}
	assertTableExists(t, st, "users_prefs", true)
}

func testBrandMarkSettingsConstraints(t *testing.T, st *store.Store) {
	t.Helper()
	ctx := t.Context()
	var bm, sm string
	if err := st.Pool().QueryRow(ctx, "SELECT brand_mark, sfw_mode FROM settings WHERE singleton").Scan(&bm, &sm); err != nil {
		t.Fatalf("query settings brand_mark/sfw_mode: %v", err)
	}
	if bm != "vulva" || sm != "auto" {
		t.Fatalf("settings defaults = (%q, %q), want (vulva, auto)", bm, sm)
	}
	if _, err := st.Pool().Exec(ctx, "UPDATE settings SET brand_mark = 'invalid' WHERE singleton"); err == nil {
		t.Fatal("expected error updating settings.brand_mark = 'invalid', got nil")
	}
	if _, err := st.Pool().Exec(ctx, "UPDATE settings SET sfw_mode = 'invalid' WHERE singleton"); err == nil {
		t.Fatal("expected error updating settings.sfw_mode = 'invalid', got nil")
	}
	for _, validMark := range []string{"vulva", "penis", "hand"} {
		if _, err := st.Pool().Exec(ctx, "UPDATE settings SET brand_mark = $1 WHERE singleton", validMark); err != nil {
			t.Fatalf("update settings.brand_mark = %q: %v", validMark, err)
		}
	}
	for _, validMode := range []string{"auto", "on", "off"} {
		if _, err := st.Pool().Exec(ctx, "UPDATE settings SET sfw_mode = $1 WHERE singleton", validMode); err != nil {
			t.Fatalf("update settings.sfw_mode = %q: %v", validMode, err)
		}
	}
}

func testUsersPrefsConstraints(t *testing.T, st *store.Store, userID uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	const insUser = `INSERT INTO users (id, username, password_hash, role, created_at, updated_at) VALUES ($1, 'pref-m44', 'unused', 'requester', $2, $2)`
	if _, err := st.Pool().Exec(ctx, insUser, userID, now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	const insPref = `INSERT INTO users_prefs (user_id, brand_mark, sfw_mode, updated_at) VALUES ($1, $2, $3, $4)`
	if _, err := st.Pool().Exec(ctx, insPref, uuid.New(), nil, nil, now); err == nil {
		t.Fatal("insert users_prefs with unknown user_id succeeded, want fk violation")
	}
	if _, err := st.Pool().Exec(ctx, insPref, userID, "invalid", nil, now); err == nil {
		t.Fatal("insert users_prefs with invalid brand_mark succeeded, want check violation")
	}
	if _, err := st.Pool().Exec(ctx, insPref, userID, nil, "invalid", now); err == nil {
		t.Fatal("insert users_prefs with invalid sfw_mode succeeded, want check violation")
	}
	if _, err := st.Pool().Exec(ctx, insPref, userID, "hand", "on", now); err != nil {
		t.Fatalf("insert valid users_prefs: %v", err)
	}
	if _, err := st.Pool().Exec(ctx, "DELETE FROM users WHERE id = $1", userID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	var count int
	if err := st.Pool().QueryRow(ctx, "SELECT count(*) FROM users_prefs WHERE user_id = $1", userID).Scan(&count); err != nil {
		t.Fatalf("count users_prefs: %v", err)
	}
	if count != 0 {
		t.Fatalf("users_prefs count after user cascade delete = %d, want 0", count)
	}
}

func TestMigration000045InstanceKindStash(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	m := storetest.Migrator(t, st)
	ctx := t.Context()
	now := time.Date(2026, 10, 27, 1, 0, 0, 0, time.UTC)

	stepDownToVersion(t, m, 44)
	const insInst = `INSERT INTO instances (id, kind, name, base_url, api_key_enc, enabled, source, created_at, updated_at) VALUES ($1, $2, $3, $4, '\x0102', true, 'manual', $5, $5)`
	if _, err := st.Pool().Exec(ctx, insInst, uuid.New(), "stash", "stash-v44", "http://stash:9999", now); err == nil {
		t.Fatal("insert kind=stash at v44 succeeded, want check violation")
	}

	if err := m.Steps(1); err != nil {
		t.Fatalf("Steps(1) up to 45: %v", err)
	}

	stashID := uuid.New()
	sonarrID := uuid.New()
	if _, err := st.Pool().Exec(ctx, insInst, stashID, "stash", "stash-v45", "http://stash:9999", now); err != nil {
		t.Fatalf("insert kind=stash at v45: %v", err)
	}
	if _, err := st.Pool().Exec(ctx, insInst, sonarrID, "sonarr", "sonarr-v45", "http://sonarr:8989", now); err != nil {
		t.Fatalf("insert kind=sonarr at v45: %v", err)
	}
	if _, err := st.Pool().Exec(ctx, insInst, uuid.New(), "plex", "plex-v45", "http://plex:32400", now); err == nil {
		t.Fatal("insert kind=plex at v45 succeeded, want check violation")
	}

	assertStashSkippedByArrQueries(t, st, sonarrID, stashID)

	if _, err := st.Pool().Exec(ctx, "DELETE FROM instances WHERE id = $1", stashID); err != nil {
		t.Fatalf("delete stash instance before down migration: %v", err)
	}
	if err := m.Steps(-1); err != nil {
		t.Fatalf("Steps(-1) down to 44: %v", err)
	}
	if _, err := st.Pool().Exec(ctx, insInst, uuid.New(), "stash", "stash-after-down", "http://stash:9999", now); err == nil {
		t.Fatal("insert kind=stash after down migration succeeded, want check violation")
	}
	if err := m.Steps(1); err != nil {
		t.Fatalf("Steps(1) back up to 45: %v", err)
	}
}

func assertStashSkippedByArrQueries(t *testing.T, st *store.Store, sonarrID, stashID uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	enabled, err := st.Q().ListEnabledInstances(ctx)
	if err != nil {
		t.Fatalf("ListEnabledInstances: %v", err)
	}
	if len(enabled) != 1 || enabled[0].ID != sonarrID {
		t.Fatalf("ListEnabledInstances returned %d rows (first=%v), want only sonarr %v (stash %v skipped)", len(enabled), enabled, sonarrID, stashID)
	}
	toPlan, err := st.Q().ListInstancesToPlan(ctx)
	if err != nil {
		t.Fatalf("ListInstancesToPlan: %v", err)
	}
	if len(toPlan) != 1 || toPlan[0].ID != sonarrID {
		t.Fatalf("ListInstancesToPlan returned %d rows (first=%v), want only sonarr %v (stash %v skipped)", len(toPlan), toPlan, sonarrID, stashID)
	}
}

type adultNormalizedSeed struct {
	instID      uuid.UUID
	perfFavID   uuid.UUID
	perfUnfavID uuid.UUID
	studioFavID uuid.UUID
	setID       uuid.UUID
	syncedAt    time.Time
}

func seedPre000046AdultData(t *testing.T, st *store.Store) adultNormalizedSeed {
	t.Helper()
	ctx := t.Context()
	seed := adultNormalizedSeed{
		instID:      uuid.New(),
		perfFavID:   uuid.New(),
		perfUnfavID: uuid.New(),
		studioFavID: uuid.New(),
		setID:       uuid.New(),
		syncedAt:    time.Date(2026, 10, 26, 14, 0, 0, 0, time.UTC),
	}
	const insInst = `INSERT INTO adult_stashbox_instances (
		id, name, endpoint, api_key_enc, priority, created_at, updated_at
	) VALUES ($1, 'stashbox-m46', 'https://stashbox.example.org/graphql', '\x0102', 10, $2, $2)`
	if _, err := st.Pool().Exec(ctx, insInst, seed.instID, seed.syncedAt); err != nil {
		t.Fatalf("insert instance: %v", err)
	}
	const insPerf = `INSERT INTO adult_favorite_performers (
		id, canonical_name, disambiguation, urls, created_at, updated_at
	) VALUES ($1, $2, $3, $4, $5, $5)`
	if _, err := st.Pool().Exec(ctx, insPerf, seed.perfFavID, "Performer A", "Alpha", []string{"https://example.org/performer-a"}, seed.syncedAt); err != nil {
		t.Fatalf("insert performer A: %v", err)
	}
	if _, err := st.Pool().Exec(ctx, insPerf, seed.perfUnfavID, "Performer B", nil, []string{}, seed.syncedAt); err != nil {
		t.Fatalf("insert performer B: %v", err)
	}
	const insPerfLink = `INSERT INTO adult_performer_links (
		favorite_id, instance_id, foreign_id, is_favorite, last_synced_at
	) VALUES ($1, $2, $3, $4, $5)`
	if _, err := st.Pool().Exec(ctx, insPerfLink, seed.perfFavID, seed.instID, "sb-perf-a", true, seed.syncedAt); err != nil {
		t.Fatalf("insert performer A link: %v", err)
	}
	if _, err := st.Pool().Exec(ctx, insPerfLink, seed.perfUnfavID, seed.instID, "sb-perf-b", false, seed.syncedAt); err != nil {
		t.Fatalf("insert performer B link: %v", err)
	}
	const insStudio = `INSERT INTO adult_favorite_studios (
		id, canonical_name, urls, created_at, updated_at
	) VALUES ($1, 'Studio B', $2, $3, $3)`
	if _, err := st.Pool().Exec(ctx, insStudio, seed.studioFavID, []string{"https://example.org/studio-b"}, seed.syncedAt); err != nil {
		t.Fatalf("insert studio B: %v", err)
	}
	const insStudioLink = `INSERT INTO adult_studio_links (
		favorite_id, instance_id, foreign_id, is_favorite, last_synced_at
	) VALUES ($1, $2, 'sb-studio-b', true, $3)`
	if _, err := st.Pool().Exec(ctx, insStudioLink, seed.studioFavID, seed.instID, seed.syncedAt); err != nil {
		t.Fatalf("insert studio B link: %v", err)
	}
	const insSet = `INSERT INTO adult_image_sets (
		id, title, directory_path, performer_id, item_count, total_size_bytes, created_at, updated_at
	) VALUES ($1, 'Gallery M46', '/media/imagesets/m46', $2, 0, 0, $3, $3)`
	if _, err := st.Pool().Exec(ctx, insSet, seed.setID, seed.perfFavID, seed.syncedAt); err != nil {
		t.Fatalf("insert image set: %v", err)
	}
	return seed
}

func assertPost000046Up(t *testing.T, st *store.Store, seed adultNormalizedSeed) {
	t.Helper()
	ctx := t.Context()
	for _, oldTbl := range []string{"adult_favorite_performers", "adult_performer_links", "adult_favorite_studios", "adult_studio_links"} {
		assertTableExists(t, st, oldTbl, false)
	}
	for _, newTbl := range []string{"adult_performers", "adult_studios", "adult_external_refs"} {
		assertTableExists(t, st, newTbl, true)
	}

	var favA, favB, favStudio bool
	if err := st.Pool().QueryRow(ctx, "SELECT is_favorite FROM adult_performers WHERE id = $1", seed.perfFavID).Scan(&favA); err != nil || !favA {
		t.Fatalf("performer A is_favorite = %v, want true (err: %v)", favA, err)
	}
	if err := st.Pool().QueryRow(ctx, "SELECT is_favorite FROM adult_performers WHERE id = $1", seed.perfUnfavID).Scan(&favB); err != nil || favB {
		t.Fatalf("performer B is_favorite = %v, want false (err: %v)", favB, err)
	}
	if err := st.Pool().QueryRow(ctx, "SELECT is_favorite FROM adult_studios WHERE id = $1", seed.studioFavID).Scan(&favStudio); err != nil || !favStudio {
		t.Fatalf("studio B is_favorite = %v, want true (err: %v)", favStudio, err)
	}

	var refFav bool
	var matchMethod string
	var fetchedAt, lastSeenAt time.Time
	const qRef = `SELECT is_favorite, match_method, fetched_at, last_seen_at FROM adult_external_refs
		WHERE entity_type = $1 AND entity_id = $2 AND source_kind = 'stashbox' AND source_instance_id = $3 AND foreign_id = $4`
	if err := st.Pool().QueryRow(ctx, qRef, "performer", seed.perfFavID, seed.instID, "sb-perf-a").Scan(&refFav, &matchMethod, &fetchedAt, &lastSeenAt); err != nil {
		t.Fatalf("query migrated performer A ref: %v", err)
	}
	if !refFav || matchMethod != "migrated" || !fetchedAt.Equal(seed.syncedAt) || !lastSeenAt.Equal(seed.syncedAt) {
		t.Fatalf("migrated performer A ref = (fav=%v, method=%q, fetched=%v, seen=%v)", refFav, matchMethod, fetchedAt, lastSeenAt)
	}
	if err := st.Pool().QueryRow(ctx, qRef, "studio", seed.studioFavID, seed.instID, "sb-studio-b").Scan(&refFav, &matchMethod, &fetchedAt, &lastSeenAt); err != nil || !refFav || matchMethod != "migrated" {
		t.Fatalf("query migrated studio B ref: %v (fav=%v, method=%q)", err, refFav, matchMethod)
	}

	var setPerfID uuid.UUID
	if err := st.Pool().QueryRow(ctx, "SELECT performer_id FROM adult_image_sets WHERE id = $1", seed.setID).Scan(&setPerfID); err != nil || setPerfID != seed.perfFavID {
		t.Fatalf("image set performer_id = %v, want %v (err: %v)", setPerfID, seed.perfFavID, err)
	}
}

func testMigration000046NegativeRefs(t *testing.T, st *store.Store, seed adultNormalizedSeed) {
	t.Helper()
	ctx := t.Context()

	const insNullRef = `INSERT INTO adult_external_refs (
		id, entity_type, entity_id, source_kind, source_instance_id, foreign_id, is_favorite, fetched_at, last_seen_at
	) VALUES ($1, 'performer', $2, 'tpdb_rest', NULL, 'tpdb-p-1', false, $3, $3)`
	if _, err := st.Pool().Exec(ctx, insNullRef, uuid.New(), seed.perfFavID, seed.syncedAt); err != nil {
		t.Fatalf("insert valid tpdb_rest ref: %v", err)
	}

	negCases := []struct {
		name  string
		query string
		args  []any
	}{
		{
			name:  "rejects bad entity_type",
			query: `INSERT INTO adult_external_refs (id, entity_type, entity_id, source_kind, foreign_id, is_favorite, fetched_at, last_seen_at) VALUES ($1, 'invalid_type', $2, 'tpdb_rest', 'f-1', false, $3, $3)`,
			args:  []any{uuid.New(), seed.perfFavID, seed.syncedAt},
		},
		{
			name:  "rejects bad source_kind",
			query: `INSERT INTO adult_external_refs (id, entity_type, entity_id, source_kind, foreign_id, is_favorite, fetched_at, last_seen_at) VALUES ($1, 'performer', $2, 'invalid_source', 'f-2', false, $3, $3)`,
			args:  []any{uuid.New(), seed.perfFavID, seed.syncedAt},
		},
		{
			name:  "rejects confidence_score below 0",
			query: `INSERT INTO adult_external_refs (id, entity_type, entity_id, source_kind, foreign_id, confidence_score, is_favorite, fetched_at, last_seen_at) VALUES ($1, 'performer', $2, 'tpdb_rest', 'f-3', -1, false, $3, $3)`,
			args:  []any{uuid.New(), seed.perfFavID, seed.syncedAt},
		},
		{
			name:  "rejects confidence_score above 100",
			query: `INSERT INTO adult_external_refs (id, entity_type, entity_id, source_kind, foreign_id, confidence_score, is_favorite, fetched_at, last_seen_at) VALUES ($1, 'performer', $2, 'tpdb_rest', 'f-4', 101, false, $3, $3)`,
			args:  []any{uuid.New(), seed.perfFavID, seed.syncedAt},
		},
		{
			name:  "rejects duplicate stashbox ref",
			query: `INSERT INTO adult_external_refs (id, entity_type, entity_id, source_kind, source_instance_id, foreign_id, is_favorite, fetched_at, last_seen_at) VALUES ($1, 'performer', $2, 'stashbox', $3, 'sb-perf-a', true, $4, $4)`,
			args:  []any{uuid.New(), seed.perfFavID, seed.instID, seed.syncedAt},
		},
		{
			name:  "rejects duplicate NULL source_instance_id ref via NULLS NOT DISTINCT",
			query: insNullRef,
			args:  []any{uuid.New(), seed.perfFavID, seed.syncedAt},
		},
	}
	for _, tc := range negCases {
		if _, err := st.Pool().Exec(ctx, tc.query, tc.args...); err == nil {
			t.Fatalf("%s: expected error, got nil", tc.name)
		}
	}
}

func testMigration000046NegativeSelfFKs(t *testing.T, st *store.Store, seed adultNormalizedSeed) {
	t.Helper()
	ctx := t.Context()
	selfCases := []struct {
		name  string
		query string
		id    uuid.UUID
	}{
		{"performers self merged_into_id", `UPDATE adult_performers SET merged_into_id = id WHERE id = $1`, seed.perfFavID},
		{"studios self merged_into_id", `UPDATE adult_studios SET merged_into_id = id WHERE id = $1`, seed.studioFavID},
		{"studios self parent_studio_id", `UPDATE adult_studios SET parent_studio_id = id WHERE id = $1`, seed.studioFavID},
		{"studios self network_studio_id", `UPDATE adult_studios SET network_studio_id = id WHERE id = $1`, seed.studioFavID},
	}
	for _, tc := range selfCases {
		if _, err := st.Pool().Exec(ctx, tc.query, tc.id); err == nil {
			t.Fatalf("%s: expected error, got nil", tc.name)
		}
	}
}

func testMigration000046BoundaryTriggers(t *testing.T, st *store.Store, seed adultNormalizedSeed) {
	t.Helper()
	ctx := t.Context()

	// Boundary: confidence_score 0 and 100 accepted
	for i, score := range []int16{0, 100} {
		const insScore = `INSERT INTO adult_external_refs (
			id, entity_type, entity_id, source_kind, foreign_id, confidence_score, is_favorite, fetched_at, last_seen_at
		) VALUES ($1, 'performer', $2, 'tpdb_rest', $3, $4, false, $5, $5)`
		if _, err := st.Pool().Exec(ctx, insScore, uuid.New(), seed.perfFavID, "tpdb-bound-"+string(rune('0'+i)), score, seed.syncedAt); err != nil {
			t.Fatalf("insert boundary confidence_score=%d: %v", score, err)
		}
	}

	// Boundary: deleting a performer deletes its polymorphic adult_external_refs via trigger
	tempPerfID := uuid.New()
	if _, err := st.Pool().Exec(ctx, `INSERT INTO adult_performers (id, canonical_name, created_at, updated_at) VALUES ($1, 'Performer Temp', $2, $2)`, tempPerfID, seed.syncedAt); err != nil {
		t.Fatalf("insert temp performer: %v", err)
	}
	if _, err := st.Pool().Exec(ctx, `INSERT INTO adult_external_refs (id, entity_type, entity_id, source_kind, source_instance_id, foreign_id, is_favorite, fetched_at, last_seen_at) VALUES ($1, 'performer', $2, 'stashbox', $3, 'sb-temp-p', true, $4, $4)`, uuid.New(), tempPerfID, seed.instID, seed.syncedAt); err != nil {
		t.Fatalf("insert temp performer ref: %v", err)
	}
	if _, err := st.Pool().Exec(ctx, `DELETE FROM adult_performers WHERE id = $1`, tempPerfID); err != nil {
		t.Fatalf("delete temp performer: %v", err)
	}
	var count int
	if err := st.Pool().QueryRow(ctx, `SELECT count(*) FROM adult_external_refs WHERE entity_type = 'performer' AND entity_id = $1`, tempPerfID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("refs after deleting performer = %d, want 0 (err: %v)", count, err)
	}
}

func TestMigration000046AdultNormalizedModel(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	m := storetest.Migrator(t, st)

	stepDownToVersion(t, m, 45)
	assertTableExists(t, st, "adult_external_refs", false)
	assertTableExists(t, st, "adult_favorite_performers", true)

	seed := seedPre000046AdultData(t, st)

	if err := m.Steps(1); err != nil {
		t.Fatalf("Steps(1) up to 46: %v", err)
	}
	assertPost000046Up(t, st, seed)
	testMigration000046NegativeRefs(t, st, seed)
	testMigration000046NegativeSelfFKs(t, st, seed)
	testMigration000046BoundaryTriggers(t, st, seed)

	if err := m.Steps(-1); err != nil {
		t.Fatalf("Steps(-1) down from 46: %v", err)
	}
	assertTableExists(t, st, "adult_external_refs", false)
	assertTableExists(t, st, "adult_performer_links", true)
	assertTableExists(t, st, "adult_studio_links", true)

	var restoredPerfLinks, restoredStudioLinks int
	if err := st.Pool().QueryRow(t.Context(), `SELECT count(*) FROM adult_performer_links WHERE favorite_id = $1 AND foreign_id = 'sb-perf-a' AND is_favorite = TRUE`, seed.perfFavID).Scan(&restoredPerfLinks); err != nil || restoredPerfLinks != 1 {
		t.Fatalf("restored performer links = %d, want 1 (err: %v)", restoredPerfLinks, err)
	}
	if err := st.Pool().QueryRow(t.Context(), `SELECT count(*) FROM adult_studio_links WHERE favorite_id = $1 AND foreign_id = 'sb-studio-b' AND is_favorite = TRUE`, seed.studioFavID).Scan(&restoredStudioLinks); err != nil || restoredStudioLinks != 1 {
		t.Fatalf("restored studio links = %d, want 1 (err: %v)", restoredStudioLinks, err)
	}

	if err := m.Steps(1); err != nil {
		t.Fatalf("Steps(1) back up to 46: %v", err)
	}
	assertPost000046Up(t, st, seed)
}
