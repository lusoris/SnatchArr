// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package domain

import (
	"time"

	"github.com/google/uuid"
)

// RunStatus is the lifecycle state of a hunt run.
type RunStatus string

// Run states.
const (
	RunQueued    RunStatus = "queued"
	RunLeased    RunStatus = "leased"
	RunDone      RunStatus = "done"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
)

// Run is one hunt cycle for one instance and hunt kind, executed by a worker.
type Run struct {
	ID             uuid.UUID
	InstanceID     uuid.UUID
	Kind           HuntKind
	Status         RunStatus
	LeasedBy       string
	LeaseExpiresAt *time.Time
	QueuedAt       time.Time
	StartedAt      *time.Time
	FinishedAt     *time.Time
	SearchedCount  int
	Error          string
}

// Event is one history entry, also streamed live over SSE.
type Event struct {
	ID         int64
	RunID      *uuid.UUID
	InstanceID uuid.UUID
	Timestamp  time.Time
	Level      string
	Type       string
	EntityType string
	EntityID   int64
	Title      string
	Detail     string
}

// SearchedItem is what a worker reports as dispatched.
type SearchedItem struct {
	EntityType string
	EntityID   int64
	Title      string
}
