// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package recovery_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

func TestDumpRestoreRoundTrip(t *testing.T) {
	t.Parallel()

	src := storetest.New(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	now := time.Now().Truncate(time.Microsecond).UTC()
	instanceID := storetest.Instance(t, src, now)

	policy, err := src.Q().EnsureDefaultPolicy(ctx, sqlcgen.EnsureDefaultPolicyParams{
		InstanceID: instanceID,
		UpdatedAt:  now,
	})
	if err != nil {
		t.Fatalf("ensure default policy: %v", err)
	}

	tables := []string{"instances", "snatch_policies"}
	dumps := make(map[string][]byte, len(tables))
	for _, tbl := range tables {
		var buf bytes.Buffer
		conn, connErr := src.Pool().Acquire(ctx)
		if connErr != nil {
			t.Fatalf("acquire conn for dump: %v", connErr)
		}
		_, copyErr := conn.Conn().PgConn().CopyTo(ctx, &buf, fmt.Sprintf("COPY %s TO STDOUT WITH (FORMAT binary)", pgx.Identifier{tbl}.Sanitize()))
		conn.Release()
		if copyErr != nil {
			t.Fatalf("COPY %s TO STDOUT: %v", tbl, copyErr)
		}
		if buf.Len() == 0 {
			t.Fatalf("COPY %s produced zero bytes", tbl)
		}
		dumps[tbl] = buf.Bytes()
	}

	dstPool := createFreshDB(t, src.Pool())
	applyMigrations(t, dstPool)

	for _, tbl := range tables {
		conn, connErr := dstPool.Acquire(ctx)
		if connErr != nil {
			t.Fatalf("acquire conn for restore: %v", connErr)
		}
		_, copyErr := conn.Conn().PgConn().CopyFrom(ctx, bytes.NewReader(dumps[tbl]), fmt.Sprintf("COPY %s FROM STDIN WITH (FORMAT binary)", pgx.Identifier{tbl}.Sanitize()))
		conn.Release()
		if copyErr != nil {
			t.Fatalf("COPY %s FROM STDIN: %v", tbl, copyErr)
		}
	}

	dstStore := store.New(dstPool)
	inst, err := dstStore.Q().GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("read back instance: %v", err)
	}
	if inst.Name != "storetest-"+instanceID.String() {
		t.Errorf("instance name mismatch: got %q", inst.Name)
	}

	restored, err := dstStore.Q().GetPolicy(ctx, instanceID)
	if err != nil {
		t.Fatalf("read back policy: %v", err)
	}
	if restored.MissingPerCycle != policy.MissingPerCycle {
		t.Errorf("policy missing_per_cycle: got %d, want %d", restored.MissingPerCycle, policy.MissingPerCycle)
	}
	if restored.HourlyCap != policy.HourlyCap {
		t.Errorf("policy hourly_cap: got %d, want %d", restored.HourlyCap, policy.HourlyCap)
	}
	if restored.SonarrMissingMode != policy.SonarrMissingMode {
		t.Errorf("policy sonarr_missing_mode: got %q, want %q", restored.SonarrMissingMode, policy.SonarrMissingMode)
	}
}

func TestCorruptedDumpRefused(t *testing.T) {
	t.Parallel()

	src := storetest.New(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	now := time.Now().Truncate(time.Microsecond).UTC()
	storetest.Instance(t, src, now)

	var buf bytes.Buffer
	conn, err := src.Pool().Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire conn: %v", err)
	}
	_, err = conn.Conn().PgConn().CopyTo(ctx, &buf, "COPY instances TO STDOUT WITH (FORMAT binary)")
	conn.Release()
	if err != nil {
		t.Fatalf("COPY TO: %v", err)
	}

	corrupted := make([]byte, buf.Len())
	copy(corrupted, buf.Bytes())
	if len(corrupted) > 20 {
		corrupted[15] ^= 0xFF
		corrupted[16] ^= 0xFF
		corrupted[17] ^= 0xFF
		corrupted[18] ^= 0xFF
	}

	dstPool := createFreshDB(t, src.Pool())
	applyMigrations(t, dstPool)

	dstConn, err := dstPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire conn for restore: %v", err)
	}
	_, err = dstConn.Conn().PgConn().CopyFrom(ctx, bytes.NewReader(corrupted), "COPY instances FROM STDIN WITH (FORMAT binary)")
	dstConn.Release()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "22P04" {
		t.Fatalf("expected COPY format error (22P04) for corrupted header, got: %v", err)
	}
}

func createFreshDB(t *testing.T, srcPool *pgxpool.Pool) *pgxpool.Pool {
	t.Helper()

	cc := srcPool.Config().ConnConfig
	adminDSN := (&url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(cc.User, cc.Password),
		Host:     net.JoinHostPort(cc.Host, strconv.Itoa(int(cc.Port))),
		Path:     "/postgres",
		RawQuery: "sslmode=disable",
	}).String()

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	adminConn, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		t.Fatalf("connect admin: %v", err)
	}

	dbName := "recovery_" + uuid.New().String()[:8]
	_, err = adminConn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{dbName}.Sanitize())
	if err != nil {
		t.Fatalf("create recovery db: %v", err)
	}
	if closeErr := adminConn.Close(ctx); closeErr != nil {
		t.Logf("close admin: %v", closeErr)
	}
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer dropCancel()
		dc, dcErr := pgx.Connect(dropCtx, adminDSN)
		if dcErr != nil {
			t.Logf("cleanup: connect admin: %v", dcErr)
			return
		}
		_, _ = dc.Exec(dropCtx, "DROP DATABASE IF EXISTS "+pgx.Identifier{dbName}.Sanitize()+" WITH (FORCE)")
		_ = dc.Close(dropCtx)
	})

	dstDSN := (&url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(cc.User, cc.Password),
		Host:     net.JoinHostPort(cc.Host, strconv.Itoa(int(cc.Port))),
		Path:     "/" + dbName,
		RawQuery: "sslmode=disable",
	}).String()

	pool, err := pgxpool.New(ctx, dstDSN)
	if err != nil {
		t.Fatalf("open recovery pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func applyMigrations(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	st := store.New(pool)
	m := storetest.Migrator(t, st)
	if err := m.Up(); err != nil {
		t.Fatalf("migrate recovery db: %v", err)
	}
}
