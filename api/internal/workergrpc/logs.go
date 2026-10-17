// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package workergrpc

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	snatcharrv1 "github.com/lusoris/SnatchArr/api/internal/gen/snatcharr/v1"
	"github.com/lusoris/SnatchArr/api/internal/logring"
)

// maxLogBatch caps the records stored from one ReportLogs call; the worker sends at most 200.
const maxLogBatch = 500

// maxLogFields caps the fields read from one record; the ring keeps at most 64 attributes.
const maxLogFields = 64

// logTargetKey is the attribute that carries a worker record's tracing target.
const logTargetKey = "target"

var protoLevels = map[snatcharrv1.Level]slog.Level{
	snatcharrv1.Level_LEVEL_DEBUG: slog.LevelDebug,
	snatcharrv1.Level_LEVEL_INFO:  slog.LevelInfo,
	snatcharrv1.Level_LEVEL_WARN:  slog.LevelWarn,
	snatcharrv1.Level_LEVEL_ERROR: slog.LevelError,
}

// ReportLogs stores a batch of worker log records in the log ring under the worker's
// source, redacted like the API's own records (#349). Records past maxLogBatch are not
// stored; a dropped count from the worker becomes one warn record of its own.
func (s *Server) ReportLogs(ctx context.Context, req *snatcharrv1.ReportLogsRequest) (*snatcharrv1.ReportLogsResponse, error) {
	if req.GetWorkerId() == "" {
		return nil, status.Error(codes.InvalidArgument, "worker_id is required")
	}
	if s.logs == nil {
		return &snatcharrv1.ReportLogsResponse{}, nil
	}
	source := logring.SourceWorker(req.GetWorkerId())
	now := s.clk.Now()
	records := req.GetRecords()
	records = records[:min(len(records), maxLogBatch)]
	for _, r := range records {
		s.logs.AddRemote(ctx, source, remoteRecord(r, now))
	}
	if n := req.GetDropped(); n > 0 {
		s.logs.AddRemote(ctx, source, logring.RemoteRecord{
			Time: now, Level: slog.LevelWarn, Message: "worker dropped log records: its buffer was full",
			Attrs: []logring.Attr{{Key: "dropped", Value: strconv.FormatUint(n, 10)}},
		})
	}
	return &snatcharrv1.ReportLogsResponse{Accepted: uint32(len(records))}, nil // #nosec G115 -- len is at most maxLogBatch
}

// remoteRecord converts one worker record; a record without a time takes now.
func remoteRecord(r *snatcharrv1.LogRecord, now time.Time) logring.RemoteRecord {
	at := now
	if ms := r.GetTsUnixMs(); ms > 0 {
		at = time.UnixMilli(ms)
	}
	level, ok := protoLevels[r.GetLevel()]
	if !ok {
		level = slog.LevelInfo
	}
	fields := r.GetFields()
	fields = fields[:min(len(fields), maxLogFields)]
	attrs := make([]logring.Attr, 0, len(fields)+1)
	if t := r.GetTarget(); t != "" {
		attrs = append(attrs, logring.Attr{Key: logTargetKey, Value: t})
	}
	for _, f := range fields {
		attrs = append(attrs, logring.Attr{Key: f.GetKey(), Value: f.GetValue()})
	}
	return logring.RemoteRecord{Time: at, Level: level, Message: r.GetMessage(), Attrs: attrs}
}
