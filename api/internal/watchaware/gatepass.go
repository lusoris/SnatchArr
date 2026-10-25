// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package watchaware

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/golusoris/goenvoy/arr/sonarr"
	arr "github.com/golusoris/goenvoy/arr/v2"
	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

const (
	maxGateInstances     = 32
	maxSeriesPerInstance = 5000
	pruneDecisionsBatch  = 1000
	pruneDecisionsAge    = 30 * 24 * time.Hour
)

// SeriesReader reads series and tags from an instance.
type SeriesReader interface {
	Tags(ctx context.Context, inst domain.Instance) ([]arr.Tag, error)
	Series(ctx context.Context, inst domain.Instance) ([]GateSeries, error)
}

type instanceCredentialsProvider interface {
	Credentials(ctx context.Context, instID uuid.UUID) (instances.Credentials, error)
}

type goenvoySeriesReader struct {
	inst instanceCredentialsProvider
	opts arrclient.Options
}

// NewGoenvoySeriesReader returns a SeriesReader backed by goenvoy.
func NewGoenvoySeriesReader(inst *instances.Service) SeriesReader {
	return NewSeriesReader(inst, arrclient.DefaultOptions())
}

// NewSeriesReader constructs a SeriesReader with custom options.
func NewSeriesReader(inst instanceCredentialsProvider, opts arrclient.Options) SeriesReader {
	return &goenvoySeriesReader{inst: inst, opts: opts}
}

func (r *goenvoySeriesReader) Tags(ctx context.Context, inst domain.Instance) ([]arr.Tag, error) {
	creds, err := r.inst.Credentials(ctx, inst.ID)
	if err != nil {
		return nil, fmt.Errorf("watchaware: get credentials for %s: %w", inst.ID, err)
	}
	client, err := sonarr.New(creds.BaseURL, creds.APIKey, r.opts.Options()...)
	if err != nil {
		return nil, fmt.Errorf("watchaware: init sonarr client: %w", err)
	}
	tags, err := client.GetTags(ctx)
	if err != nil {
		return nil, fmt.Errorf("watchaware: get tags: %w", err)
	}
	return tags, nil
}

func (r *goenvoySeriesReader) Series(ctx context.Context, inst domain.Instance) ([]GateSeries, error) {
	creds, err := r.inst.Credentials(ctx, inst.ID)
	if err != nil {
		return nil, fmt.Errorf("watchaware: get credentials for %s: %w", inst.ID, err)
	}
	client, err := sonarr.New(creds.BaseURL, creds.APIKey, r.opts.Options()...)
	if err != nil {
		return nil, fmt.Errorf("watchaware: init sonarr client: %w", err)
	}
	rawSeries, err := client.GetAllSeries(ctx)
	if err != nil {
		return nil, fmt.Errorf("watchaware: get series: %w", err)
	}
	return mapGoenvoySeries(rawSeries), nil
}

func mapGoenvoySeries(raw []sonarr.Series) []GateSeries {
	out := make([]GateSeries, 0, len(raw))
	for _, s := range raw {
		seasons := make([]GateSeason, 0, len(s.Seasons))
		for _, sn := range s.Seasons {
			fileCount := 0
			if sn.Statistics != nil {
				fileCount = sn.Statistics.EpisodeFileCount
			}
			seasons = append(seasons, GateSeason{
				Number:    sn.SeasonNumber,
				Monitored: sn.Monitored,
				FileCount: fileCount,
			})
		}
		out = append(out, GateSeries{
			ID:        s.ID,
			TvdbID:    s.TvdbID,
			Title:     s.Title,
			TagIDs:    s.Tags,
			Monitored: s.Monitored,
			Seasons:   seasons,
		})
	}
	return out
}

func (s *Service) pruneDecisions(ctx context.Context) {
	before := s.clk.Now().Add(-pruneDecisionsAge)
	if _, err := s.st.Q().PruneGateDecisions(ctx, sqlcgen.PruneGateDecisionsParams{
		Before:  before,
		MaxRows: pruneDecisionsBatch,
	}); err != nil {
		s.logger.WarnContext(ctx, "watchaware: prune gate decisions failed", slog.Any("error", err))
	}
}

func filterSonarrInstances(insts []domain.Instance, cfg GateConfig) []domain.Instance {
	var out []domain.Instance
	for _, inst := range insts {
		if inst.Kind == domain.KindSonarr && inst.Enabled && cfg.AppliesTo(inst.ID) {
			out = append(out, inst)
			if len(out) == maxGateInstances {
				break
			}
		}
	}
	return out
}

func resolveExemptTag(tags []arr.Tag, target string) (int, bool) {
	trimmed := strings.TrimSpace(target)
	for _, t := range tags {
		if strings.EqualFold(strings.TrimSpace(t.Label), trimmed) {
			return t.ID, true
		}
	}
	return 0, false
}

func (s *Service) processBaseline(ctx context.Context, instID uuid.UUID, series []GateSeries, now time.Time) error {
	return s.st.Tx(ctx, func(q *sqlcgen.Queries) error {
		for _, item := range series {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			err := q.UpsertGateSeries(ctx, sqlcgen.UpsertGateSeriesParams{
				InstanceID:      instID,
				SeriesID:        int32(item.ID),     // #nosec G115 -- series ID fits in int32
				TvdbID:          int32(item.TvdbID), // #nosec G115 -- tvdb ID fits in int32
				Title:           item.Title,
				GateSeason:      0,
				State:           GateStateBeforeGate,
				AddedBeforeGate: true,
				HeldSeasons:     []int32{},
				FirstSeenAt:     now,
				UpdatedAt:       now,
			})
			if err != nil {
				return fmt.Errorf("upsert series %d: %w", item.ID, store.MapError(err))
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := q.InsertGateBaseline(ctx, sqlcgen.InsertGateBaselineParams{
			InstanceID:  instID,
			BaselinedAt: now,
			SeriesCount: int32(len(series)), // #nosec G115 -- series count fits in int32
		})
		if err != nil {
			return fmt.Errorf("insert baseline: %w", store.MapError(err))
		}
		return nil
	})
}

func deriveHeldSeasons(state string, decisions []GateDecision) []int32 {
	if state != GateStateGated {
		return []int32{}
	}
	heldSeasons := make([]int32, 0, len(decisions))
	for _, d := range decisions {
		if d.Action == GateActionUnmonitor {
			heldSeasons = append(heldSeasons, int32(d.Season)) // #nosec G115 -- season fits in int32
		}
	}
	slices.Sort(heldSeasons)
	return heldSeasons
}

func (s *Service) recordNewGateSeries(
	ctx context.Context,
	instID uuid.UUID,
	item GateSeries,
	cfg GateConfig,
	exemptTagID int,
	hasExemptTag bool,
	now time.Time,
) error {
	sID := int32(item.ID) // #nosec G115 -- series ID fits in int32
	state, gateSeason, decisions := EvaluateOnAdd(item, cfg, exemptTagID, hasExemptTag)
	heldSeasons := deriveHeldSeasons(state, decisions)

	err := s.st.Tx(ctx, func(q *sqlcgen.Queries) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if uErr := q.UpsertGateSeries(ctx, sqlcgen.UpsertGateSeriesParams{
			InstanceID:      instID,
			SeriesID:        sID,
			TvdbID:          int32(item.TvdbID), // #nosec G115 -- tvdb ID fits in int32
			Title:           item.Title,
			GateSeason:      int32(gateSeason), // #nosec G115 -- season fits in int32
			State:           state,
			AddedBeforeGate: false,
			HeldSeasons:     heldSeasons,
			FirstSeenAt:     now,
			UpdatedAt:       now,
		}); uErr != nil {
			return fmt.Errorf("upsert series %d: %w", item.ID, store.MapError(uErr))
		}

		return s.insertDecisions(ctx, q, instID, sID, decisions, now)
	})
	if err != nil {
		return err
	}

	if state == GateStateGated && len(decisions) > 0 {
		s.emitGateDecidedEvent(ctx, instID, item, decisions, now)
	}
	return nil
}

func (s *Service) insertDecisions(
	ctx context.Context,
	q *sqlcgen.Queries,
	instID uuid.UUID,
	sID int32,
	decisions []GateDecision,
	now time.Time,
) error {
	for _, d := range decisions {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		decID, idErr := s.ids.NewUUID()
		if idErr != nil {
			return fmt.Errorf("generate decision id: %w", idErr)
		}
		if dErr := q.UpsertGateDecision(ctx, sqlcgen.UpsertGateDecisionParams{
			ID:           decID,
			InstanceID:   instID,
			SeriesID:     sID,
			SeasonNumber: int32(d.Season), // #nosec G115 -- season fits in int32
			Action:       d.Action,
			WouldAct:     d.WouldAct,
			Reason:       d.Reason,
			DecidedAt:    now,
		}); dErr != nil {
			return fmt.Errorf("upsert decision %d season %d: %w", sID, d.Season, store.MapError(dErr))
		}
	}
	return nil
}

func formatGateDecidedDetail(decisions []GateDecision) string {
	if len(decisions) == 0 {
		return ""
	}
	action := decisions[0].Action
	wouldAct := decisions[0].WouldAct
	seasons := make([]int, 0, len(decisions))
	for _, d := range decisions {
		seasons = append(seasons, d.Season)
	}
	slices.Sort(seasons)
	seasonStrs := make([]string, 0, len(seasons))
	for _, sn := range seasons {
		seasonStrs = append(seasonStrs, strconv.Itoa(sn))
	}
	seasonWord := "seasons"
	if len(seasons) == 1 {
		seasonWord = "season"
	}
	prefix := ""
	if !wouldAct {
		prefix = "would "
	}
	return fmt.Sprintf("%s%s %s %s", prefix, action, seasonWord, strings.Join(seasonStrs, ", "))
}

func (s *Service) emitGateDecidedEvent(
	ctx context.Context,
	instID uuid.UUID,
	item GateSeries,
	decisions []GateDecision,
	now time.Time,
) {
	if s.rec == nil {
		return
	}
	detail := formatGateDecidedDetail(decisions)
	ev := domain.Event{
		InstanceID: instID,
		Timestamp:  now,
		Level:      "info",
		Type:       "watchaware_gate_decided",
		EntityType: "series",
		EntityID:   int64(item.ID),
		Title:      item.Title,
		Detail:     detail,
	}
	if err := s.rec.Record(ctx, ev); err != nil {
		s.logger.WarnContext(ctx, "watchaware: record gate decided event failed",
			slog.String("instance_id", instID.String()),
			slog.Int("series_id", item.ID),
			slog.Any("error", err),
		)
	}
}

func (s *Service) processIncremental(
	ctx context.Context,
	instID uuid.UUID,
	series []GateSeries,
	cfg GateConfig,
	exemptTagID int,
	hasExemptTag bool,
	now time.Time,
) {
	existingIDs, err := s.st.Q().ListGateSeriesIDsForInstance(ctx, instID)
	if err != nil {
		s.logger.ErrorContext(ctx, "watchaware: list existing gate series IDs failed",
			slog.String("instance_id", instID.String()),
			slog.Any("error", err),
		)
		return
	}
	existingMap := make(map[int32]struct{}, len(existingIDs))
	for _, id := range existingIDs {
		existingMap[id] = struct{}{}
	}

	for _, item := range series {
		if ctx.Err() != nil {
			return
		}
		sID := int32(item.ID) // #nosec G115 -- series ID fits in int32
		if _, exists := existingMap[sID]; exists {
			if uErr := s.st.Q().UpdateGateSeriesTitle(ctx, sqlcgen.UpdateGateSeriesTitleParams{
				InstanceID: instID,
				SeriesID:   sID,
				Title:      item.Title,
				UpdatedAt:  now,
			}); uErr != nil {
				s.logger.ErrorContext(ctx, "watchaware: update series title failed",
					slog.String("instance_id", instID.String()),
					slog.Int("series_id", item.ID),
					slog.Any("error", uErr),
				)
			}
			continue
		}
		if rErr := s.recordNewGateSeries(ctx, instID, item, cfg, exemptTagID, hasExemptTag, now); rErr != nil {
			s.logger.ErrorContext(ctx, "watchaware: record new gate series failed",
				slog.String("instance_id", instID.String()),
				slog.Int("series_id", item.ID),
				slog.Any("error", rErr),
			)
		}
	}
}

func (s *Service) processGateInstance(ctx context.Context, inst domain.Instance, cfg GateConfig) {
	tags, err := s.reader.Tags(ctx, inst)
	if err != nil {
		s.logger.ErrorContext(ctx, "watchaware: fetch tags failed",
			slog.String("instance_id", inst.ID.String()),
			slog.Any("error", err),
		)
		return
	}

	exemptTagID, hasExemptTag := resolveExemptTag(tags, cfg.ExemptTag)

	seriesList, err := s.reader.Series(ctx, inst)
	if err != nil {
		s.logger.ErrorContext(ctx, "watchaware: fetch series failed",
			slog.String("instance_id", inst.ID.String()),
			slog.Any("error", err),
		)
		return
	}
	if len(seriesList) > maxSeriesPerInstance {
		seriesList = seriesList[:maxSeriesPerInstance]
	}

	now := s.clk.Now()
	_, err = s.st.Q().GetGateBaseline(ctx, inst.ID)
	if err != nil {
		if errors.Is(store.MapError(err), domain.ErrNotFound) {
			if bErr := s.processBaseline(ctx, inst.ID, seriesList, now); bErr != nil {
				s.logger.ErrorContext(ctx, "watchaware: process baseline failed",
					slog.String("instance_id", inst.ID.String()),
					slog.Any("error", bErr),
				)
			}
			return
		}
		s.logger.ErrorContext(ctx, "watchaware: get gate baseline failed",
			slog.String("instance_id", inst.ID.String()),
			slog.Any("error", err),
		)
		return
	}

	s.processIncremental(ctx, inst.ID, seriesList, cfg, exemptTagID, hasExemptTag, now)
	s.processPromotion(ctx, inst.ID, cfg, now)

	monitoredBySeries := make(map[int]bool, len(seriesList))
	for _, item := range seriesList {
		monitoredBySeries[item.ID] = item.Monitored
	}
	s.actPending(ctx, inst, cfg, monitoredBySeries, now)
}

// GatePass evaluates the season gate and carries out pending decisions across configured Sonarr instances.
func (s *Service) GatePass(ctx context.Context) error {
	enabled, err := s.mod.IsEnabled(ctx, domain.ModuleWatchAware)
	if err != nil {
		return fmt.Errorf("watchaware: check module enabled: %w", err)
	}
	if !enabled {
		return nil
	}

	mod, err := s.mod.Get(ctx, domain.ModuleWatchAware)
	if err != nil {
		return fmt.Errorf("watchaware: get module config: %w", err)
	}
	gateCfg := GateConfigFrom(mod.Config)
	if !gateCfg.Enabled || s.inst == nil || s.reader == nil {
		return nil
	}

	s.pruneDecisions(ctx)

	allInsts, err := s.inst.List(ctx)
	if err != nil {
		return fmt.Errorf("watchaware: list instances: %w", err)
	}

	targetInsts := filterSonarrInstances(allInsts, gateCfg)
	for _, inst := range targetInsts {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		s.processGateInstance(ctx, inst, gateCfg)
	}
	return nil
}
