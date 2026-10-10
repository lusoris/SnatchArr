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
	"os"
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
	// image is the Postgres golusoris testutil/pg pins.
	image        = "postgres:17-alpine@sha256:f02121de6f74d30d8a94cd1d9584125e2178d7e6c377d8130112d4e52d867995"
	templateName = "snatcharr_template"
	// Bounds (HISS-02): container boot including the image pull, and one database operation.
	startTimeout = 3 * time.Minute
	opTimeout    = 30 * time.Second
)

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

func start() (func() error, error) {
	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()
	c, runErr := tcpostgres.Run(ctx, image, tcpostgres.WithDatabase(templateName),
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
	fsys, err := store.MigrationsFS()
	if err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	// golusoris registers golang-migrate's pgx/v5 driver under the pgx5:// scheme.
	m, err := dbmigrate.Open(dbmigrate.Options{FS: fsys, Path: "."}, "pgx5://"+strings.TrimPrefix(dsn, "postgres://"),
		slog.New(slog.DiscardHandler))
	if err != nil {
		return fmt.Errorf("open migrator: %w", err)
	}
	if upErr := errors.Join(m.Up(), m.Close()); upErr != nil {
		return fmt.Errorf("migrate template: %w", upErr)
	}
	return nil
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
		ID: instanceID, Kind: "sonarr", Name: "storetest-" + instanceID.String()[:8], BaseUrl: "http://arr.invalid",
		ApiKeyEnc: []byte("placeholder"), Enabled: true, Source: "manual", CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("storetest: create instance: %v", err)
	}
	return instanceID
}
