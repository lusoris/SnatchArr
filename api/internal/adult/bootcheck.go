// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/gen2brain/webp"
	"go.uber.org/fx"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/modules"
)

func logWebPBackend(logger *slog.Logger) {
	if err := webp.Dynamic(); err == nil {
		logger.Info("adult: webp backend", "backend", "system libwebp")
	} else {
		logger.Info("adult: webp backend", "backend", "transpiled", "error", err.Error())
	}
}

// CheckBoot verifies that if the adult module is enabled and image_set_dir is set,
// image_set_dir exists and is a directory. It also creates the thumbnail cache directory.
func CheckBoot(ctx context.Context, mod ModuleChecker, _ *slog.Logger) error {
	ctxTimeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	enabled, err := mod.IsEnabled(ctxTimeout, domain.ModuleAdult)
	if err != nil {
		return fmt.Errorf("adult: check module enabled: %w", err)
	}
	if !enabled {
		return nil
	}

	m, err := mod.Get(ctxTimeout, domain.ModuleAdult)
	if err != nil {
		return fmt.Errorf("adult: get module config: %w", err)
	}
	cfg, err := parseAdultConfig(m.Config)
	if err != nil {
		return fmt.Errorf("adult: parse module config: %w", err)
	}
	if cfg.ImageSetDir == nil || *cfg.ImageSetDir == "" {
		return nil
	}

	dir := filepath.Clean(*cfg.ImageSetDir)
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("adult: image_set_dir stat: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: image_set_dir %q is not a directory", domain.ErrInvalid, dir)
	}

	thumbDir := resolveThumbnailDir(cfg)
	if err := os.MkdirAll(thumbDir, 0o750); err != nil {
		return fmt.Errorf("adult: thumbnail cache dir mkdir: %w", err)
	}
	return nil
}

func registerBootCheck(lc fx.Lifecycle, mod *modules.Service, logger *slog.Logger) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			logWebPBackend(logger)
			return CheckBoot(ctx, mod, logger)
		},
	})
}
