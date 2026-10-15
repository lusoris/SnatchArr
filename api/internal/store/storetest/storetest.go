// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package storetest gives integration tests a migrated Postgres database each.
//
// One container serves a whole test binary: Main (called from the package's TestMain) starts
// it, applies every migration once to a template database and terminates it when the tests
// end. New then clones that template into a fresh database per test, so tests stay isolated
// and may run in parallel without starting a container each. The tests need Docker and are
// skipped under `go test -short`. CI routes the Docker Hub pulls through mirror.gcr.io with
// TESTCONTAINERS_HUB_IMAGE_NAME_PREFIX.
package storetest

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	dbmigrate "github.com/golusoris/golusoris/db/migrate"

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
	// Bounds (HISS-02): container boot including the image pull, and one database operation.
	startTimeout = 3 * time.Minute
	opTimeout    = 30 * time.Second
	// ryukLinger is how long the reaper outlives its last client. go test ./... runs each package
	// as its own process of one testcontainers session, so they share one reaper; with the 10s
	// default it exits in a gap between packages, and the next package reuses the exited
	// container and waits 60s on it (testcontainers/testcontainers-go#3867).
	ryukLinger = 5 * time.Minute
)

// ryukLingerEnv are the names testcontainers reads the reaper's reconnection timeout from.
var ryukLingerEnv = [...]string{"RYUK_RECONNECTION_TIMEOUT", "TESTCONTAINERS_RYUK_RECONNECTION_TIMEOUT"}

// shared is the container one test binary uses.
var shared struct {
	admin  *pgxpool.Pool   // connected to the postgres database, for CREATE / DROP DATABASE
	cfg    *pgxpool.Config // the template database's pool config, copied per test
	err    error
	mu     sync.Mutex // CREATE DATABASE from one template is not safe concurrently
	serial atomic.Int64
}

// Main runs the package's tests around one shared Postgres. Call it from TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }
func Main(m *testing.M) int {
	flag.Parse()
	if testing.Short() {
		return m.Run()
	}
	stop, err := start()
	shared.err = err
	code := m.Run()
	if stopErr := stop(); stopErr != nil {
		fmt.Fprintln(os.Stderr, "storetest: stop postgres:", stopErr)
	}
	return code
}

// lingerReaper sets ryukLinger unless the environment already chooses a reconnection timeout.
// It must run before the first container: testcontainers reads its configuration once.
func lingerReaper() error {
	for _, name := range ryukLingerEnv {
		if os.Getenv(name) != "" {
			return nil
		}
	}
	if err := os.Setenv(ryukLingerEnv[1], ryukLinger.String()); err != nil {
		return fmt.Errorf("set reaper reconnection timeout: %w", err)
	}
	return nil
}

func start() (func() error, error) {
	if err := lingerReaper(); err != nil {
		return func() error { return nil }, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()
	c, runErr := tcpostgres.Run(ctx, Image, tcpostgres.WithDatabase(templateName),
		tcpostgres.WithUsername("test"), tcpostgres.WithPassword("test"), tcpostgres.BasicWaitStrategies())
	stop := func() error {
		if shared.admin != nil {
			shared.admin.Close()
		}
		if c == nil {
			return nil
		}
		stopCtx, stopCancel := context.WithTimeout(context.Background(), opTimeout)
		defer stopCancel()
		return c.Terminate(stopCtx)
	}
	if runErr != nil {
		return stop, fmt.Errorf("run postgres: %w", runErr)
	}
	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return stop, fmt.Errorf("connection string: %w", err)
	}
	if migrateErr := migrateTemplate(dsn); migrateErr != nil {
		return stop, migrateErr
	}
	return stop, connectAdmin(ctx, dsn)
}

// migrateTemplate applies every migration to the template database and disconnects from it,
// since CREATE DATABASE ... TEMPLATE refuses a template that has open connections.
func migrateTemplate(dsn string) error {
	// golusoris registers golang-migrate's pgx/v5 driver under the pgx5:// scheme.
	m, err := openMigrator("pgx5://" + strings.TrimPrefix(dsn, "postgres://"))
	if err != nil {
		return err
	}
	if upErr := errors.Join(m.Up(), m.Close()); upErr != nil {
		return fmt.Errorf("migrate template: %w", upErr)
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

func connectAdmin(ctx context.Context, dsn string) error {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("parse dsn: %w", err)
	}
	shared.cfg = cfg.Copy()
	cfg.ConnConfig.Database = "postgres"
	if shared.admin, err = pgxpool.NewWithConfig(ctx, cfg); err != nil {
		return fmt.Errorf("admin pool: %w", err)
	}
	return nil
}

// New returns a Store over a fresh database cloned from the migrated template.
func New(t *testing.T) *store.Store {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test: needs Docker; run without -short")
	}
	if shared.err != nil || shared.admin == nil {
		t.Fatalf("storetest: shared postgres unavailable (TestMain must call storetest.Main): %v", shared.err)
	}
	dbName := fmt.Sprintf("t%d_%d", os.Getpid(), shared.serial.Add(1))
	quoted := pgx.Identifier{dbName}.Sanitize()
	adminExec(t, "CREATE DATABASE "+quoted+" TEMPLATE "+pgx.Identifier{templateName}.Sanitize())
	t.Cleanup(func() { adminExec(t, "DROP DATABASE IF EXISTS "+quoted+" WITH (FORCE)") })

	cfg := shared.cfg.Copy()
	cfg.ConnConfig.Database = dbName
	ctx, cancel := context.WithTimeout(t.Context(), opTimeout)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("storetest: open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return store.New(pool)
}

// adminExec runs one statement on the admin connection. It also runs from t.Cleanup, after
// t.Context is cancelled, hence WithoutCancel.
func adminExec(t *testing.T, sql string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), opTimeout)
	defer cancel()
	shared.mu.Lock()
	defer shared.mu.Unlock()
	if _, err := shared.admin.Exec(ctx, sql); err != nil {
		t.Fatalf("storetest: %s: %v", sql, err)
	}
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
