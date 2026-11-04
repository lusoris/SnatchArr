// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/leader/gate"

	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/metrics"
	"github.com/lusoris/SnatchArr/api/internal/settings"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

type storedPrioritySource struct {
	clients   *Service
	instances *instances.Service
	settings  *settings.Service
}

func (s storedPrioritySource) Load(ctx context.Context) (priorityInputs, error) {
	clients, err := s.clients.List(ctx)
	if err != nil {
		return priorityInputs{}, err
	}
	instances, err := s.instances.List(ctx)
	if err != nil {
		return priorityInputs{}, err
	}
	settings, err := s.settings.Get(ctx)
	if err != nil {
		return priorityInputs{}, err
	}
	return priorityInputs{Clients: clients, Instances: instances, Preset: settings.DownloadPriorityPreset}, nil
}

func (s storedPrioritySource) Secret(ctx context.Context, clientID uuid.UUID) (string, error) {
	row, err := s.clients.st.Q().GetDownloadClient(ctx, clientID)
	if err != nil {
		return "", fmt.Errorf("priority client: %w", err)
	}
	secret, err := s.clients.enc.Open(row.SecretEnc)
	if err != nil {
		return "", fmt.Errorf("priority secret: %w", err)
	}
	return string(secret), nil
}

func (s storedPrioritySource) ListOwnership(ctx context.Context, clientIDs []uuid.UUID) ([]PriorityOwnership, error) {
	rows, err := s.clients.st.Q().ListPriorityOwnershipByClientIDs(ctx, clientIDs)
	if err != nil {
		return nil, fmt.Errorf("list priority ownership: %w", err)
	}
	out := make([]PriorityOwnership, len(rows))
	for i, r := range rows {
		var lastPrio *int
		if r.LastWrittenPriority != nil {
			v := int(*r.LastWrittenPriority)
			lastPrio = &v
		}
		out[i] = PriorityOwnership{
			ClientID:            r.ClientID,
			ItemID:              r.ItemID,
			LastWrittenPriority: lastPrio,
			PausedBySnatchArr:   r.PausedBySnatcharr,
			PendingAction:       r.PendingAction,
			PendingPriority:     priorityInt(r.PendingPriority),
		}
	}
	return out, nil
}

func (s storedPrioritySource) UpsertPriority(ctx context.Context, clientID uuid.UUID, itemID string, priority int) error {
	p32 := int32(priority) // #nosec G115 -- priority levels are -100..100
	err := s.clients.st.Q().UpsertPriorityOwnershipPriority(ctx, sqlcgen.UpsertPriorityOwnershipPriorityParams{
		ClientID:            clientID,
		ItemID:              itemID,
		LastWrittenPriority: &p32,
		UpdatedAt:           s.clients.clk.Now(),
	})
	if err != nil {
		return fmt.Errorf("upsert priority ownership: %w", err)
	}
	return nil
}

func (s storedPrioritySource) SetPaused(ctx context.Context, clientID uuid.UUID, itemID string) error {
	err := s.clients.st.Q().SetPriorityOwnershipPaused(ctx, sqlcgen.SetPriorityOwnershipPausedParams{
		ClientID:  clientID,
		ItemID:    itemID,
		UpdatedAt: s.clients.clk.Now(),
	})
	if err != nil {
		return fmt.Errorf("set priority ownership paused: %w", err)
	}
	return nil
}

func (s storedPrioritySource) ClearPaused(ctx context.Context, clientID uuid.UUID, itemID string) error {
	err := s.clients.st.Q().ClearPriorityOwnershipPaused(ctx, sqlcgen.ClearPriorityOwnershipPausedParams{
		ClientID:  clientID,
		ItemID:    itemID,
		UpdatedAt: s.clients.clk.Now(),
	})
	if err != nil {
		return fmt.Errorf("clear priority ownership paused: %w", err)
	}
	return nil
}

func (s storedPrioritySource) DeleteNotIn(ctx context.Context, clientID uuid.UUID, keepItemIDs []string) error {
	if keepItemIDs == nil {
		keepItemIDs = []string{}
	}
	_, err := s.clients.st.Q().DeletePriorityOwnershipNotIn(ctx, sqlcgen.DeletePriorityOwnershipNotInParams{
		ClientID:    clientID,
		KeepItemIds: keepItemIDs,
	})
	if err != nil {
		return fmt.Errorf("delete priority ownership: %w", err)
	}
	return nil
}

// NewReconciler wires a timer without starting it. Only leader/gate starts the loop.
func NewReconciler(clients *Service, inst *instances.Service, settings *settings.Service, cfg config.Options,
	clk clock.Clock, logger *slog.Logger, m *metrics.Metrics,
) (*Reconciler, error) {
	if err := cfg.DownloadPriority.Validate(); err != nil {
		return nil, err
	}
	source := storedPrioritySource{clients: clients, instances: inst, settings: settings}
	envoy := NewGoenvoy(clk)
	r := newPriorityReconciler(source, envoy, envoy, clk, logger)
	interval := time.Duration(cfg.DownloadPriority.IntervalSeconds) * time.Second
	r.Periodic = gate.NewPeriodic("download_priority_observe", interval, clk, m, r.Tick)
	return r, nil
}

func priorityInt(value *int32) *int {
	if value == nil {
		return nil
	}
	return new(int(*value))
}

func (s storedPrioritySource) PrepareWrite(ctx context.Context, clientID uuid.UUID, itemID, action string, priority *int) error {
	var value *int32
	if priority != nil {
		p := int32(*priority) // #nosec G115 -- planned ordinary priority levels are -100..100.
		value = &p
	}
	if err := s.clients.st.Q().PreparePriorityIntent(ctx, sqlcgen.PreparePriorityIntentParams{
		ClientID: clientID, ItemID: itemID, PendingAction: action, PendingPriority: value, UpdatedAt: s.clients.clk.Now(),
	}); err != nil {
		return fmt.Errorf("prepare priority ownership: %w", err)
	}
	return nil
}

func (s storedPrioritySource) CancelWrite(ctx context.Context, clientID uuid.UUID, itemID string) error {
	if err := s.clients.st.Q().CancelPriorityIntent(ctx, sqlcgen.CancelPriorityIntentParams{
		ClientID: clientID, ItemID: itemID, UpdatedAt: s.clients.clk.Now(),
	}); err != nil {
		return fmt.Errorf("cancel priority ownership: %w", err)
	}
	return nil
}
