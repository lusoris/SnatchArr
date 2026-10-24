// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package watchaware

import (
	"context"
	"errors"
	"fmt"
)

// ErrNilSink is returned by SyncAll when the provided sink is nil.
var ErrNilSink = errors.New("watchaware: sink is nil")

var errNilSource = errors.New("nil source")

// Sink stores synchronized watch state.
type Sink interface {
	StoreWatches(ctx context.Context, kind Kind, sourceID string, ws []Watch) error
}

// Named pairs a source identifier with its WatchSource implementation.
type Named struct {
	ID     string
	Source WatchSource
}

// SyncAll iterates through sources (bounded by maxSources) and stores their
// watch state in sink. For each source, it reads watches; on error it records
// a wrapped error formatted as "watchaware: source <id>: <err>", logs it via
// logf (if logf is non-nil), and continues with the next source. On success, it
// stores watches via sink.StoreWatches. SyncAll returns all collected errors.
func SyncAll(ctx context.Context, sources []Named, sink Sink, logf func(format string, args ...any)) []error {
	if sink == nil {
		return []error{ErrNilSink}
	}
	if len(sources) > maxSources {
		sources = sources[:maxSources]
	}

	var errs []error
	for _, s := range sources {
		if err := syncOne(ctx, s, sink, logf); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

func syncOne(ctx context.Context, s Named, sink Sink, logf func(format string, args ...any)) error {
	err := readAndStore(ctx, s, sink)
	if err == nil {
		return nil
	}
	if logf != nil {
		logf("watchaware: source %s: %v", s.ID, err)
	}
	return fmt.Errorf("watchaware: source %s: %w", s.ID, err)
}

func readAndStore(ctx context.Context, s Named, sink Sink) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	if s.Source == nil {
		return errNilSource
	}
	ws, err := s.Source.Watches(ctx)
	if err != nil {
		return fmt.Errorf("read watches: %w", err)
	}
	if err := sink.StoreWatches(ctx, s.Source.Kind(), s.ID, ws); err != nil {
		return fmt.Errorf("store watches: %w", err)
	}
	return nil
}
