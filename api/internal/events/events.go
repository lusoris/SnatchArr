// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package events fans snatch events out to browser clients over Server-Sent Events.
// Event names are the snatcharr.v1.EventType names in lower snake case.
package events

import (
	"context"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/fx"

	"github.com/golusoris/golusoris/realtime/sse"

	snatcharrv1 "github.com/lusoris/SnatchArr/api/internal/gen/snatcharr/v1"
)

// Frame is the JSON payload of every SSE event.
type Frame struct {
	ID         int64     `json:"id,omitempty"`
	RunID      string    `json:"run_id,omitempty"`
	InstanceID uuid.UUID `json:"instance_id"`
	Timestamp  time.Time `json:"ts"`
	Level      string    `json:"level"`
	Type       string    `json:"type"`
	EntityType string    `json:"entity_type,omitempty"`
	EntityID   int64     `json:"entity_id,omitempty"`
	Title      string    `json:"title,omitempty"`
	Detail     string    `json:"detail,omitempty"`
}

// Bus wraps the golusoris SSE hub with SnatchArr's frame shape.
type Bus struct {
	hub *sse.Hub
}

// New builds a Bus.
func New(logger *slog.Logger) *Bus {
	return &Bus{hub: sse.NewHub(logger)}
}

// Hub exposes the underlying hub for mounting its HTTP handler.
func (b *Bus) Hub() *sse.Hub { return b.hub }

// Publish sends one frame to every connected client. The SSE event name is the
// frame type, e.g. "search_dispatched".
func (b *Bus) Publish(ctx context.Context, f Frame) {
	evt := sse.Event{Event: EventName(f.Type), Data: f}
	if f.ID != 0 {
		evt.ID = strconv.FormatInt(f.ID, 10)
	}
	b.hub.Publish(ctx, evt)
}

// EventName normalises a proto enum name such as EVENT_TYPE_SEARCH_DISPATCHED to
// search_dispatched.
func EventName(protoName string) string {
	n := strings.TrimPrefix(protoName, "EVENT_TYPE_")
	return strings.ToLower(n)
}

// EventTypes lists the SSE name of every defined proto event type.
func EventTypes() []string {
	names := make([]string, 0, len(snatcharrv1.EventType_name))
	for _, name := range snatcharrv1.EventType_name {
		names = append(names, EventName(name))
	}
	return names
}

// apiTypes are the event types the API records itself, outside
// snatcharr.v1.EventType. web/src/lib/live-events.ts mirrors this list.
var apiTypes = []string{
	"run_queued", "snatch_paced", "stamina_exhausted", "seerr_request_snatched",
	"events_unconfirmed", "snatch_backoff", "notification_failed", "snatch_deferred", "snatch_resumed",
}

// IsKnownType reports whether name is an event type a worker or the API records.
func IsKnownType(name string) bool {
	return slices.Contains(apiTypes, name) || slices.Contains(EventTypes(), name)
}

// Module provides the Bus.
var Module = fx.Module("snatcharr.events", fx.Provide(New))
