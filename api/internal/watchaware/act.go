// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package watchaware

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/golusoris/goenvoy/arr/sonarr"
	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

const (
	maxActDecisionsPerPass = 500
	maxEpisodesPerSeries   = 5000 // HISS-02 upper bound
	maxSeasonsPerUpdate    = 200  // HISS-02 upper bound
)

// SeasonWriter updates season and episode monitoring in Sonarr.
type SeasonWriter interface {
	SetSeasonsMonitored(
		ctx context.Context,
		inst domain.Instance,
		seriesID int,
		seriesMonitored bool,
		seasons map[int]bool,
	) error
}

type goenvoySeasonWriter struct {
	inst instanceCredentialsProvider
	opts arrclient.Options
}

// NewGoenvoySeasonWriter returns a SeasonWriter backed by goenvoy.
func NewGoenvoySeasonWriter(inst *instances.Service) SeasonWriter {
	return NewSeasonWriter(inst, arrclient.DefaultOptions())
}

// NewSeasonWriter constructs a SeasonWriter with custom options.
func NewSeasonWriter(inst instanceCredentialsProvider, opts arrclient.Options) SeasonWriter {
	return &goenvoySeasonWriter{inst: inst, opts: opts}
}

func (w *goenvoySeasonWriter) SetSeasonsMonitored(
	ctx context.Context,
	inst domain.Instance,
	seriesID int,
	seriesMonitored bool,
	seasons map[int]bool,
) error {
	creds, err := w.inst.Credentials(ctx, inst.ID)
	if err != nil {
		return fmt.Errorf("watchaware: get credentials for %s: %w", inst.ID, err)
	}
	client, err := sonarr.New(creds.BaseURL, creds.APIKey, w.opts.Options()...)
	if err != nil {
		return fmt.Errorf("watchaware: init sonarr client: %w", err)
	}

	if err := updateSeasonPass(ctx, client, seriesID, seriesMonitored, seasons); err != nil {
		return err
	}
	return updateEpisodeMonitoring(ctx, client, seriesID, seasons)
}

func updateSeasonPass(
	ctx context.Context,
	client *sonarr.Client,
	seriesID int,
	seriesMonitored bool,
	seasons map[int]bool,
) error {
	passSeasons := make([]sonarr.Season, 0, min(len(seasons), maxSeasonsPerUpdate))
	for seasonNum, monitored := range seasons {
		passSeasons = append(passSeasons, sonarr.Season{
			SeasonNumber: seasonNum,
			Monitored:    monitored,
		})
		if len(passSeasons) == maxSeasonsPerUpdate {
			break
		}
	}
	slices.SortFunc(passSeasons, func(a, b sonarr.Season) int {
		return cmp.Compare(a.SeasonNumber, b.SeasonNumber)
	})

	pass := sonarr.SeasonPassResource{
		Series: []sonarr.SeasonPassSeries{
			{
				ID:        seriesID,
				Seasons:   passSeasons,
				Monitored: seriesMonitored,
			},
		},
	}
	if err := client.UpdateSeasonPass(ctx, pass); err != nil {
		return fmt.Errorf("watchaware: update season pass for series %d: %w", seriesID, err)
	}
	return nil
}

func updateEpisodeMonitoring(
	ctx context.Context,
	client *sonarr.Client,
	seriesID int,
	seasons map[int]bool,
) error {
	episodes, err := client.GetEpisodes(ctx, seriesID)
	if err != nil {
		return fmt.Errorf("watchaware: get episodes for series %d: %w", seriesID, err)
	}
	if len(episodes) > maxEpisodesPerSeries {
		episodes = episodes[:maxEpisodesPerSeries]
	}

	var trueIDs []int
	var falseIDs []int
	for _, ep := range episodes {
		targetMonitored, ok := seasons[ep.SeasonNumber]
		if !ok {
			continue
		}
		if targetMonitored {
			trueIDs = append(trueIDs, ep.ID)
		} else {
			falseIDs = append(falseIDs, ep.ID)
		}
	}

	if len(trueIDs) > 0 {
		if err := client.MonitorEpisodes(ctx, trueIDs, true); err != nil {
			return fmt.Errorf("watchaware: monitor episodes for series %d: %w", seriesID, err)
		}
	}
	if len(falseIDs) > 0 {
		if err := client.MonitorEpisodes(ctx, falseIDs, false); err != nil {
			return fmt.Errorf("watchaware: unmonitor episodes for series %d: %w", seriesID, err)
		}
	}
	return nil
}

type seriesPendingBatch struct {
	seriesID int
	seasons  map[int]bool
	decIDs   []uuid.UUID
}

func groupPendingBySeries(
	pending []sqlcgen.WatchawareGateDecision,
	monitoredBySeries map[int]bool,
) []seriesPendingBatch {
	groups := make(map[int]*seriesPendingBatch)
	var ordered []*seriesPendingBatch

	for _, d := range pending {
		sID := int(d.SeriesID)
		if _, exists := monitoredBySeries[sID]; !exists {
			continue
		}
		b, ok := groups[sID]
		if !ok {
			b = &seriesPendingBatch{
				seriesID: sID,
				seasons:  make(map[int]bool),
			}
			groups[sID] = b
			ordered = append(ordered, b)
		}
		// Promote wins: a season promoted in the same pass stays monitored
		// whatever order the rows arrive in.
		n := int(d.SeasonNumber)
		b.seasons[n] = b.seasons[n] || d.Action == GateActionPromote
		b.decIDs = append(b.decIDs, d.ID)
	}

	out := make([]seriesPendingBatch, len(ordered))
	for i, b := range ordered {
		out[i] = *b
	}
	return out
}

func (s *Service) actPending(
	ctx context.Context,
	inst domain.Instance,
	cfg GateConfig,
	monitoredBySeries map[int]bool,
	now time.Time,
) {
	if cfg.Mode != GateModeAct || s.writer == nil {
		return
	}

	pending, err := s.st.Q().ListPendingGateDecisions(ctx, sqlcgen.ListPendingGateDecisionsParams{
		InstanceID: inst.ID,
		MaxRows:    maxActDecisionsPerPass,
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "watchaware: list pending gate decisions failed",
			slog.String("instance_id", inst.ID.String()),
			slog.Any("error", err),
		)
		return
	}
	if len(pending) == 0 {
		return
	}

	batches := groupPendingBySeries(pending, monitoredBySeries)
	for _, batch := range batches {
		if ctx.Err() != nil {
			return
		}
		s.applySeriesBatch(ctx, inst, batch, monitoredBySeries[batch.seriesID], now)
	}
}

func (s *Service) applySeriesBatch(
	ctx context.Context,
	inst domain.Instance,
	batch seriesPendingBatch,
	seriesMonitored bool,
	now time.Time,
) {
	if err := s.writer.SetSeasonsMonitored(ctx, inst, batch.seriesID, seriesMonitored, batch.seasons); err != nil {
		s.logger.ErrorContext(ctx, "watchaware: act pending decisions failed for series",
			slog.String("instance_id", inst.ID.String()),
			slog.Int("series_id", batch.seriesID),
			slog.Any("error", err),
		)
		return
	}

	if err := s.st.Q().MarkGateDecisionsApplied(ctx, sqlcgen.MarkGateDecisionsAppliedParams{
		AppliedAt: now,
		Ids:       batch.decIDs,
	}); err != nil {
		s.logger.ErrorContext(ctx, "watchaware: mark decisions applied failed",
			slog.String("instance_id", inst.ID.String()),
			slog.Int("series_id", batch.seriesID),
			slog.Any("error", err),
		)
	}
}

// ReleaseGateSeries releases all held seasons for a gated series.
func (s *Service) ReleaseGateSeries(
	ctx context.Context,
	instanceID uuid.UUID,
	seriesID int,
) (GateSeriesRecord, error) {
	sID := int32(seriesID) // #nosec G115 -- series ID fits in int32
	row, err := s.st.Q().GetGateSeries(ctx, sqlcgen.GetGateSeriesParams{
		InstanceID: instanceID,
		SeriesID:   sID,
	})
	if err != nil {
		return GateSeriesRecord{}, fmt.Errorf("watchaware: get gate series: %w", store.MapError(err))
	}

	if row.State == GateStateReleased {
		return gateSeriesRecordFromRow(row), nil
	}
	if row.State != GateStateGated {
		return GateSeriesRecord{}, fmt.Errorf("%w: series state %q cannot be released", domain.ErrConflict, row.State)
	}

	wouldAct := s.isGateModeAct(ctx)
	decisions := make([]GateDecision, 0, len(row.HeldSeasons))
	for _, sn := range row.HeldSeasons {
		decisions = append(decisions, GateDecision{
			SeriesID: seriesID,
			Season:   int(sn),
			Action:   GateActionPromote,
			WouldAct: wouldAct,
			Reason:   "released by admin",
		})
	}

	now := s.clk.Now()
	var updated sqlcgen.WatchawareGateSeries
	err = s.st.Tx(ctx, func(q *sqlcgen.Queries) error {
		if iErr := s.insertDecisions(ctx, q, instanceID, sID, decisions, now); iErr != nil {
			return iErr
		}
		var relErr error
		updated, relErr = q.ReleaseGateSeries(ctx, sqlcgen.ReleaseGateSeriesParams{
			InstanceID: instanceID,
			SeriesID:   sID,
			ReleasedAt: &now,
		})
		if relErr != nil {
			return fmt.Errorf("release gate series: %w", store.MapError(relErr))
		}
		return nil
	})
	if err != nil {
		return GateSeriesRecord{}, err
	}

	if len(decisions) > 0 {
		s.emitGateDecidedEvent(ctx, instanceID, GateSeries{ID: seriesID, Title: row.Title}, decisions, now)
	}
	return gateSeriesRecordFromRow(updated), nil
}

func (s *Service) isGateModeAct(ctx context.Context) bool {
	if s.mod == nil {
		return false
	}
	mod, err := s.mod.Get(ctx, domain.ModuleWatchAware)
	if err != nil {
		return false
	}
	return GateConfigFrom(mod.Config).Mode == GateModeAct
}
