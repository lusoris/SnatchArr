// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package logring

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"
)

// debugSink is an enabled handler that drops every record: slog.DiscardHandler reports
// every level disabled, so the logger would never call Handle.
type debugSink struct{}

func (debugSink) Enabled(context.Context, slog.Level) bool  { return true }
func (debugSink) Handle(context.Context, slog.Record) error { return nil }
func (s debugSink) WithAttrs([]slog.Attr) slog.Handler      { return s }
func (s debugSink) WithGroup(string) slog.Handler           { return s }

func debugHandler() slog.Handler { return debugSink{} }

func messages(recs []Record) []string {
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.Message)
	}
	return out
}

func TestNewClampsSize(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want int }{
		{0, MinSize},
		{-5, MinSize},
		{MinSize - 1, MinSize},
		{MinSize, MinSize},
		{DefaultSize, DefaultSize},
		{MaxSize, MaxSize},
		{MaxSize + 1, MaxSize},
	}
	for _, tc := range cases {
		if got := New(tc.in).Capacity(); got != tc.want {
			t.Errorf("New(%d).Capacity() = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestRingListEmpty(t *testing.T) {
	t.Parallel()
	recs, dropped := New(MinSize).List(Query{MinLevel: slog.LevelDebug, Limit: 10})
	if len(recs) != 0 || dropped != 0 {
		t.Fatalf("List() on an empty ring = %d records, %d dropped; want 0, 0", len(recs), dropped)
	}
}

// Writing past capacity keeps the newest records, newest first, with consecutive
// sequence numbers, and counts the overwritten ones.
func TestRingWraps(t *testing.T) {
	t.Parallel()
	r := New(MinSize)
	total := MinSize + 37
	for i := range total {
		r.Add(t.Context(), Record{Message: fmt.Sprintf("m%d", i)})
	}
	recs, dropped := r.List(Query{Limit: MaxSize})
	if len(recs) != MinSize || dropped != 37 {
		t.Fatalf("List() = %d records, %d dropped; want %d, 37", len(recs), dropped, MinSize)
	}
	for i, rec := range recs {
		wantSeq := int64(total - i)
		if rec.Seq != wantSeq || rec.Message != fmt.Sprintf("m%d", wantSeq-1) {
			t.Fatalf("record %d = seq %d %q, want seq %d m%d", i, rec.Seq, rec.Message, wantSeq, wantSeq-1)
		}
	}
}

func TestRingListFilters(t *testing.T) {
	t.Parallel()
	r := New(MinSize)
	r.Add(t.Context(), Record{Level: slog.LevelDebug, Source: SourceAPI, Message: "debug line"})
	r.Add(t.Context(), Record{Level: slog.LevelInfo, Source: "worker:w1", Message: "leased run", Attrs: []Attr{{"run_id", "R-42"}}})
	r.Add(t.Context(), Record{Level: slog.LevelError, Source: SourceAPI, Message: "Sonarr down"})
	cases := []struct {
		name string
		q    Query
		want []string
	}{
		{"positive everything from debug", Query{MinLevel: slog.LevelDebug, Limit: 10}, []string{"Sonarr down", "leased run", "debug line"}},
		{"positive zero level is info", Query{Limit: 10}, []string{"Sonarr down", "leased run"}},
		{"positive source", Query{MinLevel: slog.LevelDebug, Source: "worker:w1", Limit: 10}, []string{"leased run"}},
		{"positive text in message ignores case", Query{MinLevel: slog.LevelDebug, Text: "sonarr", Limit: 10}, []string{"Sonarr down"}},
		{"positive text in attribute", Query{MinLevel: slog.LevelDebug, Text: "r-42", Limit: 10}, []string{"leased run"}},
		{"boundary limit 1", Query{MinLevel: slog.LevelDebug, Limit: 1}, []string{"Sonarr down"}},
		{"boundary limit 0 is 1", Query{MinLevel: slog.LevelDebug}, []string{"Sonarr down"}},
		{"negative no match", Query{MinLevel: slog.LevelDebug, Text: "radarr", Limit: 10}, []string{}},
		{"negative unknown source", Query{MinLevel: slog.LevelDebug, Source: "api ", Limit: 10}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			recs, _ := r.List(tc.q)
			if got := messages(recs); !slices.Equal(got, tc.want) {
				t.Fatalf("List(%+v) = %v, want %v", tc.q, got, tc.want)
			}
		})
	}
}

// Records pass through to the next handler unchanged while the ring keeps a redacted
// copy with groups flattened to dotted keys.
func TestHandlerTeesAndFlattens(t *testing.T) {
	t.Parallel()
	r := New(MinSize)
	var out bytes.Buffer
	l := slog.New(r.Wrap(slog.NewJSONHandler(&out, nil), SourceAPI)).
		With("component", "planner").WithGroup("arr").With("instance", "sonarr")
	l.Info("tick", slog.Group("http", "status", 503), "password", "hunter2")
	var passed map[string]any
	if err := json.Unmarshal(out.Bytes(), &passed); err != nil {
		t.Fatalf("next handler output %q: %v", out.String(), err)
	}
	if passed["arr"].(map[string]any)["password"] != "hunter2" {
		t.Errorf("next handler got %v, want the original record", passed)
	}
	recs, _ := r.List(Query{Limit: 1})
	got := recs[0]
	want := []Attr{{"component", "planner"}, {"arr.instance", "sonarr"}, {"arr.http.status", "503"}, {"arr.password", Redacted}}
	if got.Source != SourceAPI || got.Message != "tick" || !slices.Equal(got.Attrs, want) {
		t.Fatalf("stored %+v, want source api, message tick, attrs %v", got, want)
	}
}

// Boundary: a record with more attributes than maxAttrs is stored with exactly
// maxAttrs, and an over-long message is cut at maxMessageBytes.
func TestHandlerBounds(t *testing.T) {
	t.Parallel()
	r := New(MinSize)
	l := slog.New(r.Wrap(debugHandler(), SourceAPI))
	args := make([]any, 0, 2*(maxAttrs+10))
	for i := range maxAttrs + 10 {
		args = append(args, fmt.Sprintf("k%d", i), i)
	}
	l.Info(strings.Repeat("x", maxMessageBytes+100), args...)
	recs, _ := r.List(Query{Limit: 1})
	if n := len(recs[0].Attrs); n != maxAttrs {
		t.Errorf("stored %d attrs, want %d", n, maxAttrs)
	}
	if n := len(recs[0].Message); n > maxMessageBytes+len("…") {
		t.Errorf("stored message of %d bytes, want at most %d", n, maxMessageBytes+len("…"))
	}
}

func TestLevelName(t *testing.T) {
	t.Parallel()
	cases := map[slog.Level]string{
		slog.LevelDebug - 4: "debug", slog.LevelDebug: "debug", slog.LevelInfo: "info", slog.LevelInfo + 1: "info",
		slog.LevelWarn: "warn", slog.LevelError: "error", slog.LevelError + 4: "error",
	}
	for l, want := range cases {
		if got := LevelName(l); got != want {
			t.Errorf("LevelName(%v) = %q, want %q", l, got, want)
		}
	}
}
