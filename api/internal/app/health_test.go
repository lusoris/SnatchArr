// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package app

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx/fxtest"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/k8s/health"
	"github.com/golusoris/golusoris/observability/statuspage"

	"github.com/lusoris/SnatchArr/api/internal/leaderx"
)

// Readiness includes the leader gate: with the gate not running, /readyz reports the
// "leader" check down whatever Postgres says. Before this check existed, a replica whose
// periodic loops had stopped stayed ready.
func TestReadinessIncludesLeaderGate(t *testing.T) {
	t.Parallel()
	// Never connects: the pool dials lazily and the postgres check's result is not asserted.
	pool, err := pgxpool.New(context.Background(), "postgres://snatcharr@127.0.0.1:1/snatcharr?connect_timeout=1")
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	clk := clock.NewFake()
	reg := statuspage.NewRegistry(clk)
	gate := leaderx.New(clk, slog.New(slog.DiscardHandler))
	registerHealthChecks(reg, pool, health.NewStartupGate(), gate, fxtest.NewLifecycle(t))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var found bool
	for _, r := range reg.RunTagged(ctx, health.TagReadiness) {
		if r.Name != "leader" {
			continue
		}
		found = true
		if r.Status != statuspage.StatusDown || r.Message != leaderx.ErrNotRunning.Error() {
			t.Fatalf("leader check = %s %q, want down %q", r.Status, r.Message, leaderx.ErrNotRunning)
		}
	}
	if !found {
		t.Fatal("readiness has no leader check")
	}
}
