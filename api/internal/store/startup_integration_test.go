// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package store_test

import (
	"context"
	"testing"

	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	dbmigrate "github.com/golusoris/golusoris/db/migrate"

	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

// On an empty database the schema exists before any store user starts (#124). The graph
// mirrors the app: a child module appends a start hook that reads a table (as leaderx does
// for the leader loops), and the migrator is first asked for by a root invoke, which fx runs
// after every child module's invokes.
func TestStoreUsersStartAfterMigrations(t *testing.T) {
	t.Parallel()
	st := storetest.New(t)
	if err := storetest.Migrator(t, st).Down(); err != nil {
		t.Fatalf("empty the database: %v", err)
	}
	m := storetest.Migrator(t, st)
	var started bool
	var loopErr error
	app := fxtest.New(t,
		fx.Supply(st.Pool()),
		fx.Provide(func(lc fx.Lifecycle) *dbmigrate.Migrator {
			lc.Append(fx.Hook{OnStart: func(context.Context) error { return m.Up() }})
			return m
		}),
		store.Module,
		fx.Module("loops", fx.Invoke(func(lc fx.Lifecycle, s *store.Store) {
			lc.Append(fx.Hook{OnStart: func(ctx context.Context) error {
				started = true
				_, loopErr = s.Pool().Exec(ctx, "SELECT 1 FROM instances LIMIT 1")
				return nil
			}})
		})),
		fx.Invoke(func(*dbmigrate.Migrator) {}),
	)
	app.RequireStart()
	app.RequireStop()
	if !started || loopErr != nil {
		t.Fatalf("store user on start: started %v, %v; want the migrated instances table", started, loopErr)
	}
}
