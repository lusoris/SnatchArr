// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestResolveSyncInterval(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  json.RawMessage
		want time.Duration
	}{
		{
			name: "empty json",
			raw:  json.RawMessage("{}"),
			want: 360 * time.Minute,
		},
		{
			name: "custom 120 minutes",
			raw:  json.RawMessage(`{"sync_interval_minutes":120}`),
			want: 120 * time.Minute,
		},
		{
			name: "below minimum falls back to default",
			raw:  json.RawMessage(`{"sync_interval_minutes":5}`),
			want: 360 * time.Minute,
		},
		{
			name: "above maximum falls back to default",
			raw:  json.RawMessage(`{"sync_interval_minutes":2000}`),
			want: 360 * time.Minute,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := resolveSyncInterval(tc.raw)
			if got != tc.want {
				t.Errorf("resolveSyncInterval(%s) = %v, want %v", string(tc.raw), got, tc.want)
			}
		})
	}
}

type staticModuleChecker struct {
	enabled bool
	cfg     domain.Module
}

func (m *staticModuleChecker) IsEnabled(_ context.Context, _ string) (bool, error) {
	return m.enabled, nil
}

func (m *staticModuleChecker) Get(_ context.Context, _ string) (domain.Module, error) {
	return m.cfg, nil
}

func TestTicker_ModuleDisabled_ZeroHTTPRequests(t *testing.T) {
	t.Parallel()
	var serverHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		serverHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	clk := clock.NewFake()
	enc := testEncryptor(t)
	ids := id.New()
	logger := slog.New(slog.DiscardHandler)

	checker := &staticModuleChecker{enabled: false}
	svc := NewServiceWithChecker(nil, enc, clk, ids, checker, logger)
	ctx := context.Background()

	// shouldTick must return false because module is disabled
	due, err := svc.shouldTick(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if due {
		t.Fatalf("expected shouldTick to return false when module is disabled, got %v", due)
	}

	// Server hits must be strictly 0
	if hits := serverHits.Load(); hits != 0 {
		t.Fatalf("expected 0 HTTP hits when module is disabled, got %d", hits)
	}
}

func TestScanTicker_DisabledOrUnsetDir(t *testing.T) {
	t.Parallel()
	clk := clock.NewFake()
	enc := testEncryptor(t)
	ids := id.New()
	logger := slog.New(slog.DiscardHandler)
	ctx := context.Background()

	t.Run("module disabled -> false", func(t *testing.T) {
		t.Parallel()
		checker := &staticModuleChecker{enabled: false}
		svc := NewServiceWithChecker(nil, enc, clk, ids, checker, logger)
		due, err := svc.shouldScanTick(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if due {
			t.Fatal("expected shouldScanTick to return false when disabled")
		}
	})

	t.Run("image_set_dir unset -> false", func(t *testing.T) {
		t.Parallel()
		checker := &staticModuleChecker{
			enabled: true,
			cfg: domain.Module{
				ID:      domain.ModuleAdult,
				Enabled: true,
				Config:  json.RawMessage(`{"sync_interval_minutes":15}`),
			},
		}
		svc := NewServiceWithChecker(nil, enc, clk, ids, checker, logger)
		due, err := svc.shouldScanTick(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if due {
			t.Fatal("expected shouldScanTick to return false when image_set_dir unset")
		}
	})

	t.Run("image_set_dir set and interval elapsed -> true then false", func(t *testing.T) {
		t.Parallel()
		checker := &staticModuleChecker{
			enabled: true,
			cfg: domain.Module{
				ID:      domain.ModuleAdult,
				Enabled: true,
				Config:  json.RawMessage(`{"image_set_dir":"/var/media/imagesets","sync_interval_minutes":15}`),
			},
		}
		svc := NewServiceWithChecker(nil, enc, clk, ids, checker, logger)
		due, err := svc.shouldScanTick(ctx)
		if err != nil || !due {
			t.Fatalf("expected shouldScanTick to return true, got due=%v, err=%v", due, err)
		}
		due2, err := svc.shouldScanTick(ctx)
		if err != nil || due2 {
			t.Fatalf("expected immediate second call to return false, got due=%v, err=%v", due2, err)
		}
	})
}
