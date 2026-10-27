// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package queue

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/events"
	"github.com/lusoris/SnatchArr/api/internal/store"
)

// Sweep bounds: pages per instance, records per page, expired strike rows
// purged per sweep.
const (
	sweepMaxPages = 10
	sweepPageSize = 200
	purgeBatch    = 1000
)

// Instance holds credentials and metadata of an enabled *arr instance.
type Instance struct {
	ID               uuid.UUID
	Name             string
	Kind             domain.AppKind
	BaseURL          string
	APIKey           string
	DownloadPriority *int
}

// InstanceSource lists enabled instances with decrypted API keys.
type InstanceSource interface {
	ListEnabledInstances(ctx context.Context) ([]Instance, error)
	EnabledInstance(ctx context.Context, id uuid.UUID) (Instance, error)
}

// DBInstanceSource loads enabled instances from Postgres and decrypts their keys.
type DBInstanceSource struct {
	st  *store.Store
	enc *crypto.Encryptor
}

// NewDBInstanceSource builds a DBInstanceSource.
func NewDBInstanceSource(st *store.Store, enc *crypto.Encryptor) *DBInstanceSource {
	return &DBInstanceSource{st: st, enc: enc}
}

// ListEnabledInstances returns enabled instances without logging or returning raw keys in errors.
// An instance whose key does not decrypt is left out and reported in the
// joined error; the others are still returned.
func (s *DBInstanceSource) ListEnabledInstances(ctx context.Context) ([]Instance, error) {
	rows, err := s.st.Q().ListEnabledInstances(ctx)
	if err != nil {
		return nil, fmt.Errorf("queue: list instances: %w", store.MapError(err))
	}
	out := make([]Instance, 0, len(rows))
	var errs []error
	for _, row := range rows {
		key, err := s.enc.Open(row.ApiKeyEnc)
		if err != nil {
			errs = append(errs, fmt.Errorf("queue: open api key of %s: %w", row.Name, err))
			continue
		}
		prio := store.InstancePriorityFromValues(row.SearchPriority, row.DownloadPriority)
		out = append(out, Instance{
			ID:               row.ID,
			Name:             row.Name,
			Kind:             domain.AppKind(row.Kind),
			BaseURL:          row.BaseUrl,
			APIKey:           string(key),
			DownloadPriority: prio.Download,
		})
	}
	return out, errors.Join(errs...)
}

// EnabledInstance returns one enabled instance with its decrypted API key.
// Returns domain.ErrNotFound when the instance does not exist or is disabled.
func (s *DBInstanceSource) EnabledInstance(ctx context.Context, id uuid.UUID) (Instance, error) {
	row, err := s.st.Q().GetInstance(ctx, id)
	if err != nil {
		mapped := store.MapError(err)
		if errors.Is(mapped, domain.ErrNotFound) {
			return Instance{}, domain.ErrNotFound
		}
		return Instance{}, fmt.Errorf("queue: get instance: %w", mapped)
	}
	if !row.Enabled || !domain.AppKind(row.Kind).IsArr() {
		return Instance{}, domain.ErrNotFound
	}
	key, err := s.enc.Open(row.ApiKeyEnc)
	if err != nil {
		return Instance{}, fmt.Errorf("queue: open api key of %s: %w", row.Name, err)
	}
	prio := store.InstancePriorityFromValues(row.SearchPriority, row.DownloadPriority)
	return Instance{
		ID:               row.ID,
		Name:             row.Name,
		Kind:             domain.AppKind(row.Kind),
		BaseURL:          row.BaseUrl,
		APIKey:           string(key),
		DownloadPriority: prio.Download,
	}, nil
}

// ArrClient defines the *arr queue operations needed by the sweep.
type ArrClient interface {
	Queue(ctx context.Context, kind domain.AppKind, baseURL, apiKey string, maxPages, pageSize int) ([]Item, error)
	RemoveQueueItem(ctx context.Context, kind domain.AppKind, baseURL, apiKey string, id int32, blocklist, research bool) error
	ForceImport(ctx context.Context, kind domain.AppKind, baseURL, apiKey, downloadID string, mode ImportMode) (int, error)
}

// EventPublisher publishes events on the bus.
type EventPublisher interface {
	Publish(ctx context.Context, f events.Frame)
}

// MetricsRecorder records queue sweep metrics.
type MetricsRecorder interface {
	QueueStruck(instance, rule, mode string)
	QueueRemoval(instance, rule, result string)
	QueueSkipped(instance, reason string)
	QueueImport(instance, result string)
}

// Service manages download queues across *arr instances.
type Service struct {
	store     StoreInterface
	instances InstanceSource
	arr       ArrClient
	clk       clock.Clock
	events    EventPublisher
	metrics   MetricsRecorder
	logger    *slog.Logger
}

// StoreInterface defines the persistence methods required by Service.
type StoreInterface interface {
	Settings(ctx context.Context, instanceID uuid.UUID) (Settings, error)
	Strikes(ctx context.Context, instanceID uuid.UUID) (map[string]Strike, error)
	Record(ctx context.Context, instanceID uuid.UUID, decisions []Decision) error
	Forget(ctx context.Context, instanceID uuid.UUID, downloadID string) error
	Purge(ctx context.Context, batch int) (int64, error)
	DownloadPriorityPreset(ctx context.Context) (domain.PriorityPreset, error)
}

// NewService constructs a queue Service.
func NewService(
	st StoreInterface,
	instances InstanceSource,
	arr ArrClient,
	clk clock.Clock,
	ev EventPublisher,
	m MetricsRecorder,
	logger *slog.Logger,
) *Service {
	return &Service{
		store:     st,
		instances: instances,
		arr:       arr,
		clk:       clk,
		events:    ev,
		metrics:   m,
		logger:    logger,
	}
}

func (s *Service) sortByDownloadPriority(ctx context.Context, insts []Instance) {
	preset, err := s.store.DownloadPriorityPreset(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "queue: read download priority preset failed; using default", slog.String("error", err.Error()))
		preset = domain.MediaFirstPreset()
	}
	slices.SortStableFunc(insts, func(a, b Instance) int {
		rankA, _ := domain.ResolvePriorityRank(a.DownloadPriority, a.Kind, preset)
		rankB, _ := domain.ResolvePriorityRank(b.DownloadPriority, b.Kind, preset)
		if c := cmp.Compare(rankA, rankB); c != 0 {
			return c
		}
		if c := cmp.Compare(string(a.Kind), string(b.Kind)); c != 0 {
			return c
		}
		return cmp.Compare(a.Name, b.Name)
	})
}

// SweepAll performs a queue sweep for every enabled instance and purges expired strikes.
// Instances the source could not load are reported; the ones it returned are swept.
func (s *Service) SweepAll(ctx context.Context) error {
	insts, err := s.instances.ListEnabledInstances(ctx)
	if err != nil && len(insts) == 0 {
		return fmt.Errorf("queue: sweep all: %w", err)
	}
	s.sortByDownloadPriority(ctx, insts)

	var errs []error
	if err != nil {
		s.logger.WarnContext(ctx, "queue: some instances not loaded", slog.String("error", err.Error()))
		errs = append(errs, err)
	}
	for _, inst := range insts {
		if sweepErr := s.sweepInstance(ctx, inst); sweepErr != nil {
			s.logger.WarnContext(ctx, "queue: sweep instance failed", slog.String("instance", inst.Name), slog.String("error", sweepErr.Error()))
			errs = append(errs, sweepErr)
		}
	}

	if _, purgeErr := s.store.Purge(ctx, purgeBatch); purgeErr != nil {
		s.logger.WarnContext(ctx, "queue: purge expired strikes failed", slog.String("error", purgeErr.Error()))
		errs = append(errs, purgeErr)
	}

	return errors.Join(errs...)
}

func (s *Service) sweepInstance(ctx context.Context, inst Instance) error {
	cfg, err := s.store.Settings(ctx, inst.ID)
	if err != nil {
		return fmt.Errorf("queue settings for %s: %w", inst.Name, err)
	}
	if !cfg.Enabled {
		s.metrics.QueueSkipped(inst.Name, "disabled")
		return nil
	}
	cfg.ForceImport = SupportsForceImport(inst.Kind)
	now := s.clk.Now()
	decisions, err := s.fetchAndEvaluate(ctx, inst, cfg, now)
	if err != nil {
		if errors.Is(err, ErrQueueUnsupported) {
			s.logger.InfoContext(ctx, "queue: sweep skipped unsupported kind", slog.String("instance", inst.Name), slog.String("kind", string(inst.Kind)))
			s.metrics.QueueSkipped(inst.Name, "unsupported")
			return nil
		}
		return err
	}

	return s.processDecisions(ctx, inst, cfg, decisions, now)
}

func (s *Service) fetchAndEvaluate(ctx context.Context, inst Instance, cfg Settings, now time.Time) ([]Decision, error) {
	items, err := s.arr.Queue(ctx, inst.Kind, inst.BaseURL, inst.APIKey, sweepMaxPages, sweepPageSize)
	if err != nil {
		return nil, fmt.Errorf("fetch queue for %s: %w", inst.Name, err)
	}
	prev, err := s.store.Strikes(ctx, inst.ID)
	if err != nil {
		return nil, fmt.Errorf("fetch strikes for %s: %w", inst.Name, err)
	}
	return Sweep(cfg, items, prev, now), nil
}

func (s *Service) processDecisions(ctx context.Context, inst Instance, cfg Settings, ds []Decision, now time.Time) error {
	toRecord := make([]Decision, 0, len(ds))
	for _, d := range ds {
		if d.Remove {
			if s.handleRemoval(ctx, inst, d, now) {
				continue
			}
		}
		toRecord = append(toRecord, d)
	}
	s.processImports(ctx, inst, toRecord, now)

	if err := s.store.Record(ctx, inst.ID, toRecord); err != nil {
		s.logger.WarnContext(ctx, "queue: record strikes failed", slog.String("instance", inst.Name), slog.String("error", err.Error()))
		return fmt.Errorf("record strikes for %s: %w", inst.Name, err)
	}

	for _, d := range ds {
		if d.Strike {
			s.emitStrikeEvent(ctx, inst, cfg, d, now)
			s.logShadow(ctx, inst, d)
		}
	}
	return nil
}

func (s *Service) handleRemoval(ctx context.Context, inst Instance, d Decision, now time.Time) bool {
	err := s.arr.RemoveQueueItem(ctx, inst.Kind, inst.BaseURL, inst.APIKey, d.ItemID, d.Blocklist, d.Research)
	if err != nil {
		s.logger.WarnContext(ctx, "queue: remove item failed", slog.String("instance", inst.Name), slog.Int("item_id", int(d.ItemID)), slog.String("download_id", d.DownloadID), slog.String("error", err.Error()))
		s.metrics.QueueRemoval(inst.Name, string(d.Rule), "error")
		return false
	}

	if fErr := s.store.Forget(ctx, inst.ID, d.DownloadID); fErr != nil {
		s.logger.WarnContext(ctx, "queue: forget strike failed", slog.String("instance", inst.Name), slog.String("download_id", d.DownloadID), slog.String("error", fErr.Error()))
	}
	s.emitRemovalEvent(ctx, inst, d, now)
	s.metrics.QueueRemoval(inst.Name, string(d.Rule), "ok")
	return true
}

type strikePayload struct {
	Rule         string   `json:"rule"`
	Class        string   `json:"class,omitempty"`
	Pattern      string   `json:"pattern,omitempty"`
	MatchPercent *float64 `json:"match_percent,omitempty"`
	Count        int      `json:"count"`
	Threshold    int      `json:"threshold"`
	Mode         string   `json:"mode"`
	WouldRemove  bool     `json:"would_remove"`
	WouldImport  bool     `json:"would_import"`
}

func (s *Service) emitStrikeEvent(ctx context.Context, inst Instance, cfg Settings, d Decision, now time.Time) {
	rs := ResolveRule(cfg, d.Rule)
	mode := string(rs.Mode)
	if mode == "" {
		mode = string(ModeObserve)
	}

	raw, err := json.Marshal(strikePayload{
		Rule:         string(d.Rule),
		Class:        string(d.Class),
		Pattern:      d.Pattern,
		MatchPercent: d.MatchPercent,
		Count:        d.Next.Count,
		Threshold:    d.Threshold,
		Mode:         mode,
		WouldRemove:  d.WouldRemove,
		WouldImport:  d.WouldImport,
	})
	if err != nil {
		s.logger.WarnContext(ctx, "queue: encode strike payload failed", slog.String("error", err.Error()))
		raw = nil
	}

	s.events.Publish(ctx, events.Frame{
		InstanceID: inst.ID,
		Timestamp:  now,
		Level:      "info",
		Type:       "queue_item_struck",
		EntityID:   int64(d.ItemID),
		Title:      d.DownloadID,
		Detail:     string(raw),
	})
	s.metrics.QueueStruck(inst.Name, string(d.Rule), mode)
}

func (s *Service) emitRemovalEvent(ctx context.Context, inst Instance, d Decision, now time.Time) {
	detail := fmt.Sprintf("removed by rule %s", d.Rule)
	if d.Class != "" {
		detail = fmt.Sprintf("removed by rule %s (class: %s, pattern: %s)", d.Rule, d.Class, d.Pattern)
	}
	s.events.Publish(ctx, events.Frame{
		InstanceID: inst.ID,
		Timestamp:  now,
		Level:      "info",
		Type:       "queue_item_removed",
		EntityID:   int64(d.ItemID),
		Title:      d.DownloadID,
		Detail:     detail,
	})
}
