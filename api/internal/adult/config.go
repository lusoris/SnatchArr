// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/lusoris/SnatchArr/api/internal/modules"
)

type adultSettings struct {
	ImageSetDir         *string `json:"image_set_dir"`
	ThumbnailCacheDir   *string `json:"thumbnail_cache_dir"`
	SyncIntervalMinutes *int    `json:"sync_interval_minutes"`
}

func parseAdultConfig(raw json.RawMessage) (adultSettings, error) {
	var cfg adultSettings
	if len(raw) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("unmarshal adult config: %w", err)
	}
	return cfg, nil
}

func resolveThumbnailDir(cfg adultSettings) string {
	if cfg.ThumbnailCacheDir != nil && *cfg.ThumbnailCacheDir != "" {
		return *cfg.ThumbnailCacheDir
	}
	return modules.DefaultAdultThumbnailCacheDir()
}

func isCleanSubdir(baseDir, targetPath string) bool {
	cleanBase := filepath.Clean(baseDir)
	cleanTarget := filepath.Clean(targetPath)
	if cleanTarget == cleanBase {
		return false
	}
	rel, err := filepath.Rel(cleanBase, cleanTarget)
	if err != nil {
		return false
	}
	if strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return false
	}
	return true
}

func isAcceptedImageExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif":
		return true
	default:
		return false
	}
}

func matchMimeWithExt(ext, mime string) bool {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		return mime == "image/jpeg"
	case ".png":
		return mime == "image/png"
	case ".gif":
		return mime == "image/gif"
	case ".webp":
		return mime == "image/webp"
	default:
		return false
	}
}
