// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package watchaware

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// maxPromotionWatchesBatch bounds the episode watch rows loaded per instance (HISS-02).
const maxPromotionWatchesBatch = 50000

func (s *Service) processPromotion(
	ctx context.Context,
	instID uuid.UUID,
	cfg GateConfig,
	now time.Time,
) {
	if cfg.LookAhead <= 0 {
		return
	}

	gatedSeries, err := s.st.Q().ListGatedSeriesWithHeldSeasons(ctx, sqlcgen.ListGatedSeriesWithHeldSeasonsParams{
		InstanceID: instID,
		Limit:      maxSeriesPerInstance,
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "watchaware: list gated series with held seasons failed",
			slog.String("instance_id", instID.String()),
			slog.Any("error", err),
		)
		return
	}
	if len(gatedSeries) == 0 {
		return
	}

	watchesByTvdb, err := s.loadEpisodeWatchesForSeries(ctx, gatedSeries)
	if err != nil {
		s.logger.ErrorContext(ctx, "watchaware: load episode watches for promotion failed",
			slog.String("instance_id", instID.String()),
			slog.Any("error", err),
		)
		return
	}

	for _, gs := range gatedSeries {
		if ctx.Err() != nil {
			return
		}
		s.promoteSeriesIfWatched(ctx, instID, gs, cfg, watchesByTvdb, now)
	}
}

func (s *Service) loadEpisodeWatchesForSeries(
	ctx context.Context,
	gatedSeries []sqlcgen.WatchawareGateSeries,
) (map[string][]SeasonWatch, error) {
	tvdbSeen := make(map[string]struct{})
	var tvdbIDs []string
	for _, gs := range gatedSeries {
		if gs.TvdbID > 0 {
			key := strconv.Itoa(int(gs.TvdbID))
			if _, exists := tvdbSeen[key]; !exists {
				tvdbSeen[key] = struct{}{}
				tvdbIDs = append(tvdbIDs, key)
			}
		}
	}
	watchesByTvdb := make(map[string][]SeasonWatch)
	if len(tvdbIDs) == 0 {
		return watchesByTvdb, nil
	}

	rows, err := s.st.Q().ListEpisodeWatchesByTvdbIDs(ctx, sqlcgen.ListEpisodeWatchesByTvdbIDsParams{
		TvdbIds: tvdbIDs,
		MaxRows: maxPromotionWatchesBatch,
	})
	if err != nil {
		return nil, store.MapError(err)
	}

	if len(rows) >= maxPromotionWatchesBatch {
		s.logger.WarnContext(ctx, "watchaware: promotion watch rows truncated",
			slog.Int("max_rows", maxPromotionWatchesBatch),
		)
	}
	for _, r := range rows {
		w := SeasonWatch{
			UserKey: r.SourceID.String() + "/" + r.UserID,
			Season:  int(r.SeasonNumber),
			Episode: int(r.EpisodeNumber),
			Watched: Watched(Watch{Played: r.Played, PlayedPercentage: r.PlayedPercentage}),
		}
		watchesByTvdb[r.ProviderTvdbID] = append(watchesByTvdb[r.ProviderTvdbID], w)
	}
	return watchesByTvdb, nil
}

func (s *Service) promoteSeriesIfWatched(
	ctx context.Context,
	instID uuid.UUID,
	gs sqlcgen.WatchawareGateSeries,
	cfg GateConfig,
	watchesByTvdb map[string][]SeasonWatch,
	now time.Time,
) {
	if gs.TvdbID <= 0 {
		return
	}
	tvdbKey := strconv.Itoa(int(gs.TvdbID))
	watches := watchesByTvdb[tvdbKey]
	if len(watches) == 0 {
		return
	}

	decisions := EvaluatePromotion(int(gs.SeriesID), gs.HeldSeasons, watches, cfg)
	if len(decisions) == 0 {
		return
	}

	if err := s.applyPromotion(ctx, instID, gs, decisions, now); err != nil {
		s.logger.ErrorContext(ctx, "watchaware: apply promotion failed",
			slog.String("instance_id", instID.String()),
			slog.Int("series_id", int(gs.SeriesID)),
			slog.Any("error", err),
		)
	}
}

func (s *Service) applyPromotion(
	ctx context.Context,
	instID uuid.UUID,
	gs sqlcgen.WatchawareGateSeries,
	decisions []GateDecision,
	now time.Time,
) error {
	promotedSeasons := make(map[int32]struct{}, len(decisions))
	for _, d := range decisions {
		promotedSeasons[int32(d.Season)] = struct{}{} // #nosec G115 -- season fits in int32
	}

	newHeld := make([]int32, 0, len(gs.HeldSeasons))
	for _, h := range gs.HeldSeasons {
		if _, promoted := promotedSeasons[h]; !promoted {
			newHeld = append(newHeld, h)
		}
	}

	err := s.st.Tx(ctx, func(q *sqlcgen.Queries) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if dErr := s.insertDecisions(ctx, q, instID, gs.SeriesID, decisions, now); dErr != nil {
			return fmt.Errorf("insert promotion decisions: %w", dErr)
		}
		if uErr := q.UpdateGateSeriesHeldSeasons(ctx, sqlcgen.UpdateGateSeriesHeldSeasonsParams{
			InstanceID:  instID,
			SeriesID:    gs.SeriesID,
			HeldSeasons: newHeld,
			UpdatedAt:   now,
		}); uErr != nil {
			return fmt.Errorf("update held seasons: %w", store.MapError(uErr))
		}
		return nil
	})
	if err != nil {
		return err
	}

	item := GateSeries{
		ID:     int(gs.SeriesID),
		TvdbID: int(gs.TvdbID),
		Title:  gs.Title,
	}
	s.emitGateDecidedEvent(ctx, instID, item, decisions, now)
	return nil
}
