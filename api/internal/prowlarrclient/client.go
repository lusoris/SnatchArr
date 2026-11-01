// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package prowlarrclient interacts with a Prowlarr server via goenvoy typed
// client calls (X-Api-Key auth). Grab posts a chosen release to /api/v1/search
// so Prowlarr hands it to its own download client; SnatchArr adds no download
// itself (ADR-0006 amendment, ledger snatcharr-oq-0036-14-grab-path).
package prowlarrclient

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	prowlarr "github.com/golusoris/goenvoy/arr/prowlarr/v2"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// Status is what /api/v1/system/status reports.
type Status struct {
	Version string
}

// Indexer is one indexer configured in Prowlarr.
type Indexer struct {
	ID         int
	Name       string
	Enabled    bool
	QueryLimit *int
	GrabLimit  *int
	LimitsUnit int // 0=day, 1=hour
}

// Usage counts queries and grabs over a time window.
type Usage struct {
	Queries int
	Grabs   int
}

// SearchQuery configures parameters for searching releases in Prowlarr.
type SearchQuery struct {
	Query      string
	Type       string
	IndexerIDs []int
	Categories []int
	Limit      int
	Offset     int
}

// Release is a media release indexed by Prowlarr.
type Release struct {
	GUID        string
	IndexerID   int
	Title       string
	Size        int64
	Indexer     string
	DownloadURL string
	InfoHash    string
	Categories  []int
}

// Client is the subset of the Prowlarr API SnatchArr reads and dispatches.
type Client interface {
	Status(ctx context.Context, baseURL, apiKey string) (Status, error)
	Indexers(ctx context.Context, baseURL, apiKey string) ([]Indexer, error)
	Usage(ctx context.Context, baseURL, apiKey string, start, end time.Time) (map[int]Usage, error)
	Statuses(ctx context.Context, baseURL, apiKey string) (map[int]time.Time, error)
	ProwlarrURLs(ctx context.Context, baseURL, apiKey string) ([]string, error)
	Search(ctx context.Context, baseURL, apiKey string, q SearchQuery) ([]Release, error)
	Grab(ctx context.Context, baseURL, apiKey string, r Release) error
}

// HTTP implements Client over goenvoy's base client (X-Api-Key convention).
type HTTP struct {
	arrclient.BaseFactory
}

// New builds the client.
func New(timeout time.Duration, userAgent string) *HTTP {
	return &HTTP{Timeout: timeout, UserAgent: userAgent}
}

// Status implements Client.
func (h *HTTP) Status(ctx context.Context, baseURL, apiKey string) (Status, error) {
	c, err := h.Base(baseURL, apiKey)
	if err != nil {
		return Status{}, fmt.Errorf("prowlarr: client: %w", err)
	}
	var out Status
	if err := c.Get(ctx, "/api/v1/system/status", &out); err != nil {
		return Status{}, fmt.Errorf("%w: prowlarr status: %w", domain.ErrUnreachable, arrclient.HintAPIError(err, "Prowlarr"))
	}
	return out, nil
}

type indexerFieldJSON struct {
	Name  string `json:"name"`
	Value any    `json:"value"`
}

type indexerJSON struct {
	ID     int                `json:"id"`
	Name   string             `json:"name"`
	Enable *bool              `json:"enable"`
	Fields []indexerFieldJSON `json:"fields"`
}

// Indexers implements Client.
func (h *HTTP) Indexers(ctx context.Context, baseURL, apiKey string) ([]Indexer, error) {
	c, err := h.Base(baseURL, apiKey)
	if err != nil {
		return nil, fmt.Errorf("prowlarr: client: %w", err)
	}
	var list []indexerJSON
	if err := c.Get(ctx, "/api/v1/indexer", &list); err != nil {
		return nil, fmt.Errorf("%w: prowlarr indexers: %w", domain.ErrUnreachable, arrclient.HintAPIError(err, "Prowlarr"))
	}
	out := make([]Indexer, 0, len(list))
	for _, idx := range list {
		out = append(out, parseIndexer(idx))
	}
	return out, nil
}

func parseIndexer(idx indexerJSON) Indexer {
	enabled := true
	if idx.Enable != nil {
		enabled = *idx.Enable
	}
	item := Indexer{ID: idx.ID, Name: idx.Name, Enabled: enabled}
	for _, f := range idx.Fields {
		switch f.Name {
		case "baseSettings.queryLimit":
			if val, ok := parseJSONInt(f.Value); ok {
				item.QueryLimit = &val
			}
		case "baseSettings.grabLimit":
			if val, ok := parseJSONInt(f.Value); ok {
				item.GrabLimit = &val
			}
		case "baseSettings.limitsUnit":
			if val, ok := parseJSONInt(f.Value); ok {
				item.LimitsUnit = val
			}
		}
	}
	return item
}

func parseJSONInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	case string:
		i, err := strconv.Atoi(n)
		return i, err == nil
	default:
		return 0, false
	}
}

// typed constructs a goenvoy Prowlarr client configured with h's HTTP options,
// returning a wrapped error when baseURL or client options are invalid.
func (h *HTTP) typed(baseURL, apiKey string) (*prowlarr.Client, error) {
	pc, err := prowlarr.New(baseURL, apiKey, h.Options()...)
	if err != nil {
		return nil, fmt.Errorf("prowlarr: client: %w", err)
	}
	return pc, nil
}

// Usage fetches indexer query and grab statistics between start and end in UTC.
// It returns an error wrapping domain.ErrUnreachable on transport or API failures
// and domain.ErrInvalid when the response omits the indexers list.
func (h *HTTP) Usage(ctx context.Context, baseURL, apiKey string, start, end time.Time) (map[int]Usage, error) {
	pc, err := h.typed(baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	stats, err := pc.GetIndexerStats(ctx, start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("%w: prowlarr stats: %w", domain.ErrUnreachable, arrclient.HintAPIError(err, "Prowlarr"))
	}
	if stats.Indexers == nil {
		return nil, fmt.Errorf("%w: prowlarr stats payload missing indexers field", domain.ErrInvalid)
	}
	return aggregateStats(stats.Indexers), nil
}

// aggregateStats sums standard queries, RSS queries, and grabs per positive
// indexer ID, skipping entries whose IndexerID is zero or negative.
func aggregateStats(items []prowlarr.IndexerStatistic) map[int]Usage {
	out := make(map[int]Usage, len(items))
	for _, it := range items {
		if it.IndexerID <= 0 {
			continue
		}
		u := out[it.IndexerID]
		u.Queries += it.NumberOfQueries + it.NumberOfRssQueries
		u.Grabs += it.NumberOfGrabs
		out[it.IndexerID] = u
	}
	return out
}

// Statuses fetches active indexer backoff timestamps keyed by indexer ID.
// It returns an error wrapping domain.ErrUnreachable on transport or API failures
// and domain.ErrInvalid if the response is null or contains a malformed timestamp.
func (h *HTTP) Statuses(ctx context.Context, baseURL, apiKey string) (map[int]time.Time, error) {
	pc, err := h.typed(baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	items, err := pc.GetIndexerStatuses(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: prowlarr indexerstatus: %w", domain.ErrUnreachable, arrclient.HintAPIError(err, "Prowlarr"))
	}
	if items == nil {
		return nil, fmt.Errorf("%w: prowlarr indexerstatus: response is null", domain.ErrInvalid)
	}
	return collectDisabledStatuses(items)
}

// collectDisabledStatuses parses non-empty DisabledTill timestamps as UTC and
// maps them by positive IndexerID, returning an error wrapping domain.ErrInvalid on parse failure.
func collectDisabledStatuses(items []prowlarr.IndexerStatus) (map[int]time.Time, error) {
	out := make(map[int]time.Time, len(items))
	for _, it := range items {
		if it.DisabledTill == "" {
			continue
		}
		t, parseErr := time.Parse(time.RFC3339Nano, it.DisabledTill)
		if parseErr != nil {
			return nil, fmt.Errorf("%w: prowlarr indexerstatus: indexer %d: invalid disabledTill %q: %w", domain.ErrInvalid, it.IndexerID, it.DisabledTill, parseErr)
		}
		if it.IndexerID <= 0 {
			continue
		}
		out[it.IndexerID] = t.UTC()
	}
	return out, nil
}

const maxApplications = 100

// extractAppProwlarrURL returns the trimmed string value of the application's
// prowlarrUrl field matched case-insensitively, or an empty string if absent.
func extractAppProwlarrURL(app prowlarr.Application) string {
	for _, f := range app.Fields {
		if strings.EqualFold(f.Name, "prowlarrUrl") {
			if s, ok := f.Value.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

// ProwlarrURLs reads the first maxApplications application entries and returns
// their unique, non-empty prowlarrUrl values in order. It returns an error
// wrapping domain.ErrUnreachable on client or transport failures.
func (h *HTTP) ProwlarrURLs(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	pc, err := h.typed(baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	list, err := pc.GetApplications(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: prowlarr applications: %w", domain.ErrUnreachable, arrclient.HintAPIError(err, "Prowlarr"))
	}
	if len(list) > maxApplications {
		list = list[:maxApplications]
	}
	out := make([]string, 0, len(list))
	seen := make(map[string]struct{}, len(list))
	for _, app := range list {
		u := extractAppProwlarrURL(app)
		if u == "" {
			continue
		}
		if _, exists := seen[u]; !exists {
			seen[u] = struct{}{}
			out = append(out, u)
		}
	}
	return out, nil
}

func mapReleaseCategories(cats []prowlarr.IndexerCategory) []int {
	out := make([]int, 0, len(cats))
	for _, c := range cats {
		out = append(out, c.ID)
	}
	return out
}

// Search implements Client: GET /api/v1/search.
func (h *HTTP) Search(ctx context.Context, baseURL, apiKey string, q SearchQuery) ([]Release, error) {
	pc, err := prowlarr.New(baseURL, apiKey, h.Options()...)
	if err != nil {
		return nil, fmt.Errorf("prowlarr: client: %w", err)
	}
	opts := &prowlarr.SearchOptions{
		Query:      q.Query,
		Type:       q.Type,
		IndexerIDs: q.IndexerIDs,
		Categories: q.Categories,
		Limit:      q.Limit,
		Offset:     q.Offset,
	}
	raw, err := pc.Search(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("%w: prowlarr search: %w", domain.ErrUnreachable, arrclient.HintAPIError(err, "Prowlarr"))
	}
	out := make([]Release, 0, len(raw))
	for _, r := range raw {
		out = append(out, Release{
			GUID:        r.GUID,
			IndexerID:   r.IndexerID,
			Title:       r.Title,
			Size:        r.Size,
			Indexer:     r.Indexer,
			DownloadURL: r.DownloadURL,
			InfoHash:    r.InfoHash,
			Categories:  mapReleaseCategories(r.Categories),
		})
	}
	return out, nil
}

// Grab implements Client: POST /api/v1/search.
func (h *HTTP) Grab(ctx context.Context, baseURL, apiKey string, r Release) error {
	if strings.TrimSpace(r.GUID) == "" {
		return fmt.Errorf("%w: prowlarr grab: empty release guid", domain.ErrInvalid)
	}
	if r.IndexerID <= 0 {
		return fmt.Errorf("%w: prowlarr grab: indexerId must be > 0 (got %d)", domain.ErrInvalid, r.IndexerID)
	}
	pc, err := prowlarr.New(baseURL, apiKey, h.Options()...)
	if err != nil {
		return fmt.Errorf("prowlarr: client: %w", err)
	}
	req := &prowlarr.Release{
		GUID:        r.GUID,
		IndexerID:   r.IndexerID,
		Title:       r.Title,
		Size:        r.Size,
		Indexer:     r.Indexer,
		DownloadURL: r.DownloadURL,
		InfoHash:    r.InfoHash,
	}
	if _, err := pc.GrabRelease(ctx, req); err != nil {
		return fmt.Errorf("%w: prowlarr grab: %w", domain.ErrUnreachable, arrclient.HintAPIError(err, "Prowlarr"))
	}
	return nil
}
