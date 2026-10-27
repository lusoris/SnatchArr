// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteStashIgnore(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	tests := []struct {
		name       string
		setup      func(t *testing.T) string
		checkFile  bool
		wantFile   bool
		wantExact  bool
		wantCustom string
		wantErr    bool
	}{
		{
			name: "absent -> written with exact content",
			setup: func(t *testing.T) string {
				t.Helper()
				return t.TempDir()
			},
			checkFile: true,
			wantFile:  true,
			wantExact: true,
			wantErr:   false,
		},
		{
			name: "existing custom content -> unchanged",
			setup: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				custom := "# custom operator rules\n*.jpg\n"
				target := filepath.Join(dir, StashIgnoreFilename)
				if err := os.WriteFile(target, []byte(custom), 0o600); err != nil {
					t.Fatalf("writeFile: %v", err)
				}
				return dir
			},
			checkFile:  true,
			wantFile:   true,
			wantCustom: "# custom operator rules\n*.jpg\n",
			wantErr:    false,
		},
		{
			name: "read-only dir -> nil and no file",
			setup: func(t *testing.T) string {
				t.Helper()
				if os.Geteuid() == 0 {
					t.Skip("skipping read-only test as root")
				}
				dir := t.TempDir()
				roDir := filepath.Join(dir, "ro")
				if err := os.MkdirAll(roDir, 0o750); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := os.Chmod(roDir, 0o555); err != nil {
					t.Fatalf("chmod: %v", err)
				}
				t.Cleanup(func() {
					if err := os.Chmod(roDir, 0o750); err != nil {
						t.Logf("cleanup chmod: %v", err)
					}
				})
				return roDir
			},
			checkFile: true,
			wantFile:  false,
			wantErr:   false,
		},
		{
			name: "path is a file not dir -> error",
			setup: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				filePath := filepath.Join(dir, "a-regular-file.txt")
				if err := os.WriteFile(filePath, []byte("regular content"), 0o600); err != nil {
					t.Fatalf("writeFile: %v", err)
				}
				return filePath
			},
			checkFile: false,
			wantErr:   true,
		},
		{
			name: "boundary nil logger -> written with exact content",
			setup: func(t *testing.T) string {
				t.Helper()
				return t.TempDir()
			},
			checkFile: true,
			wantFile:  true,
			wantExact: true,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			targetDir := tt.setup(t)

			var testLogger *slog.Logger
			if tt.name != "boundary nil logger -> written with exact content" {
				testLogger = logger
			}

			err := writeStashIgnore(targetDir, testLogger)
			if (err != nil) != tt.wantErr {
				t.Fatalf("writeStashIgnore() err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.checkFile {
				return
			}

			filePath := filepath.Join(targetDir, StashIgnoreFilename)
			data, readErr := os.ReadFile(filePath) // #nosec G304 -- test path in temp dir
			if tt.wantFile {
				if readErr != nil {
					t.Fatalf("expected %s to exist: %v", StashIgnoreFilename, readErr)
				}
				if tt.wantExact && string(data) != StashIgnoreContent {
					t.Fatalf("file content = %q, want %q", string(data), StashIgnoreContent)
				}
				if tt.wantCustom != "" && string(data) != tt.wantCustom {
					t.Fatalf("file content = %q, want custom %q", string(data), tt.wantCustom)
				}
			} else {
				if readErr == nil {
					t.Fatalf("expected %s to not exist, but found it", StashIgnoreFilename)
				}
				if !errors.Is(readErr, fs.ErrNotExist) {
					t.Fatalf("expected ErrNotExist, got %v", readErr)
				}
			}
		})
	}
}
