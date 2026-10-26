// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/leader/gate"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/metrics"
	"github.com/lusoris/SnatchArr/api/internal/modules"
)

const (
	// DefaultSyncIntervalMinutes is the default interval between sync passes (6 hours = 360 minutes).
	DefaultSyncIntervalMinutes = 360
	// BaseTickPeriod is the periodic evaluation frequency of the leader loop ticker.
	BaseTickPeriod = 1 * time.Minute
)

// Ticker periodically synchronizes stashbox favourites while this replica leads.
type Ticker struct {
	*gate.Periodic
}

// NewTicker constructs the adult leader loop ticker.
func NewTicker(svc *Service, clk clock.Clock, m *metrics.Metrics) *Ticker {
	return &Ticker{gate.NewPeriodic("adult: sync", BaseTickPeriod, clk, m, svc.Tick)}
}

// resolveSyncInterval extracts the configured sync interval, falling back to 360 minutes.
func resolveSyncInterval(raw json.RawMessage) time.Duration {
	if len(raw) == 0 {
		return DefaultSyncIntervalMinutes * time.Minute
	}
	var cfg struct {
		SyncIntervalMinutes *int `json:"sync_interval_minutes"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil || cfg.SyncIntervalMinutes == nil {
		return DefaultSyncIntervalMinutes * time.Minute
	}
	val := *cfg.SyncIntervalMinutes
	if val < modules.MinAdultSyncIntervalMinutes || val > modules.MaxAdultSyncIntervalMinutes {
		return DefaultSyncIntervalMinutes * time.Minute
	}
	return time.Duration(val) * time.Minute
}

// shouldTick verifies module enablement and interval elapsed.
func (s *Service) shouldTick(ctx context.Context) (bool, error) {
	enabled, err := s.mod.IsEnabled(ctx, domain.ModuleAdult)
	if err != nil {
		return false, fmt.Errorf("adult: check module enabled: %w", err)
	}
	if !enabled {
		s.logger.DebugContext(ctx, "adult: module disabled; skipping tick")
		return false, nil
	}

	mod, err := s.mod.Get(ctx, domain.ModuleAdult)
	if err != nil {
		return false, fmt.Errorf("adult: get module config: %w", err)
	}
	interval := resolveSyncInterval(mod.Config)

	now := s.clk.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.lastPass.IsZero() && now.Sub(s.lastPass) < interval {
		return false, nil
	}
	s.lastPass = now
	return true, nil
}

// Tick evaluates whether a sync pass is due and executes it on the elected leader replica.
func (s *Service) Tick(ctx context.Context) error {
	due, err := s.shouldTick(ctx)
	if err != nil || !due {
		return err
	}
	return s.SyncAll(ctx)
}
