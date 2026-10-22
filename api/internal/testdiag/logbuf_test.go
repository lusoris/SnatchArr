// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package testdiag

import (
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestLogBuffer_Positive(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		logAction  func(logger *slog.Logger)
		wantSub    []string
		wantAbsent []string
	}{
		{
			name: "warn record with message and attrs",
			logAction: func(l *slog.Logger) {
				l.Warn("webhook delivery slow", slog.String("channel", "discord"), slog.Int("attempt", 3))
			},
			wantSub: []string{"msg=\"webhook delivery slow\"", "channel=discord", "attempt=3", "level=WARN"},
		},
		{
			name: "record through with attrs child",
			logAction: func(l *slog.Logger) {
				child := l.With(slog.String("component", "engine"), slog.Int("worker_id", 1))
				child.Warn("channel deliver failed", slog.String("target", "webhook-f1"))
			},
			wantSub: []string{"component=engine", "worker_id=1", "msg=\"channel deliver failed\"", "target=webhook-f1"},
		},
		{
			name: "record through with group child",
			logAction: func(l *slog.Logger) {
				grp := l.WithGroup("meta")
				grp.Warn("rate bucket low", slog.Int("remaining", 5))
			},
			wantSub: []string{"msg=\"rate bucket low\"", "meta.remaining=5"},
		},
		{
			name: "multiple records from parent and children in same buffer",
			logAction: func(l *slog.Logger) {
				l.Warn("parent alert", slog.String("role", "parent"))
				child := l.With(slog.String("child", "one"))
				child.Warn("child alert")
			},
			wantSub: []string{"parent alert", "child alert", "child=one"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			buf := NewLogBuffer(slog.LevelWarn, 64<<10)
			logger := slog.New(buf)

			tc.logAction(logger)

			got := buf.String()
			for _, sub := range tc.wantSub {
				if !strings.Contains(got, sub) {
					t.Errorf("buf.String() missing %q; full content:\n%s", sub, got)
				}
			}
			for _, absent := range tc.wantAbsent {
				if strings.Contains(got, absent) {
					t.Errorf("buf.String() contains unexpected %q", absent)
				}
			}
		})
	}
}

func TestLogBuffer_Negative(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		logAction func(logger *slog.Logger)
	}{
		{
			name: "empty buffer produces empty string",
			logAction: func(_ *slog.Logger) {
			},
		},
		{
			name: "info record below warn level not kept",
			logAction: func(l *slog.Logger) {
				l.Info("regular progress info", slog.String("event", "dispatch"))
			},
		},
		{
			name: "debug record below warn level not kept",
			logAction: func(l *slog.Logger) {
				l.Debug("verbose debugging trace", slog.Int("seq", 42))
			},
		},
		{
			name: "child with attrs below warn level not kept",
			logAction: func(l *slog.Logger) {
				child := l.With(slog.String("child", "info-child"))
				child.Info("child info message")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			buf := NewLogBuffer(slog.LevelWarn, 64<<10)
			logger := slog.New(buf)

			tc.logAction(logger)

			if got := buf.String(); got != "" {
				t.Fatalf("buf.String() = %q, want empty string", got)
			}
		})
	}
}

func TestLogBuffer_Boundary(t *testing.T) {
	t.Parallel()

	t.Run("cap hit truncation line with exact dropped count", func(t *testing.T) {
		t.Parallel()
		// Determine exact single record size first with identical attributes
		probeBuf := NewLogBuffer(slog.LevelWarn, 1024)
		probeLogger := slog.New(probeBuf)
		probeLogger.Warn("test-record-size-msg", slog.Int("index", 0))
		singleSize := len(probeBuf.String())
		if singleSize == 0 {
			t.Fatal("probe record resulted in 0 bytes")
		}

		// Cap buffer to hold exactly 2 records
		capBytes := singleSize*2 + 10
		buf := NewLogBuffer(slog.LevelWarn, capBytes)
		logger := slog.New(buf)

		// Write 5 records: first 2 should fit, last 3 must be dropped
		for i := range 5 {
			logger.Warn("test-record-size-msg", slog.Int("index", i))
		}

		out := buf.String()
		lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
		if len(lines) < 2 {
			t.Fatalf("expected at least 2 lines, got %d:\n%s", len(lines), out)
		}
		lastLine := lines[len(lines)-1]
		const wantLast = "logbuf: truncated, 3 records dropped"
		if lastLine != wantLast {
			t.Fatalf("last line = %q, want %q; full content:\n%s", lastLine, wantLast, out)
		}
	})

	t.Run("zero cap hit immediately", func(t *testing.T) {
		t.Parallel()
		buf := NewLogBuffer(slog.LevelWarn, 0)
		logger := slog.New(buf)

		logger.Warn("should be dropped immediately")
		logger.Warn("second dropped")

		out := buf.String()
		const wantLast = "logbuf: truncated, 2 records dropped"
		if strings.TrimSpace(out) != wantLast {
			t.Fatalf("out = %q, want %q", out, wantLast)
		}
	})

	t.Run("concurrent writers under race", func(t *testing.T) {
		t.Parallel()
		const goroutines = 8
		const recordsPerGoroutine = 100
		const totalRecords = goroutines * recordsPerGoroutine

		// Small cap: forces truncation and concurrent contention on dropped counter
		buf := NewLogBuffer(slog.LevelWarn, 500)
		logger := slog.New(buf)

		var wg sync.WaitGroup
		for g := range goroutines {
			wg.Add(1)
			go func(gid int) {
				defer wg.Done()
				child := logger.With(slog.Int("gid", gid))
				for i := range recordsPerGoroutine {
					child.Warn("concurrent msg", slog.Int("i", i))
				}
			}(g)
		}
		wg.Wait()

		out := buf.String()
		lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
		lastLine := lines[len(lines)-1]
		if !strings.HasPrefix(lastLine, "logbuf: truncated, ") {
			t.Fatalf("expected truncation prefix on last line, got %q", lastLine)
		}

		// Parse dropped count from last line: "logbuf: truncated, N records dropped"
		parts := strings.Split(lastLine, " ")
		if len(parts) != 5 {
			t.Fatalf("unexpected last line format: %q", lastLine)
		}
		dropped, err := strconv.Atoi(parts[2])
		if err != nil {
			t.Fatalf("parse dropped count from %q: %v", lastLine, err)
		}

		keptLines := len(lines) - 1 // excluding the truncation line
		if keptLines+dropped != totalRecords {
			t.Fatalf("kept %d + dropped %d = %d, want total %d", keptLines, dropped, keptLines+dropped, totalRecords)
		}
	})
}
