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

// NewReconciler wires a timer without starting it. Only leader/gate starts the loop.
func NewReconciler(clients *Service, inst *instances.Service, settings *settings.Service, cfg config.Options,
	clk clock.Clock, logger *slog.Logger, m *metrics.Metrics,
) (*Reconciler, error) {
	if err := cfg.DownloadPriority.Validate(); err != nil {
		return nil, err
	}
	source := storedPrioritySource{clients: clients, instances: inst, settings: settings}
	r := newPriorityReconciler(source, NewGoenvoy(clk), clk, logger)
	interval := time.Duration(cfg.DownloadPriority.IntervalSeconds) * time.Second
	r.Periodic = gate.NewPeriodic("download_priority_observe", interval, clk, m, r.Tick)
	return r, nil
}
