// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package configarr

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/fx"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/leader/gate"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/dlclients"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/policies"
	"github.com/lusoris/SnatchArr/api/internal/queue"
)

// Result summarises one import.
type Result struct {
	Imported        int
	Updated         int
	Disabled        int
	ClientsImported int
	ClientsUpdated  int
	ClientsRemoved  int
	Warnings        []string
	At              time.Time
}

// Status is the linked-mode state for the API.
type Status struct {
	Linked       bool
	ConfigPath   string
	SecretsPath  string
	Watching     bool
	LastImportAt *time.Time
	LastResult   *Result
	LastError    string
}

// PollInterval is how often linked mode checks the files for changes. Polling rather than
// inotify because Kubernetes ConfigMap and Secret mounts swap symlinks, which inotify
// watchers regularly miss.
const PollInterval = 30 * time.Second

// maxPolls bounds the watch loop (HISS-02).
const maxPolls = 1 << 26

// Service imports Configarr definitions and, in linked mode, keeps them in sync.
type Service struct {
	cfg       config.ConfigarrOptions
	instances *instances.Service
	dlclients *dlclients.Service
	policies  *policies.Service
	queue     *queue.Store
	probe     arrclient.Prober
	clk       clock.Clock
	logger    *slog.Logger

	mu        sync.Mutex
	last      *Result
	lastErr   string
	lastAt    *time.Time
	watching  bool
	fileState string
	cancel    context.CancelFunc
	done      chan struct{}
}

// New wires the service.
func New(
	cfg config.Options,
	inst *instances.Service,
	dl *dlclients.Service,
	pol *policies.Service,
	q *queue.Store,
	probe arrclient.Prober,
	clk clock.Clock,
	logger *slog.Logger,
) *Service {
	return &Service{
		cfg:       cfg.Configarr,
		instances: inst,
		dlclients: dl,
		policies:  pol,
		queue:     q,
		probe:     probe,
		clk:       clk,
		logger:    logger,
	}
}

// Status reports the linked-mode state.
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Status{
		Linked: s.cfg.Linked(), ConfigPath: s.cfg.Config, SecretsPath: s.cfg.Secrets, Watching: s.watching,
		LastImportAt: s.lastAt, LastResult: s.last, LastError: s.lastErr,
	}
}

// Import reads the files and upserts every defined instance. Empty paths fall back to the
// linked configuration; without either the import is refused.
func (s *Service) Import(ctx context.Context, configPath, secretsPath string) (Result, error) {
	if configPath == "" {
		configPath, secretsPath = s.cfg.Config, s.cfg.Secrets
	}
	if configPath == "" {
		return Result{}, fmt.Errorf("%w: no config path given and no linked Configarr configuration", domain.ErrInvalid)
	}
	res, err := s.importFiles(ctx, configPath, secretsPath)
	s.mu.Lock()
	now := s.clk.Now()
	s.lastAt = &now
	if err != nil {
		s.lastErr = err.Error()
	} else {
		s.lastErr = ""
		s.last = &res
	}
	s.mu.Unlock()
	return res, err
}

func parseConfig(configPath, secretsPath string) (Parsed, error) {
	parsed, err := Parse(configPath, secretsPath)
	if err != nil {
		if errors.Is(err, ErrParse) {
			return Parsed{}, fmt.Errorf("%w: %w", domain.ErrInvalid, err)
		}
		return Parsed{}, err
	}
	return parsed, nil
}

func (s *Service) importOptionalComponents(ctx context.Context, secretsPath string, res *Result) error {
	if s.cfg.DownloadClients != "" && s.dlclients != nil {
		if err := s.importDownloadClients(ctx, secretsPath, res); err != nil {
			return err
		}
	}
	if s.cfg.Policies != "" && s.policies != nil && s.queue != nil {
		if err := s.importPolicies(ctx, secretsPath, res); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) importFiles(ctx context.Context, configPath, secretsPath string) (Result, error) {
	parsed, err := parseConfig(configPath, secretsPath)
	if err != nil {
		return Result{}, err
	}
	res := Result{Warnings: parsed.Warnings, At: s.clk.Now()}
	keys, err := s.importInstances(ctx, parsed.Instances, &res)
	if err != nil {
		return res, err
	}
	disabled, err := s.instances.DisableConfigarrNotIn(ctx, keys)
	if err != nil {
		return res, err
	}
	res.Disabled = disabled
	if err := s.importOptionalComponents(ctx, secretsPath, &res); err != nil {
		return res, err
	}
	return res, nil
}

func (s *Service) importInstances(ctx context.Context, instances []Definition, res *Result) ([]string, error) {
	keys := make([]string, 0, len(instances))
	for _, def := range instances {
		key, err := s.upsertOneInstance(ctx, def, res)
		if err != nil {
			return nil, err
		}
		if key != "" {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func (s *Service) upsertOneInstance(ctx context.Context, def Definition, res *Result) (string, error) {
	kind, warn := s.kindFor(ctx, def)
	if warn != "" {
		res.Warnings = append(res.Warnings, warn)
	}
	outcome, upErr := s.instances.UpsertConfigarr(ctx, def.Key, instances.Input{
		Kind:    kind,
		Name:    def.Name,
		BaseURL: def.BaseURL,
		APIKey:  def.APIKey,
		Enabled: def.Enabled,
	})
	switch {
	case upErr == nil && outcome.Inserted:
		res.Imported++
		return def.Key, nil
	case upErr == nil:
		res.Updated++
		return def.Key, nil
	case errors.Is(upErr, domain.ErrConflict), errors.Is(upErr, domain.ErrInvalid):
		res.Warnings = append(res.Warnings, def.Key+": "+upErr.Error())
		return "", nil
	default:
		return "", upErr
	}
}

func (s *Service) importDownloadClients(ctx context.Context, secretsPath string, res *Result) error {
	parsed, err := ParseDownloadClients(s.cfg.DownloadClients, secretsPath, os.LookupEnv)
	if err != nil {
		if errors.Is(err, ErrParse) {
			return fmt.Errorf("%w: %w", domain.ErrInvalid, err)
		}
		return err
	}
	res.Warnings = append(res.Warnings, parsed.Warnings...)
	keys, err := s.upsertDownloadClients(ctx, parsed.Clients, res)
	if err != nil {
		return err
	}
	removed, pErr := s.dlclients.PruneConfigarr(ctx, keys)
	if pErr != nil {
		return pErr
	}
	res.ClientsRemoved = removed
	return nil
}

func (s *Service) upsertDownloadClients(ctx context.Context, clients []ClientDefinition, res *Result) ([]string, error) {
	keys := make([]string, 0, len(clients))
	for _, def := range clients {
		key, err := s.upsertOneDownloadClient(ctx, def, res)
		if err != nil {
			return nil, err
		}
		if key != "" {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func (s *Service) resolveClientInstanceID(ctx context.Context, def ClientDefinition, res *Result) (*uuid.UUID, bool) {
	if def.InstanceKey == "" {
		return nil, true
	}
	id, ok := s.instances.IDByConfigarrKey(ctx, def.InstanceKey)
	if !ok {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%s: unknown instance %q; skipped", def.Key, def.InstanceKey))
		return nil, false
	}
	return &id, true
}

func (s *Service) upsertOneDownloadClient(ctx context.Context, def ClientDefinition, res *Result) (string, error) {
	instID, ok := s.resolveClientInstanceID(ctx, def, res)
	if !ok {
		return "", nil
	}
	inserted, upErr := s.dlclients.UpsertConfigarr(ctx, def.Key, dlclients.Input{
		InstanceID:         instID,
		Kind:               def.Kind,
		Name:               def.Name,
		BaseURL:            def.BaseURL,
		Username:           def.Username,
		Secret:             def.Secret,
		Enabled:            def.Enabled,
		MaxActive:          def.MaxActive,
		BandwidthBudgetBPS: def.BandwidthBudgetBPS,
	})
	switch {
	case upErr == nil && inserted:
		res.ClientsImported++
		return def.Key, nil
	case upErr == nil:
		res.ClientsUpdated++
		return def.Key, nil
	case errors.Is(upErr, domain.ErrConflict), errors.Is(upErr, domain.ErrInvalid):
		res.Warnings = append(res.Warnings, def.Key+": "+upErr.Error())
		return "", nil
	default:
		return "", upErr
	}
}

// kindFor maps a Configarr section to an instance kind. Whisparr v2 and v3 share a
// section, so the instance is probed: a v3 answer wins, anything else keeps v2 (or the
// kind already stored) with a warning when the probe failed.
func (s *Service) kindFor(ctx context.Context, def Definition) (domain.AppKind, string) {
	switch def.App {
	case "sonarr":
		return domain.KindSonarr, ""
	case "radarr":
		return domain.KindRadarr, ""
	case "lidarr":
		return domain.KindLidarr, ""
	case "readarr":
		return domain.KindReadarr, ""
	}
	_, err := s.probe.Probe(ctx, domain.KindWhisparrV3, def.BaseURL, def.APIKey)
	switch {
	case err == nil:
		return domain.KindWhisparrV3, ""
	case errors.Is(err, domain.ErrInvalid):
		return domain.KindWhisparrV2, ""
	}
	if existing, ok := s.instances.KindByConfigarrKey(ctx, def.Key); ok {
		return existing, ""
	}
	return domain.KindWhisparrV2, fmt.Sprintf("%s: could not probe Whisparr to tell v2 from v3 (%v); assuming v2", def.Key, err)
}

// Start imports once and, when watching is on, polls the files for changes.
func (s *Service) Start(parent context.Context) error {
	if !s.cfg.Linked() {
		return nil
	}
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	s.cancel = cancel
	s.done = make(chan struct{})
	s.mu.Lock()
	s.watching = s.cfg.Watch
	s.mu.Unlock()
	go s.loop(ctx)
	return nil
}

// Stop ends the watch loop.
func (s *Service) Stop(ctx context.Context) error {
	if s.cancel == nil {
		return nil
	}
	s.cancel()
	select {
	case <-s.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (s *Service) loop(ctx context.Context) {
	defer close(s.done)
	s.importIfChanged(ctx, true)
	if !s.cfg.Watch {
		return
	}
	timer := s.clk.NewTicker(PollInterval)
	defer timer.Stop()
	for range maxPolls {
		select {
		case <-ctx.Done():
			return
		case <-timer.Chan():
			s.importIfChanged(ctx, false)
		}
	}
}

// importIfChanged re-imports when the files' size or modification time moved.
func (s *Service) importIfChanged(ctx context.Context, force bool) {
	state := fileFingerprint(s.cfg.Config) + "|" + fileFingerprint(s.cfg.Secrets) + "|" + fileFingerprint(s.cfg.DownloadClients) + "|" + fileFingerprint(s.cfg.Policies)
	s.mu.Lock()
	changed := state != s.fileState
	s.fileState = state
	s.mu.Unlock()
	if !changed && !force {
		return
	}
	tctx, cancel := context.WithTimeout(ctx, PollInterval)
	defer cancel()
	res, err := s.Import(tctx, "", "")
	if err != nil {
		s.logger.WarnContext(ctx, "configarr: import", slog.String("error", err.Error()))
		return
	}
	s.logger.InfoContext(ctx, "configarr: imported",
		slog.Int("imported", res.Imported),
		slog.Int("updated", res.Updated),
		slog.Int("disabled", res.Disabled),
		slog.Int("clients_imported", res.ClientsImported),
		slog.Int("clients_updated", res.ClientsUpdated),
		slog.Int("clients_removed", res.ClientsRemoved),
		slog.Int("warnings", len(res.Warnings)),
	)
}

func fileFingerprint(path string) string {
	if path == "" {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil {
		return "missing"
	}
	return fmt.Sprintf("%d:%d", info.Size(), info.ModTime().UnixNano())
}

// Module provides the service and runs linked mode for the process lifetime.
var Module = fx.Module("snatcharr.configarr",
	fx.Provide(New),
	// The poll runs on the leader only (leader/gate).
	fx.Provide(gate.Provide[*Service]()),
)
