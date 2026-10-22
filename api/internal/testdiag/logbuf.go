// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package testdiag

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

type sharedBuffer struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	capBytes int
	dropped  int
}

func (s *sharedBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dropped > 0 || s.buf.Len()+len(p) > s.capBytes {
		s.dropped++
		return len(p), nil
	}
	s.buf.Write(p)
	return len(p), nil
}

// LogBuffer is a slog.Handler that keeps records at or above a level in memory, capped in bytes.
type LogBuffer struct {
	shared *sharedBuffer
	level  slog.Level
	h      slog.Handler
}

// Compile-time check that LogBuffer implements slog.Handler.
var _ slog.Handler = (*LogBuffer)(nil)

// NewLogBuffer creates a LogBuffer capturing records at or above level up to capBytes.
func NewLogBuffer(level slog.Level, capBytes int) *LogBuffer {
	shared := &sharedBuffer{
		capBytes: capBytes,
	}
	inner := slog.NewTextHandler(shared, &slog.HandlerOptions{
		Level: level,
	})
	return &LogBuffer{
		shared: shared,
		level:  level,
		h:      inner,
	}
}

// Enabled reports whether the handler handles records at the given level.
func (b *LogBuffer) Enabled(_ context.Context, level slog.Level) bool {
	if b == nil {
		return false
	}
	return level >= b.level
}

// Handle handles the Record by passing it to the underlying text handler if enabled.
func (b *LogBuffer) Handle(ctx context.Context, r slog.Record) error {
	if b == nil || !b.Enabled(ctx, r.Level) {
		return nil
	}
	if err := b.h.Handle(ctx, r); err != nil {
		return fmt.Errorf("logbuf: handle: %w", err)
	}
	return nil
}

// WithAttrs returns a new Handler whose attributes consist of both the receiver's attributes and the arguments.
func (b *LogBuffer) WithAttrs(attrs []slog.Attr) slog.Handler {
	if b == nil {
		return nil
	}
	return &LogBuffer{
		shared: b.shared,
		level:  b.level,
		h:      b.h.WithAttrs(attrs),
	}
}

// WithGroup returns a new Handler with the given group appended.
func (b *LogBuffer) WithGroup(name string) slog.Handler {
	if b == nil {
		return nil
	}
	if name == "" {
		return b
	}
	return &LogBuffer{
		shared: b.shared,
		level:  b.level,
		h:      b.h.WithGroup(name),
	}
}

// String returns the captured log records as text lines.
// When the cap was hit, the last line is "logbuf: truncated, N records dropped".
func (b *LogBuffer) String() string {
	if b == nil {
		return ""
	}
	b.shared.mu.Lock()
	defer b.shared.mu.Unlock()
	if b.shared.buf.Len() == 0 && b.shared.dropped == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(b.shared.buf.String())
	if b.shared.dropped > 0 {
		if sb.Len() > 0 && !strings.HasSuffix(sb.String(), "\n") {
			sb.WriteByte('\n')
		}
		fmt.Fprintf(&sb, "logbuf: truncated, %d records dropped\n", b.shared.dropped)
	}
	return sb.String()
}
