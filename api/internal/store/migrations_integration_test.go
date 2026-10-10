// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package store_test

import (
	"os"
	"testing"

	dbmigrate "github.com/golusoris/golusoris/db/migrate"

	"github.com/lusoris/SnatchArr/api/internal/store"
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
