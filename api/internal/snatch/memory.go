// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package snatch

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// MaxFilterBatch bounds one FilterCandidates call (HISS-02).
const MaxFilterBatch = 5000

// Memory remembers which entities were searched so they are not searched again inside
// their TTL. Namespaced per instance × snatch kind × entity type.
type Memory struct {
	st  *store.Store
	clk clock.Clock
}

// NewMemory wires the memory.
func NewMemory(st *store.Store, clk clock.Clock) *Memory {
	return &Memory{st: st, clk: clk}
}

// FilterUnprocessed returns the subset of ids with no live memory entry.
func (m *Memory) FilterUnprocessed(ctx context.Context, instanceID uuid.UUID, kind domain.SnatchKind, entityType string, ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return []int64{}, nil
	}
	if len(ids) > MaxFilterBatch {
		return nil, fmt.Errorf("%w: at most %d ids per filter call", domain.ErrInvalid, MaxFilterBatch)
	}
	out, err := m.st.Q().FilterUnprocessed(ctx, sqlcgen.FilterUnprocessedParams{
		EntityIds: ids, InstanceID: instanceID, Kind: string(kind), EntityType: entityType, Now: m.clk.Now(),
	})
	if err != nil {
		return nil, fmt.Errorf("snatch: filter unprocessed: %w", store.MapError(err))
	}
	return out, nil
}

// PurgeGrace is how long an expired afterglow row is kept so its attempt count still
// counts when the item comes back; after that it is forgotten for good.
const PurgeGrace = 90 * 24 * time.Hour

// Mark records ids as searched: the first snatch rests `base`, every further snatch of the
// same item doubles the rest up to `maxRest` (exponential afterglow).
func (m *Memory) Mark(ctx context.Context, instanceID uuid.UUID, kind domain.SnatchKind, entityType string, ids []int64, base, maxRest time.Duration) error {
	if len(ids) == 0 {
		return nil
	}
	if maxRest < base {
		maxRest = base
	}
	err := m.st.Q().MarkProcessed(ctx, sqlcgen.MarkProcessedParams{
		InstanceID: instanceID, Kind: string(kind), EntityType: entityType,
		Now: m.clk.Now(), BaseS: int64(base / time.Second), MaxS: int64(maxRest / time.Second), EntityIds: ids,
	})
	if err != nil {
		return fmt.Errorf("snatch: mark processed: %w", store.MapError(err))
	}
	return nil
}

// Reset forgets everything for one instance, or for all when instanceID is nil.
func (m *Memory) Reset(ctx context.Context, instanceID *uuid.UUID) (int64, error) {
	n, err := m.st.Q().ResetProcessed(ctx, instanceID)
	if err != nil {
		return 0, fmt.Errorf("snatch: reset processed: %w", store.MapError(err))
	}
	return n, nil
}

// Purge drops long-expired entries and old buckets; meant for a periodic job. Rows that
// expired less than PurgeGrace ago stay so the afterglow backoff remembers them.
func (m *Memory) Purge(ctx context.Context) (int64, error) {
	now := m.clk.Now()
	n, err := m.st.Q().PurgeExpiredProcessed(ctx, now.Add(-PurgeGrace))
	if err != nil {
		return 0, fmt.Errorf("snatch: purge processed: %w", store.MapError(err))
	}
	if _, err := m.st.Q().PurgeBucketsBefore(ctx, Window(now).Add(-24*time.Hour)); err != nil {
		return n, fmt.Errorf("snatch: purge buckets: %w", store.MapError(err))
	}
	if _, err := m.st.Q().PurgeGlobalBucketsBefore(ctx, Window(now).Add(-24*time.Hour)); err != nil {
		return n, fmt.Errorf("snatch: purge global buckets: %w", store.MapError(err))
	}
	return n, nil
}
