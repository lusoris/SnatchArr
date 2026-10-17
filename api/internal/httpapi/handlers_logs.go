// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"log/slog"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/logring"
)

// defaultLogLimit matches the OpenAPI default of listLogs.limit.
const defaultLogLimit = 200

var logLevels = map[oas.LogLevel]slog.Level{
	oas.LogLevelDebug: slog.LevelDebug,
	oas.LogLevelInfo:  slog.LevelInfo,
	oas.LogLevelWarn:  slog.LevelWarn,
	oas.LogLevelError: slog.LevelError,
}

// ListLogs returns recent, already redacted log records of this process (admin only).
func (h *Handlers) ListLogs(ctx context.Context, params oas.ListLogsParams) (*oas.LogPage, error) {
	if err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	q := logring.Query{
		MinLevel: slog.LevelDebug,
		Text:     params.Q.Or(""),
		Source:   params.Source.Or(""),
		Limit:    params.Limit.Or(defaultLogLimit),
	}
	if lv, ok := params.Level.Get(); ok {
		q.MinLevel = logLevels[lv]
	}
	recs, dropped := h.logs.List(q)
	out := &oas.LogPage{Records: make([]oas.LogRecord, 0, len(recs)), Capacity: h.logs.Capacity(), Dropped: dropped}
	for _, r := range recs {
		attrs := make([]oas.LogAttr, 0, len(r.Attrs))
		for _, a := range r.Attrs {
			attrs = append(attrs, oas.LogAttr{Key: a.Key, Value: a.Value})
		}
		out.Records = append(out.Records, oas.LogRecord{
			Seq: r.Seq, Time: r.Time, Level: oas.LogLevel(logring.LevelName(r.Level)),
			Source: r.Source, Message: r.Message, Attrs: attrs,
		})
	}
	return out, nil
}
