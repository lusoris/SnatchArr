// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package policies_test

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/policies"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

type fixture struct {
	svc        *policies.Service
	st         *store.Store
	instanceID uuid.UUID
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	st := storetest.New(t)
	clk := clock.NewFake()
	return fixture{svc: policies.New(st, clk), st: st, instanceID: storetest.Instance(t, st, clk.Now())}
}

// withoutStamp drops the update time so policies compare by their settings alone.
func withoutStamp(p domain.Policy) domain.Policy {
	p.UpdatedAt = time.Time{}
	return p
}

func TestGetCreatesTheDefaultPolicy(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	for range 2 {
		got, err := f.svc.Get(t.Context(), f.instanceID)
		if err != nil {
			t.Fatalf("Get() = %v", err)
		}
		if want := domain.DefaultPolicy(f.instanceID); !reflect.DeepEqual(withoutStamp(got), want) {
			t.Fatalf("Get() = %+v, want the defaults %+v", got, want)
		}
	}
}

func TestUpdateReplacesThePolicy(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	p := domain.DefaultPolicy(f.instanceID)
	p.HourlyCap = 500
	p.Selection = domain.SelectionSequential
	p.PageSize = 10
	p.CycleInterval = 7 * 24 * time.Hour
	p.MaxQueueSize = 100000
	p.MonitoredOnly = false
	p.IncludeTags, p.ExcludeTags = []int32{1, 2}, []int32{3}
	p.IncludeQualityProfiles, p.ExcludeQualityProfiles = []int32{4}, []int32{5}
	p.ReleaseDelay = 8760 * time.Hour
	updated, err := f.svc.Update(t.Context(), p)
	if err != nil {
		t.Fatalf("Update() = %v", err)
	}
	got, err := f.svc.Get(t.Context(), f.instanceID)
	if err != nil || !reflect.DeepEqual(withoutStamp(got), p) || !reflect.DeepEqual(withoutStamp(updated), p) {
		t.Errorf("Update() = %+v, then Get() = %+v, %v; want %+v", updated, got, err, p)
	}
}

func TestPolicyRefusals(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	// The defaults exist before the refusals run, so each can compare against them.
	if _, err := f.svc.Get(t.Context(), f.instanceID); err != nil {
		t.Fatalf("Get() = %v", err)
	}
	// update builds a default policy for instanceID, applies change and sends it.
	update := func(instanceID uuid.UUID, change func(*domain.Policy)) func(context.Context) error {
		return func(ctx context.Context) error {
			p := domain.DefaultPolicy(instanceID)
			change(&p)
			_, err := f.svc.Update(ctx, p)
			return err
		}
	}
	keep := func(*domain.Policy) {}
	cases := []struct {
		name string
		call func(context.Context) error
		want error
	}{
		{"negative get of an unknown instance", func(ctx context.Context) error { _, err := f.svc.Get(ctx, uuid.New()); return err }, domain.ErrNotFound},
		{"negative update of an unknown instance", update(uuid.New(), keep), domain.ErrNotFound},
		{"boundary hourly cap one above the limit", update(f.instanceID, func(p *domain.Policy) { p.HourlyCap = 501 }), domain.ErrInvalid},
		// The database has no upper bound on these two; only Validate refuses them.
		{"boundary cycle interval one second above a week", update(f.instanceID, func(p *domain.Policy) { p.CycleInterval = 7*24*time.Hour + time.Second }), domain.ErrInvalid},
		{"boundary queue size one above the limit", update(f.instanceID, func(p *domain.Policy) { p.MaxQueueSize = 100001 }), domain.ErrInvalid},
		{"negative tag both included and excluded", update(f.instanceID, func(p *domain.Policy) { p.IncludeTags, p.ExcludeTags = []int32{1}, []int32{1} }), domain.ErrInvalid},
		{"boundary release delay one hour above a year", update(f.instanceID, func(p *domain.Policy) { p.ReleaseDelay = 8761 * time.Hour }), domain.ErrInvalid},
		{"negative invalid policy of an unknown instance is refused before the lookup", update(uuid.New(), func(p *domain.Policy) { p.PageSize = 9 }), domain.ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.call(t.Context()); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
			got, err := f.svc.Get(t.Context(), f.instanceID)
			if want := domain.DefaultPolicy(f.instanceID); err != nil || !reflect.DeepEqual(withoutStamp(got), want) {
				t.Errorf("Get() after the refused update = %+v, %v; want the defaults %+v", got, err, want)
			}
		})
	}
}

func TestSaveCursorKeepsEachKindApart(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	if _, err := f.svc.Get(t.Context(), f.instanceID); err != nil {
		t.Fatalf("Get() = %v", err)
	}
	longest := strings.Repeat("c", 256)
	if err := f.svc.SaveCursor(t.Context(), f.instanceID, domain.SnatchMissing, longest); err != nil {
		t.Fatalf("SaveCursor(missing, 256 chars) = %v", err)
	}
	if err := f.svc.SaveCursor(t.Context(), f.instanceID, domain.SnatchUpgrade, "u1"); err != nil {
		t.Fatalf("SaveCursor(upgrade) = %v", err)
	}
	if err := f.svc.SaveCursor(t.Context(), f.instanceID, domain.SnatchMissing, longest+"c"); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("SaveCursor(257 chars) = %v, want %v", err, domain.ErrInvalid)
	}
	row, err := f.st.Q().GetPolicy(t.Context(), f.instanceID)
	if err != nil || row.CursorMissing != longest || row.CursorUpgrade != "u1" {
		t.Errorf("GetPolicy() cursors = %q, %q, %v; want the 256-char missing cursor and u1", row.CursorMissing, row.CursorUpgrade, err)
	}
}
