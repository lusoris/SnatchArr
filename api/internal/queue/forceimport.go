// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package queue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/events"
)

// Import outcomes, the result label of QueueImport and the reason of a
// queue_item_import_failed event.
const (
	importOK         = "ok"
	importError      = "error"
	importRefused    = "no_clean_candidate"
	importNotCleared = "still_id_matched"
	importWould      = "would_import"
)

// processImports sends the force imports of one sweep and reports the
// read-back of the previous one. A sent or refused import stamps the strike
// row of its download, so the next sweep can read it back and does not try
// again within strikeTTL; a send error stamps nothing and the next sweep retries.
func (s *Service) processImports(ctx context.Context, inst Instance, ds []Decision, now time.Time) {
	stamps := make(map[string]bool)
	for i := range ds {
		d := &ds[i]
		switch {
		case d.Import:
			if refused, ok := s.handleImport(ctx, inst, *d, now); ok {
				if refused {
					d.Import = false
					d.WouldImport = true
				}
				stamps[d.DownloadID] = refused
			}
		case d.ImportFailed:
			s.emitImportFailed(ctx, inst, *d, now, importNotCleared)
			s.metrics.QueueImport(inst.Name, importNotCleared)
		}
	}
	for i := range ds {
		if refused, ok := stamps[ds[i].DownloadID]; ok {
			ds[i].Next.ImportedAt = now
			ds[i].Next.ImportRefused = refused
		}
	}
}

// handleImport sends one force import. It returns whether the *arr offered no
// clean candidate, and whether the strike row is to be stamped.
func (s *Service) handleImport(ctx context.Context, inst Instance, d Decision, now time.Time) (bool, bool) {
	mode := ImportModeClean
	if d.Rule == RuleImportFixable {
		mode = ImportModeFixable
	}
	n, err := s.arr.ForceImport(ctx, inst.Kind, inst.BaseURL, inst.APIKey, d.DownloadID, mode)
	switch {
	case errors.Is(err, ErrQueueUnsupported):
		s.logger.InfoContext(ctx, "queue: force import unsupported for kind", slog.String("instance", inst.Name), slog.String("kind", string(inst.Kind)), slog.String("download_id", d.DownloadID))
		s.emitImportFailed(ctx, inst, d, now, importRefused)
		s.metrics.QueueImport(inst.Name, importRefused)
		return true, true
	case errors.Is(err, ErrNoCleanCandidate):
		s.logger.InfoContext(ctx, "queue: force import refused", slog.String("instance", inst.Name), slog.String("download_id", d.DownloadID))
		s.emitImportFailed(ctx, inst, d, now, importRefused)
		s.metrics.QueueImport(inst.Name, importRefused)
		return true, true
	case err != nil:
		s.logger.WarnContext(ctx, "queue: force import failed", slog.String("instance", inst.Name), slog.String("download_id", d.DownloadID), slog.String("error", err.Error()))
		s.emitImportFailed(ctx, inst, d, now, importError)
		s.metrics.QueueImport(inst.Name, importError)
		return false, false
	}
	s.events.Publish(ctx, events.Frame{
		InstanceID: inst.ID,
		Timestamp:  now,
		Level:      "info",
		Type:       "queue_item_imported",
		EntityID:   int64(d.ItemID),
		Title:      d.DownloadID,
		Detail:     fmt.Sprintf("imported %d files by rule %s", n, d.Rule),
	})
	s.metrics.QueueImport(inst.Name, importOK)
	return false, true
}

func (s *Service) emitImportFailed(ctx context.Context, inst Instance, d Decision, now time.Time, reason string) {
	s.events.Publish(ctx, events.Frame{
		InstanceID: inst.ID,
		Timestamp:  now,
		Level:      "warn",
		Type:       "queue_item_import_failed",
		EntityID:   int64(d.ItemID),
		Title:      d.DownloadID,
		Detail:     reason,
	})
}

// logShadow writes the lines the id_matched parity check reads (ADR-0014): each
// rule's first strike of an item, and each download the observe mode would import,
// once per download when the threshold is first reached.
func (s *Service) logShadow(ctx context.Context, inst Instance, d Decision) {
	if d.NewStrike {
		s.logger.InfoContext(ctx, "queue: item struck", slog.String("instance", inst.Name), slog.String("rule", string(d.Rule)), slog.String("download_id", d.DownloadID), slog.String("title", d.Title))
	}
	if d.WouldImport && d.Reached {
		s.logger.InfoContext(ctx, "queue: would force import", slog.String("instance", inst.Name), slog.String("download_id", d.DownloadID), slog.String("title", d.Title))
		s.metrics.QueueImport(inst.Name, importWould)
	}
}
