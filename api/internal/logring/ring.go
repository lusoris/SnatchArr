// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package logring keeps the most recent log records of this process in a fixed-size
// in-memory ring, so the in-app log viewer can show them without shell or cluster access.
// Records are redacted before they are stored; every reader sees the redacted copy.
package logring

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golusoris/golusoris/realtime/sse"
)

// Ring size bounds and default (records).
const (
	MinSize     = 100
	MaxSize     = 20000
	DefaultSize = 2000
)

// SourceAPI names records of this api process.
const SourceAPI = "api"

// EventName is the SSE event name of a streamed record. It is the default "message"
// because the SPA's SSE client listens to that event type only; a named event would
// never reach it.
const EventName = "message"

// Attr is one rendered attribute; group names are joined to the key with dots.
type Attr struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Record is one stored, already redacted log record.
type Record struct {
	Seq     int64      `json:"seq"`
	Time    time.Time  `json:"time"`
	Level   slog.Level `json:"-"`
	Source  string     `json:"source"`
	Message string     `json:"message"`
	Attrs   []Attr     `json:"attrs"`
}

// LevelName is the lower-case level name the API uses (debug, info, warn, error).
func LevelName(l slog.Level) string {
	switch {
	case l < slog.LevelInfo:
		return "debug"
	case l < slog.LevelWarn:
		return "info"
	case l < slog.LevelError:
		return "warn"
	default:
		return "error"
	}
}

// streamRecord is the SSE payload: the record plus its level name.
type streamRecord struct {
	Record
	LevelName string `json:"level"`
}

// Query filters List. Empty Text and Source match everything; Limit is clamped to
// [1, capacity]. MinLevel is a real level: its zero value is info, so pass
// slog.LevelDebug to include debug records.
type Query struct {
	MinLevel slog.Level
	Text     string
	Source   string
	Limit    int
}

// Ring is a fixed-size, overwrite-oldest store of log records. It is safe for
// concurrent use.
type Ring struct {
	mu  sync.Mutex
	buf []Record
	seq int64 // sequence number of the newest record; 0 when empty
	hub *sse.Hub
}

// New returns a ring holding size records, clamped to [MinSize, MaxSize].
func New(size int) *Ring {
	size = min(max(size, MinSize), MaxSize)
	// The hub must not log: it runs inside the log handler, and a record about the
	// stream would re-enter the ring.
	return &Ring{buf: make([]Record, size), hub: sse.NewHub(slog.New(slog.DiscardHandler))}
}

// Capacity is the number of records the ring holds.
func (r *Ring) Capacity() int { return len(r.buf) }

// Add stores rec (already redacted), assigns its sequence number and pushes it to
// connected stream clients without blocking; ctx only scopes that push.
func (r *Ring) Add(ctx context.Context, rec Record) {
	r.mu.Lock()
	r.seq++
	rec.Seq = r.seq
	r.buf[(r.seq-1)%int64(len(r.buf))] = rec
	r.mu.Unlock()
	r.hub.Publish(ctx, sse.Event{
		ID: strconv.FormatInt(rec.Seq, 10), Event: EventName,
		Data: streamRecord{Record: rec, LevelName: LevelName(rec.Level)},
	})
}

// List returns the records matching q, newest first, and how many records the ring
// has overwritten since the process started.
func (r *Ring) List(q Query) (records []Record, dropped int64) {
	limit := min(max(q.Limit, 1), len(r.buf))
	text := strings.ToLower(q.Text)
	r.mu.Lock()
	defer r.mu.Unlock()
	held := min(r.seq, int64(len(r.buf)))
	records = make([]Record, 0, min(int64(limit), held))
	for i := range held {
		rec := r.buf[(r.seq-1-i)%int64(len(r.buf))]
		if !rec.matches(q, text) {
			continue
		}
		records = append(records, rec)
		if len(records) == limit {
			break
		}
	}
	return records, r.seq - held
}

func (rec Record) matches(q Query, lowerText string) bool {
	if rec.Level < q.MinLevel || !rec.fromSource(q.Source) {
		return false
	}
	if lowerText == "" || strings.Contains(strings.ToLower(rec.Message), lowerText) {
		return true
	}
	for _, a := range rec.Attrs {
		if strings.Contains(strings.ToLower(a.Key), lowerText) || strings.Contains(strings.ToLower(a.Value), lowerText) {
			return true
		}
	}
	return false
}

// fromSource reports whether rec matches the source filter: empty matches everything,
// SourceWorkers every worker, any other value exactly one source.
func (rec Record) fromSource(source string) bool {
	switch source {
	case "", rec.Source:
		return true
	case SourceWorkers:
		return strings.HasPrefix(rec.Source, sourceWorkerPrefix)
	default:
		return false
	}
}

// StreamHandler serves new records as Server-Sent Events (default event type message, id = seq).
// The caller mounts it behind the admin check.
func (r *Ring) StreamHandler() http.Handler { return r.hub.Handler() }
