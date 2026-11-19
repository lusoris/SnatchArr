// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package arrclient

import (
	"context"
	"fmt"

	"github.com/golusoris/goenvoy/arr/lidarr"
	"github.com/golusoris/goenvoy/arr/radarr"
	"github.com/golusoris/goenvoy/arr/readarr"
	"github.com/golusoris/goenvoy/arr/sonarr"
	"github.com/golusoris/goenvoy/arr/v2"
	"github.com/golusoris/goenvoy/arr/whisparr"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

const maxFolderEntries = 10000

// ListFolder lists the entry names the *arr process can see in dir.
func (p *GoenvoyProber) ListFolder(ctx context.Context, kind domain.AppKind, baseURL, apiKey, dir string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, p.opts.Timeout)
	defer cancel()
	names, err := p.listFolder(ctx, kind, baseURL, apiKey, dir)
	if err != nil {
		return nil, fmt.Errorf("arrclient: list folder %q for %s: %w", dir, kind, err)
	}
	return names, nil
}

func (p *GoenvoyProber) listFolder(ctx context.Context, kind domain.AppKind, baseURL, apiKey, dir string) ([]string, error) {
	o := p.clientOptions()
	switch kind {
	case domain.KindSonarr:
		return sonarrFolder(ctx, baseURL, apiKey, dir, o)
	case domain.KindRadarr:
		return radarrFolder(ctx, baseURL, apiKey, dir, o)
	case domain.KindLidarr:
		return lidarrFolder(ctx, baseURL, apiKey, dir, o)
	case domain.KindReadarr:
		return readarrFolder(ctx, baseURL, apiKey, dir, o)
	case domain.KindWhisparrV2:
		return whisparrV2Folder(ctx, baseURL, apiKey, dir, o)
	case domain.KindWhisparrV3:
		return whisparrV3Folder(ctx, baseURL, apiKey, dir, o)
	case domain.KindStash:
		return nil, fmt.Errorf("%w: unsupported kind %q", domain.ErrInvalid, kind)
	default:
		return nil, fmt.Errorf("%w: unsupported kind %q", domain.ErrInvalid, kind)
	}
}

func sonarrFolder(ctx context.Context, baseURL, apiKey, dir string, o []arr.Option) ([]string, error) {
	c, err := sonarr.New(baseURL, apiKey, o...)
	if err != nil {
		return nil, fmt.Errorf("sonarr filesystem client: %w", err)
	}
	r, err := c.BrowseFileSystem(ctx, dir, true)
	if err != nil {
		return nil, fmt.Errorf("sonarr filesystem %q: %w", dir, err)
	}
	names, err := sonarrNames(r)
	if err != nil {
		return nil, fmt.Errorf("sonarr filesystem %q response: %w", dir, err)
	}
	return names, nil
}

func radarrFolder(ctx context.Context, baseURL, apiKey, dir string, o []arr.Option) ([]string, error) {
	c, err := radarr.New(baseURL, apiKey, o...)
	if err != nil {
		return nil, fmt.Errorf("radarr filesystem client: %w", err)
	}
	r, err := c.BrowseFileSystem(ctx, dir, true)
	if err != nil {
		return nil, fmt.Errorf("radarr filesystem %q: %w", dir, err)
	}
	names, err := radarrNames(r)
	if err != nil {
		return nil, fmt.Errorf("radarr filesystem %q response: %w", dir, err)
	}
	return names, nil
}

func lidarrFolder(ctx context.Context, baseURL, apiKey, dir string, o []arr.Option) ([]string, error) {
	c, err := lidarr.New(baseURL, apiKey, o...)
	if err != nil {
		return nil, fmt.Errorf("lidarr filesystem client: %w", err)
	}
	r, err := c.BrowseFileSystem(ctx, dir, true)
	if err != nil {
		return nil, fmt.Errorf("lidarr filesystem %q: %w", dir, err)
	}
	names, err := lidarrNames(r)
	if err != nil {
		return nil, fmt.Errorf("lidarr filesystem %q response: %w", dir, err)
	}
	return names, nil
}

func readarrFolder(ctx context.Context, baseURL, apiKey, dir string, o []arr.Option) ([]string, error) {
	c, err := readarr.New(baseURL, apiKey, o...)
	if err != nil {
		return nil, fmt.Errorf("readarr filesystem client: %w", err)
	}
	r, err := c.BrowseFileSystem(ctx, dir)
	if err != nil {
		return nil, fmt.Errorf("readarr filesystem %q: %w", dir, err)
	}
	names, err := readarrNames(r)
	if err != nil {
		return nil, fmt.Errorf("readarr filesystem %q response: %w", dir, err)
	}
	return names, nil
}

func whisparrV2Folder(ctx context.Context, baseURL, apiKey, dir string, o []arr.Option) ([]string, error) {
	c, err := whisparr.New(baseURL, apiKey, o...)
	if err != nil {
		return nil, fmt.Errorf("whisparr v2 filesystem client: %w", err)
	}
	r, err := c.BrowseFileSystem(ctx, dir)
	if err != nil {
		return nil, fmt.Errorf("whisparr v2 filesystem %q: %w", dir, err)
	}
	names, err := whisparrNames(r)
	if err != nil {
		return nil, fmt.Errorf("whisparr v2 filesystem %q response: %w", dir, err)
	}
	return names, nil
}

func whisparrV3Folder(ctx context.Context, baseURL, apiKey, dir string, o []arr.Option) ([]string, error) {
	c, err := whisparr.NewV3(baseURL, apiKey, o...)
	if err != nil {
		return nil, fmt.Errorf("whisparr v3 filesystem client: %w", err)
	}
	r, err := c.BrowseFileSystem(ctx, dir)
	if err != nil {
		return nil, fmt.Errorf("whisparr v3 filesystem %q: %w", dir, err)
	}
	names, err := whisparrNames(r)
	if err != nil {
		return nil, fmt.Errorf("whisparr v3 filesystem %q response: %w", dir, err)
	}
	return names, nil
}

func sonarrNames(r *sonarr.FileSystemResource) ([]string, error) {
	if r == nil {
		return nil, nil
	}
	return entryNames(r.Directories, r.Files, func(entry sonarr.FileSystemEntry) string { return entry.Name })
}

func radarrNames(r *radarr.FileSystemResource) ([]string, error) {
	if r == nil {
		return nil, nil
	}
	return entryNames(r.Directories, r.Files, func(entry radarr.FileSystemEntry) string { return entry.Name })
}

func lidarrNames(r *lidarr.FileSystemResource) ([]string, error) {
	if r == nil {
		return nil, nil
	}
	return entryNames(r.Directories, r.Files, func(entry lidarr.FileSystemEntry) string { return entry.Name })
}

func readarrNames(r *readarr.FileSystemResource) ([]string, error) {
	if r == nil {
		return nil, nil
	}
	return entryNames(r.Directories, nil, func(entry readarr.FileSystemEntry) string { return entry.Name })
}

func entryNames[E any](dirs, files []E, name func(E) string) ([]string, error) {
	if err := checkFolderEntryCount(len(dirs) + len(files)); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(dirs)+len(files))
	for _, entry := range dirs {
		names = append(names, name(entry))
	}
	for _, entry := range files {
		names = append(names, name(entry))
	}
	return names, nil
}

func whisparrNames(raw map[string]any) ([]string, error) {
	entryCount := 0
	for _, key := range []string{"directories", "files"} {
		entries, ok := raw[key].([]any)
		if !ok {
			return nil, fmt.Errorf("invalid filesystem %s entries", key)
		}
		entryCount += len(entries)
	}
	if err := checkFolderEntryCount(entryCount); err != nil {
		return nil, err
	}
	names := make([]string, 0)
	for _, key := range []string{"directories", "files"} {
		entries, ok := raw[key].([]any)
		if !ok {
			return nil, fmt.Errorf("invalid filesystem %s entries", key)
		}
		for _, entry := range entries {
			value, ok := entry.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("invalid filesystem %s entry", key)
			}
			name, ok := value["name"].(string)
			if !ok {
				return nil, fmt.Errorf("invalid filesystem %s name", key)
			}
			names = append(names, name)
		}
	}
	return names, nil
}

func checkFolderEntryCount(count int) error {
	if count > maxFolderEntries {
		return fmt.Errorf("filesystem has %d entries; maximum is %d", count, maxFolderEntries)
	}
	return nil
}
