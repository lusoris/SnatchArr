// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package snatch

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

type memoryFixture struct {
	mem      *Memory
	st       *store.Store
	clk      *clockwork.FakeClock
	instance uuid.UUID
}

func newMemoryFixture(t *testing.T) memoryFixture {
	t.Helper()
	st := storetest.New(t)
	clk := clock.NewFake()
	return memoryFixture{mem: NewMemory(st, clk), st: st, clk: clk, instance: storetest.Instance(t, st, clk.Now())}
}

func (f memoryFixture) mark(t *testing.T, base, maxRest time.Duration, ids ...int64) {
	t.Helper()
	if err := f.mem.Mark(t.Context(), f.instance, domain.SnatchMissing, "episode", ids, base, maxRest); err != nil {
		t.Fatalf("Mark() = %v", err)
	}
}

// unprocessed filters ids 7 and 8 for the fixture's instance, missing kind and episodes.
func (f memoryFixture) unprocessed(t *testing.T) []int64 {
	t.Helper()
	out, err := f.mem.FilterUnprocessed(t.Context(), f.instance, domain.SnatchMissing, "episode", []int64{7, 8})
	if err != nil {
		t.Fatalf("FilterUnprocessed() = %v", err)
	}
	slices.Sort(out)
	return out
}

// Every further snatch of an item doubles its rest up to the cap; the item is filtered out
// until one second before the rest ends and is back exactly when it ends.
func TestAfterglowDoublesUpToCap(t *testing.T) {
	t.Parallel()
	f := newMemoryFixture(t)
	const base, maxRest = time.Hour, 3 * time.Hour
	rests := []time.Duration{time.Hour, 2 * time.Hour, 3 * time.Hour, 3 * time.Hour} // 4 h capped to 3 h
	for i, rest := range rests {
		f.mark(t, base, maxRest, 7)
		f.clk.Advance(rest - time.Second)
		if got := f.unprocessed(t); !slices.Equal(got, []int64{8}) {
			t.Fatalf("snatch %d: unprocessed 1 s before a %v rest ends = %v, want [8]", i+1, rest, got)
		}
		f.clk.Advance(time.Second)
		if got := f.unprocessed(t); !slices.Equal(got, []int64{7, 8}) {
			t.Fatalf("snatch %d: unprocessed when a %v rest ends = %v, want [7 8]", i+1, rest, got)
		}
	}
	attempts, err := f.st.Q().ProcessedAttempts(t.Context(), sqlcgen.ProcessedAttemptsParams{
		InstanceID: f.instance, Kind: string(domain.SnatchMissing), EntityType: "episode", EntityID: 7,
	})
	if err != nil || int(attempts) != len(rests) {
		t.Fatalf("ProcessedAttempts() = %d, %v; want %d", attempts, err, len(rests))
	}
}

// Boundary: a cap below the base rest is raised to the base, so a repeat snatch (the only
// one the cap applies to) never rests shorter than the first.
func TestAfterglowCapBelowBaseKeepsBase(t *testing.T) {
	t.Parallel()
	f := newMemoryFixture(t)
	for snatch := 1; snatch <= 2; snatch++ {
		f.mark(t, 2*time.Hour, time.Hour, 7)
		f.clk.Advance(2*time.Hour - time.Second)
		if got := f.unprocessed(t); !slices.Equal(got, []int64{8}) {
			t.Fatalf("snatch %d: unprocessed before the 2 h base rest ends = %v, want [8]", snatch, got)
		}
		f.clk.Advance(time.Second)
	}
}

// Negative: an afterglow only hides the same instance, kind, entity type and id.
func TestAfterglowIsScoped(t *testing.T) {
	t.Parallel()
	f := newMemoryFixture(t)
	f.mark(t, time.Hour, time.Hour, 7)
	other := storetest.Instance(t, f.st, f.clk.Now())
	for _, tc := range []struct {
		name       string
		instance   uuid.UUID
		kind       domain.SnatchKind
		entityType string
	}{
		{"other instance", other, domain.SnatchMissing, "episode"},
		{"other kind", f.instance, domain.SnatchUpgrade, "episode"},
		{"other entity type", f.instance, domain.SnatchMissing, "movie"},
	} {
		got, err := f.mem.FilterUnprocessed(t.Context(), tc.instance, tc.kind, tc.entityType, []int64{7})
		if err != nil || !slices.Equal(got, []int64{7}) {
			t.Errorf("%s: FilterUnprocessed() = %v, %v; want [7]", tc.name, got, err)
		}
	}
}

// Boundary: an empty filter needs no query; more than MaxFilterBatch ids are refused.
func TestFilterUnprocessedBatchBounds(t *testing.T) {
	t.Parallel()
	f := newMemoryFixture(t)
	if got, err := f.mem.FilterUnprocessed(t.Context(), f.instance, domain.SnatchMissing, "episode", nil); err != nil || len(got) != 0 {
		t.Fatalf("FilterUnprocessed(nil) = %v, %v; want empty", got, err)
	}
	ids := make([]int64, MaxFilterBatch+1)
	if _, err := f.mem.FilterUnprocessed(t.Context(), f.instance, domain.SnatchMissing, "episode", ids); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("FilterUnprocessed(%d ids) = %v, want ErrInvalid", len(ids), err)
	}
	if _, err := f.mem.FilterUnprocessed(t.Context(), f.instance, domain.SnatchMissing, "episode", ids[:MaxFilterBatch]); err != nil {
		t.Fatalf("FilterUnprocessed(%d ids) = %v, want nil", MaxFilterBatch, err)
	}
}
