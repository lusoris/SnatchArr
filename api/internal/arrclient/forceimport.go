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

// ForceImport imports the clean, mapped manual-import candidates of one
// download (rule id_matched, ADR-0013) and returns the number of files sent.
// A candidate qualifies with no rejection and a mapping from grab history: a
// series and at least one episode on Sonarr, a movie on Radarr. Without a
// qualifying candidate it sends nothing and returns queue.ErrNoCleanCandidate.
// The command runs asynchronously; the next sweep reads the result back.
func (p *GoenvoyProber) ForceImport(ctx context.Context, kind domain.AppKind, baseURL, apiKey, downloadID string) (int, error) {
	var (
		n   int
		err error
	)
	switch kind {
	case domain.KindSonarr:
		n, err = p.sonarrForceImport(ctx, baseURL, apiKey, downloadID)
	case domain.KindRadarr:
		n, err = p.radarrForceImport(ctx, baseURL, apiKey, downloadID)
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

func (p *GoenvoyProber) sonarrForceImport(ctx context.Context, baseURL, apiKey, downloadID string) (int, error) {
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
		if len(cands[i].Rejections) > 0 {
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

func (p *GoenvoyProber) radarrForceImport(ctx context.Context, baseURL, apiKey, downloadID string) (int, error) {
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
		if len(cands[i].Rejections) > 0 {
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
