// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package tvdbprovider

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	tvdb "github.com/golusoris/goenvoy/metadata/video/tvdb"

	"github.com/lusoris/SnatchArr/api/internal/metrics"
)

// errBodyTooLarge reports a TVDB response past the byte cap.
var errBodyTooLarge = errors.New("tvdb: response body exceeds byte cap")

const (
	cacheStatusHeader = "X-Httpcache-Status"
	cacheStatusNone   = "NONE"
)

// cacheMetricsTransport observes the status emitted by golusoris/httpcache.
type cacheMetricsTransport struct {
	next    http.RoundTripper
	metrics *metrics.Metrics
}

func (t *cacheMetricsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.next.RoundTrip(req)
	if err != nil {
		return nil, err //nolint:wrapcheck // preserve ratelimit.ErrLimited for provider mapping
	}
	if t.metrics != nil {
		// golusoris passes HTTP 429 through without a cache status header.
		t.metrics.MetadataCacheRequest(Source, normalizedCacheStatus(resp.Header.Get(cacheStatusHeader)))
	}
	return resp, nil
}

func normalizedCacheStatus(status string) string {
	switch status {
	case "HIT", "MISS", "STALE", "REVALIDATED", "BYPASS":
		return status
	default:
		return cacheStatusNone
	}
}

// originGuard prevents an undecodable TVDB success from becoming a long-lived cache entry.
type originGuard struct {
	next     http.RoundTripper
	basePath string
}

func (t *originGuard) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.next.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("tvdb: origin round trip: %w", err)
	}
	removeStaleWhileRevalidate(resp.Header)
	if req.Method != http.MethodGet || resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return resp, nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("tvdb: read response body: %w", err), resp.Body.Close())
	}
	if err := resp.Body.Close(); err != nil {
		return nil, fmt.Errorf("tvdb: close response body: %w", err)
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	if len(body) > 0 {
		path := relativeCachePath(req.URL.Path, t.basePath)
		if err := decodeTVDBResponse(path, body); err != nil {
			prependNoStore(resp.Header)
		}
	}
	return resp, nil
}

func removeStaleWhileRevalidate(header http.Header) {
	values := header.Values("Cache-Control")
	if len(values) == 0 {
		return
	}
	header.Del("Cache-Control")
	for _, value := range values {
		kept := make([]string, 0, 2)
		for directive := range strings.SplitSeq(value, ",") {
			directive = strings.TrimSpace(directive)
			name := strings.TrimSpace(strings.SplitN(directive, "=", 2)[0])
			if directive != "" && !strings.EqualFold(name, "stale-while-revalidate") {
				kept = append(kept, directive)
			}
		}
		if len(kept) > 0 {
			header.Add("Cache-Control", strings.Join(kept, ", "))
		}
	}
}

func prependNoStore(header http.Header) {
	values := header.Values("Cache-Control")
	header.Del("Cache-Control")
	header.Add("Cache-Control", "no-store")
	for _, value := range values {
		header.Add("Cache-Control", value)
	}
}

func relativeCachePath(path, basePath string) string {
	basePath = strings.TrimRight(basePath, "/")
	if basePath == "" || basePath == "/" {
		return path
	}
	if path == basePath {
		return "/"
	}
	if strings.HasPrefix(path, basePath+"/") {
		return strings.TrimPrefix(path, basePath)
	}
	return ""
}

type tvdbResponse[T any] struct {
	Data   T      `json:"data"`
	Status string `json:"status"`
}

func decodeTVDBResponse(path string, body []byte) error {
	switch responseKind(path) {
	case responseSeries:
		return decodeTVDBEnvelope[tvdb.SeriesBase](body)
	case responseTranslation:
		return decodeTVDBEnvelope[tvdb.Translation](body)
	case responseMovie:
		return decodeTVDBEnvelope[tvdb.MovieBase](body)
	case responseSearch:
		return decodeTVDBEnvelope[[]tvdb.SearchByRemoteIDResult](body)
	case responseUnknown:
		return errors.New("tvdb: response path is not cacheable")
	}
	return errors.New("tvdb: response path is not cacheable")
}

type tvdbResponseKind uint8

const (
	responseUnknown tvdbResponseKind = iota
	responseSeries
	responseTranslation
	responseMovie
	responseSearch
)

func responseKind(path string) tvdbResponseKind {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	switch len(segments) {
	case 2:
		return entityResponseKind(segments[0])
	case 3:
		return searchResponseKind(segments)
	case 4:
		return translationResponseKind(segments)
	default:
		return responseUnknown
	}
}

func entityResponseKind(resource string) tvdbResponseKind {
	switch resource {
	case "series":
		return responseSeries
	case "movies":
		return responseMovie
	default:
		return responseUnknown
	}
}

func searchResponseKind(segments []string) tvdbResponseKind {
	if segments[0] == "search" && segments[1] == "remoteid" {
		return responseSearch
	}
	return responseUnknown
}

func translationResponseKind(segments []string) tvdbResponseKind {
	if segments[2] == "translations" && (segments[0] == "series" || segments[0] == "movies") {
		return responseTranslation
	}
	return responseUnknown
}

func decodeTVDBEnvelope[T any](body []byte) error {
	var response tvdbResponse[T]
	// Mirrors goenvoy metadata/video/tvdb/tvdb.go:151, which decodes this envelope.
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("tvdb: decode response: %w", err)
	}
	return nil
}

// capTransport bounds every response body: goenvoy reads bodies without a cap.
type capTransport struct {
	next http.RoundTripper
	max  int64
}

func (t *capTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.next.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("tvdb: round trip: %w", err)
	}
	resp.Body = &capBody{rc: resp.Body, left: t.max}
	return resp, nil
}

type capBody struct {
	rc   io.ReadCloser
	left int64
}

func (b *capBody) Read(p []byte) (int, error) {
	n, err := b.rc.Read(p)
	b.left -= int64(n)
	if b.left < 0 {
		return 0, errBodyTooLarge
	}
	return n, err //nolint:wrapcheck // io.Reader contract: io.EOF must pass through unwrapped
}

func (b *capBody) Close() error { return b.rc.Close() } //nolint:wrapcheck // thin Closer
