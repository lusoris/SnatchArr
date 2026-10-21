// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package queue

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/events"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
)

// EventRecorder persists domain events.
type EventRecorder interface {
	Record(ctx context.Context, e domain.Event) error
}

// RecorderAdapter adapts an EventRecorder to EventPublisher, persisting queue
// events into snatch_events and publishing them live over SSE.
type RecorderAdapter struct {
	rec    EventRecorder
	bus    *events.Bus
	logger *slog.Logger
}

// NewRecorderAdapter creates a RecorderAdapter using snatch.Recorder.
func NewRecorderAdapter(rec *snatch.Recorder, bus *events.Bus, logger *slog.Logger) *RecorderAdapter {
	return NewCustomRecorderAdapter(rec, bus, logger)
}

// NewCustomRecorderAdapter creates a RecorderAdapter with any EventRecorder (for testing).
func NewCustomRecorderAdapter(rec EventRecorder, bus *events.Bus, logger *slog.Logger) *RecorderAdapter {
	return &RecorderAdapter{rec: rec, bus: bus, logger: logger}
}

// Publish persists the event and streams it live. If persistence fails, it logs
// a warning and still publishes directly to the bus.
func (a *RecorderAdapter) Publish(ctx context.Context, f events.Frame) {
	ev := frameToDomainEvent(f)
	if err := a.rec.Record(ctx, ev); err != nil {
		a.logger.WarnContext(ctx, "queue: persist event failed, falling back to bus",
			slog.String("type", f.Type),
			slog.String("error", err.Error()),
		)
		a.bus.Publish(ctx, f)
	}
}

func frameToDomainEvent(f events.Frame) domain.Event {
	var runID *uuid.UUID
	if f.RunID != "" {
		if id, err := uuid.Parse(f.RunID); err == nil {
			runID = &id
		}
	}
	return domain.Event{
		RunID:      runID,
		InstanceID: f.InstanceID,
		Timestamp:  f.Timestamp,
		Level:      f.Level,
		Type:       f.Type,
		EntityType: f.EntityType,
		EntityID:   f.EntityID,
		Title:      f.Title,
		Detail:     f.Detail,
	}
}
