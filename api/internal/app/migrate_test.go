// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package app

import (
	"testing"

	dbmigrate "github.com/golusoris/golusoris/db/migrate"
)

// The decorator sets the source and the automatic run and leaves the DSN to golusoris: an
// explicit db.migrate.dsn stays as given, an empty one stays empty and inherits db.dsn.
func TestEmbedMigrations(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   dbmigrate.Options
	}{
		{"positive: an explicit DSN stays as given", dbmigrate.Options{DSN: "postgres://migrator:secret@db.invalid/snatcharr?sslmode=verify-ca"}},
		{"negative: a configured path and manual run are overridden", dbmigrate.Options{Path: "elsewhere", Auto: false}},
		{"boundary: an empty DSN stays empty", dbmigrate.Options{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := embedMigrations(tc.in)
			if err != nil {
				t.Fatalf("embedMigrations() = %v", err)
			}
			if got.DSN != tc.in.DSN {
				t.Errorf("DSN = %q, want %q unchanged", got.DSN, tc.in.DSN)
			}
			if !got.Auto || got.Path != "." || got.FS == nil {
				t.Errorf("auto=%v path=%q fs set=%v, want the embedded migrations run at start", got.Auto, got.Path, got.FS != nil)
			}
		})
	}
}
