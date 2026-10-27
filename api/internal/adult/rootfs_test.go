// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestOpenRegularInRoot(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, "base")
	if err := os.MkdirAll(baseDir, 0o750); err != nil {
		t.Fatalf("mkdir base: %v", err)
	}

	validFile := filepath.Join(baseDir, "regular.png")
	if err := os.WriteFile(validFile, []byte("valid content"), 0o600); err != nil {
		t.Fatalf("write valid file: %v", err)
	}

	symlinkFile := filepath.Join(baseDir, "symlink.png")
	if err := os.Symlink(validFile, symlinkFile); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	subDir := filepath.Join(baseDir, "subdir")
	if err := os.MkdirAll(subDir, 0o750); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}

	outsideFile := filepath.Join(tmp, "outside.png")
	if err := os.WriteFile(outsideFile, []byte("outside content"), 0o600); err != nil {
		t.Fatalf("write outside: %v", err)
	}

	cases := []struct {
		name    string
		path    string
		wantErr error
	}{
		{
			name:    "regular file ok",
			path:    validFile,
			wantErr: nil,
		},
		{
			name:    "symlink inside root refused",
			path:    symlinkFile,
			wantErr: domain.ErrInvalid,
		},
		{
			name:    "path escaping root with dotdot",
			path:    filepath.Join(baseDir, "../outside.png"),
			wantErr: domain.ErrInvalid,
		},
		{
			name:    "path escaping root absolute outside",
			path:    outsideFile,
			wantErr: domain.ErrInvalid,
		},
		{
			name:    "directory refused",
			path:    subDir,
			wantErr: domain.ErrInvalid,
		},
		{
			name:    "base directory itself refused",
			path:    baseDir,
			wantErr: domain.ErrInvalid,
		},
		{
			name:    "missing file -> not-exist error",
			path:    filepath.Join(baseDir, "missing.png"),
			wantErr: os.ErrNotExist,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f, fi, err := openRegularInRoot(baseDir, tc.path)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("openRegularInRoot unexpected error: %v", err)
				}
				if f == nil || fi == nil {
					t.Fatalf("openRegularInRoot returned nil file or info")
				}
				defer func() {
					if closeErr := f.Close(); closeErr != nil {
						t.Errorf("close file: %v", closeErr)
					}
				}()
				if !fi.Mode().IsRegular() {
					t.Fatalf("expected regular file, got mode %v", fi.Mode())
				}
			} else {
				if err == nil {
					if f != nil {
						_ = f.Close()
					}
					t.Fatalf("openRegularInRoot succeeded, want error %v", tc.wantErr)
				}
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("openRegularInRoot error = %v, want %v", err, tc.wantErr)
				}
			}
		})
	}
}

func TestOpenRegularInRootSwap(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	baseDir := filepath.Join(tmp, "swap_base")
	if err := os.MkdirAll(baseDir, 0o750); err != nil {
		t.Fatalf("mkdir base: %v", err)
	}

	filePath := filepath.Join(baseDir, "target.png")
	if err := os.WriteFile(filePath, []byte("initial regular content"), 0o600); err != nil {
		t.Fatalf("write initial file: %v", err)
	}

	swapToPath := filepath.Join(baseDir, "replacement.png")
	if err := os.WriteFile(swapToPath, []byte("replacement regular content"), 0o600); err != nil {
		t.Fatalf("write replacement file: %v", err)
	}

	// openFn simulates a swap between Lstat and Open by replacing filePath with a symlink to replacement.png
	openFn := func(root *os.Root, rel string) (*os.File, error) {
		if err := os.Remove(filePath); err != nil {
			return nil, err
		}
		if err := os.Symlink("replacement.png", filePath); err != nil {
			return nil, err
		}
		return root.Open(rel)
	}

	f, fi, err := openRegularInRootWith(baseDir, filePath, openFn)
	if err == nil {
		if f != nil {
			_ = f.Close()
		}
		t.Fatal("openRegularInRootWith succeeded despite symlink swap between Lstat and Open, want ErrInvalid")
	}
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("openRegularInRootWith error = %v, want ErrInvalid", err)
	}
	if f != nil || fi != nil {
		t.Fatalf("openRegularInRootWith returned non-nil file or info on swap: f=%v, fi=%v", f, fi)
	}
}
