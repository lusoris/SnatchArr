// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClassifySetDir(t *testing.T) {
	t.Parallel()
	baseDir := t.TempDir()

	plainDir := filepath.Join(baseDir, "gallery_1")
	tripleDotDir := filepath.Join(baseDir, "...")
	twoDotsSpaceDir := filepath.Join(baseDir, ".. ")
	spaceDotDir := filepath.Join(baseDir, " .")
	for _, dir := range []string{plainDir, tripleDotDir, twoDotsSpaceDir, spaceDotDir} {
		if err := os.Mkdir(dir, 0o750); err != nil {
			t.Fatalf("mkdir %q: %v", dir, err)
		}
	}

	symlinkDir := filepath.Join(baseDir, "symlink_dir")
	if err := os.Symlink(plainDir, symlinkDir); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	regularFile := filepath.Join(baseDir, "notes.txt")
	if err := os.WriteFile(regularFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	missingPath := filepath.Join(baseDir, "missing")

	cases := []struct {
		name     string
		subPath  string
		wantScan bool
		wantKeep bool
	}{
		{name: "plain dir", subPath: plainDir, wantScan: true, wantKeep: true},
		{name: "triple dot dir", subPath: tripleDotDir, wantScan: true, wantKeep: true},
		{name: "two dots space dir", subPath: twoDotsSpaceDir, wantScan: false, wantKeep: true},
		{name: "space dot dir", subPath: spaceDotDir, wantScan: false, wantKeep: true},
		{name: "symlink to dir", subPath: symlinkDir, wantScan: false, wantKeep: false},
		{name: "regular file", subPath: regularFile, wantScan: false, wantKeep: false},
		{name: "missing path", subPath: missingPath, wantScan: false, wantKeep: false},
	}
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
