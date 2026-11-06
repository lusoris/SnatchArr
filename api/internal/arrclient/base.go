// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package arrclient

import (
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golusoris/goenvoy/arr/v2"
)

// BaseFactory builds goenvoy clients with a shared API response policy.
type BaseFactory struct {
	Timeout time.Duration
	// ImportTimeout is the bound for manual-import listing and submission, which scan
	// the download's files on the *arr side (Lidarr measured 39-48 s).
	ImportTimeout time.Duration
	UserAgent     string
}

// httptest.Server.Close() calls CloseIdleConnections() on http.DefaultTransport,
// which is shared with every other package in the process. A dedicated package-level
// transport clone prevents parallel test teardowns from closing idle connections
// while preserving cross-client connection pooling in production.
var arrTransport = defaultTransport()

func defaultTransport() http.RoundTripper {
	return cloneTransport(http.DefaultTransport)
}

func cloneTransport(rt http.RoundTripper) http.RoundTripper {
	if t, ok := rt.(*http.Transport); ok {
		return t.Clone()
	}
	return rt
}

// Options converts the factory settings to goenvoy arr.Option values.
func (f BaseFactory) Options() []arr.Option {
	return []arr.Option{arr.WithHTTPClient(f.httpClient()), arr.WithUserAgent(f.UserAgent)}
}

// httpClient shares timeout, response, and redirect policies across typed and base clients.
func (f BaseFactory) httpClient() *http.Client {
	return &http.Client{
		Timeout:       f.Timeout,
		Transport:     apiTransport{base: arrTransport},
		CheckRedirect: rejectRedirect,
	}
}

// Base builds a goenvoy BaseClient configured with the factory's timeout and user agent.
func (f BaseFactory) Base(baseURL, apiKey string) (*arr.BaseClient, error) {
	return f.baseWithOptions(baseURL, apiKey, f.Options())
}

// BaseCapped builds a client whose response body cannot exceed maxBytes.
// The limit applies only to this client, including chunked and decompressed bodies.
func (f BaseFactory) BaseCapped(baseURL, apiKey string, maxBytes int64) (*arr.BaseClient, error) {
	opts, err := f.OptionsCapped(maxBytes)
	if err != nil {
		return nil, err
	}
	return f.baseWithOptions(baseURL, apiKey, opts)
}

// OptionsCapped configures typed clients with the same response limit as BaseCapped.
func (f BaseFactory) OptionsCapped(maxBytes int64) ([]arr.Option, error) {
	if maxBytes < 0 || maxBytes == math.MaxInt64 {
		return nil, errors.New("base client: invalid response body limit")
	}
	client := f.httpClient()
	client.Transport = cappedTransport{base: client.Transport, maxBytes: maxBytes}
	return []arr.Option{arr.WithHTTPClient(client), arr.WithUserAgent(f.UserAgent)}, nil
}

// baseWithOptions centralizes base client construction so capped and uncapped clients report errors consistently.
func (f BaseFactory) baseWithOptions(baseURL, apiKey string, opts []arr.Option) (*arr.BaseClient, error) {
	c, err := arr.NewBaseClient(baseURL, apiKey, opts...)
	if err != nil {
		return nil, fmt.Errorf("base client: %w", err)
	}
	return c, nil
}

// RedirectError reports a base URL redirect using a destination without credentials or query data.
// Redirects are never followed: net/http would resend the X-Api-Key header to the new destination.
type RedirectError struct {
	Location string
}

// Error explains how to reach the app API directly.
func (e *RedirectError) Error() string {
	return fmt.Sprintf("base URL redirects to %s: an auth proxy, login page or HTTPS upgrade sits in front of the app; use the app's direct API address", e.Location)
}

func rejectRedirect(req *http.Request, _ []*http.Request) error {
	return &RedirectError{Location: safeLocation(req.URL)}
}

type apiTransport struct {
	base http.RoundTripper
}

func (t apiTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("API transport: %w", err)
	}
	if resp.StatusCode >= http.StatusMultipleChoices && resp.StatusCode < http.StatusBadRequest {
		sanitizeRedirect(req, resp)
	}
	contentType, _, _ := strings.Cut(resp.Header.Get("Content-Type"), ";")
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices || !strings.EqualFold(strings.TrimSpace(contentType), "text/html") {
		return resp, nil
	}
	pageErr := fmt.Errorf("%s %s returned an HTML page, not the app's API: base URL points at a web page or login screen", req.Method, req.URL.EscapedPath())
	if err := resp.Body.Close(); err != nil {
		return nil, errors.Join(pageErr, fmt.Errorf("close HTML response: %w", err))
	}
	return nil, pageErr
}

// HintAPIError adds a base URL hint for an empty 404, preserving the API error chain.
func HintAPIError(err error, app string) error {
	apiErr, ok := errors.AsType[*arr.APIError](err)
	if ok && apiErr.StatusCode == http.StatusNotFound && len(apiErr.Body) == 0 {
		return fmt.Errorf("%w (empty 404: base URL does not point at %s)", err, app)
	}
	return err
}

func safeLocation(u *url.URL) string {
	return (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path, RawPath: u.RawPath}).String()
}

func sanitizeRedirect(req *http.Request, resp *http.Response) {
	raw := resp.Header.Get("Location")
	if raw == "" {
		return
	}
	location, err := req.URL.Parse(raw)
	if err != nil {
		return
	}
	// net/http captures Location before CheckRedirect and includes it in url.Error.
	resp.Header.Set("Location", safeLocation(location))
}

var errResponseBodyTooLarge = errors.New("API response body exceeds limit")

type cappedTransport struct {
	base     http.RoundTripper
	maxBytes int64
}

func (t cappedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("capped API transport: %w", err)
	}
	resp.Body = &cappedBody{ReadCloser: resp.Body, reader: &io.LimitedReader{R: resp.Body, N: t.maxBytes + 1}}
	return resp, nil
}

type cappedBody struct {
	io.ReadCloser
	reader *io.LimitedReader
}

func (b *cappedBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	if b.reader.N == 0 {
		return n, errResponseBodyTooLarge
	}
	// io.Reader contract: io.EOF is returned unwrapped, since io.ReadAll and the JSON decoder
	// compare it by identity; other errors pass through as the base body returned them.
	return n, err //nolint:wrapcheck // io.Reader passthrough; wrapping io.EOF breaks every reader
}
