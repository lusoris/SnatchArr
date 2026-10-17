// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package logring

import (
	"context"
	"log/slog"
	"strings"
	"time"
)

// SourceWorkers is the source filter that selects the records of every worker; a single
// worker's records carry SourceWorker(id).
const SourceWorkers = "worker"

// sourceWorkerPrefix starts the source of every worker record.
const sourceWorkerPrefix = SourceWorkers + ":"

// maxSourceIDBytes caps the worker id kept in a source name.
const maxSourceIDBytes = 128

// maxKeyBytes caps an attribute key sent by another process.
const maxKeyBytes = 256

// SourceWorker names the records of the worker with the given id.
func SourceWorker(id string) string {
	if len(id) > maxSourceIDBytes {
		id = strings.ToValidUTF8(id[:maxSourceIDBytes], "")
	}
	return sourceWorkerPrefix + id
}

// RemoteRecord is a log record another process sent, not yet redacted.
type RemoteRecord struct {
	Time    time.Time
	Level   slog.Level
	Message string
	Attrs   []Attr
}

// AddRemote redacts rec exactly like a record of this process and stores it under source.
// Attributes past maxAttrs are dropped.
func (r *Ring) AddRemote(ctx context.Context, source string, rec RemoteRecord) {
	attrs := make([]Attr, 0, min(len(rec.Attrs), maxAttrs))
	for _, a := range rec.Attrs[:min(len(rec.Attrs), maxAttrs)] {
		key := a.Key
		if len(key) > maxKeyBytes {
			key = strings.ToValidUTF8(key[:maxKeyBytes], "")
		}
		if key == "" {
			continue
		}
		attrs = append(attrs, Attr{Key: key, Value: redactAttr(key, a.Value)})
	}
	r.Add(ctx, Record{
		Time: rec.Time, Level: rec.Level, Source: source,
		Message: redactText(rec.Message, maxMessageBytes), Attrs: attrs,
	})
}
