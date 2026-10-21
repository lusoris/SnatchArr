// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// strikeTTL is how long a strike row is kept before it expires and can be purged.
const strikeTTL = 24 * time.Hour

// maxPurgeBatch bounds the rows one Purge call deletes.
const maxPurgeBatch = 10000

// strikeTTLSeconds is strikeTTL in seconds for SQL interval calculation.
const strikeTTLSeconds = int64(strikeTTL / time.Second)

// Store persists queue policies and strike state per instance.
type Store struct {
	st  *store.Store
	clk clock.Clock
}

// NewStore creates a Store backed by Postgres and the given clock.
func NewStore(st *store.Store, clk clock.Clock) *Store {
	return &Store{st: st, clk: clk}
}

// DownloadPriorityPreset returns the active download priority preset from settings, or an error.
func (s *Store) DownloadPriorityPreset(ctx context.Context) (domain.PriorityPreset, error) {
	row, err := s.st.Q().GetSettings(ctx)
	if err != nil {
		return domain.MediaFirstPreset(), fmt.Errorf("queue: get settings: %w", store.MapError(err))
	}
	return store.SettingsFromRow(row).DownloadPriorityPreset, nil
}

// Settings returns the queue settings for an instance, or DefaultSettings if not yet configured.
func (s *Store) Settings(ctx context.Context, instanceID uuid.UUID) (Settings, error) {
	row, err := s.st.Q().GetQueuePolicy(ctx, instanceID)
	if err != nil {
		if errors.Is(store.MapError(err), domain.ErrNotFound) {
			return DefaultSettings(), nil
		}
		return Settings{}, fmt.Errorf("queue: get settings: %w", store.MapError(err))
	}
	return policyFromRow(row)
}

type ruleConfigJSON struct {
	Mode      Mode  `json:"mode"`
	Threshold int   `json:"threshold"`
	Blocklist *bool `json:"blocklist,omitempty"`
}

type matchFloors struct {
	matchFixable float64
	matchBroken  float64
	trackFixable float64
	trackBroken  float64
}

func policyFromRow(row sqlcgen.QueuePolicy) (Settings, error) {
	rules, floors, err := parseRulesAndFloorsJSON(row.Rules)
	if err != nil {
		return Settings{}, fmt.Errorf("queue: parse rules: %w", err)
	}
	return Settings{
		Enabled:                 row.Enabled,
		Rules:                   rules,
		Grace:                   time.Duration(row.GraceS) * time.Second,
		MinBytesPerSecond:       row.MinBytesPerSecond,
		MinCustomFormatScore:    row.MinCustomFormatScore,
		MaxRemovalsPerSweep:     int(row.MaxRemovalsPerSweep),
		MaxImportsPerSweep:      int(row.MaxImportsPerSweep),
		MatchFixableMinPercent:  floors.matchFixable,
		MatchBrokenBelowPercent: floors.matchBroken,
		TrackFixableMinPercent:  floors.trackFixable,
		TrackBrokenBelowPercent: floors.trackBroken,
	}, nil
}

func parseRulesAndFloorsJSON(data []byte) (map[Rule]RuleSettings, matchFloors, error) {
	defs := DefaultSettings()
	rules := defs.Rules
	floors := matchFloors{
		matchFixable: defs.MatchFixableMinPercent,
		matchBroken:  defs.MatchBrokenBelowPercent,
		trackFixable: defs.TrackFixableMinPercent,
		trackBroken:  defs.TrackBrokenBelowPercent,
	}
	if len(data) == 0 {
		return rules, floors, nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, floors, fmt.Errorf("queue: unmarshal rules: %w", err)
	}
	for k, v := range raw {
		parseRuleOrFloorEntry(k, v, rules, &floors)
	}
	return rules, floors, nil
}

func parseRuleOrFloorEntry(k string, v json.RawMessage, rules map[Rule]RuleSettings, floors *matchFloors) {
	if parseFloorKey(k, v, floors) {
		return
	}
	r := Rule(k)
	if slices.Contains(RuleOrder, r) {
		var cfg ruleConfigJSON
		if err := json.Unmarshal(v, &cfg); err == nil {
			rules[r] = RuleSettings(cfg)
		}
	}
}

func parseFloorKey(k string, v json.RawMessage, floors *matchFloors) bool {
	var target *float64
	switch k {
	case "match_fixable_min_percent":
		target = &floors.matchFixable
	case "match_broken_below_percent":
		target = &floors.matchBroken
	case "track_fixable_min_percent":
		target = &floors.trackFixable
	case "track_broken_below_percent":
		target = &floors.trackBroken
	default:
		return false
	}
	var val float64
	if err := json.Unmarshal(v, &val); err == nil && val > 0 {
		*target = val
	}
	return true
}

// SaveSettings validates and persists queue settings for an instance.
func (s *Store) SaveSettings(ctx context.Context, instanceID uuid.UUID, cfg Settings) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("queue: %w", err)
	}
	rulesBytes, err := marshalRulesAndFloorsJSON(cfg)
	if err != nil {
		return fmt.Errorf("queue: marshal rules: %w", err)
	}
	err = s.st.Q().UpsertQueuePolicy(ctx, sqlcgen.UpsertQueuePolicyParams{
		InstanceID:           instanceID,
		Enabled:              cfg.Enabled,
		Rules:                rulesBytes,
		GraceS:               int32(cfg.Grace / time.Second), // #nosec G115 -- validated 0..86400
		MinBytesPerSecond:    cfg.MinBytesPerSecond,
		MinCustomFormatScore: cfg.MinCustomFormatScore,
		MaxRemovalsPerSweep:  int32(cfg.MaxRemovalsPerSweep), // #nosec G115 -- validated 1..100
		MaxImportsPerSweep:   int32(cfg.MaxImportsPerSweep),  // #nosec G115 -- validated 1..100
		UpdatedAt:            s.clk.Now(),
	})
	if err != nil {
		return fmt.Errorf("queue: save settings: %w", store.MapError(err))
	}
	return nil
}

func marshalRulesAndFloorsJSON(cfg Settings) ([]byte, error) {
	raw := make(map[string]any, len(cfg.Rules)+4)
	for r, rs := range cfg.Rules {
		raw[string(r)] = ruleConfigJSON(rs)
	}
	raw["match_fixable_min_percent"] = cfg.MatchFixableMinPercent
	raw["match_broken_below_percent"] = cfg.MatchBrokenBelowPercent
	raw["track_fixable_min_percent"] = cfg.TrackFixableMinPercent
	raw["track_broken_below_percent"] = cfg.TrackBrokenBelowPercent
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("queue: marshal rules: %w", err)
	}
	return data, nil
}

// Strikes returns all active strikes for an instance, keyed by download ID.
func (s *Store) Strikes(ctx context.Context, instanceID uuid.UUID) (map[string]Strike, error) {
	rows, err := s.strikeRows(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	strikes := make(map[string]Strike, len(rows))
	for _, row := range rows {
		strikes[row.DownloadID] = row.Strike
	}
	return strikes, nil
}

// Record persists updated strike states for a batch of sweep decisions in a single transaction.
func (s *Store) Record(ctx context.Context, instanceID uuid.UUID, decisions []Decision) error {
	if len(decisions) == 0 {
		return nil
	}
	params := buildUpsertParams(instanceID, decisions)
	if len(params.DownloadIds) == 0 {
		return nil
	}
	err := s.st.Tx(ctx, func(q *sqlcgen.Queries) error {
		return q.UpsertQueueStrikes(ctx, params)
	})
	if err != nil {
		return fmt.Errorf("queue: record strikes: %w", store.MapError(err))
	}
	return nil
}

func buildUpsertParams(instanceID uuid.UUID, decisions []Decision) sqlcgen.UpsertQueueStrikesParams {
	dedup := make(map[string]Decision, len(decisions))
	order := make([]string, 0, len(decisions))
	for _, d := range decisions {
		if d.DownloadID == "" {
			continue
		}
		if _, seen := dedup[d.DownloadID]; !seen {
			order = append(order, d.DownloadID)
		}
		dedup[d.DownloadID] = d
	}
	p := sqlcgen.UpsertQueueStrikesParams{
		TtlS:           strikeTTLSeconds,
		InstanceID:     instanceID,
		DownloadIds:    make([]string, 0, len(order)),
		QueueIds:       make([]int32, 0, len(order)),
		Rules:          make([]string, 0, len(order)),
		Counts:         make([]int32, 0, len(order)),
		LastSizeLefts:  make([]int64, 0, len(order)),
		LastSeens:      make([]time.Time, 0, len(order)),
		ImportedAts:    make([]time.Time, 0, len(order)),
		ImportRefuseds: make([]bool, 0, len(order)),
	}
	for _, id := range order {
		d := dedup[id]
		p.DownloadIds = append(p.DownloadIds, d.DownloadID)
		p.QueueIds = append(p.QueueIds, d.ItemID)
		p.Rules = append(p.Rules, string(d.Next.Rule))
		p.Counts = append(p.Counts, int32(d.Next.Count)) // #nosec G115 -- strike count capped at threshold <= 100
		p.LastSizeLefts = append(p.LastSizeLefts, d.Next.LastSizeLeft)
		p.LastSeens = append(p.LastSeens, d.Next.LastSeen)
		p.ImportedAts = append(p.ImportedAts, importedAtParam(d.Next.ImportedAt))
		p.ImportRefuseds = append(p.ImportRefuseds, d.Next.ImportRefused)
	}
	return p
}

// noImport is the array sentinel for "never imported": the query turns it into NULL,
// because a timestamptz array element cannot be NULL through sqlc's []time.Time.
var noImport = time.Unix(0, 0).UTC()

func importedAtParam(t time.Time) time.Time {
	if t.IsZero() {
		return noImport
	}
	return t
}

// Forget removes the strike record for a download ID after a successful removal.
func (s *Store) Forget(ctx context.Context, instanceID uuid.UUID, downloadID string) error {
	err := s.st.Q().DeleteQueueStrike(ctx, sqlcgen.DeleteQueueStrikeParams{
		InstanceID: instanceID,
		DownloadID: downloadID,
	})
	if err != nil {
		return fmt.Errorf("queue: forget strike: %w", store.MapError(err))
	}
	return nil
}

// Purge removes up to batch expired strikes (1..maxPurgeBatch; out-of-range values are
// clamped), returning the number of deleted rows.
func (s *Store) Purge(ctx context.Context, batch int) (int64, error) {
	batch = min(max(batch, 1), maxPurgeBatch)
	n, err := s.st.Q().PurgeExpiredQueueStrikes(ctx, sqlcgen.PurgeExpiredQueueStrikesParams{
		Before: s.clk.Now(),
		Batch:  int32(batch), // #nosec G115 -- clamped to 1..maxPurgeBatch
	})
	if err != nil {
		return 0, fmt.Errorf("queue: purge expired strikes: %w", store.MapError(err))
	}
	return n, nil
}

// StrikeItem is one stored strike state with the queue identifiers of its item.
type StrikeItem struct {
	DownloadID string
	QueueID    int32
	Strike
}

// ListStrikes returns the items of an instance that hold at least one
// strike, ordered by download id. Rows the last sweep found clean are left out.
func (s *Store) ListStrikes(ctx context.Context, instanceID uuid.UUID) ([]StrikeItem, error) {
	rows, err := s.strikeRows(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	active := rows[:0]
	for _, row := range rows {
		if row.Rule != "" && row.Count > 0 {
			active = append(active, row)
		}
	}
	return active, nil
}

func (s *Store) strikeRows(ctx context.Context, instanceID uuid.UUID) ([]StrikeItem, error) {
	rows, err := s.st.Q().ListQueueStrikes(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("queue: list strikes: %w", store.MapError(err))
	}
	items := make([]StrikeItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, StrikeItem{
			DownloadID:    row.DownloadID,
			QueueID:       row.QueueID,
			Rule:          Rule(row.Rule),
			Count:         int(row.Count),
			LastSizeLeft:  row.LastSizeLeft,
			LastSeen:      row.LastSeen,
			ImportedAt:    derefTime(row.ImportedAt),
			ImportRefused: row.ImportRefused,
		})
	}
	return items, nil
}

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
