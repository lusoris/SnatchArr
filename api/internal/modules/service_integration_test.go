// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package modules_test

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"testing"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/modules"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

func newService(t *testing.T) *modules.Service {
	t.Helper()
	st := storetest.New(t)
	clk := clock.NewFake()
	reg, err := modules.DefaultRegistry()
	if err != nil {
		t.Fatalf("DefaultRegistry() error = %v", err)
	}
	return modules.New(st, clk, reg)
}

func TestEnsureIdempotent(t *testing.T) {
	t.Parallel()
	svc := newService(t)
	ctx := t.Context()

	for range 3 {
		if err := svc.Ensure(ctx); err != nil {
			t.Fatalf("Ensure() error = %v", err)
		}
	}

	list, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list) < 2 {
		t.Fatalf("List() returned %d modules, want at least 2", len(list))
	}
}

func TestSetEnabledUnknownID(t *testing.T) {
	t.Parallel()
	svc := newService(t)
	ctx := t.Context()

	err := svc.SetEnabled(ctx, "nonexistent", true)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("SetEnabled(nonexistent) error = %v, want ErrNotFound", err)
	}

	err = svc.SetConfig(ctx, "nonexistent", json.RawMessage("{}"))
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("SetConfig(nonexistent) error = %v, want ErrNotFound", err)
	}

	_, err = svc.IsEnabled(ctx, "nonexistent")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("IsEnabled(nonexistent) error = %v, want ErrNotFound", err)
	}
}

func TestBoundaryDataOnDisable(t *testing.T) {
	t.Parallel()
	svc := newService(t)
	ctx := t.Context()

	if err := svc.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	cfg := json.RawMessage(`{"sync_interval_minutes":15}`)
	if err := svc.SetConfig(ctx, domain.ModuleWatchAware, cfg); err != nil {
		t.Fatalf("SetConfig(%s) error = %v", domain.ModuleWatchAware, err)
	}

	// Switch on
	if err := svc.SetEnabled(ctx, domain.ModuleWatchAware, true); err != nil {
		t.Fatalf("SetEnabled(%s, true) error = %v", domain.ModuleWatchAware, err)
	}
	on, err := svc.IsEnabled(ctx, domain.ModuleWatchAware)
	if err != nil || !on {
		t.Fatalf("IsEnabled(%s) = %v, %v; want true", domain.ModuleWatchAware, on, err)
	}

	// Switch off
	if err = svc.SetEnabled(ctx, domain.ModuleWatchAware, false); err != nil {
		t.Fatalf("SetEnabled(%s, false) error = %v", domain.ModuleWatchAware, err)
	}
	off, err := svc.IsEnabled(ctx, domain.ModuleWatchAware)
	if err != nil || off {
		t.Fatalf("IsEnabled(%s) = %v, %v; want false", domain.ModuleWatchAware, off, err)
	}

	// Switch on again
	if err = svc.SetEnabled(ctx, domain.ModuleWatchAware, true); err != nil {
		t.Fatalf("SetEnabled(%s, true) error = %v", domain.ModuleWatchAware, err)
	}

	// Verify same config and row still present (snatcharr-adr25-data-on-disable)
	mod, err := svc.Get(ctx, domain.ModuleWatchAware)
	if err != nil {
		t.Fatalf("Get(%s) error = %v", domain.ModuleWatchAware, err)
	}
	if !mod.Enabled {
		t.Errorf("Get(%s).Enabled = false, want true", domain.ModuleWatchAware)
	}
	var gotMap, wantMap map[string]any
	if err := json.Unmarshal(mod.Config, &gotMap); err != nil {
		t.Fatalf("unmarshal got config: %v", err)
	}
	if err := json.Unmarshal(cfg, &wantMap); err != nil {
		t.Fatalf("unmarshal want config: %v", err)
	}
	if !maps.Equal(gotMap, wantMap) {
		t.Errorf("Get(%s).Config = %v, want %v", domain.ModuleWatchAware, gotMap, wantMap)
	}
}
