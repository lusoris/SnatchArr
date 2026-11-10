// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package recovery_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

// TestMain starts the shared Postgres test container for this package.
func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

// recoveryTime is the fixed clock every recovery case seeds rows with.
func recoveryTime() time.Time {
	return time.Date(2026, time.October, 14, 10, 0, 0, 0, time.UTC)
}

// TestDumpRestoreRoundTrip dumps a seeded instance and its non-default
// policy, restores both into a freshly migrated database and reads them back
// unchanged.
func TestDumpRestoreRoundTrip(t *testing.T) {
	t.Parallel()

	src := storetest.New(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	now := recoveryTime()
	instanceID := storetest.Instance(t, src, now)

	policy, err := src.Q().EnsureDefaultPolicy(ctx, sqlcgen.EnsureDefaultPolicyParams{
		InstanceID: instanceID,
		UpdatedAt:  now,
	})
	if err != nil {
		t.Fatalf("ensure default policy: %v", err)
	}
	defaultPolicy := policy
	policy = seedRecoveryPolicy(t, ctx, src, defaultPolicy)
	if policy.MissingPerCycle == defaultPolicy.MissingPerCycle {
		t.Fatal("seeded policy missing_per_cycle must differ from its default")
	}

	tables := []string{"instances", "snatch_policies"}
	dumps := dumpTables(t, ctx, src.Pool(), tables)

	dstPool := createFreshDB(t, src.Pool())
	applyMigrations(t, dstPool)

	restoreTables(t, ctx, dstPool, dumps, tables)

	dstStore := store.New(dstPool)
	inst, err := dstStore.Q().GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("read back instance: %v", err)
	}
	if inst.Name != "storetest-"+instanceID.String() {
		t.Errorf("instance name mismatch: got %q", inst.Name)
	}

	restored, err := dstStore.Q().GetPolicy(ctx, instanceID)
	if err != nil {
		t.Fatalf("read back policy: %v", err)
	}
	if !reflect.DeepEqual(restored, policy) {
		t.Errorf("restored policy mismatch: got %+v, want %+v", restored, policy)
	}
}

// seedRecoveryPolicy overwrites the default policy with a value in every
// column, saves both cursors, and returns the row as the store reads it.
func seedRecoveryPolicy(t *testing.T, ctx context.Context, src *store.Store, defaults sqlcgen.SnatchPolicy) sqlcgen.SnatchPolicy {
	t.Helper()
	updatedAt := defaults.UpdatedAt.Add(-time.Hour)
	if err := src.Q().UpsertPolicy(ctx, sqlcgen.UpsertPolicyParams{
		InstanceID:             defaults.InstanceID,
		MissingPerCycle:        7,
		UpgradePerCycle:        3,
		CycleIntervalS:         1200,
		HourlyCap:              42,
		Selection:              "recent",
		MonitoredOnly:          false,
		SkipFutureReleases:     false,
		RadarrReleaseType:      "digital",
		SonarrMissingMode:      "shows",
		SonarrUpgradeMode:      "season_packs",
		LidarrMissingMode:      "album",
		ProcessedTtlH:          72,
		MaxQueueSize:           25,
		AwaitCommand:           true,
		PageSize:               50,
		UpdatedAt:              updatedAt,
		RecentGrabWindowH:      6,
		AfterglowMaxH:          48,
		IncludeTags:            []int32{11, 12},
		ExcludeTags:            []int32{21},
		IncludeQualityProfiles: []int32{31},
		ExcludeQualityProfiles: []int32{41, 42},
		ReleaseDelayH:          12,
		DryRun:                 true,
		SkipUnavailableMovies:  false,
	}); err != nil {
		t.Fatalf("seed recovery policy: %v", err)
	}
	for _, kind := range []string{"missing", "upgrade"} {
		if err := src.Q().SavePolicyCursor(ctx, sqlcgen.SavePolicyCursorParams{
			InstanceID: defaults.InstanceID, Kind: kind, Cursor: kind + "-cursor", UpdatedAt: updatedAt,
		}); err != nil {
			t.Fatalf("seed recovery policy %s cursor: %v", kind, err)
		}
	}
	policy, err := src.Q().GetPolicy(ctx, defaults.InstanceID)
	if err != nil {
		t.Fatalf("read seeded recovery policy: %v", err)
	}
	return policy
}

// TestDumpRestoreEmptyTables round-trips empty tables: each dump still
// carries the binary COPY header and the restore adds zero rows.
func TestDumpRestoreEmptyTables(t *testing.T) {
	t.Parallel()

	src := storetest.New(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	tables := []string{"instances", "snatch_policies"}
	dumps := dumpTables(t, ctx, src.Pool(), tables)
	for _, tbl := range tables {
		if !bytes.HasPrefix(dumps[tbl], []byte("PGCOPY\n\xff\r\n\x00")) {
			t.Fatalf("COPY %s missing binary header", tbl)
		}
	}

	dstPool := createFreshDB(t, src.Pool())
	applyMigrations(t, dstPool)
	restoreTables(t, ctx, dstPool, dumps, tables)

	for _, tbl := range tables {
		var count int64
		if err := dstPool.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{tbl}.Sanitize()).Scan(&count); err != nil {
			t.Fatalf("count restored %s: %v", tbl, err)
		}
		if count != 0 {
			t.Errorf("restored %s row count: got %d, want 0", tbl, count)
		}
	}
}

// TestCorruptedDumpRefused flips bytes in a binary dump and expects Postgres
// to refuse the restore with SQLSTATE 22P04 (bad COPY file format).
func TestCorruptedDumpRefused(t *testing.T) {
	t.Parallel()

	src := storetest.New(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	now := recoveryTime()
	storetest.Instance(t, src, now)

	var buf bytes.Buffer
	conn, err := src.Pool().Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire conn: %v", err)
	}
	_, err = conn.Conn().PgConn().CopyTo(ctx, &buf, "COPY instances TO STDOUT WITH (FORMAT binary)")
	conn.Release()
	if err != nil {
		t.Fatalf("COPY TO: %v", err)
	}

	corrupted := make([]byte, buf.Len())
	copy(corrupted, buf.Bytes())
	if len(corrupted) > 20 {
		corrupted[15] ^= 0xFF
		corrupted[16] ^= 0xFF
		corrupted[17] ^= 0xFF
		corrupted[18] ^= 0xFF
	}

	dstPool := createFreshDB(t, src.Pool())
	applyMigrations(t, dstPool)

	dstConn, err := dstPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire conn for restore: %v", err)
	}
	_, err = dstConn.Conn().PgConn().CopyFrom(ctx, bytes.NewReader(corrupted), "COPY instances FROM STDIN WITH (FORMAT binary)")
	dstConn.Release()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "22P04" {
		t.Fatalf("expected COPY format error (22P04) for corrupted header, got: %v", err)
	}
}

// dumpTables copies each table to a non-empty binary dump.
func dumpTables(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tables []string) map[string][]byte {
	t.Helper()

	dumps := make(map[string][]byte, len(tables))
	for _, tbl := range tables {
		var buf bytes.Buffer
		conn, connErr := pool.Acquire(ctx)
		if connErr != nil {
			t.Fatalf("acquire conn for dump: %v", connErr)
		}
		_, copyErr := conn.Conn().PgConn().CopyTo(ctx, &buf, fmt.Sprintf("COPY %s TO STDOUT WITH (FORMAT binary)", pgx.Identifier{tbl}.Sanitize()))
		conn.Release()
		if copyErr != nil {
			t.Fatalf("COPY %s TO STDOUT: %v", tbl, copyErr)
		}
		if buf.Len() == 0 {
			t.Fatalf("COPY %s produced zero bytes", tbl)
		}
		dumps[tbl] = buf.Bytes()
	}
	return dumps
}

// restoreTables restores binary dumps in the supplied table order.
func restoreTables(t *testing.T, ctx context.Context, pool *pgxpool.Pool, dumps map[string][]byte, tables []string) {
	t.Helper()

	for _, tbl := range tables {
		conn, connErr := pool.Acquire(ctx)
		if connErr != nil {
			t.Fatalf("acquire conn for restore: %v", connErr)
		}
		_, copyErr := conn.Conn().PgConn().CopyFrom(ctx, bytes.NewReader(dumps[tbl]), fmt.Sprintf("COPY %s FROM STDIN WITH (FORMAT binary)", pgx.Identifier{tbl}.Sanitize()))
		conn.Release()
		if copyErr != nil {
			t.Fatalf("COPY %s FROM STDIN: %v", tbl, copyErr)
		}
	}
}

// createFreshDB creates an empty database on the source server with
// statement and lock timeouts, drops it on cleanup and returns a pool on it.
func createFreshDB(t *testing.T, srcPool *pgxpool.Pool) *pgxpool.Pool {
	t.Helper()

	cc := srcPool.Config().ConnConfig
	adminDSN := (&url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(cc.User, cc.Password),
		Host:     net.JoinHostPort(cc.Host, strconv.Itoa(int(cc.Port))),
		Path:     "/postgres",
		RawQuery: "sslmode=disable",
	}).String()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	adminConn, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		t.Fatalf("connect admin: %v", err)
	}
	defer func() {
		if closeErr := adminConn.Close(ctx); closeErr != nil {
			t.Errorf("close recovery admin: %v", closeErr)
		}
	}()

	dbName := "recovery_" + uuid.New().String()[:8]
	_, err = adminConn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{dbName}.Sanitize())
	if err != nil {
		t.Fatalf("create recovery db: %v", err)
	}
	t.Cleanup(func() { dropRecoveryDB(t, adminDSN, dbName) })
	// Database defaults reach the migrator's new sessions; bound SQL, not connection setup or total runtime.
	// https://www.postgresql.org/docs/18/sql-alterdatabase.html
	if _, statementErr := adminConn.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{dbName}.Sanitize()+" SET statement_timeout = '10s'"); statementErr != nil {
		t.Fatalf("set recovery db %s statement timeout: %v", dbName, statementErr)
	}
	if _, lockErr := adminConn.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{dbName}.Sanitize()+" SET lock_timeout = '5s'"); lockErr != nil {
		t.Fatalf("set recovery db %s lock timeout: %v", dbName, lockErr)
	}

	dstDSN := (&url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(cc.User, cc.Password),
		Host:     net.JoinHostPort(cc.Host, strconv.Itoa(int(cc.Port))),
		Path:     "/" + dbName,
		RawQuery: "sslmode=disable",
	}).String()

	pool, err := pgxpool.New(ctx, dstDSN)
	if err != nil {
		t.Fatalf("open recovery pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// dropRecoveryDB force-drops a recovery database from the admin database,
// reporting rather than failing on cleanup errors.
func dropRecoveryDB(t *testing.T, adminDSN, dbName string) {
	t.Helper()
	dropCtx, dropCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dropCancel()
	dc, err := pgx.Connect(dropCtx, adminDSN)
	if err != nil {
		t.Errorf("cleanup recovery db %s: connect admin: %v", dbName, err)
		return
	}
	if _, err := dc.Exec(dropCtx, "DROP DATABASE IF EXISTS "+pgx.Identifier{dbName}.Sanitize()+" WITH (FORCE)"); err != nil {
		t.Errorf("cleanup recovery db %s: drop: %v", dbName, err)
	}
	if err := dc.Close(dropCtx); err != nil {
		t.Errorf("cleanup recovery db %s: close admin: %v", dbName, err)
	}
}

// applyMigrations runs every store migration up on pool.
func applyMigrations(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	st := store.New(pool)
	m := storetest.Migrator(t, st)
	if err := m.Up(); err != nil {
		t.Fatalf("migrate recovery db: %v", err)
	}
}
