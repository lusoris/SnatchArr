// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package app

import (
	"fmt"

	dbmigrate "github.com/golusoris/golusoris/db/migrate"

	"github.com/lusoris/SnatchArr/api/internal/store"
)

// embedMigrations points golusoris db/migrate at the embedded SQL and runs it on start. The
// DSN stays as configured: golusoris gives a migrator that inherits db.dsn the pool's
// db.ssl.* and db.password_file, and uses an explicit db.migrate.dsn as given (#123).
func embedMigrations(o dbmigrate.Options) (dbmigrate.Options, error) {
	fsys, err := store.MigrationsFS()
	if err != nil {
		return dbmigrate.Options{}, fmt.Errorf("app: %w", err)
	}
	o.Auto = true
	o.Path = "." // MigrationsFS is already rooted at migrations/postgres
	return o.WithFS(fsys), nil
}
