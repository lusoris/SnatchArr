// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"

	gerr "github.com/golusoris/golusoris/core/errors"
)

// StashIgnoreFilename is the filename of the Stash ignore marker.
const StashIgnoreFilename = ".stashignore"

// StashIgnoreContent is written to .stashignore at the adult module image_set_dir root (ADR-0032).
const StashIgnoreContent = "# SnatchArr (ADR-0032): Stash v0.31.0 minimum\n*\n"

func isReadOnly(err error) bool {
	return errors.Is(err, fs.ErrPermission) || errors.Is(err, syscall.EROFS)
}

func writeStashIgnore(dir string, logger *slog.Logger) (err error) {
	if logger == nil {
		logger = slog.Default()
	}
	target := filepath.Join(dir, StashIgnoreFilename)
	if _, statErr := os.Stat(target); statErr == nil {
		return nil
	}
	// #nosec G302 G304 -- G302: Stash runs as another user and must read this file; G304: fixed file name inside the validated image_set_dir
	f, openErr := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if openErr != nil {
		if errors.Is(openErr, fs.ErrExist) {
			return nil
		}
		if isReadOnly(openErr) {
			logger.Info("adult: image_set_dir is read-only, skipping .stashignore", "dir", dir)
			return nil
		}
		return fmt.Errorf("adult: create %s: %w", StashIgnoreFilename, openErr)
	}
	defer gerr.CloseInto(f, &err, "adult: close "+StashIgnoreFilename)

	if _, writeErr := io.WriteString(f, StashIgnoreContent); writeErr != nil {
		return fmt.Errorf("adult: write %s: %w", StashIgnoreFilename, writeErr)
	}
	return nil
}
