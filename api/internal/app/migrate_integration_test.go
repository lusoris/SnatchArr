// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package app

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.uber.org/fx"

	"github.com/golusoris/golusoris"
	gconfig "github.com/golusoris/golusoris/core/config"
	dbmigrate "github.com/golusoris/golusoris/db/migrate"

	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

// migratorTestTimeout bounds one start and stop of the migrator (HISS-02).
const migratorTestTimeout = 30 * time.Second

// testDatabase is the address of a test's own database: migrated, served without TLS, and
// password-checked.
type testDatabase struct {
	user, password, host, name string
}

func newTestDatabase(t *testing.T) testDatabase {
	t.Helper()
	cc := storetest.New(t).Pool().Config().ConnConfig
	return testDatabase{
		user: cc.User, password: cc.Password,
		host: net.JoinHostPort(cc.Host, strconv.Itoa(int(cc.Port))), name: cc.Database,
	}
}

// dsn is a URL DSN for the database with the given password (none when empty) and sslmode.
func (d testDatabase) dsn(password, sslmode string) string {
	u := url.URL{Scheme: "postgres", User: url.User(d.user), Host: d.host, Path: "/" + d.name, RawQuery: "sslmode=" + sslmode}
	if password != "" {
		u.User = url.UserPassword(d.user, password)
	}
	return u.String()
}

// passwordFile writes password to a file the way a mounted secret holds it.
func passwordFile(t *testing.T, password string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "password")
	if err := os.WriteFile(file, []byte(password+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

// runMigrator builds the migrator the way the control plane does (golusoris config and DB
// modules, embedMigrations) from the "db" configuration and runs the start-time migration.
// It returns the schema version the migrator read through its own connection.
func runMigrator(t *testing.T, db map[string]any) (uint, error) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"db": db})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "config.json")
	if err = os.WriteFile(file, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := gconfig.New(gconfig.Options{Files: []string{file}, EnvPrefix: "SNATCHARR_MIGRATOR_TEST_UNSET_"})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	var migrator *dbmigrate.Migrator
	app := fx.New(
		golusoris.Core, golusoris.DB, fx.NopLogger,
		fx.Decorate(func(*gconfig.Config) *gconfig.Config { return cfg }),
		fx.Decorate(embedMigrations),
		fx.Populate(&migrator),
	)
	ctx, cancel := context.WithTimeout(t.Context(), migratorTestTimeout)
	defer cancel()
	if err = app.Start(ctx); err != nil {
		return 0, err
	}
	version, dirty, err := migrator.Version()
	if stopErr := app.Stop(ctx); stopErr != nil {
		t.Errorf("stop: %v", stopErr)
	}
	if dirty {
		t.Errorf("schema version %d is dirty", version)
	}
	return version, err
}

// unreachable is a pool address no case may connect to: an explicit migrator address replaces it.
const unreachable = "postgres://nobody@db.invalid:5432/none?sslmode=verify-full"

// migratorCase is one "db" configuration and the outcome of the start it leads to.
type migratorCase struct {
	name string
	db   func(t *testing.T, d testDatabase) map[string]any
	// wantErr is a fragment of the expected start error; empty means the start succeeds.
	wantErr string
}

// explicitAddressCases: db.migrate.dsn is set, so no pool setting may reach the migrator.
func explicitAddressCases() []migratorCase {
	return []migratorCase{
		{
			name: "positive: an explicit migrator address is used exactly as given",
			db: func(t *testing.T, d testDatabase) map[string]any {
				t.Helper()
				return map[string]any{
					"dsn": unreachable, "password_file": passwordFile(t, "not-"+d.password),
					"ssl":     map[string]any{"mode": "require"},
					"migrate": map[string]any{"dsn": d.dsn(d.password, "disable")},
				}
			},
		},
		{
			name: "negative: an explicit migrator address does not take the pool's password",
			db: func(t *testing.T, d testDatabase) map[string]any {
				t.Helper()
				return map[string]any{
					"dsn": unreachable, "password_file": passwordFile(t, d.password),
					"migrate": map[string]any{"dsn": d.dsn("", "disable")},
				}
			},
			wantErr: "password",
		},
	}
}

// inheritedAddressCases: the migrator inherits db.dsn and with it the pool's settings.
func inheritedAddressCases() []migratorCase {
	return []migratorCase{
		{
			name: "positive: the inherited address takes the password file",
			db: func(t *testing.T, d testDatabase) map[string]any {
				t.Helper()
				return map[string]any{"dsn": d.dsn("", "disable"), "password_file": passwordFile(t, d.password)}
			},
		},
		{
			name: "negative: a wrong password in the file is refused",
			db: func(t *testing.T, d testDatabase) map[string]any {
				t.Helper()
				return map[string]any{"dsn": d.dsn(d.password, "disable"), "password_file": passwordFile(t, "not-"+d.password)}
			},
			wantErr: "password authentication failed",
		},
		{
			name: "negative: the inherited address takes db.ssl.mode",
			db: func(_ *testing.T, d testDatabase) map[string]any {
				return map[string]any{"dsn": d.dsn(d.password, "disable"), "ssl": map[string]any{"mode": "require"}}
			},
			wantErr: "TLS",
		},
		{
			name: "negative: the inherited address takes db.ssl.rootcert",
			db: func(t *testing.T, d testDatabase) map[string]any {
				t.Helper()
				missing := filepath.Join(t.TempDir(), "absent-ca.crt")
				return map[string]any{"dsn": d.dsn(d.password, "disable"), "ssl": map[string]any{"mode": "verify-full", "rootcert": missing}}
			},
			wantErr: "absent-ca.crt",
		},
		{
			name: "boundary: db.ssl.mode wins over the address's own sslmode",
			db: func(_ *testing.T, d testDatabase) map[string]any {
				return map[string]any{"dsn": d.dsn(d.password, "require"), "ssl": map[string]any{"mode": "disable"}}
			},
		},
	}
}

// The migrator's connection follows docs/deployment.md: one that inherits db.dsn takes the
// pool's db.ssl.* and db.password_file, and an explicit db.migrate.dsn is used exactly as
// given (#123, golusoris#771, golusoris#810). Each case connects to a real server, which
// checks the password and serves no TLS, so a setting that is wrongly applied or wrongly
// ignored turns the outcome.
func TestMigratorConnection(t *testing.T) {
	t.Parallel()
	for _, tc := range slices.Concat(explicitAddressCases(), inheritedAddressCases()) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			version, err := runMigrator(t, tc.db(t, newTestDatabase(t)))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("start = %v, want an error that mentions %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || version == 0 {
				t.Fatalf("start = %v, schema version %d; want the migrated schema read without error", err, version)
			}
		})
	}
}
