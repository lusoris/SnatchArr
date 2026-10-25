// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package watchaware

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.uber.org/fx"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/leader/gate"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/metrics"
	"github.com/lusoris/SnatchArr/api/internal/modules"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

const (
	// DefaultSyncIntervalMinutes is the default interval between sync passes.
	DefaultSyncIntervalMinutes = 15
	// BaseTickPeriod is the frequency of the leader loop ticker.
	BaseTickPeriod = 1 * time.Minute
)

// Ticker periodically synchronizes watch state from enabled sources while this replica leads.
type Ticker struct {
	*gate.Periodic
}

// NewTicker builds the watchaware leader loop ticker.
func NewTicker(svc *Service, clk clock.Clock, m *metrics.Metrics) *Ticker {
	return &Ticker{gate.NewPeriodic("watchaware: sync", BaseTickPeriod, clk, m, svc.Tick)}
}

// Module provides the Service, its Ticker, and registers the ticker in the leader_loops fx group.
var Module = fx.Module("snatcharr.watchaware",
	fx.Provide(
		NewGoenvoySeriesReader,
		NewGoenvoySeasonWriter,
		NewService,
		NewTicker,
	),
	fx.Provide(gate.Provide[*Ticker]()),
)

func (s *Service) shouldTick(ctx context.Context) (bool, error) {
	enabled, err := s.mod.IsEnabled(ctx, domain.ModuleWatchAware)
	if err != nil {
		return false, fmt.Errorf("watchaware: check module enabled: %w", err)
	}
	if !enabled {
		s.logger.DebugContext(ctx, "watchaware: module disabled; skipping tick")
		return false, nil
	}

	mod, err := s.mod.Get(ctx, domain.ModuleWatchAware)
	if err != nil {
		return false, fmt.Errorf("watchaware: get module config: %w", err)
	}
	interval := s.resolveInterval(mod.Config)

	now := s.clk.Now()
	s.mu.Lock()
	lastPass := s.lastPass
	if !lastPass.IsZero() && now.Sub(lastPass) < interval {
		s.mu.Unlock()
		return false, nil
	}
	s.lastPass = now
	s.mu.Unlock()
	return true, nil
}

func (s *Service) createWatchSource(r sqlcgen.WatchawareSource, apiKey string, opts arrclient.Options) (WatchSource, error) {
	switch Kind(r.Kind) {
	case KindJellyfin:
		return NewJellyfin(r.Url, apiKey, opts.Timeout, opts.UserAgent)
	case KindEmby:
		return NewEmby(r.Url, apiKey, opts.Timeout, opts.UserAgent)
	default:
		return nil, fmt.Errorf("unknown source kind %q", r.Kind)
	}
}

func (s *Service) buildNamedSources(ctx context.Context, rows []sqlcgen.WatchawareSource) []Named {
	opts := arrclient.DefaultOptions()
	var namedSources []Named
	for _, r := range rows {
		keyBytes, decErr := s.enc.Open(r.ApiKeyEnc)
		if decErr != nil {
			s.logger.ErrorContext(ctx, "watchaware: decrypt api key failed", slog.String("source_id", r.ID.String()), slog.Any("error", decErr))
			if recErr := s.RecordSync(ctx, r.ID, decErr); recErr != nil {
				s.logger.ErrorContext(ctx, "watchaware: record sync failure failed", slog.String("source_id", r.ID.String()), slog.Any("error", recErr))
			}
			continue
		}
		src, initErr := s.createWatchSource(r, string(keyBytes), opts)
		if initErr != nil {
			s.logger.ErrorContext(ctx, "watchaware: init source client failed", slog.String("source_id", r.ID.String()), slog.Any("error", initErr))
			if recErr := s.RecordSync(ctx, r.ID, initErr); recErr != nil {
				s.logger.ErrorContext(ctx, "watchaware: record sync failure failed", slog.String("source_id", r.ID.String()), slog.Any("error", recErr))
			}
			continue
		}
		namedSources = append(namedSources, Named{ID: r.ID.String(), Source: src})
	}
	return namedSources
}

func (s *Service) recordResults(ctx context.Context, namedSources []Named, syncErrs []error) {
	errMap := make(map[string]error)
	for _, syncErr := range syncErrs {
		if srcErr, ok := errors.AsType[*SourceError](syncErr); ok {
			errMap[srcErr.ID] = syncErr
		}
	}

	for _, ns := range namedSources {
		srcUUID, parseErr := uuid.Parse(ns.ID)
		if parseErr != nil {
			s.logger.ErrorContext(ctx, "watchaware: parse source id failed", slog.String("source_id", ns.ID), slog.Any("error", parseErr))
			continue
		}
		if recErr := s.RecordSync(ctx, srcUUID, errMap[ns.ID]); recErr != nil {
			s.logger.ErrorContext(ctx, "watchaware: record sync failed", slog.String("source_id", ns.ID), slog.Any("error", recErr))
		}
	}
}

func (s *Service) syncSources(ctx context.Context) error {
	rows, err := s.st.Q().ListEnabledWatchAwareSources(ctx)
	if err != nil {
		return fmt.Errorf("watchaware: list enabled sources: %w", store.MapError(err))
	}
	if len(rows) == 0 {
		return nil
	}
	if len(rows) > maxSources {
		s.logger.WarnContext(ctx, "watchaware: enabled sources exceed limit; truncating",
			slog.Int("total", len(rows)),
			slog.Int("cap", maxSources),
		)
		rows = rows[:maxSources]
	}

	namedSources := s.buildNamedSources(ctx, rows)
	if len(namedSources) == 0 {
		return nil
	}

	logf := func(format string, args ...any) {
		s.logger.WarnContext(ctx, fmt.Sprintf(format, args...))
	}
	syncErrs := SyncAll(ctx, namedSources, s, logf)
	s.recordResults(ctx, namedSources, syncErrs)
	return nil
}

// Tick executes a single scheduled synchronization pass.
func (s *Service) Tick(ctx context.Context) error {
	// (a) and (b): module switch and interval check
	ok, err := s.shouldTick(ctx)
	if err != nil || !ok {
		return err
	}

	if syncErr := s.syncSources(ctx); syncErr != nil {
		s.logger.ErrorContext(ctx, "watchaware: sync sources failed", slog.Any("error", syncErr))
	}

	if gateErr := s.GatePass(ctx); gateErr != nil {
		s.logger.ErrorContext(ctx, "watchaware: gate pass failed", slog.Any("error", gateErr))
	}
	return nil
}

func (s *Service) resolveInterval(raw json.RawMessage) time.Duration {
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
	if val < modules.MinSyncIntervalMinutes || val > modules.MaxSyncIntervalMinutes {
		return DefaultSyncIntervalMinutes * time.Minute
	}
	return time.Duration(val) * time.Minute
}
