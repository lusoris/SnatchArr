// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package snatch

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/events"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

// Recorder persists snatch events and fans them out over SSE.
type Recorder struct {
	st  *store.Store
	clk clock.Clock
	bus *events.Bus
}

// NewRecorder wires the recorder.
func NewRecorder(st *store.Store, clk clock.Clock, bus *events.Bus) *Recorder {
	return &Recorder{st: st, clk: clk, bus: bus}
}

// Record stores one event (timestamp defaults to now) and publishes it live.
func (r *Recorder) Record(ctx context.Context, e domain.Event) error {
	if e.Timestamp.IsZero() {
		e.Timestamp = r.clk.Now()
	}
	e.Type = events.EventName(e.Type)
	e.Level = strings.ToLower(e.Level)
	if e.Level == "" {
		e.Level = "info"
	}
	if _, err := r.st.Q().InsertEvent(ctx, sqlcgen.InsertEventParams{
		RunID: e.RunID, InstanceID: e.InstanceID, Ts: e.Timestamp, Level: e.Level, Type: e.Type,
		EntityType: e.EntityType, EntityID: e.EntityID, Title: e.Title, Detail: e.Detail,
	}); err != nil {
		return fmt.Errorf("snatch: record event: %w", store.MapError(err))
	}
	runID := ""
	if e.RunID != nil {
		runID = e.RunID.String()
	}
	r.bus.Publish(ctx, events.Frame{
		RunID: runID, InstanceID: e.InstanceID, Timestamp: e.Timestamp, Level: e.Level, Type: e.Type,
		EntityType: e.EntityType, EntityID: e.EntityID, Title: e.Title, Detail: e.Detail,
	})
	return nil
}

// ListParams filter the history.
type ListParams struct {
	InstanceID *uuid.UUID
	BeforeID   *int64
	Type       string
	PageSize   int
}

// List pages history newest first.
func (r *Recorder) List(ctx context.Context, p ListParams) ([]domain.Event, error) {
	var typ *string
	if p.Type != "" {
		typ = &p.Type
	}
	rows, err := r.st.Q().ListEvents(ctx, sqlcgen.ListEventsParams{
		InstanceID: p.InstanceID, BeforeID: p.BeforeID, Type: typ,
		PageSize: int32(min(max(p.PageSize, 1), 1000)), // #nosec G115 -- clamped
	})
	if err != nil {
		return nil, fmt.Errorf("snatch: list events: %w", store.MapError(err))
	}
	out := make([]domain.Event, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Event{
			ID: row.ID, RunID: row.RunID, InstanceID: row.InstanceID, Timestamp: row.Ts, Level: row.Level, Type: row.Type,
			EntityType: row.EntityType, EntityID: row.EntityID, Title: row.Title, Detail: row.Detail,
		})
	}
	return out, nil
}

// DeleteForInstance clears one instance's history.
func (r *Recorder) DeleteForInstance(ctx context.Context, instanceID uuid.UUID) (int64, error) {
	n, err := r.st.Q().DeleteEventsForInstance(ctx, instanceID)
	if err != nil {
		return 0, fmt.Errorf("snatch: delete events: %w", store.MapError(err))
	}
	return n, nil
}
