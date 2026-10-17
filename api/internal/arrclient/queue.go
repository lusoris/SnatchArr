// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package arrclient

import (
	"context"
	"fmt"
	"time"

	"github.com/golusoris/goenvoy/arr/lidarr"
	"github.com/golusoris/goenvoy/arr/radarr"
	"github.com/golusoris/goenvoy/arr/readarr"
	"github.com/golusoris/goenvoy/arr/sonarr"
	"github.com/golusoris/goenvoy/arr/v2"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/queue"
)

// Defaults applied when the caller passes a non-positive page bound.
const (
	defaultQueuePages    = 10
	defaultQueuePageSize = 200
)

// queueOps binds one *arr client to the two queue calls the sweep needs.
// One constructor per kind keeps the client types concrete; the page loop
// and the record mapping are shared. Non-generic on purpose: govulncheck
// panics on generic helpers (see probe.go).
type queueOps struct {
	// page fetches one queue page and returns the mapped items, the raw
	// record count of the page and the total record count of the queue.
	page   func(ctx context.Context, page, size int) (items []queue.Item, n, total int, err error)
	remove func(ctx context.Context, id int, blocklist bool) error
}

// Queue fetches up to maxPages of queue items from an *arr instance using pageSize items per page.
func (p *GoenvoyProber) Queue(ctx context.Context, kind domain.AppKind, baseURL, apiKey string, maxPages, pageSize int) ([]queue.Item, error) {
	ops, err := p.queueOps(kind, baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	if maxPages <= 0 {
		maxPages = defaultQueuePages
	}
	if pageSize <= 0 {
		pageSize = defaultQueuePageSize
	}
	var items []queue.Item
	seen := 0
	for page := 1; page <= maxPages; page++ {
		got, n, total, err := p.queuePage(ctx, ops, page, pageSize)
		if err != nil {
			return nil, fmt.Errorf("%w: %s queue page %d: %w", domain.ErrUnreachable, kind, page, err)
		}
		items = append(items, got...)
		seen += n
		if n == 0 || seen >= total {
			break
		}
	}
	return items, nil
}

// RemoveQueueItem deletes a single item from the *arr queue, removing it from client and skipping redownload.
func (p *GoenvoyProber) RemoveQueueItem(ctx context.Context, kind domain.AppKind, baseURL, apiKey string, id int32, blocklist bool) error {
	ops, err := p.queueOps(kind, baseURL, apiKey)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, p.opts.Timeout)
	defer cancel()
	if err := ops.remove(ctx, int(id), blocklist); err != nil {
		return fmt.Errorf("arrclient: %s delete queue item %d: %w", kind, id, err)
	}
	return nil
}

// queuePage gives each page request its own client timeout.
func (p *GoenvoyProber) queuePage(ctx context.Context, ops queueOps, page, size int) ([]queue.Item, int, int, error) {
	ctx, cancel := context.WithTimeout(ctx, p.opts.Timeout)
	defer cancel()
	return ops.page(ctx, page, size)
}

func (p *GoenvoyProber) queueOps(kind domain.AppKind, baseURL, apiKey string) (queueOps, error) {
	o := p.clientOptions()
	switch kind {
	case domain.KindSonarr:
		return sonarrQueue(baseURL, apiKey, o)
	case domain.KindRadarr:
		return radarrQueue(baseURL, apiKey, o)
	case domain.KindLidarr:
		return lidarrQueue(baseURL, apiKey, o)
	case domain.KindReadarr:
		return readarrQueue(baseURL, apiKey, o)
	case domain.KindWhisparrV2, domain.KindWhisparrV3:
		// goenvoy arr/whisparr has no queue options, score or skipRedownload.
		return queueOps{}, queue.ErrQueueUnsupported
	default:
		return queueOps{}, fmt.Errorf("%w: unsupported kind %q", domain.ErrInvalid, kind)
	}
}

func sonarrQueue(baseURL, apiKey string, o []arr.Option) (queueOps, error) {
	c, err := sonarr.New(baseURL, apiKey, o...)
	if err != nil {
		return queueOps{}, fmt.Errorf("arrclient: sonarr: %w", err)
	}
	page := func(ctx context.Context, page, size int) ([]queue.Item, int, int, error) {
		resp, err := c.GetQueueWithOptions(ctx, sonarr.QueueOptions{Page: page, PageSize: size, IncludeItem: true})
		if err != nil {
			return nil, 0, 0, err
		}
		items := make([]queue.Item, 0, len(resp.Records))
		for _, r := range resp.Records {
			items = appendItem(items, r.QueueRecord, r.CustomFormatScore, r.Episode == nil || r.Episode.Monitored)
		}
		return items, len(resp.Records), resp.TotalRecords, nil
	}
	remove := func(ctx context.Context, id int, blocklist bool) error {
		return c.DeleteQueueItemWithOptions(ctx, id, sonarr.QueueDeleteOptions{RemoveFromClient: true, SkipRedownload: true, Blocklist: blocklist})
	}
	return queueOps{page: page, remove: remove}, nil
}

func radarrQueue(baseURL, apiKey string, o []arr.Option) (queueOps, error) {
	c, err := radarr.New(baseURL, apiKey, o...)
	if err != nil {
		return queueOps{}, fmt.Errorf("arrclient: radarr: %w", err)
	}
	page := func(ctx context.Context, page, size int) ([]queue.Item, int, int, error) {
		resp, err := c.GetQueueWithOptions(ctx, radarr.QueueOptions{Page: page, PageSize: size, IncludeItem: true})
		if err != nil {
			return nil, 0, 0, err
		}
		items := make([]queue.Item, 0, len(resp.Records))
		for _, r := range resp.Records {
			items = appendItem(items, r.QueueRecord, r.CustomFormatScore, r.Movie == nil || r.Movie.Monitored)
		}
		return items, len(resp.Records), resp.TotalRecords, nil
	}
	remove := func(ctx context.Context, id int, blocklist bool) error {
		return c.DeleteQueueItemWithOptions(ctx, id, radarr.QueueDeleteOptions{RemoveFromClient: true, SkipRedownload: true, Blocklist: blocklist})
	}
	return queueOps{page: page, remove: remove}, nil
}

func lidarrQueue(baseURL, apiKey string, o []arr.Option) (queueOps, error) {
	c, err := lidarr.New(baseURL, apiKey, o...)
	if err != nil {
		return queueOps{}, fmt.Errorf("arrclient: lidarr: %w", err)
	}
	page := func(ctx context.Context, page, size int) ([]queue.Item, int, int, error) {
		resp, err := c.GetQueueWithOptions(ctx, lidarr.QueueOptions{Page: page, PageSize: size, IncludeItem: true})
		if err != nil {
			return nil, 0, 0, err
		}
		items := make([]queue.Item, 0, len(resp.Records))
		for _, r := range resp.Records {
			items = appendItem(items, r.QueueRecord, r.CustomFormatScore, r.Album == nil || r.Album.Monitored)
		}
		return items, len(resp.Records), resp.TotalRecords, nil
	}
	remove := func(ctx context.Context, id int, blocklist bool) error {
		return c.DeleteQueueItemWithOptions(ctx, id, lidarr.QueueDeleteOptions{RemoveFromClient: true, SkipRedownload: true, Blocklist: blocklist})
	}
	return queueOps{page: page, remove: remove}, nil
}

func readarrQueue(baseURL, apiKey string, o []arr.Option) (queueOps, error) {
	c, err := readarr.New(baseURL, apiKey, o...)
	if err != nil {
		return queueOps{}, fmt.Errorf("arrclient: readarr: %w", err)
	}
	page := func(ctx context.Context, page, size int) ([]queue.Item, int, int, error) {
		resp, err := c.GetQueueWithOptions(ctx, readarr.QueueOptions{Page: page, PageSize: size, IncludeItem: true})
		if err != nil {
			return nil, 0, 0, err
		}
		items := make([]queue.Item, 0, len(resp.Records))
		for _, r := range resp.Records {
			items = appendItem(items, r.QueueRecord, r.CustomFormatScore, r.Book == nil || r.Book.Monitored)
		}
		return items, len(resp.Records), resp.TotalRecords, nil
	}
	remove := func(ctx context.Context, id int, blocklist bool) error {
		return c.DeleteQueueItemWithOptions(ctx, id, readarr.QueueDeleteOptions{RemoveFromClient: true, SkipRedownload: true, Blocklist: blocklist})
	}
	return queueOps{page: page, remove: remove}, nil
}

// appendItem maps one record; a record whose added time does not parse is
// dropped. A missing nested item counts as monitored: never remove on
// missing data.
func appendItem(items []queue.Item, r arr.QueueRecord, score int, monitored bool) []queue.Item {
	added, err := time.Parse(time.RFC3339, r.Added)
	if err != nil {
		return items
	}
	return append(items, queue.Item{
		ID:                    int32(r.ID), // #nosec G115 -- *arr ids are small integers
		DownloadID:            r.DownloadID,
		Added:                 added,
		Status:                r.Status,
		TrackedDownloadStatus: r.TrackedDownloadStatus,
		TrackedDownloadState:  r.TrackedDownloadState,
		SizeLeft:              int64(max(r.SizeLeft, 0)),
		CustomFormatScore:     int32(score), // #nosec G115 -- score fits in int32
		Monitored:             monitored,
	})
}
