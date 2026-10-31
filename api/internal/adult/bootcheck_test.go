// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/adult"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

type mockBootModuleChecker struct {
	enabled bool
	cfg     string
	err     error
}

func (m *mockBootModuleChecker) IsEnabled(_ context.Context, _ string) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	return m.enabled, nil
}

func (m *mockBootModuleChecker) Get(_ context.Context, id string) (domain.Module, error) {
	if m.err != nil {
		return domain.Module{}, m.err
	}
	return domain.Module{
		ID:      id,
		Enabled: m.enabled,
		Config:  json.RawMessage(m.cfg),
	}, nil
}

func TestCheckBoot(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	existingDir := filepath.Join(tmp, "existing-imagesets")
	if err := os.MkdirAll(existingDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	testFile := filepath.Join(tmp, "a-regular-file.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0o600); err != nil {
		t.Fatalf("writeFile: %v", err)
	}

	missingDir := filepath.Join(tmp, "does-not-exist")
	thumbDir := filepath.Join(tmp, "thumbs")

	logger := slog.New(slog.DiscardHandler)
	ctx := context.Background()

	t.Run("missing dir -> error", func(t *testing.T) {
		t.Parallel()
		checker := &mockBootModuleChecker{
			enabled: true,
			cfg:     fmt.Sprintf(`{"image_set_dir":%q,"thumbnail_cache_dir":%q}`, missingDir, thumbDir),
		}
		err := adult.CheckBoot(ctx, checker, logger)
		if err == nil {
			t.Fatal("CheckBoot() want error for missing dir, got nil")
		}
	})

	t.Run("file instead of dir -> error", func(t *testing.T) {
		t.Parallel()
		checker := &mockBootModuleChecker{
			enabled: true,
			cfg:     fmt.Sprintf(`{"image_set_dir":%q,"thumbnail_cache_dir":%q}`, testFile, thumbDir),
		}
		err := adult.CheckBoot(ctx, checker, logger)
		if err == nil {
			t.Fatal("CheckBoot() want error for file instead of dir, got nil")
		}
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("CheckBoot() error = %v, want ErrInvalid", err)
		}
	})

	t.Run("existing dir -> nil", func(t *testing.T) {
		t.Parallel()
		customThumb := filepath.Join(tmp, "custom-thumbs")
		checker := &mockBootModuleChecker{
			enabled: true,
			cfg:     fmt.Sprintf(`{"image_set_dir":%q,"thumbnail_cache_dir":%q}`, existingDir, customThumb),
		}
		err := adult.CheckBoot(ctx, checker, logger)
		if err != nil {
			t.Fatalf("CheckBoot() want nil, got: %v", err)
		}
		fi, err := os.Stat(customThumb)
		if err != nil || !fi.IsDir() {
			t.Fatalf("CheckBoot() did not create thumbnail dir %q: %v", customThumb, err)
		}
	})

	t.Run("module disabled + missing dir -> nil", func(t *testing.T) {
		t.Parallel()
		checker := &mockBootModuleChecker{
			enabled: false,
			cfg:     fmt.Sprintf(`{"image_set_dir":%q,"thumbnail_cache_dir":%q}`, missingDir, thumbDir),
		}
		err := adult.CheckBoot(ctx, checker, logger)
		if err != nil {
			t.Fatalf("CheckBoot() want nil for disabled module, got: %v", err)
		}
	})

	t.Run("enabled module + existing dir -> .stashignore present", func(t *testing.T) {
		t.Parallel()
		imgDir := filepath.Join(t.TempDir(), "imagesets")
		if err := os.MkdirAll(imgDir, 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		customThumb := filepath.Join(t.TempDir(), "thumbs")
		checker := &mockBootModuleChecker{
			enabled: true,
			cfg:     fmt.Sprintf(`{"image_set_dir":%q,"thumbnail_cache_dir":%q}`, imgDir, customThumb),
		}
		err := adult.CheckBoot(ctx, checker, logger)
		if err != nil {
			t.Fatalf("CheckBoot() want nil, got: %v", err)
		}
		ignorePath := filepath.Join(imgDir, ".stashignore")
		fi, err := os.Stat(ignorePath)
		if err != nil {
			t.Fatalf("CheckBoot() did not create .stashignore %q: %v", ignorePath, err)
		}
		if fi.IsDir() {
			t.Fatalf("CheckBoot() .stashignore %q is a directory", ignorePath)
		}
	})
}
