// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package storetest

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/lusoris/SnatchArr/api/internal/store"
)

// migrationSet returns a set of n paired migrations named step, then applies edit to it.
func migrationSet(n int, edit func(fstest.MapFS)) fstest.MapFS {
	fsys := fstest.MapFS{}
	for v := 1; v <= n; v++ {
		for _, dir := range []string{"up", "down"} {
			fsys[fmt.Sprintf("%06d_step.%s.sql", v, dir)] = &fstest.MapFile{Data: []byte("SELECT 1;\n")}
		}
	}
	if edit != nil {
		edit(fsys)
	}
	return fsys
}

func TestLatestMigration(t *testing.T) {
	t.Parallel()
	file := &fstest.MapFile{Data: []byte("SELECT 1;\n")}
	tests := []struct {
		name    string
		fsys    fstest.MapFS
		want    uint
		wantErr string
	}{
		{"three paired versions", migrationSet(3, nil), 3, ""},
		{"boundary: one version", migrationSet(1, nil), 1, ""},
		{"empty directory", fstest.MapFS{}, 0, "0 migration files"},
		{
			"unapplied: name golang-migrate skips", migrationSet(2, func(f fstest.MapFS) { f["000003-step.up.sql"] = file }),
			0, "000003-step.up.sql: not a migration file",
		},
		{
			"unapplied: upper-case extension", migrationSet(2, func(f fstest.MapFS) { f["000003_step.up.SQL"] = file }),
			0, "000003_step.up.SQL: not a migration file",
		},
		{"short version", migrationSet(2, func(f fstest.MapFS) { f["3_step.up.sql"] = file }), 0, "3_step.up.sql: not a migration file"},
		{"stray file", migrationSet(2, func(f fstest.MapFS) { f["README.md"] = file }), 0, "README.md: not a migration file"},
		{"subdirectory", migrationSet(2, func(f fstest.MapFS) { f["old/000001_step.up.sql"] = file }), 0, "old: not a migration file"},
		{"version zero", migrationSet(2, func(f fstest.MapFS) {
			f["000000_step.up.sql"], f["000000_step.down.sql"] = file, file
		}), 0, "version must be 1 or above"},
		{"gap", migrationSet(4, func(f fstest.MapFS) {
			delete(f, "000003_step.up.sql")
			delete(f, "000003_step.down.sql")
		}), 0, "migration 000003 missing"},
		{
			"missing down", migrationSet(3, func(f fstest.MapFS) { delete(f, "000002_step.down.sql") }),
			0, "migration 000002_step: needs both an up and a down file",
		},
		{
			"missing up of the latest", migrationSet(3, func(f fstest.MapFS) { delete(f, "000003_step.up.sql") }),
			0, "migration 000003_step: needs both",
		},
		{"up and down named apart", migrationSet(2, func(f fstest.MapFS) {
			delete(f, "000002_step.down.sql")
			f["000002_other.down.sql"] = file
		}), 0, `migration 000002: named`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := LatestMigration(tt.fsys)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("LatestMigration() error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("LatestMigration() = %d, %v, want %d", got, err, tt.want)
			}
		})
	}
}

// The embedded migrations pass the check, so every integration test binary's template is at
// their latest version.
func TestLatestMigration_EmbeddedSet(t *testing.T) {
	t.Parallel()
	fsys, err := store.MigrationsFS()
	if err != nil {
		t.Fatal(err)
	}
	if v, err := LatestMigration(fsys); err != nil || v == 0 {
		t.Fatalf("LatestMigration(embedded) = %d, %v", v, err)
	}
}

func TestAtLatest(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		version, files uint
		dirty, wantErr bool
	}{
		{"positive: at the latest file", 21, 21, false, false},
		{"negative: behind the files", 20, 21, false, true},
		{"negative: dirty at the latest", 21, 21, true, true},
		{"boundary: beyond the files", 22, 21, false, true},
	}
	for _, tt := range tests {
		if err := atLatest(tt.version, tt.dirty, tt.files); (err != nil) != tt.wantErr {
			t.Errorf("%s: atLatest(%d, %v, %d) = %v, wantErr %v", tt.name, tt.version, tt.dirty, tt.files, err, tt.wantErr)
		}
	}
}
