// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package arrclient

import (
	"context"
	"fmt"
	"strings"

	"github.com/golusoris/goenvoy/arr/lidarr"
	"github.com/golusoris/goenvoy/arr/radarr"
	"github.com/golusoris/goenvoy/arr/readarr"
	"github.com/golusoris/goenvoy/arr/sonarr"
	"github.com/golusoris/goenvoy/arr/whisparr"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/queue"
)

// ForceImport imports the clean or fixable, mapped manual-import candidates of one
// download (rules id_matched and import_fixable) and returns the number of files sent.
// In ImportModeClean (id_matched), a candidate qualifies with no rejection.
// In ImportModeFixable (import_fixable), a candidate qualifies when every rejection
// classifies as fixable via queue.ClassifyRejection.
// All modes require a mapping from grab history: a series and at least one episode
// on Sonarr or Whisparr v2, a movie on Radarr or Whisparr v3, an artist, an album,
// and at least one track on Lidarr, or an author and book on Readarr. Without a
// qualifying candidate it sends nothing and returns queue.ErrNoCleanCandidate.
// The command runs asynchronously; the next sweep reads the result back.
// Listing and sending use Options.ImportTimeout.
func (p *GoenvoyProber) ForceImport(ctx context.Context, kind domain.AppKind, baseURL, apiKey, downloadID string, mode queue.ImportMode) (int, error) {
	ip := p.importProber()
	var (
		n   int
		err error
	)
	switch kind {
	case domain.KindSonarr:
		n, err = ip.sonarrForceImport(ctx, baseURL, apiKey, downloadID, mode)
	case domain.KindRadarr:
		n, err = ip.radarrForceImport(ctx, baseURL, apiKey, downloadID, mode)
	case domain.KindLidarr:
		n, err = ip.lidarrForceImport(ctx, baseURL, apiKey, downloadID, mode)
	case domain.KindReadarr:
		n, err = ip.readarrForceImport(ctx, baseURL, apiKey, downloadID, mode)
	case domain.KindWhisparrV2:
		n, err = ip.whisparrV2ForceImport(ctx, baseURL, apiKey, downloadID, mode)
	case domain.KindWhisparrV3:
		n, err = ip.whisparrV3ForceImport(ctx, baseURL, apiKey, downloadID, mode)
	case domain.KindStash:
		return 0, fmt.Errorf("arrclient: force import: %w: unsupported kind %q", domain.ErrInvalid, kind)
	default:
		return 0, fmt.Errorf("arrclient: force import: %w: unsupported kind %q", domain.ErrInvalid, kind)
	}
	if err != nil {
		return 0, fmt.Errorf("arrclient: %s force import %s: %w", kind, downloadID, err)
	}
	return n, nil
}

func (p *GoenvoyProber) importProber() *GoenvoyProber {
	o := p.opts
	o.Timeout = o.ImportTimeout
	return &GoenvoyProber{opts: o}
}

const (
	maxRejectionReasons   = 5
	maxRejectionReasonLen = 200
)

func collectRejectionReasons[C any, R any](cands []C, rejections func(*C) []R, reason func(R) string) []string {
	var reasons []string
	seen := make(map[string]bool)
	for i := range cands {
		reasons = appendCandidateReasons(reasons, seen, rejections(&cands[i]), reason)
	}
	return reasons
}

func appendCandidateReasons[R any](dst []string, seen map[string]bool, rejs []R, reason func(R) string) []string {
	for _, r := range rejs {
		if len(dst) >= maxRejectionReasons {
			break
		}
		rs := truncateChars(strings.TrimSpace(reason(r)), maxRejectionReasonLen)
		if rs == "" || seen[rs] {
			continue
		}
		seen[rs] = true
		dst = append(dst, rs)
	}
	return dst
}

func truncateChars(s string, maxChars int) string {
	runes := []rune(s)
	if len(runes) > maxChars {
		return string(runes[:maxChars])
	}
	return s
}

func filterQualifiedFiles[C any, R any, F any](
	cands []C,
	rejections func(*C) []R,
	reason func(R) string,
	file func(*C) (F, bool),
	mode queue.ImportMode,
) []F {
	files := make([]F, 0, len(cands))
	for i := range cands {
		if !qualifies(rejections(&cands[i]), reason, mode) {
			continue
		}
		if f, ok := file(&cands[i]); ok {
			files = append(files, f)
		}
	}
	return files
}

// forceImport runs candidate discovery, qualification, and submission across *arr kinds.
func forceImport[C any, R any, F any](
	ctx context.Context,
	p *GoenvoyProber,
	list func(context.Context) ([]C, error),
	rejections func(*C) []R,
	reason func(R) string,
	file func(*C) (F, bool),
	send func(context.Context, []F) error,
	mode queue.ImportMode,
) (int, error) {
	getCtx, cancel := context.WithTimeout(ctx, p.opts.Timeout)
	cands, err := list(getCtx)
	cancel()
	if err != nil {
		return 0, fmt.Errorf("list candidates: %w", err)
	}
	files := filterQualifiedFiles(cands, rejections, reason, file, mode)
	if len(files) == 0 {
		return 0, &queue.NoCleanCandidateError{
			Reasons: collectRejectionReasons(cands, rejections, reason),
		}
	}
	postCtx, cancel := context.WithTimeout(ctx, p.opts.Timeout)
	defer cancel()
	if err := send(postCtx, files); err != nil {
		return 0, err
	}
	return len(files), nil
}

func (p *GoenvoyProber) sonarrForceImport(ctx context.Context, baseURL, apiKey, downloadID string, mode queue.ImportMode) (int, error) {
	c, err := sonarr.New(baseURL, apiKey, p.clientOptions()...)
	if err != nil {
		return 0, fmt.Errorf("new client: %w", err)
	}
	return forceImport(
		ctx, p,
		func(ctx context.Context) ([]sonarr.ManualImportCandidate, error) {
			return c.GetManualImportCandidates(ctx, sonarr.ManualImportOptions{DownloadID: downloadID, FilterExistingFiles: true})
		},
		func(c *sonarr.ManualImportCandidate) []sonarr.ImportRejection { return c.Rejections },
		sonarrReason,
		func(c *sonarr.ManualImportCandidate) (sonarr.ManualImportFile, bool) { return c.ImportFile() },
		func(ctx context.Context, files []sonarr.ManualImportFile) error {
			if _, err := c.SendManualImport(ctx, sonarr.ImportModeAuto, files); err != nil {
				return fmt.Errorf("send manual import: %w", err)
			}
			return nil
		},
		mode,
	)
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

func sonarrReason(r sonarr.ImportRejection) string     { return r.Reason }
func radarrReason(r radarr.ImportRejection) string     { return r.Reason }
func lidarrReason(r lidarr.ImportRejection) string     { return r.Reason }
func readarrReason(r readarr.ImportRejection) string   { return r.Reason }
func whisparrReason(r whisparr.ImportRejection) string { return r.Reason }

func (p *GoenvoyProber) radarrForceImport(ctx context.Context, baseURL, apiKey, downloadID string, mode queue.ImportMode) (int, error) {
	c, err := radarr.New(baseURL, apiKey, p.clientOptions()...)
	if err != nil {
		return 0, fmt.Errorf("new client: %w", err)
	}
	return forceImport(
		ctx, p,
		func(ctx context.Context) ([]radarr.ManualImportCandidate, error) {
			return c.GetManualImportCandidates(ctx, radarr.ManualImportOptions{DownloadID: downloadID, FilterExistingFiles: true})
		},
		func(c *radarr.ManualImportCandidate) []radarr.ImportRejection { return c.Rejections },
		radarrReason,
		func(c *radarr.ManualImportCandidate) (radarr.ManualImportFile, bool) { return c.ImportFile() },
		func(ctx context.Context, files []radarr.ManualImportFile) error {
			if _, err := c.SendManualImport(ctx, radarr.ImportModeAuto, files); err != nil {
				return fmt.Errorf("send manual import: %w", err)
			}
			return nil
		},
		mode,
	)
}

func (p *GoenvoyProber) lidarrForceImport(ctx context.Context, baseURL, apiKey, downloadID string, mode queue.ImportMode) (int, error) {
	c, err := lidarr.New(baseURL, apiKey, p.clientOptions()...)
	if err != nil {
		return 0, fmt.Errorf("new client: %w", err)
	}
	return forceImport(
		ctx, p,
		func(ctx context.Context) ([]lidarr.ManualImportCandidate, error) {
			return c.GetManualImportCandidates(ctx, lidarr.ManualImportOptions{DownloadID: downloadID, FilterExistingFiles: true})
		},
		func(c *lidarr.ManualImportCandidate) []lidarr.ImportRejection { return c.Rejections },
		lidarrReason,
		func(c *lidarr.ManualImportCandidate) (lidarr.ManualImportFile, bool) { return c.ImportFile() },
		func(ctx context.Context, files []lidarr.ManualImportFile) error {
			if _, err := c.SendManualImport(ctx, lidarr.ImportModeAuto, files); err != nil {
				return fmt.Errorf("send manual import: %w", err)
			}
			return nil
		},
		mode,
	)
}

func (p *GoenvoyProber) readarrForceImport(ctx context.Context, baseURL, apiKey, downloadID string, mode queue.ImportMode) (int, error) {
	c, err := readarr.New(baseURL, apiKey, p.clientOptions()...)
	if err != nil {
		return 0, fmt.Errorf("new client: %w", err)
	}
	return forceImport(
		ctx, p,
		func(ctx context.Context) ([]readarr.ManualImportCandidate, error) {
			return c.GetManualImportCandidates(ctx, readarr.ManualImportOptions{DownloadID: downloadID, FilterExistingFiles: true})
		},
		func(c *readarr.ManualImportCandidate) []readarr.ImportRejection { return c.Rejections },
		readarrReason,
		func(c *readarr.ManualImportCandidate) (readarr.ManualImportFile, bool) { return c.ImportFile() },
		func(ctx context.Context, files []readarr.ManualImportFile) error {
			if _, err := c.SendManualImport(ctx, readarr.ImportModeAuto, files); err != nil {
				return fmt.Errorf("send manual import: %w", err)
			}
			return nil
		},
		mode,
	)
}

func (p *GoenvoyProber) whisparrV2ForceImport(ctx context.Context, baseURL, apiKey, downloadID string, mode queue.ImportMode) (int, error) {
	c, err := whisparr.New(baseURL, apiKey, p.clientOptions()...)
	if err != nil {
		return 0, fmt.Errorf("new client: %w", err)
	}
	return forceImport(
		ctx, p,
		func(ctx context.Context) ([]whisparr.ManualImportCandidate, error) {
			return c.GetManualImportCandidates(ctx, whisparr.ManualImportOptions{DownloadID: downloadID, FilterExistingFiles: true})
		},
		func(c *whisparr.ManualImportCandidate) []whisparr.ImportRejection { return c.Rejections },
		whisparrReason,
		func(c *whisparr.ManualImportCandidate) (whisparr.ManualImportFile, bool) { return c.ImportFile() },
		func(ctx context.Context, files []whisparr.ManualImportFile) error {
			if _, err := c.SendManualImport(ctx, whisparr.ImportModeAuto, files); err != nil {
				return fmt.Errorf("send manual import: %w", err)
			}
			return nil
		},
		mode,
	)
}

func (p *GoenvoyProber) whisparrV3ForceImport(ctx context.Context, baseURL, apiKey, downloadID string, mode queue.ImportMode) (int, error) {
	c, err := whisparr.NewV3(baseURL, apiKey, p.clientOptions()...)
	if err != nil {
		return 0, fmt.Errorf("new client: %w", err)
	}
	return forceImport(
		ctx, p,
		func(ctx context.Context) ([]whisparr.ManualImportCandidateV3, error) {
			return c.GetManualImportCandidates(ctx, whisparr.ManualImportOptionsV3{DownloadID: downloadID, FilterExistingFiles: true})
		},
		func(c *whisparr.ManualImportCandidateV3) []whisparr.ImportRejection { return c.Rejections },
		whisparrReason,
		func(c *whisparr.ManualImportCandidateV3) (whisparr.ManualImportFileV3, bool) { return c.ImportFile() },
		func(ctx context.Context, files []whisparr.ManualImportFileV3) error {
			if _, err := c.SendManualImport(ctx, whisparr.ImportModeAuto, files); err != nil {
				return fmt.Errorf("send manual import: %w", err)
			}
			return nil
		},
		mode,
	)
}
