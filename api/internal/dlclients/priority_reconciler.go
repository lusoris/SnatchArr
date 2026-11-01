// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/leader/gate"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

const maxPriorityWorkers = 4

// PriorityPlan is the latest successful observation. Mode describes configuration;
// even act computes a plan only in S3a. No item mutation capability is held here.
type PriorityPlan struct {
	ClientID         uuid.UUID
	Mode             string
	ObservedAt       time.Time
	QueueingDisabled bool
	Unattributed     int
	Plan             domain.DownloadClientPlan
}

type priorityInputs struct {
	Clients   []domain.DownloadClient
	Instances []domain.Instance
	Preset    domain.PriorityPreset
}

type prioritySource interface {
	Load(context.Context) (priorityInputs, error)
	Secret(context.Context, uuid.UUID) (string, error)
}

// Reconciler is started exclusively by leader/gate as a leader_loops member.
type Reconciler struct {
	*gate.Periodic
	source   prioritySource
	observer PriorityObserver
	clk      clock.Clock
	logger   *slog.Logger
	mu       sync.RWMutex
	plans    map[uuid.UUID]PriorityPlan
}

func newPriorityReconciler(source prioritySource, observer PriorityObserver, clk clock.Clock, logger *slog.Logger) *Reconciler {
	return &Reconciler{source: source, observer: observer, clk: clk, logger: logger, plans: make(map[uuid.UUID]PriorityPlan)}
}

// Latest returns an owned copy, preventing HTTP consumers from mutating the cache.
func (r *Reconciler) Latest(clientID uuid.UUID) (PriorityPlan, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	plan, ok := r.plans[clientID]
	if !ok {
		return PriorityPlan{}, fmt.Errorf("priority plan: %w", domain.ErrNotFound)
	}
	plan.Plan.Priority = slices.Clone(plan.Plan.Priority)
	plan.Plan.Order = slices.Clone(plan.Plan.Order)
	plan.Plan.Pause = slices.Clone(plan.Plan.Pause)
	plan.Plan.Resume = slices.Clone(plan.Plan.Resume)
	return plan, nil
}

// Tick snapshots configuration once and isolates physical client failures.
func (r *Reconciler) Tick(ctx context.Context) error {
	loadCtx, cancel := context.WithTimeout(ctx, ObserveTimeout)
	inputs, err := r.source.Load(loadCtx)
	cancel()
	if err != nil {
		r.logger.WarnContext(ctx, "download priority inputs failed")
		return fmt.Errorf("priority inputs: %w", err)
	}
	groups := priorityGroups(inputs.Clients)
	r.prunePlans(inputs.Clients)
	sem := make(chan struct{}, maxPriorityWorkers)
	var wg sync.WaitGroup
	for _, rows := range groups {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return fmt.Errorf("priority tick: %w", ctx.Err())
		}
		wg.Go(func() {
			defer func() { <-sem }()
			r.observePriorityGroup(ctx, rows, inputs)
		})
	}
	wg.Wait()
	return nil
}

func (r *Reconciler) prunePlans(clients []domain.DownloadClient) {
	keep := make(map[uuid.UUID]bool, len(clients))
	for _, c := range clients {
		if c.Enabled && priorityKind(c.Kind) {
			if _, err := domain.NormalizeBaseURL(c.BaseURL); err == nil {
				keep[c.ID] = true
			}
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for id := range r.plans {
		if !keep[id] {
			delete(r.plans, id)
		}
	}
}

func priorityGroups(clients []domain.DownloadClient) [][]domain.DownloadClient {
	groups := make([][]domain.DownloadClient, 0)
	indexes := make(map[string]int)
	for _, c := range clients {
		if !c.Enabled || !priorityKind(c.Kind) {
			continue
		}
		normalized, err := domain.NormalizeBaseURL(c.BaseURL)
		if err != nil {
			continue
		}
		c.BaseURL = normalized
		key := string(c.Kind) + "\n" + normalized
		index, exists := indexes[key]
		if !exists {
			index = len(groups)
			indexes[key] = index
			groups = append(groups, nil)
		}
		groups[index] = append(groups[index], c)
	}
	return groups
}

func priorityKind(kind domain.DownloadClientKind) bool {
	return kind == domain.ClientQBittorrent || kind == domain.ClientNZBGet || kind == domain.ClientSABnzbd
}

func (r *Reconciler) observePriorityGroup(ctx context.Context, rows []domain.DownloadClient, inputs priorityInputs) {
	client := rows[0]
	ctx, cancel := context.WithTimeout(ctx, ObserveTimeout)
	defer cancel()
	secret, err := r.source.Secret(ctx, client.ID)
	if err != nil {
		r.priorityFailed(ctx, rows, "credentials")
		return
	}
	obs, err := r.observer.PriorityItems(ctx, client, secret)
	if err != nil {
		r.priorityFailed(ctx, rows, "read")
		return
	}
	items, unattributed := attributePriorityItems(obs.Items, rows, inputs.Instances, inputs.Preset)
	plan, err := planPriorityObservation(client.Kind, obs.QueueingDisabled, items)
	if err != nil {
		r.priorityFailed(ctx, rows, "plan")
		return
	}
	observedAt := r.clk.Now()
	r.mu.Lock()
	for _, row := range rows {
		mode := row.PriorityMode
		if mode == "" {
			mode = "observe"
		}
		r.plans[row.ID] = PriorityPlan{
			ClientID: row.ID, Mode: mode, ObservedAt: observedAt,
			QueueingDisabled: obs.QueueingDisabled, Unattributed: unattributed, Plan: plan,
		}
	}
	r.mu.Unlock()
	for _, row := range rows {
		r.logPriorityPlan(ctx, row.ID)
	}
}

func (r *Reconciler) priorityFailed(ctx context.Context, rows []domain.DownloadClient, stage string) {
	for _, row := range rows {
		// Remote error text can include URLs or library content; retain identifiers only.
		r.logger.WarnContext(ctx, "download priority observation failed", slog.String("client_id", row.ID.String()), slog.String("stage", stage))
	}
}

func (r *Reconciler) logPriorityPlan(ctx context.Context, clientID uuid.UUID) {
	p, err := r.Latest(clientID)
	if err != nil {
		return
	}
	r.logger.InfoContext(ctx, "download priority plan", slog.String("client_id", clientID.String()),
		slog.String("mode", p.Mode), slog.Bool("queueing_disabled", p.QueueingDisabled),
		slog.Int("unattributed", p.Unattributed), slog.Int("priority", len(p.Plan.Priority)),
		slog.Int("order", len(p.Plan.Order)), slog.Int("pause", len(p.Plan.Pause)), slog.Int("resume", len(p.Plan.Resume)))
}
