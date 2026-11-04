// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients

import (
	"context"
	"errors"
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

// PriorityPlan is the latest successful observation. Mode describes effective group mode;
// in act mode, allow-listed priority, order and pause/resume mutations are executed.
type PriorityPlan struct {
	ClientID         uuid.UUID
	Mode             string
	ObservedAt       time.Time
	QueueingDisabled bool
	Unattributed     int
	ObserveRowNames  []string
	Plan             domain.DownloadClientPlan
}

// PriorityOwnership records SnatchArr's ownership state for an item in a download client.
type PriorityOwnership struct {
	ClientID            uuid.UUID
	ItemID              string
	LastWrittenPriority *int
	PausedBySnatchArr   bool
}

type priorityInputs struct {
	Clients   []domain.DownloadClient
	Instances []domain.Instance
	Preset    domain.PriorityPreset
}

type prioritySource interface {
	Load(context.Context) (priorityInputs, error)
	Secret(context.Context, uuid.UUID) (string, error)
	ListOwnership(context.Context, []uuid.UUID) ([]PriorityOwnership, error)
	UpsertPriority(context.Context, uuid.UUID, string, int) error
	SetPaused(context.Context, uuid.UUID, string) error
	ClearPaused(context.Context, uuid.UUID, string) error
	DeleteNotIn(context.Context, uuid.UUID, []string) error
}

// Reconciler is started exclusively by leader/gate as a leader_loops member.
type Reconciler struct {
	*gate.Periodic
	source   prioritySource
	observer PriorityObserver
	writer   PriorityWriter
	clk      clock.Clock
	logger   *slog.Logger
	mu       sync.RWMutex
	plans    map[uuid.UUID]PriorityPlan
}

func newPriorityReconciler(source prioritySource, observer PriorityObserver, writer PriorityWriter, clk clock.Clock, logger *slog.Logger) *Reconciler {
	return &Reconciler{source: source, observer: observer, writer: writer, clk: clk, logger: logger, plans: make(map[uuid.UUID]PriorityPlan)}
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
	plan.ObserveRowNames = slices.Clone(plan.ObserveRowNames)
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
		if ctx.Err() != nil {
			break
		}
		// Each observation ends within ObserveTimeout, so a full semaphore frees a slot in bounded time.
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			r.observePriorityGroup(ctx, rows, inputs)
		})
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("priority tick: %w", err)
	}
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

// groupCredentials returns the first row of a group that holds a stored secret,
// with that row's username. Rows of one group share a physical client but each
// keeps its own credentials, and discovery leaves new rows without a secret.
// Without any secret it falls back to rows[0]; it fails only when no row has a
// secret and a lookup failed.
func (r *Reconciler) groupCredentials(ctx context.Context, rows []domain.DownloadClient) (domain.DownloadClient, string, error) {
	var lookupErr error
	for _, row := range rows {
		secret, err := r.source.Secret(ctx, row.ID)
		if err != nil {
			lookupErr = errors.Join(lookupErr, err)
			continue
		}
		if secret != "" {
			return row, secret, nil
		}
	}
	if lookupErr != nil {
		return domain.DownloadClient{}, "", lookupErr
	}
	return rows[0], "", nil
}

func (r *Reconciler) observePriorityGroup(ctx context.Context, rows []domain.DownloadClient, inputs priorityInputs) {
	ctx, cancel := context.WithTimeout(ctx, ObserveTimeout)
	defer cancel()
	client, secret, err := r.groupCredentials(ctx, rows)
	if err != nil {
		r.priorityFailed(ctx, rows, "credentials")
		return
	}
	obs, err := r.observer.PriorityItems(ctx, client, secret)
	if err != nil {
		r.priorityFailed(ctx, rows, "read")
		return
	}
	ownership, stage := r.refreshOwnership(ctx, rows, obs.Items)
	if stage != "" {
		r.priorityFailed(ctx, rows, stage)
		return
	}
	items, unattributed, itemClients := attributePriorityItems(obs.Items, rows, inputs.Instances, inputs.Preset, ownership)
	plan, err := planPriorityObservation(client.Kind, obs.QueueingDisabled, items)
	if err != nil {
		r.priorityFailed(ctx, rows, "plan")
		return
	}
	mode, observeRowNames := groupPriorityMode(rows)
	if mode == "act" && r.writer != nil {
		r.executeActPlan(ctx, client, secret, plan, obs.QueueingDisabled, items, itemClients, ownership)
	}
	r.storeGroupPlans(rows, PriorityPlan{
		Mode:             mode,
		ObservedAt:       r.clk.Now(),
		QueueingDisabled: obs.QueueingDisabled,
		Unattributed:     unattributed,
		ObserveRowNames:  observeRowNames,
		Plan:             plan,
	})
	for _, row := range rows {
		r.logPriorityPlan(ctx, row.ID)
	}
}

// refreshOwnership drops ownership of items the client no longer lists, then loads what remains.
// The keep set is every observed item of the physical client, whatever its category: a row whose
// category turned empty or ambiguous must keep its paused-by-SnatchArr markers so those items resume.
// A non-empty stage names the failed step.
func (r *Reconciler) refreshOwnership(ctx context.Context, rows []domain.DownloadClient, items []PriorityItem) ([]PriorityOwnership, string) {
	observed := make([]string, 0, len(items))
	for _, item := range items {
		observed = append(observed, item.ID)
	}
	clientIDs := make([]uuid.UUID, len(rows))
	for i, row := range rows {
		if err := r.source.DeleteNotIn(ctx, row.ID, observed); err != nil {
			return nil, "prune_ownership"
		}
		clientIDs[i] = row.ID
	}
	ownership, err := r.source.ListOwnership(ctx, clientIDs)
	if err != nil {
		return nil, "load_ownership"
	}
	return ownership, ""
}

// groupPriorityMode applies the group mode rule: act only when every row of the physical client is act.
// It returns the effective mode and the names of the rows still on observe.
func groupPriorityMode(rows []domain.DownloadClient) (string, []string) {
	var observeRowNames []string
	for _, row := range rows {
		if row.PriorityMode != "act" {
			observeRowNames = append(observeRowNames, row.Name)
		}
	}
	if len(observeRowNames) > 0 {
		return "observe", observeRowNames
	}
	return "act", nil
}

// storeGroupPlans caches one copy of the group plan per row.
func (r *Reconciler) storeGroupPlans(rows []domain.DownloadClient, plan PriorityPlan) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, row := range rows {
		rowPlan := plan
		rowPlan.ClientID = row.ID
		rowPlan.ObserveRowNames = slices.Clone(plan.ObserveRowNames)
		r.plans[row.ID] = rowPlan
	}
}

// executeActPlan executes mutations across priority, order, pause and resume.
func (r *Reconciler) executeActPlan(
	ctx context.Context, client domain.DownloadClient, secret string, plan domain.DownloadClientPlan,
	queueingDisabled bool, items []domain.DownloadQueueItem, itemClients map[string]uuid.UUID, ownership []PriorityOwnership,
) {
	if !r.executePriorityWrites(ctx, client, secret, plan, itemClients) {
		return
	}
	if !r.executeOrderWrites(ctx, client, secret, plan, queueingDisabled, items, itemClients) {
		return
	}
	if !r.executePauseWrites(ctx, client, secret, plan, itemClients) {
		return
	}
	r.executeResumeWrites(ctx, client, secret, plan, itemClients, ownership)
}

// executePriorityWrites mutates priority levels on NZBGet and SABnzbd items.
func (r *Reconciler) executePriorityWrites(
	ctx context.Context, client domain.DownloadClient, secret string,
	plan domain.DownloadClientPlan, itemClients map[string]uuid.UUID,
) bool {
	if client.Kind != domain.ClientNZBGet && client.Kind != domain.ClientSABnzbd {
		return true
	}
	for _, m := range plan.Priority {
		targetID := itemClients[m.ID]
		callCtx, cancel := context.WithTimeout(ctx, ObserveTimeout)
		err := r.writer.SetItemPriority(callCtx, client, secret, m.ID, m.To)
		cancel()
		if err != nil {
			r.priorityWriteFailed(ctx, targetID, "priority")
			return false
		}
		if err := r.source.UpsertPriority(ctx, targetID, m.ID, m.To); err != nil {
			r.priorityWriteFailed(ctx, targetID, "priority")
			return false
		}
	}
	return true
}

// executeOrderWrites orders eligible queued qBittorrent torrents with MoveToTop in reverse plan order.
func (r *Reconciler) executeOrderWrites(
	ctx context.Context, client domain.DownloadClient, secret string,
	plan domain.DownloadClientPlan, queueingDisabled bool, items []domain.DownloadQueueItem, itemClients map[string]uuid.UUID,
) bool {
	if client.Kind != domain.ClientQBittorrent || queueingDisabled {
		return true
	}
	queuedAttributed := make(map[string]bool, len(items))
	for _, it := range items {
		if it.State == domain.DownloadItemQueued {
			queuedAttributed[it.ID] = true
		}
	}
	var hashes []string
	for _, id := range plan.Order {
		if queuedAttributed[id] {
			hashes = append(hashes, id)
		}
	}
	for i := len(hashes) - 1; i >= 0; i-- {
		hash := hashes[i]
		targetID := itemClients[hash]
		callCtx, cancel := context.WithTimeout(ctx, ObserveTimeout)
		err := r.writer.MoveToTop(callCtx, client, secret, []string{hash})
		cancel()
		if err != nil {
			r.priorityWriteFailed(ctx, targetID, "order")
			return false
		}
	}
	return true
}

// executePauseWrites pauses active lower-rank transfers while higher-rank work waits.
func (r *Reconciler) executePauseWrites(
	ctx context.Context, client domain.DownloadClient, secret string,
	plan domain.DownloadClientPlan, itemClients map[string]uuid.UUID,
) bool {
	if len(plan.Pause) == 0 {
		return true
	}
	callCtx, cancel := context.WithTimeout(ctx, ObserveTimeout)
	err := r.writer.PauseItems(callCtx, client, secret, plan.Pause)
	cancel()
	if err != nil {
		r.markPartial(ctx, err, itemClients, r.source.SetPaused, "pause_record")
		r.priorityWriteFailed(ctx, writeTarget(client.ID, itemClients, plan.Pause), "pause")
		return false
	}
	return r.markAll(ctx, plan.Pause, itemClients, r.source.SetPaused, "pause")
}

// executeResumeWrites resumes transfers SnatchArr paused once higher-rank work is no longer waiting or downloading.
func (r *Reconciler) executeResumeWrites(
	ctx context.Context, client domain.DownloadClient, secret string,
	plan domain.DownloadClientPlan, itemClients map[string]uuid.UUID, ownership []PriorityOwnership,
) bool {
	eligible := pausedBySnatchArr(plan.Resume, ownership)
	if len(eligible) == 0 {
		return true
	}
	callCtx, cancel := context.WithTimeout(ctx, ObserveTimeout)
	err := r.writer.ResumeItems(callCtx, client, secret, eligible)
	cancel()
	if err != nil {
		r.markPartial(ctx, err, itemClients, r.source.ClearPaused, "resume_record")
		r.priorityWriteFailed(ctx, writeTarget(client.ID, itemClients, eligible), "resume")
		return false
	}
	return r.markAll(ctx, eligible, itemClients, r.source.ClearPaused, "resume")
}

// ownershipMark records one ownership change for an item of a download client row.
type ownershipMark func(ctx context.Context, clientID uuid.UUID, itemID string) error

// pausedBySnatchArr keeps the resume candidates whose stored ownership says SnatchArr paused them.
func pausedBySnatchArr(resume []string, ownership []PriorityOwnership) []string {
	stored := make(map[string]bool, len(ownership))
	for _, o := range ownership {
		if o.PausedBySnatchArr {
			stored[o.ItemID] = true
		}
	}
	var eligible []string
	for _, id := range resume {
		if stored[id] {
			eligible = append(eligible, id)
		}
	}
	return eligible
}

// markPartial records ownership for the items a partially failed write completed; a failed record is logged.
func (r *Reconciler) markPartial(ctx context.Context, err error, itemClients map[string]uuid.UUID, mark ownershipMark, stage string) {
	var partial *PartialWriteError
	if !errors.As(err, &partial) {
		return
	}
	for _, id := range partial.Done {
		if markErr := mark(ctx, itemClients[id], id); markErr != nil {
			r.priorityWriteFailed(ctx, itemClients[id], stage)
		}
	}
}

// markAll records ownership for every written item and stops at the first failed record.
func (r *Reconciler) markAll(ctx context.Context, ids []string, itemClients map[string]uuid.UUID, mark ownershipMark, stage string) bool {
	for _, id := range ids {
		if err := mark(ctx, itemClients[id], id); err != nil {
			r.priorityWriteFailed(ctx, itemClients[id], stage)
			return false
		}
	}
	return true
}

// writeTarget names the row a failed write is logged against: the first item's row, else the physical client.
func writeTarget(fallback uuid.UUID, itemClients map[string]uuid.UUID, ids []string) uuid.UUID {
	if len(ids) > 0 {
		if id := itemClients[ids[0]]; id != uuid.Nil {
			return id
		}
	}
	return fallback
}

func (r *Reconciler) priorityWriteFailed(ctx context.Context, clientID uuid.UUID, stage string) {
	r.logger.WarnContext(ctx, "download priority write failed", slog.String("client_id", clientID.String()), slog.String("stage", stage))
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
