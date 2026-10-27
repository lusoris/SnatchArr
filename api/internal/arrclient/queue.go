// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package arrclient

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golusoris/goenvoy/arr/lidarr"
	"github.com/golusoris/goenvoy/arr/radarr"
	"github.com/golusoris/goenvoy/arr/readarr"
	"github.com/golusoris/goenvoy/arr/sonarr"
	"github.com/golusoris/goenvoy/arr/v2"
	"github.com/golusoris/goenvoy/arr/whisparr"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/queue"
)

// Defaults applied when the caller passes a non-positive page bound.
const (
	defaultQueuePages    = 10
	defaultQueuePageSize = 200
	// maxFileLookups bounds the file-score requests of one queue read; items
	// past the bound keep an unknown existing score and get no strike.
	maxFileLookups = 50
)

// queueOps binds one *arr client to the two queue calls the sweep needs.
// One constructor per kind keeps the client types concrete; the page loop
// and the record mapping are shared. Non-generic on purpose: govulncheck
// panics on generic helpers (see probe.go).
type queueOps struct {
	// page fetches one queue page and returns the mapped items, the raw
	// record count of the page and the total record count of the queue.
	page   func(ctx context.Context, page, size int) (items []mappedItem, n, total int, err error)
	remove func(ctx context.Context, id int, blocklist, research bool) error
	// fileScore reads the custom-format score of the file on disk; nil for
	// kinds without a single file per queue item (Lidarr, Readarr).
	fileScore func(ctx context.Context, fileID int) (*int32, error)
}

// errNoFileScore marks a file read that returned no custom-format score.
var errNoFileScore = errors.New("file has no custom-format score")

// mappedItem is a queue item plus the id of the file already on disk for
// its media item; fileID 0 means no file or a score already known.
type mappedItem struct {
	item   queue.Item
	fileID int
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
	var mapped []mappedItem
	seen := 0
	for page := 1; page <= maxPages; page++ {
		got, n, total, err := p.queuePage(ctx, ops, page, pageSize)
		if err != nil {
			return nil, fmt.Errorf("%w: %s queue page %d: %w", domain.ErrUnreachable, kind, page, err)
		}
		mapped = append(mapped, got...)
		seen += n
		if n == 0 || seen >= total {
			break
		}
	}
	return p.withFileScores(ctx, ops, mapped), nil
}

// withFileScores fills ExistingScore from at most maxFileLookups file reads,
// one client timeout each, cached per file. A failed or skipped read leaves
// the score unknown, so the not-an-upgrade rule never acts on missing data.
func (p *GoenvoyProber) withFileScores(ctx context.Context, ops queueOps, mapped []mappedItem) []queue.Item {
	items := make([]queue.Item, 0, len(mapped))
	scores := make(map[int]*int32)
	for _, m := range mapped {
		if m.fileID > 0 && ops.fileScore != nil {
			score, ok := scores[m.fileID]
			if !ok && len(scores) < maxFileLookups {
				score = p.fileScore(ctx, ops, m.fileID)
				scores[m.fileID] = score
			}
			m.item.ExistingScore = score
		}
		items = append(items, m.item)
	}
	return items
}

func (p *GoenvoyProber) fileScore(ctx context.Context, ops queueOps, fileID int) *int32 {
	ctx, cancel := context.WithTimeout(ctx, p.opts.Timeout)
	defer cancel()
	score, err := ops.fileScore(ctx, fileID)
	if err != nil {
		return nil
	}
	return score
}

// RemoveQueueItem deletes a single item from the *arr queue, removing it from client and setting skipRedownload = !research.
func (p *GoenvoyProber) RemoveQueueItem(ctx context.Context, kind domain.AppKind, baseURL, apiKey string, id int32, blocklist, research bool) error {
	ops, err := p.queueOps(kind, baseURL, apiKey)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, p.opts.Timeout)
	defer cancel()
	if err := ops.remove(ctx, int(id), blocklist, research); err != nil {
		return fmt.Errorf("arrclient: %s delete queue item %d: %w", kind, id, err)
	}
	return nil
}

// queuePage gives each page request its own client timeout.
func (p *GoenvoyProber) queuePage(ctx context.Context, ops queueOps, page, size int) ([]mappedItem, int, int, error) {
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
	case domain.KindWhisparrV2:
		return whisparrQueue(baseURL, apiKey, o)
	case domain.KindWhisparrV3:
		return whisparrV3Queue(baseURL, apiKey, o)
	case domain.KindStash:
		return queueOps{}, fmt.Errorf("%w: unsupported kind %q", domain.ErrInvalid, kind)
	default:
		return queueOps{}, fmt.Errorf("%w: unsupported kind %q", domain.ErrInvalid, kind)
	}
}

func sonarrQueue(baseURL, apiKey string, o []arr.Option) (queueOps, error) {
	c, err := sonarr.New(baseURL, apiKey, o...)
	if err != nil {
		return queueOps{}, fmt.Errorf("arrclient: sonarr: %w", err)
	}
	page := func(ctx context.Context, page, size int) ([]mappedItem, int, int, error) {
		resp, err := c.GetQueueWithOptions(ctx, sonarr.QueueOptions{Page: page, PageSize: size, IncludeItem: true})
		if err != nil {
			return nil, 0, 0, fmt.Errorf("sonarr queue: %w", err)
		}
		items := make([]mappedItem, 0, len(resp.Records))
		for _, r := range resp.Records {
			items = appendItem(items, r.QueueRecord, recordFacts{score: r.CustomFormatScore, monitored: r.Episode == nil || r.Episode.Monitored, fileID: sonarrFileID(r), mediaID: r.EpisodeID})
		}
		return items, len(resp.Records), resp.TotalRecords, nil
	}
	remove := func(ctx context.Context, id int, blocklist, research bool) error {
		return c.DeleteQueueItemWithOptions(ctx, id, sonarr.QueueDeleteOptions{RemoveFromClient: true, SkipRedownload: !research, Blocklist: blocklist})
	}
	fileScore := func(ctx context.Context, id int) (*int32, error) {
		f, err := c.GetEpisodeFile(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("sonarr episode file %d: %w", id, err)
		}
		return new(int32(f.CustomFormatScore)), nil // #nosec G115 -- score fits in int32
	}
	return queueOps{page: page, remove: remove, fileScore: fileScore}, nil
}

func radarrQueue(baseURL, apiKey string, o []arr.Option) (queueOps, error) {
	c, err := radarr.New(baseURL, apiKey, o...)
	if err != nil {
		return queueOps{}, fmt.Errorf("arrclient: radarr: %w", err)
	}
	page := func(ctx context.Context, page, size int) ([]mappedItem, int, int, error) {
		resp, err := c.GetQueueWithOptions(ctx, radarr.QueueOptions{Page: page, PageSize: size, IncludeItem: true})
		if err != nil {
			return nil, 0, 0, fmt.Errorf("radarr queue: %w", err)
		}
		items := make([]mappedItem, 0, len(resp.Records))
		for _, r := range resp.Records {
			items = appendItem(items, r.QueueRecord, recordFacts{score: r.CustomFormatScore, monitored: r.Movie == nil || r.Movie.Monitored, fileID: radarrFileID(r), mediaID: r.MovieID})
		}
		return items, len(resp.Records), resp.TotalRecords, nil
	}
	remove := func(ctx context.Context, id int, blocklist, research bool) error {
		return c.DeleteQueueItemWithOptions(ctx, id, radarr.QueueDeleteOptions{RemoveFromClient: true, SkipRedownload: !research, Blocklist: blocklist})
	}
	fileScore := func(ctx context.Context, id int) (*int32, error) {
		f, err := c.GetMovieFile(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("radarr movie file %d: %w", id, err)
		}
		if f.CustomFormatScore == nil {
			return nil, fmt.Errorf("radarr movie file %d: %w", id, errNoFileScore)
		}
		return new(int32(*f.CustomFormatScore)), nil // #nosec G115 -- score fits in int32
	}
	return queueOps{page: page, remove: remove, fileScore: fileScore}, nil
}

func lidarrQueue(baseURL, apiKey string, o []arr.Option) (queueOps, error) {
	c, err := lidarr.New(baseURL, apiKey, o...)
	if err != nil {
		return queueOps{}, fmt.Errorf("arrclient: lidarr: %w", err)
	}
	page := func(ctx context.Context, page, size int) ([]mappedItem, int, int, error) {
		resp, err := c.GetQueueWithOptions(ctx, lidarr.QueueOptions{Page: page, PageSize: size, IncludeItem: true})
		if err != nil {
			return nil, 0, 0, fmt.Errorf("lidarr queue: %w", err)
		}
		items := make([]mappedItem, 0, len(resp.Records))
		for _, r := range resp.Records {
			items = appendItem(items, r.QueueRecord, recordFacts{score: r.CustomFormatScore, monitored: r.Album == nil || r.Album.Monitored, mediaID: r.AlbumID})
		}
		return items, len(resp.Records), resp.TotalRecords, nil
	}
	remove := func(ctx context.Context, id int, blocklist, research bool) error {
		return c.DeleteQueueItemWithOptions(ctx, id, lidarr.QueueDeleteOptions{RemoveFromClient: true, SkipRedownload: !research, Blocklist: blocklist})
	}
	return queueOps{page: page, remove: remove}, nil
}

func readarrQueue(baseURL, apiKey string, o []arr.Option) (queueOps, error) {
	c, err := readarr.New(baseURL, apiKey, o...)
	if err != nil {
		return queueOps{}, fmt.Errorf("arrclient: readarr: %w", err)
	}
	page := func(ctx context.Context, page, size int) ([]mappedItem, int, int, error) {
		resp, err := c.GetQueueWithOptions(ctx, readarr.QueueOptions{Page: page, PageSize: size, IncludeItem: true})
		if err != nil {
			return nil, 0, 0, fmt.Errorf("readarr queue: %w", err)
		}
		items := make([]mappedItem, 0, len(resp.Records))
		for _, r := range resp.Records {
			items = appendItem(items, r.QueueRecord, recordFacts{score: r.CustomFormatScore, monitored: r.Book == nil || r.Book.Monitored, mediaID: r.BookID})
		}
		return items, len(resp.Records), resp.TotalRecords, nil
	}
	remove := func(ctx context.Context, id int, blocklist, research bool) error {
		return c.DeleteQueueItemWithOptions(ctx, id, readarr.QueueDeleteOptions{RemoveFromClient: true, SkipRedownload: !research, Blocklist: blocklist})
	}
	return queueOps{page: page, remove: remove}, nil
}

func whisparrQueue(baseURL, apiKey string, o []arr.Option) (queueOps, error) {
	c, err := whisparr.New(baseURL, apiKey, o...)
	if err != nil {
		return queueOps{}, fmt.Errorf("arrclient: whisparr: %w", err)
	}
	page := func(ctx context.Context, page, size int) ([]mappedItem, int, int, error) {
		resp, err := c.GetQueueWithOptions(ctx, whisparr.QueueOptions{Page: page, PageSize: size, IncludeItem: true})
		if err != nil {
			return nil, 0, 0, fmt.Errorf("whisparr queue: %w", err)
		}
		items := make([]mappedItem, 0, len(resp.Records))
		for _, r := range resp.Records {
			items = appendItem(items, r.QueueRecord, recordFacts{score: r.CustomFormatScore, monitored: r.Episode == nil || r.Episode.Monitored, fileID: whisparrV2FileID(r), mediaID: r.EpisodeID})
		}
		return items, len(resp.Records), resp.TotalRecords, nil
	}
	remove := func(ctx context.Context, id int, blocklist, research bool) error {
		return c.DeleteQueueItemWithOptions(ctx, id, whisparr.QueueDeleteOptions{RemoveFromClient: true, SkipRedownload: !research, Blocklist: blocklist})
	}
	fileScore := func(ctx context.Context, id int) (*int32, error) {
		return nil, fmt.Errorf("whisparr episode file %d: %w", id, errNoFileScore)
	}
	return queueOps{page: page, remove: remove, fileScore: fileScore}, nil
}

func whisparrV3Queue(baseURL, apiKey string, o []arr.Option) (queueOps, error) {
	c, err := whisparr.NewV3(baseURL, apiKey, o...)
	if err != nil {
		return queueOps{}, fmt.Errorf("arrclient: whisparr v3: %w", err)
	}
	page := func(ctx context.Context, page, size int) ([]mappedItem, int, int, error) {
		resp, err := c.GetQueueWithOptions(ctx, whisparr.QueueOptions{Page: page, PageSize: size, IncludeItem: true})
		if err != nil {
			return nil, 0, 0, fmt.Errorf("whisparr v3 queue: %w", err)
		}
		items := make([]mappedItem, 0, len(resp.Records))
		for _, r := range resp.Records {
			items = appendItem(items, r.QueueRecord, recordFacts{score: r.CustomFormatScore, monitored: r.Movie == nil || r.Movie.Monitored, fileID: whisparrV3FileID(r), mediaID: r.MovieID})
		}
		return items, len(resp.Records), resp.TotalRecords, nil
	}
	remove := func(ctx context.Context, id int, blocklist, research bool) error {
		return c.DeleteQueueItemWithOptions(ctx, id, whisparr.QueueDeleteOptions{RemoveFromClient: true, SkipRedownload: !research, Blocklist: blocklist})
	}
	fileScore := func(ctx context.Context, id int) (*int32, error) {
		return nil, fmt.Errorf("whisparr movie file %d: %w", id, errNoFileScore)
	}
	return queueOps{page: page, remove: remove, fileScore: fileScore}, nil
}

// recordFacts carries what each *arr kind adds to the shared queue record.
type recordFacts struct {
	score     int
	monitored bool
	fileID    int
	mediaID   int
}

// appendItem maps one record; a record whose added time does not parse is
// dropped. A missing nested item counts as monitored: never remove on
// missing data.
func appendItem(items []mappedItem, r arr.QueueRecord, x recordFacts) []mappedItem {
	added, err := time.Parse(time.RFC3339, r.Added)
	if err != nil {
		return items
	}
	return append(items, mappedItem{fileID: x.fileID, item: queue.Item{
		ID:                    int32(r.ID), // #nosec G115 -- *arr ids are small integers
		DownloadID:            r.DownloadID,
		Added:                 added,
		Status:                r.Status,
		TrackedDownloadStatus: r.TrackedDownloadStatus,
		TrackedDownloadState:  r.TrackedDownloadState,
		SizeLeft:              int64(max(r.SizeLeft, 0)),
		CustomFormatScore:     int32(x.score),   // #nosec G115 -- score fits in int32
		MediaID:               int32(x.mediaID), // #nosec G115 -- *arr ids are small integers
		Monitored:             x.monitored,
		Title:                 r.Title,
		StatusLines:           statusLines(r.StatusMessages),
		ReasonLines:           reasonLines(r.StatusMessages),
		Protocol:              r.Protocol,
		Size:                  int64(max(r.Size, 0)),
	}})
}

// sonarrFileID returns the episode file on disk for the queue item's episode.
func sonarrFileID(r sonarr.QueueRecord) int {
	if r.Episode == nil {
		return 0
	}
	return r.Episode.EpisodeFileID
}

// radarrFileID returns the movie file on disk. The queue embeds the movie
// without custom formats, so its score is read from /moviefile.
func radarrFileID(r radarr.QueueRecord) int {
	if r.Movie == nil {
		return 0
	}
	return r.Movie.MovieFileID
}

// whisparrV2FileID returns the episode file on disk for the queue item's episode,
// or 0 if the record carries no episode or no episode file id.
func whisparrV2FileID(r whisparr.QueueRecord) int {
	if r.Episode == nil {
		return 0
	}
	return r.Episode.EpisodeFileID
}

// whisparrV3FileID returns the movie file on disk for the queue item's movie,
// or 0 if the record carries no movie or no movie file id.
func whisparrV3FileID(r whisparr.QueueRecord) int {
	if r.Movie == nil {
		return 0
	}
	return r.Movie.MovieFileID
}

// statusLines flattens status messages in the order the *arr returns them,
// dropping empty strings.
func statusLines(msgs []arr.StatusMessage) []string {
	var lines []string
	for _, m := range msgs {
		if m.Title != "" {
			lines = append(lines, m.Title)
		}
		for _, l := range m.Messages {
			if l != "" {
				lines = append(lines, l)
			}
		}
	}
	return lines
}

// reasonLines flattens rejection reasons in the order the *arr returns them,
// dropping empty strings: each message's messages, its title only when it
// carries none (title over messages names the release or file).
func reasonLines(msgs []arr.StatusMessage) []string {
	var lines []string
	for _, m := range msgs {
		if len(m.Messages) == 0 {
			if m.Title != "" {
				lines = append(lines, m.Title)
			}
			continue
		}
		for _, l := range m.Messages {
			if l != "" {
				lines = append(lines, l)
			}
		}
	}
	return lines
}
