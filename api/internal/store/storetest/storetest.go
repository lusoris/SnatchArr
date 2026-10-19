// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package storetest gives integration tests a migrated Postgres database each.
//
// One container serves a whole test binary: Main (called from the package's TestMain) starts
// it via golusoris testutil/pg, applies every migration once to a template database and terminates
// it when the tests end. New then clones that template into a fresh database per test, so tests
// stay isolated and may run in parallel without starting a container each. The tests need Docker
// and are skipped under `go test -short`.
package storetest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"

	dbmigrate "github.com/golusoris/golusoris/db/migrate"
	"github.com/golusoris/golusoris/testutil/pg"

	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

const (
	// Image is the Postgres golusoris testutil/pg pins, of major Major.
	Image = "postgres:18-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873"
	// Major is the server major version of Image.
	Major = 18
	// PreviousMajorImage is the major before it, which installed databases may still run;
	// the upgrade test moves a database from it to Image (renovate.json holds it on 17).
	PreviousMajorImage = "postgres:17-alpine@sha256:b0f9560a2de083e2cc7382e75f808c7381a32852a7ec49117deedb300e552b24"
	// PreviousMajor is the server major version of PreviousMajorImage.
	PreviousMajor = 17
	templateName  = "snatcharr_template"
)

// Main runs the package's tests around one shared Postgres. Call it from TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }
func Main(m *testing.M) int {
	migrate := func(_ context.Context, dsn string) error {
		return migrateTemplate(dsn)
	}
	return pg.Main(m, migrate, pg.Options{
		Image:       Image,
		Database:    templateName,
		Customizers: []testcontainers.ContainerCustomizer{QuickInit()},
	})
}

// QuickInit makes initdb skip its final sync of the new data directory. A test database needs
// no crash durability, and that sync took about 9 of the 10.5 s of a container boot on a btrfs
// Docker root (1.3 s without it; measured 2026-10-18), once per test package.
func QuickInit() testcontainers.CustomizeRequestOption {
	return testcontainers.WithEnv(map[string]string{"POSTGRES_INITDB_ARGS": "--no-sync"})
}

// migrateTemplate applies every migration to the template database and disconnects from it,
// since CREATE DATABASE ... TEMPLATE refuses a template that has open connections. The
// template must end at the latest migration file: a file golang-migrate skipped fails here.
func migrateTemplate(dsn string) error {
	fsys, err := store.MigrationsFS()
	if err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	latest, err := LatestMigration(fsys)
	if err != nil {
		return err
	}
	// golusoris registers golang-migrate's pgx/v5 driver under the pgx5:// scheme.
	m, err := openMigrator("pgx5://" + strings.TrimPrefix(dsn, "postgres://"))
	if err != nil {
		return err
	}
	upErr := m.Up()
	version, dirty, versionErr := m.Version()
	if joined := errors.Join(upErr, versionErr, m.Close()); joined != nil {
		return fmt.Errorf("migrate template: %w", joined)
	}
	return atLatest(version, dirty, latest)
}

// atLatest refuses a template that is dirty or at another version than the latest file.
func atLatest(version uint, dirty bool, latest uint) error {
	if dirty || version != latest {
		return fmt.Errorf("migrate template: at version %d (dirty %v), the migration files end at %d", version, dirty, latest)
	}
	return nil
}

func openMigrator(url string) (*dbmigrate.Migrator, error) {
	fsys, err := store.MigrationsFS()
	if err != nil {
		return nil, fmt.Errorf("migrations: %w", err)
	}
	m, err := dbmigrate.Open(dbmigrate.Options{FS: fsys, Path: "."}, url, slog.New(slog.DiscardHandler))
	if err != nil {
		return nil, fmt.Errorf("open migrator: %w", err)
	}
	return m, nil
}

// Migrator opens the migrations against the test's own database (the one New cloned for
// st), already at the latest version, for tests that walk migrations down and up.
func Migrator(t *testing.T, st *store.Store) *dbmigrate.Migrator {
	t.Helper()
	cc := st.Pool().Config().ConnConfig
	u := url.URL{
		Scheme: "pgx5", User: url.UserPassword(cc.User, cc.Password),
		Host: net.JoinHostPort(cc.Host, strconv.Itoa(int(cc.Port))), Path: "/" + cc.Database, RawQuery: "sslmode=disable",
	}
	m, err := openMigrator(u.String())
	if err != nil {
		t.Fatalf("storetest: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := m.Close(); closeErr != nil {
			t.Errorf("storetest: close migrator: %v", closeErr)
		}
	})
	return m
}

// New returns a Store over a fresh database cloned from the migrated template.
func New(t *testing.T) *store.Store {
	t.Helper()
	return store.New(pg.Clone(t))
}

// Instance inserts an enabled Sonarr instance and returns its id. The API key is a
// placeholder: nothing in these tests talks to an *arr.
func Instance(t *testing.T, st *store.Store, now time.Time) uuid.UUID {
	t.Helper()
	instanceID := uuid.New()
	_, err := st.Q().CreateInstance(t.Context(), sqlcgen.CreateInstanceParams{
		ID: instanceID, Kind: "sonarr", Name: "storetest-" + instanceID.String(), BaseUrl: "http://arr.invalid",
		ApiKeyEnc: []byte("placeholder"), Enabled: true, Source: "manual", CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("storetest: create instance: %v", err)
	}
	return instanceID
}
