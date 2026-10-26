// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package prowlarrclient interacts with a Prowlarr server (X-Api-Key auth,
// /api/v1/system/status, /api/v1/indexer, /api/v1/indexerstats,
// /api/v1/indexerstatus, /api/v1/search). Grab posts a chosen release to
// /api/v1/search so Prowlarr hands it to its own download client; SnatchArr
// adds no download itself (ADR-0006 amendment, ledger
// snatcharr-oq-0036-14-grab-path).
package prowlarrclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
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

//nolint:tagliatelle // Prowlarr speaks camelCase
type indexerStatItem struct {
	IndexerID          int `json:"indexerId"`
	NumberOfQueries    int `json:"numberOfQueries"`
	NumberOfRssQueries int `json:"numberOfRssQueries"`
	NumberOfGrabs      int `json:"numberOfGrabs"`
}

type indexerStatsPayload struct {
	Indexers *[]indexerStatItem `json:"indexers"`
}

// Usage implements Client.
func (h *HTTP) Usage(ctx context.Context, baseURL, apiKey string, start, end time.Time) (map[int]Usage, error) {
	c, err := h.Base(baseURL, apiKey)
	if err != nil {
		return nil, fmt.Errorf("prowlarr: client: %w", err)
	}
	q := url.Values{}
	q.Set("startDate", start.UTC().Format(time.RFC3339))
	q.Set("endDate", end.UTC().Format(time.RFC3339))
	var raw json.RawMessage
	if getErr := c.Get(ctx, "/api/v1/indexerstats?"+q.Encode(), &raw); getErr != nil {
		return nil, fmt.Errorf("%w: prowlarr stats: %w", domain.ErrUnreachable, arrclient.HintAPIError(getErr, "Prowlarr"))
	}
	items, err := parseStatsItems(raw)
	if err != nil {
		return nil, fmt.Errorf("prowlarr: parse stats: %w", err)
	}
	return aggregateStats(items), nil
}

func parseStatsItems(raw []byte) ([]indexerStatItem, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, fmt.Errorf("%w: prowlarr stats payload must be a JSON object", domain.ErrInvalid)
	}
	var obj indexerStatsPayload
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return nil, fmt.Errorf("prowlarr: unmarshal stats payload: %w", err)
	}
	if obj.Indexers == nil {
		return nil, fmt.Errorf("%w: prowlarr stats payload missing indexers field", domain.ErrInvalid)
	}
	return *obj.Indexers, nil
}

func aggregateStats(items []indexerStatItem) map[int]Usage {
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

//nolint:tagliatelle // Prowlarr speaks camelCase
type indexerStatusItem struct {
	IndexerID    int        `json:"indexerId"`
	DisabledTill *time.Time `json:"disabledTill"`
}

// Statuses implements Client.
func (h *HTTP) Statuses(ctx context.Context, baseURL, apiKey string) (map[int]time.Time, error) {
	c, err := h.Base(baseURL, apiKey)
	if err != nil {
		return nil, fmt.Errorf("prowlarr: client: %w", err)
	}
	var raw json.RawMessage
	if getErr := c.Get(ctx, "/api/v1/indexerstatus", &raw); getErr != nil {
		return nil, fmt.Errorf("%w: prowlarr indexerstatus: %w", domain.ErrUnreachable, arrclient.HintAPIError(getErr, "Prowlarr"))
	}
	items, err := parseStatusItems(raw)
	if err != nil {
		return nil, fmt.Errorf("prowlarr: parse indexerstatus: %w", err)
	}
	out := make(map[int]time.Time, len(items))
	for _, it := range items {
		if it.IndexerID > 0 && it.DisabledTill != nil {
			out[it.IndexerID] = it.DisabledTill.UTC()
		}
	}
	return out, nil
}

func parseStatusItems(raw []byte) ([]indexerStatusItem, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, fmt.Errorf("%w: prowlarr indexerstatus payload must be a JSON array", domain.ErrInvalid)
	}
	var list []indexerStatusItem
	if err := json.Unmarshal(trimmed, &list); err != nil {
		return nil, fmt.Errorf("prowlarr: unmarshal status list: %w", err)
	}
	return list, nil
}

const maxApplications = 100

type applicationFieldJSON struct {
	Name  string `json:"name"`
	Value any    `json:"value"`
}

type applicationJSON struct {
	Fields []applicationFieldJSON `json:"fields"`
}

func extractAppProwlarrURL(app applicationJSON) string {
	for _, f := range app.Fields {
		if strings.EqualFold(f.Name, "prowlarrUrl") {
			if s, ok := f.Value.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

// ProwlarrURLs implements Client: GET /api/v1/applications.
func (h *HTTP) ProwlarrURLs(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	c, err := h.Base(baseURL, apiKey)
	if err != nil {
		return nil, fmt.Errorf("prowlarr: client: %w", err)
	}
	var list []applicationJSON
	if err := c.Get(ctx, "/api/v1/applications", &list); err != nil {
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
