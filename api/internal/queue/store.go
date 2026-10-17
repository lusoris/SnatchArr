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
	Mode      Mode `json:"mode"`
	Threshold int  `json:"threshold"`
}

func policyFromRow(row sqlcgen.QueuePolicy) (Settings, error) {
	rules, err := parseRulesJSON(row.Rules)
	if err != nil {
		return Settings{}, fmt.Errorf("queue: parse rules: %w", err)
	}
	return Settings{
		Enabled:              row.Enabled,
		Rules:                rules,
		Grace:                time.Duration(row.GraceS) * time.Second,
		MinBytesPerSecond:    row.MinBytesPerSecond,
		MinCustomFormatScore: row.MinCustomFormatScore,
		MaxRemovalsPerSweep:  int(row.MaxRemovalsPerSweep),
	}, nil
}

// parseRulesJSON overlays the stored rules on the defaults, so a rule missing from the row
// keeps its default instead of silently switching off. Unknown keys are dropped.
func parseRulesJSON(data []byte) (map[Rule]RuleSettings, error) {
	rules := DefaultSettings().Rules
	if len(data) == 0 {
		return rules, nil
	}
	var raw map[string]ruleConfigJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("queue: unmarshal rules: %w", err)
	}
	for k, v := range raw {
		r := Rule(k)
		if slices.Contains(RuleOrder, r) {
			rules[r] = RuleSettings(v)
		}
	}
	return rules, nil
}

// SaveSettings validates and persists queue settings for an instance.
func (s *Store) SaveSettings(ctx context.Context, instanceID uuid.UUID, cfg Settings) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("queue: %w", err)
	}
	rulesBytes, err := marshalRulesJSON(cfg.Rules)
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
		UpdatedAt:            s.clk.Now(),
	})
	if err != nil {
		return fmt.Errorf("queue: save settings: %w", store.MapError(err))
	}
	return nil
}

func marshalRulesJSON(rules map[Rule]RuleSettings) ([]byte, error) {
	if len(rules) == 0 {
		return []byte("{}"), nil
	}
	raw := make(map[string]ruleConfigJSON, len(rules))
	for r, rs := range rules {
		raw[string(r)] = ruleConfigJSON(rs)
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("queue: marshal rules: %w", err)
	}
	return data, nil
}

// Strikes returns all active strikes for an instance, keyed by download ID.
func (s *Store) Strikes(ctx context.Context, instanceID uuid.UUID) (map[string]Strike, error) {
	rows, err := s.st.Q().ListQueueStrikes(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("queue: list strikes: %w", store.MapError(err))
	}
	strikes := make(map[string]Strike, len(rows))
	for _, row := range rows {
		strikes[row.DownloadID] = Strike{
			Rule:         Rule(row.Rule),
			Count:        int(row.Count),
			LastSizeLeft: row.LastSizeLeft,
			LastSeen:     row.LastSeen,
		}
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
		TtlS:          strikeTTLSeconds,
		InstanceID:    instanceID,
		DownloadIds:   make([]string, 0, len(order)),
		QueueIds:      make([]int32, 0, len(order)),
		Rules:         make([]string, 0, len(order)),
		Counts:        make([]int32, 0, len(order)),
		LastSizeLefts: make([]int64, 0, len(order)),
		LastSeens:     make([]time.Time, 0, len(order)),
	}
	for _, id := range order {
		d := dedup[id]
		p.DownloadIds = append(p.DownloadIds, d.DownloadID)
		p.QueueIds = append(p.QueueIds, d.ItemID)
		p.Rules = append(p.Rules, string(d.Next.Rule))
		p.Counts = append(p.Counts, int32(d.Next.Count)) // #nosec G115 -- strike count capped at threshold <= 100
		p.LastSizeLefts = append(p.LastSizeLefts, d.Next.LastSizeLeft)
		p.LastSeens = append(p.LastSeens, d.Next.LastSeen)
	}
	return p
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
