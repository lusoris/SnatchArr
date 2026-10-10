// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package app

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"time"

	dbmigrate "github.com/golusoris/golusoris/db/migrate"
	dbpgx "github.com/golusoris/golusoris/db/pgx"
	"github.com/golusoris/golusoris/secrets"

	"github.com/lusoris/SnatchArr/api/internal/store"
)

// passwordReadTimeout bounds reading db.password_file for the migrator (HISS-02).
const passwordReadTimeout = 5 * time.Second

// embedMigrations points golusoris db/migrate at the embedded SQL and runs it on start. Unless
// db.migrate.dsn is set explicitly, the migrator gets the pool's DSN with db.ssl.* and
// db.password_file applied, which golusoris applies to the pool only (#123, golusoris#771).
func embedMigrations(o dbmigrate.Options, pgxOpts dbpgx.Options) (dbmigrate.Options, error) {
	fsys, err := store.MigrationsFS()
	if err != nil {
		return dbmigrate.Options{}, fmt.Errorf("app: %w", err)
	}
	if o.DSN == "" {
		if o.DSN, err = migratorDSN(pgxOpts); err != nil {
			return dbmigrate.Options{}, err
		}
	}
	o.Auto = true
	o.Path = "." // MigrationsFS is already rooted at migrations/postgres
	return o.WithFS(fsys), nil
}

// migratorDSN returns db.dsn with the non-empty db.ssl.* fields set as libpq parameters
// (overriding the DSN's own, as for the pool) and the password from db.password_file.
// golang-migrate's pgx/v5 driver hands these parameters to pgx. The migrator only accepts
// URL DSNs, so any other form is returned unchanged.
func migratorDSN(p dbpgx.Options) (string, error) {
	// An unparsable or non-URL DSN goes through unchanged: golusoris db/migrate reports it.
	u, err := url.Parse(p.DSN)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return p.DSN, nil
	}
	q := u.Query()
	for _, kv := range [][2]string{{"sslmode", p.SSL.Mode}, {"sslrootcert", p.SSL.RootCert}, {"sslcert", p.SSL.Cert}, {"sslkey", p.SSL.Key}} {
		if kv[1] != "" {
			q.Set(kv[0], kv[1])
		}
	}
	u.RawQuery = q.Encode()
	if p.PasswordFile != "" {
		ctx, cancel := context.WithTimeout(context.Background(), passwordReadTimeout)
		defer cancel()
		// secrets.File bounds the size, refuses non-regular files and trims the trailing
		// newline, exactly as db/pgx reads the same file for the pool.
		password, readErr := secrets.File(filepath.Dir(p.PasswordFile)).Get(ctx, filepath.Base(p.PasswordFile))
		if readErr != nil {
			return "", fmt.Errorf("app: migrator password file: %w", readErr)
		}
		u.User = url.UserPassword(u.User.Username(), password)
	}
	return u.String(), nil
}
