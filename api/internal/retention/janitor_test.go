// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package retention

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/auth"
	"github.com/lusoris/SnatchArr/api/internal/leaderx"
	"github.com/lusoris/SnatchArr/api/internal/settings"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
)

// fakePurge returns the scripted batch sizes in order and counts the calls.
type fakePurge struct {
	sizes []int64
	errAt int // 1-based call that fails; 0 = never
	calls int
}

func (f *fakePurge) purge(_ context.Context, limit int32) (int64, error) {
	f.calls++
	if f.calls == f.errAt {
		return 0, errors.New("boom")
	}
	if f.calls <= len(f.sizes) {
		return f.sizes[f.calls-1], nil
	}
	return int64(limit), nil
}

// Positive: drain repeats full batches and stops after the first short one.
func TestDrainStopsAfterShortBatch(t *testing.T) {
	t.Parallel()
	f := &fakePurge{sizes: []int64{10, 10, 3}}
	n, err := drain(t.Context(), 10, f.purge)
	if err != nil || n != 23 || f.calls != 3 {
		t.Fatalf("drain() = %d, %v after %d calls; want 23, nil after 3", n, err, f.calls)
	}
}

// Negative: a failing batch stops the table and keeps the count of what already went.
func TestDrainStopsOnError(t *testing.T) {
	t.Parallel()
	f := &fakePurge{sizes: []int64{10}, errAt: 2}
	n, err := drain(t.Context(), 10, f.purge)
	if err == nil || n != 10 || f.calls != 2 {
		t.Fatalf("drain() = %d, %v after %d calls; want 10, error after 2", n, err, f.calls)
	}
}

// Boundary: a table that never runs dry is cut off after MaxBatches batches.
func TestDrainIsBounded(t *testing.T) {
	t.Parallel()
	f := &fakePurge{}
	n, err := drain(t.Context(), 10, f.purge)
	if err != nil || f.calls != MaxBatches || n != int64(MaxBatches)*10 {
		t.Fatalf("drain() = %d, %v after %d calls; want %d, nil after %d", n, err, f.calls, MaxBatches*10, MaxBatches)
	}
}

// The module registers the janitor in the leader_loops group and never starts it itself,
// so only the elected replica runs it.
func TestModuleRegistersLeaderLoop(t *testing.T) {
	t.Parallel()
	var loops []leaderx.Loop
	app := fxtest.New(t,
		fx.Supply((*settings.Service)(nil), (*snatch.Recorder)(nil), (*snatch.Runs)(nil), (*snatch.Memory)(nil),
			(*snatch.Budget)(nil), (*auth.PgSessionStore)(nil), slog.New(slog.DiscardHandler)),
		fx.Provide(func() clock.Clock { return clock.NewFake() }),
		Module,
		fx.Invoke(fx.Annotate(func(l []leaderx.Loop) { loops = l }, fx.ParamTags(`group:"leader_loops"`))),
	)
	app.RequireStart()
	app.RequireStop()
	if len(loops) != 1 {
		t.Fatalf("leader_loops has %d loops, want 1", len(loops))
	}
	j, ok := loops[0].(*Janitor)
	if !ok {
		t.Fatalf("leader loop is %T, want *Janitor", loops[0])
	}
	if j.done != nil {
		t.Fatal("the janitor was started outside the leader gate")
	}
}
