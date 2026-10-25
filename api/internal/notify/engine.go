// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package notify manages notification channels, delivery, and event-driven rules.
package notify

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

const (
	queueCapacity = 1024
	workerCount   = 2
	staminaWindow = 1 * time.Hour
)

// RecorderSink defines the subset of Recorder needed to publish failure events.
type RecorderSink interface {
	Record(ctx context.Context, e domain.Event) error
}

// Engine observes snatch events and dispatches notifications via configured rules.
type Engine struct {
	st          *store.Store
	ws          *WebhookStore
	svc         *Service
	enc         *crypto.Encryptor
	clk         clock.Clock
	rec         RecorderSink
	logger      *slog.Logger
	queue       chan domain.Event
	mu          sync.Mutex
	lastStamina map[uuid.UUID]time.Time
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	// slots holds one token per running worker, so the pool never exceeds workerCount.
	slots           chan struct{}
	MaxWorkerEvents int
}

// NewEngine builds a background notification engine.
func NewEngine(st *store.Store, ws *WebhookStore, svc *Service, enc *crypto.Encryptor, clk clock.Clock, logger *slog.Logger) *Engine {
	return &Engine{
		st:          st,
		ws:          ws,
		svc:         svc,
		enc:         enc,
		clk:         clk,
		logger:      logger,
		queue:       make(chan domain.Event, queueCapacity),
		lastStamina: make(map[uuid.UUID]time.Time),
	}
}

// SetRecorder sets the event recorder for failure events.
func (e *Engine) SetRecorder(rec RecorderSink) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rec = rec
}

// Start launches the background worker pool (HISS-06 bounded workers).
func (e *Engine) Start(parent context.Context) error {
	wctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	e.cancel = cancel
	e.slots = make(chan struct{}, workerCount)
	for range workerCount {
		e.slots <- struct{}{}
		e.wg.Add(1)
		go e.worker(wctx)
	}
	return nil
}

// Stop shuts down the background worker pool gracefully.
func (e *Engine) Stop(ctx context.Context) error {
	if e.cancel != nil {
		e.cancel()
	}
	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("notify: stop: %w", ctx.Err())
	}
}

// OnEvent implements snatch.EventObserver. Delivery never blocks the caller.
func (e *Engine) OnEvent(ctx context.Context, ev domain.Event) {
	if ev.Type == "notification_failed" {
		return
	}
	select {
	case e.queue <- ev:
	default:
		e.logger.WarnContext(ctx, "notify: event queue full, dropped", slog.String("type", ev.Type))
	}
}

const (
	defaultMaxWorkerEvents = 10_000_000
	maxWorkerRestarts      = 10_000_000
)

func (e *Engine) worker(ctx context.Context) {
	defer e.wg.Done()
	defer func() { <-e.slots }()
	bound := e.MaxWorkerEvents
	if bound <= 0 {
		bound = defaultMaxWorkerEvents
	}
	for range maxWorkerRestarts {
		for range bound {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-e.queue:
				if !ok {
					return
				}
				e.ProcessEvent(ctx, ev)
			}
		}
		e.logger.ErrorContext(ctx, "notify: worker loop iteration bound reached, resetting iteration budget", slog.Int("bound", bound))
	}
	e.logger.ErrorContext(ctx, "notify: worker exceeded maximum restart cycles, stopping")
}

// ProcessEvent matches an event against all active rules and executes fan-out delivery.
func (e *Engine) ProcessEvent(ctx context.Context, ev domain.Event) {
	pctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	rules, err := e.matchRules(pctx, ev)
	if err != nil {
		e.logger.WarnContext(pctx, "notify: match rules failed", slog.String("error", err.Error()))
		return
	}
	for _, rule := range rules {
		e.deliverRule(pctx, rule, ev)
	}
}

func (e *Engine) matchRules(ctx context.Context, ev domain.Event) ([]domain.NotificationRule, error) {
	rows, err := e.st.Q().ListEnabledNotificationRules(ctx)
	if err != nil {
		return nil, fmt.Errorf("list enabled rules: %w", store.MapError(err))
	}
	rules := store.NotificationRulesFromRows(rows)
	matched := make([]domain.NotificationRule, 0, len(rules))
	now := e.clk.Now()
	for _, rule := range rules {
		if e.ruleMatches(ctx, rule, ev, now) {
			matched = append(matched, rule)
		}
	}
	return matched, nil
}

func (e *Engine) ruleMatches(ctx context.Context, rule domain.NotificationRule, ev domain.Event, now time.Time) bool {
	switch rule.EventKind {
	case domain.RuleCircuitOpened:
		return ev.Type == "circuit_opened"
	case domain.RuleInstanceUnreachable:
		return ev.Type == "instance_unreachable"
	case domain.RuleStaminaExhausted:
		return ev.Type == "stamina_exhausted" && e.matchStamina(ev.InstanceID, now)
	case domain.RuleRunFailureStreak:
		return e.matchStreak(ctx, rule, ev)
	case domain.RuleSeerrRequestSnatched:
		return e.matchSeerr(ev)
	case domain.RuleWatchawareGateDecided:
		return ev.Type == "watchaware_gate_decided"
	default:
		return false
	}
}

func (e *Engine) matchStamina(instanceID uuid.UUID, now time.Time) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	last, ok := e.lastStamina[instanceID]
	if ok && now.Sub(last) < staminaWindow {
		return false
	}
	e.lastStamina[instanceID] = now
	return true
}

// ResetStaminaDedupe clears cached stamina notification timestamps (used in boundary tests).
func (e *Engine) ResetStaminaDedupe() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.lastStamina = make(map[uuid.UUID]time.Time)
}

func (e *Engine) matchStreak(ctx context.Context, rule domain.NotificationRule, ev domain.Event) bool {
	if ev.Type != "run_finished" || ev.Level != "error" || ev.InstanceID == uuid.Nil {
		return false
	}
	limit := int32(50)
	statuses, err := e.st.Q().RecentInstanceRunStatuses(ctx, sqlcgen.RecentInstanceRunStatusesParams{
		InstanceID: ev.InstanceID,
		Limit:      limit,
	})
	if err != nil {
		e.logger.WarnContext(ctx, "notify: streak check query failed", slog.String("error", err.Error()))
		return false
	}
	streak := snatch.CountTrailingFailures(statuses)
	threshold := max(rule.Threshold, 1)
	return streak == threshold
}

func (e *Engine) matchSeerr(ev domain.Event) bool {
	return ev.Type == "seerr_request_snatched"
}

func (e *Engine) deliverRule(ctx context.Context, rule domain.NotificationRule, ev domain.Event) {
	if len(rule.ChannelIDs) == 0 {
		return
	}
	rows, err := e.st.Q().ListNotificationChannelsByIDs(ctx, rule.ChannelIDs)
	if err != nil {
		e.logger.WarnContext(ctx, "notify: load channels failed", slog.String("error", err.Error()))
		return
	}
	payload := Payload{
		Event:      string(rule.EventKind),
		Timestamp:  ev.Timestamp,
		Title:      ev.Title,
		Detail:     ev.Detail,
		Level:      ev.Level,
		InstanceID: &ev.InstanceID,
		RunID:      ev.RunID,
	}
	for _, r := range rows {
		if !r.Enabled {
			continue
		}
		ch, err := e.svc.channelFromRow(r)
		if err != nil {
			e.logger.WarnContext(ctx, "notify: decrypt channel config failed", slog.String("error", err.Error()))
			continue
		}
		e.deliverChannel(ctx, ch, payload, ev)
	}
}

func (e *Engine) deliverChannel(ctx context.Context, ch domain.NotificationChannel, p Payload, ev domain.Event) {
	dctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := Deliver(dctx, ch, p, e.ws, e.logger, e.clk); err != nil {
		sanitized := SanitizeError(err, ch.Config)
		e.logger.WarnContext(ctx, "notify: channel deliver failed", slog.String("channel", ch.Name), slog.String("error", sanitized))
		if e.rec != nil {
			if rErr := e.rec.Record(ctx, domain.Event{
				InstanceID: ev.InstanceID,
				RunID:      ev.RunID,
				Level:      "warn",
				Type:       "notification_failed",
				Title:      "Notification failed: " + ch.Name,
				Detail:     sanitized,
			}); rErr != nil {
				e.logger.WarnContext(ctx, "notify: record failure event error", slog.String("error", rErr.Error()))
			}
		}
	}
}
