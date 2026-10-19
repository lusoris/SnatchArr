// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package arrclient

import (
	"context"
	"fmt"

	"github.com/golusoris/goenvoy/arr/radarr"
	"github.com/golusoris/goenvoy/arr/sonarr"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/queue"
)

// ForceImport imports the clean or fixable, mapped manual-import candidates of one
// download (rules id_matched and import_fixable) and returns the number of files sent.
// In ImportModeClean (id_matched), a candidate qualifies with no rejection.
// In ImportModeFixable (import_fixable), a candidate qualifies when every rejection
// classifies as fixable via queue.ClassifyRejection.
// Both modes require a mapping from grab history: a series and at least one episode
// on Sonarr, a movie on Radarr. Without a qualifying candidate it sends nothing and
// returns queue.ErrNoCleanCandidate.
// The command runs asynchronously; the next sweep reads the result back.
func (p *GoenvoyProber) ForceImport(ctx context.Context, kind domain.AppKind, baseURL, apiKey, downloadID string, mode queue.ImportMode) (int, error) {
	var (
		n   int
		err error
	)
	switch kind {
	case domain.KindSonarr:
		n, err = p.sonarrForceImport(ctx, baseURL, apiKey, downloadID, mode)
	case domain.KindRadarr:
		n, err = p.radarrForceImport(ctx, baseURL, apiKey, downloadID, mode)
	case domain.KindLidarr, domain.KindReadarr, domain.KindWhisparrV2, domain.KindWhisparrV3:
		return 0, fmt.Errorf("arrclient: %s force import: %w", kind, queue.ErrQueueUnsupported)
	default:
		return 0, fmt.Errorf("arrclient: force import: %w: unsupported kind %q", domain.ErrInvalid, kind)
	}
	if err != nil {
		return 0, fmt.Errorf("arrclient: %s force import %s: %w", kind, downloadID, err)
	}
	return n, nil
}

func (p *GoenvoyProber) sonarrForceImport(ctx context.Context, baseURL, apiKey, downloadID string, mode queue.ImportMode) (int, error) {
	c, err := sonarr.New(baseURL, apiKey, p.clientOptions()...)
	if err != nil {
		return 0, fmt.Errorf("new client: %w", err)
	}
	getCtx, cancel := context.WithTimeout(ctx, p.opts.Timeout)
	cands, err := c.GetManualImportCandidates(getCtx, sonarr.ManualImportOptions{DownloadID: downloadID, FilterExistingFiles: true})
	cancel()
	if err != nil {
		return 0, fmt.Errorf("list candidates: %w", err)
	}
	files := make([]sonarr.ManualImportFile, 0, len(cands))
	for i := range cands {
		if !qualifies(cands[i].Rejections, sonarrReason, mode) {
			continue
		}
		if f, ok := cands[i].ImportFile(); ok {
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		return 0, queue.ErrNoCleanCandidate
	}
	postCtx, cancel := context.WithTimeout(ctx, p.opts.Timeout)
	defer cancel()
	if _, err := c.SendManualImport(postCtx, sonarr.ImportModeAuto, files); err != nil {
		return 0, fmt.Errorf("send manual import: %w", err)
	}
	return len(files), nil
}

// qualifies reports whether a candidate may be imported: a clean candidate
// always, a rejected one only in fixable mode when every rejection classifies
// fixable through the shared queue table.
func qualifies[R any](rejections []R, reason func(R) string, mode queue.ImportMode) bool {
	if len(rejections) == 0 {
		return true
	}
	if mode != queue.ImportModeFixable {
		return false
	}
	for _, r := range rejections {
		if queue.ClassifyRejection(reason(r)) != queue.ClassFixable {
			return false
		}
	}
	return true
}

func sonarrReason(r sonarr.ImportRejection) string { return r.Reason }

func radarrReason(r radarr.ImportRejection) string { return r.Reason }

func (p *GoenvoyProber) radarrForceImport(ctx context.Context, baseURL, apiKey, downloadID string, mode queue.ImportMode) (int, error) {
	c, err := radarr.New(baseURL, apiKey, p.clientOptions()...)
	if err != nil {
		return 0, fmt.Errorf("new client: %w", err)
	}
	getCtx, cancel := context.WithTimeout(ctx, p.opts.Timeout)
	cands, err := c.GetManualImportCandidates(getCtx, radarr.ManualImportOptions{DownloadID: downloadID, FilterExistingFiles: true})
	cancel()
	if err != nil {
		return 0, fmt.Errorf("list candidates: %w", err)
	}
	files := make([]radarr.ManualImportFile, 0, len(cands))
	for i := range cands {
		if !qualifies(cands[i].Rejections, radarrReason, mode) {
			continue
		}
		if f, ok := cands[i].ImportFile(); ok {
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		return 0, queue.ErrNoCleanCandidate
	}
	postCtx, cancel := context.WithTimeout(ctx, p.opts.Timeout)
	defer cancel()
	if _, err := c.SendManualImport(postCtx, radarr.ImportModeAuto, files); err != nil {
		return 0, fmt.Errorf("send manual import: %w", err)
	}
	return len(files), nil
}
