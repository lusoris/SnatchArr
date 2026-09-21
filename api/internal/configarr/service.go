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

	"go.uber.org/fx"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/leaderx"
)

// Result summarises one import.
type Result struct {
	Imported int
	Updated  int
	Disabled int
	Warnings []string
	At       time.Time
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
func New(cfg config.Options, inst *instances.Service, probe arrclient.Prober, clk clock.Clock, logger *slog.Logger) *Service {
	return &Service{cfg: cfg.Configarr, instances: inst, probe: probe, clk: clk, logger: logger}
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

func (s *Service) importFiles(ctx context.Context, configPath, secretsPath string) (Result, error) {
	parsed, err := Parse(configPath, secretsPath)
	if err != nil {
		if errors.Is(err, ErrParse) {
			return Result{}, fmt.Errorf("%w: %w", domain.ErrInvalid, err)
		}
		return Result{}, err
	}
	res := Result{Warnings: parsed.Warnings, At: s.clk.Now()}
	keys := make([]string, 0, len(parsed.Instances))
	for _, def := range parsed.Instances {
		kind, warn := s.kindFor(ctx, def)
		if warn != "" {
			res.Warnings = append(res.Warnings, warn)
		}
		outcome, upErr := s.instances.UpsertConfigarr(ctx, def.Key, instances.Input{Kind: kind, Name: def.Name, BaseURL: def.BaseURL, APIKey: def.APIKey, Enabled: def.Enabled})
		switch {
		case upErr == nil && outcome.Inserted:
			res.Imported++
			keys = append(keys, def.Key)
		case upErr == nil:
			res.Updated++
			keys = append(keys, def.Key)
		case errors.Is(upErr, domain.ErrConflict), errors.Is(upErr, domain.ErrInvalid):
			res.Warnings = append(res.Warnings, def.Key+": "+upErr.Error())
		default:
			return res, upErr
		}
	}
	disabled, err := s.instances.DisableConfigarrNotIn(ctx, keys)
	if err != nil {
		return res, err
	}
	res.Disabled = disabled
	return res, nil
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
	state := fileFingerprint(s.cfg.Config) + "|" + fileFingerprint(s.cfg.Secrets)
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
	s.logger.InfoContext(ctx, "configarr: imported", slog.Int("imported", res.Imported), slog.Int("updated", res.Updated), slog.Int("disabled", res.Disabled), slog.Int("warnings", len(res.Warnings)))
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
	// The poll runs on the leader only (internal/leaderx).
	fx.Provide(leaderx.Provide[*Service]()),
)
