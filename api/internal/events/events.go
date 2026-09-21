// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package events fans hunt events out to browser clients over Server-Sent Events.
// Event names are the snatcharr.v1.EventType names in lower snake case.
package events

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/fx"

	"github.com/golusoris/golusoris/realtime/sse"
)

// Frame is the JSON payload of every SSE event.
type Frame struct {
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
	b.hub.Publish(ctx, sse.Event{Event: EventName(f.Type), Data: f})
}

// EventName normalises a proto enum name such as EVENT_TYPE_SEARCH_DISPATCHED to
// search_dispatched.
func EventName(protoName string) string {
	n := strings.TrimPrefix(protoName, "EVENT_TYPE_")
	return strings.ToLower(n)
}

// Module provides the Bus.
var Module = fx.Module("snatcharr.events", fx.Provide(New))
