// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package workergrpc

import (
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/golusoris/golusoris/core/clock"

	snatcharrv1 "github.com/lusoris/SnatchArr/api/internal/gen/snatcharr/v1"
	"github.com/lusoris/SnatchArr/api/internal/logring"
)

// plantedWorkerHex has the *arr API key shape; no worker record may carry it into the ring.
const plantedWorkerHex = "fedcba9876543210fedcba9876543210"

func logsServer(ring *logring.Ring) (*Server, clock.Clock) {
	clk := clock.NewFake()
	return &Server{logs: ring, clk: clk, logger: slog.New(slog.DiscardHandler), stopping: make(chan struct{})}, clk
}

func renderRecords(recs []logring.Record) string {
	var sb strings.Builder
	for _, r := range recs {
		sb.WriteString(r.Source + " " + logring.LevelName(r.Level) + " " + r.Message)
		for _, a := range r.Attrs {
			sb.WriteString(" " + a.Key + "=" + a.Value)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func TestReportLogsStoresRedactedWorkerRecords(t *testing.T) {
	t.Parallel()
	ring := logring.New(logring.MinSize)
	srv, clk := logsServer(ring)
	resp, err := srv.ReportLogs(t.Context(), &snatcharrv1.ReportLogsRequest{
		WorkerId: "w1",
		Records: []*snatcharrv1.LogRecord{
			{
				TsUnixMs: 1_700_000_000_123, Level: snatcharrv1.Level_LEVEL_ERROR, Target: "snatch_worker::executor",
				Message: "search failed: GET /api/v3/command?apikey=" + plantedWorkerHex,
				Fields:  []*snatcharrv1.LogField{{Key: "api_key", Value: plantedWorkerHex}, {Key: "run", Value: "r-1"}},
			},
			{Message: "no time, no level"},
		},
	})
	if err != nil {
		t.Fatalf("ReportLogs: %v", err)
	}
	if resp.GetAccepted() != 2 {
		t.Errorf("accepted = %d, want 2", resp.GetAccepted())
	}
	recs, _ := ring.List(logring.Query{MinLevel: slog.LevelDebug, Source: logring.SourceWorkers, Limit: 10})
	stored := renderRecords(recs)
	if strings.Contains(stored, plantedWorkerHex) {
		t.Fatalf("ring leaks the planted worker key:\n%s", stored)
	}
	if len(recs) != 2 {
		t.Fatalf("records = %d, want 2:\n%s", len(recs), stored)
	}
	first, second := recs[1], recs[0] // newest first
	if first.Source != "worker:w1" || first.Level != slog.LevelError || !first.Time.Equal(time.UnixMilli(1_700_000_000_123)) {
		t.Errorf("first record = %+v", first)
	}
	for _, want := range []string{"target=snatch_worker::executor", "run=r-1", "api_key=" + logring.Redacted} {
		if !strings.Contains(stored, want) {
			t.Errorf("stored records lack %q:\n%s", want, stored)
		}
	}
	if second.Level != slog.LevelInfo || !second.Time.Equal(clk.Now()) {
		t.Errorf("record without time and level = %+v, want info at %v", second, clk.Now())
	}
}

func TestReportLogsDroppedCountBecomesAWarning(t *testing.T) {
	t.Parallel()
	ring := logring.New(logring.MinSize)
	srv, _ := logsServer(ring)
	if _, err := srv.ReportLogs(t.Context(), &snatcharrv1.ReportLogsRequest{WorkerId: "w2", Dropped: 37}); err != nil {
		t.Fatalf("ReportLogs: %v", err)
	}
	recs, _ := ring.List(logring.Query{Source: logring.SourceWorker("w2"), Limit: 10})
	if len(recs) != 1 || recs[0].Level != slog.LevelWarn || renderRecords(recs) != "worker:w2 warn worker dropped log records: its buffer was full dropped=37\n" {
		t.Errorf("records = %q, want one warn record with dropped=37", renderRecords(recs))
	}
}

func TestReportLogsBounds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		records   int
		wantStore int
	}{
		{"empty batch", 0, 0},
		{"one record", 1, 1},
		{"at the batch cap", maxLogBatch, maxLogBatch},
		{"over the batch cap", maxLogBatch + 1, maxLogBatch},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ring := logring.New(logring.MaxSize)
			srv, _ := logsServer(ring)
			batch := make([]*snatcharrv1.LogRecord, 0, c.records)
			for i := range c.records {
				batch = append(batch, &snatcharrv1.LogRecord{Level: snatcharrv1.Level_LEVEL_INFO, Message: "r" + strconv.Itoa(i)})
			}
			resp, err := srv.ReportLogs(t.Context(), &snatcharrv1.ReportLogsRequest{WorkerId: "w", Records: batch})
			if err != nil {
				t.Fatalf("ReportLogs: %v", err)
			}
			recs, _ := ring.List(logring.Query{Limit: logring.MaxSize})
			if int(resp.GetAccepted()) != c.wantStore || len(recs) != c.wantStore {
				t.Errorf("accepted = %d, stored = %d, want %d", resp.GetAccepted(), len(recs), c.wantStore)
			}
		})
	}
}

func TestReportLogsRefusals(t *testing.T) {
	t.Parallel()
	ring := logring.New(logring.MinSize)
	srv, _ := logsServer(ring)
	_, err := srv.ReportLogs(t.Context(), &snatcharrv1.ReportLogsRequest{Records: []*snatcharrv1.LogRecord{{Message: "anonymous"}}})
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("no worker id: code = %v, want InvalidArgument", status.Code(err))
	}
	if recs, _ := ring.List(logring.Query{Limit: 10}); len(recs) != 0 {
		t.Errorf("refused batch stored %d records", len(recs))
	}
	// Without a ring the batch is accepted and discarded, so a worker never retries it.
	bare, _ := logsServer(nil)
	resp, err := bare.ReportLogs(t.Context(), &snatcharrv1.ReportLogsRequest{WorkerId: "w", Records: []*snatcharrv1.LogRecord{{Message: "x"}}})
	if err != nil || resp.GetAccepted() != 0 {
		t.Errorf("no ring: accepted = %d, err = %v, want 0, nil", resp.GetAccepted(), err)
	}
}
