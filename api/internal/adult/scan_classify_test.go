// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type classifyCase struct {
	name     string
	subPath  string
	wantScan bool
	wantKeep bool
}

func TestClassifySetDir(t *testing.T) {
	t.Parallel()
	baseDir := t.TempDir()

	plainDir := filepath.Join(baseDir, "gallery_1")
	mkdirAll(t, plainDir)

	symlinkDir := filepath.Join(baseDir, "symlink_dir")
	if err := os.Symlink(plainDir, symlinkDir); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	regularFile := filepath.Join(baseDir, "notes.txt")
	if err := os.WriteFile(regularFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	missingPath := filepath.Join(baseDir, "missing")

	runClassifyCases(t, baseDir, []classifyCase{
		{name: "plain dir", subPath: plainDir, wantScan: true, wantKeep: true},
		{name: "symlink to dir", subPath: symlinkDir, wantScan: false, wantKeep: false},
		{name: "regular file", subPath: regularFile, wantScan: false, wantKeep: false},
		{name: "missing path", subPath: missingPath, wantScan: false, wantKeep: false},
	})
}

func TestClassifySetDirDotNames(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows trims trailing periods and spaces from the last path segment, so these names collapse onto existing paths")
	}
	baseDir := t.TempDir()

	tripleDotDir := filepath.Join(baseDir, "...")
	twoDotsSpaceDir := filepath.Join(baseDir, ".. ")
	spaceDotDir := filepath.Join(baseDir, " .")
	mkdirAll(t, tripleDotDir, twoDotsSpaceDir, spaceDotDir)

	runClassifyCases(t, baseDir, []classifyCase{
		{name: "triple dot dir", subPath: tripleDotDir, wantScan: true, wantKeep: true},
		{name: "two dots space dir", subPath: twoDotsSpaceDir, wantScan: false, wantKeep: true},
		{name: "space dot dir", subPath: spaceDotDir, wantScan: false, wantKeep: true},
	})
}

func mkdirAll(t *testing.T, dirs ...string) {
	t.Helper()
	for _, dir := range dirs {
		if err := os.Mkdir(dir, 0o750); err != nil {
			t.Fatalf("mkdir %q: %v", dir, err)
		}
	}
}

func runClassifyCases(t *testing.T, baseDir string, cases []classifyCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotScan, gotKeep := classifySetDir(baseDir, tc.subPath)
			if gotScan != tc.wantScan || gotKeep != tc.wantKeep {
				t.Fatalf(
					"classifySetDir(%q, %q) = (scan=%v, keep=%v), want (scan=%v, keep=%v)",
					baseDir, tc.subPath, gotScan, gotKeep, tc.wantScan, tc.wantKeep,
				)
			}
		})
	}
}

func TestWarnSkippedSetDir(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	svc := &Service{
		logger: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})),
	}

	svc.warnSkippedSetDir(t.Context(), false, "notes.txt")
	if buf.Len() != 0 {
		t.Fatalf("unexpected log for non-kept entry: %q", buf.String())
	}

	svc.warnSkippedSetDir(t.Context(), true, ".. ")
	logged := buf.String()
	if !strings.Contains(logged, `dir=".. "`) {
		t.Fatalf("log %q missing skipped directory name", logged)
	}
}
