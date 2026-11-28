// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package store_test

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

func newSingleConnStore(t *testing.T, base *store.Store) *store.Store {
	t.Helper()
	cc := base.Pool().Config().ConnConfig
	dsn := (&url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(cc.User, cc.Password),
		Host:     net.JoinHostPort(cc.Host, strconv.Itoa(int(cc.Port))),
		Path:     "/" + cc.Database,
		RawQuery: "sslmode=disable",
	}).String()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse pool config: %v", err)
	}
	cfg.MaxConns = 1
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open single-conn pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return store.New(pool)
}

func querySnapshotPage(ctx context.Context, q *sqlcgen.Queries) error {
	_, err := q.ListLibraryProcessingRulesKeyset(ctx, sqlcgen.ListLibraryProcessingRulesKeysetParams{
		AfterCreatedAt: time.Unix(0, 0).UTC(),
		PageLimit:      1,
	})
	return err
}

func TestSnapshotReadTxAbnormalExitReleasesConnection(t *testing.T) {
	t.Parallel()
	base := storetest.New(t)
	st := newSingleConnStore(t, base)

	const panicPayload = "snapshot callback panic"
	var recovered any
	func() {
		defer func() {
			recovered = recover()
		}()
		_ = st.SnapshotReadTx(t.Context(), func(q *sqlcgen.Queries) error {
			if err := querySnapshotPage(t.Context(), q); err != nil {
				t.Errorf("query inside snapshot tx: %v", err)
			}
			panic(panicPayload)
		})
	}()
	if recovered != panicPayload {
		t.Fatalf("recovered = %v, want %q", recovered, panicPayload)
	}

	// With MaxConns=1, a leaked transaction blocks subsequent acquisitions until timeout.
	followCtx, followCancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer followCancel()
	if err := st.SnapshotReadTx(followCtx, func(q *sqlcgen.Queries) error {
		return querySnapshotPage(followCtx, q)
	}); err != nil {
		t.Fatalf("follow-up SnapshotReadTx after panic failed (connection leaked): %v", err)
	}

	sentinel := errors.New("callback failed")
	canceledCtx, cancel := context.WithCancel(t.Context())
	err := st.SnapshotReadTx(canceledCtx, func(q *sqlcgen.Queries) error {
		if qErr := querySnapshotPage(t.Context(), q); qErr != nil {
			return qErr
		}
		cancel()
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("SnapshotReadTx callback error = %v, want %v", err, sentinel)
	}

	verifyCtx, verifyCancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer verifyCancel()
	if err := st.SnapshotReadTx(verifyCtx, func(q *sqlcgen.Queries) error {
		return querySnapshotPage(verifyCtx, q)
	}); err != nil {
		t.Fatalf("follow-up SnapshotReadTx after canceled callback error failed: %v", err)
	}
}
