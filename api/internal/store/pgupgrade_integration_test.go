// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package store_test

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/network"

	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

// The major upgrade of an installed database (docs/deployment.md, "Postgres major upgrade"):
// a database on the previous major is dumped with the new major's pg_dump and restored into
// the new major, and the API then starts on it with nothing changed but the DSN.
const (
	// sourceDSN reaches the old server from inside the new one over the test network.
	sourceDSN = "postgres://test:test@pg-source:5432/snatcharr?sslmode=disable"
	targetDSN = "postgres://test:test@localhost:5432/snatcharr?sslmode=disable"
	// Bounds (HISS-02): container boot including the image pull, and dump plus restore.
	upgradeBootTimeout      = 3 * time.Minute
	upgradeOpTimeout        = time.Minute
	upgradeTerminateTimeout = 30 * time.Second
)

// runPostgres starts one Postgres of the given image on nw under the alias and returns it.
func runPostgres(t *testing.T, nw *testcontainers.DockerNetwork, image, alias string) *tcpostgres.PostgresContainer {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), upgradeBootTimeout)
	defer cancel()

	// Upstream testcontainers-go #3948: attemptToPullImage drains pull stream with io.Copy
	// and swallows HTTP-200 stream errors (e.g. mirror 404), leading to silent success.
	// Pre-pull explicitly and verify stream response before creating the container.
	cli, err := testcontainers.NewDockerClientWithOpts(ctx)
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	defer cli.Close()

	if pullErr := prePull(ctx, cli, hubRef(testcontainers.ReadConfig().Config.HubImageNamePrefix, image)); pullErr != nil {
		t.Fatalf("pre-pull %s: %v", image, pullErr)
	}

	c, err := tcpostgres.Run(ctx, image, tcpostgres.WithDatabase("snatcharr"),
		tcpostgres.WithUsername("test"), tcpostgres.WithPassword("test"),
		tcpostgres.BasicWaitStrategies(), storetest.QuickInit(), network.WithNetwork([]string{alias}, nw))
	t.Cleanup(func() {
		if c == nil {
			return
		}
		// Terminate directly: TerminateContainer bounds only the stop and removes the container
		// under context.Background, the call that hung until the package timeout.
		ctx, cancel := context.WithTimeout(context.Background(), upgradeTerminateTimeout)
		defer cancel()
		if termErr := c.Terminate(ctx, testcontainers.StopTimeout(0)); termErr != nil {
			t.Errorf("terminate %s: %v", image, termErr)
		}
	})
	if err != nil {
		t.Fatalf("run %s: %v", image, err)
	}
	return c
}

// openStore connects to the container's database and checks the server's major version.
func openStore(t *testing.T, c *tcpostgres.PostgresContainer, wantMajor int) *store.Store {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), upgradeOpTimeout)
	defer cancel()
	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	var num int
	if err := pool.QueryRow(ctx, "SELECT current_setting('server_version_num')::int").Scan(&num); err != nil {
		t.Fatalf("server version: %v", err)
	}
	if got := num / 10000; got != wantMajor {
		t.Fatalf("server major = %d, want %d", got, wantMajor)
	}
	return store.New(pool)
}

// dumpRestore runs the documented command inside the target container: its own pg_dump
// reads the source over the network and pg_restore writes into the target.
func dumpRestore(t *testing.T, target *tcpostgres.PostgresContainer) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), upgradeOpTimeout)
	defer cancel()
	script := fmt.Sprintf("set -o pipefail; pg_dump --format=custom --dbname=%q | "+
		"pg_restore --no-owner --exit-on-error --dbname=%q", sourceDSN, targetDSN)
	code, out, err := target.Exec(ctx, []string{"bash", "-c", script}, tcexec.Multiplexed())
	if err != nil {
		t.Fatalf("exec dump and restore: %v", err)
	}
	if code != 0 {
		msg, _ := io.ReadAll(out)
		t.Fatalf("dump and restore exit %d: %s", code, msg)
	}
}

// An installed release on Postgres 17, with a full dataset, moves to Postgres 18 by dump and
// restore; the migrations then bring it to the latest schema and every row reads back.
func TestPostgresMajorUpgradeByDumpRestore(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("integration test: needs Docker; run without -short")
	}
	nwCtx, cancel := context.WithTimeout(t.Context(), upgradeOpTimeout)
	defer cancel()
	nw, err := network.New(nwCtx)
	testcontainers.CleanupNetwork(t, nw)
	if err != nil {
		t.Fatalf("network: %v", err)
	}

	source := openStore(t, runPostgres(t, nw, storetest.PreviousMajorImage, "pg-source"), storetest.PreviousMajor)
	sm := storetest.Migrator(t, source)
	if err := sm.Steps(previousRelease); err != nil {
		t.Fatalf("migrate the source to %d: %v", previousRelease, err)
	}
	applyFixture(t, source, fixturePath)

	targetC := runPostgres(t, nw, storetest.Image, "pg-target")
	dumpRestore(t, targetC)
	target := openStore(t, targetC, storetest.Major)
	tm := storetest.Migrator(t, target)
	if v := version(t, tm); v != previousRelease {
		t.Fatalf("restored version = %d, want %d", v, previousRelease)
	}
	if err := tm.Up(); err != nil {
		t.Fatalf("Up() on the restored database: %v", err)
	}

	ctx := t.Context()
	q := sqlcgen.New(target.Pool())
	assertInstancesAndPolicies(t, q, ctx)
	assertRuns(t, q, ctx, fixtureRunID)
	assertEventsAndProcessed(t, q, ctx, fixtureSonarrID)
}
