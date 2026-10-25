// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package arrclient

import (
	"errors"
	"fmt"
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

func (f BaseFactory) httpClient() *http.Client {
	return &http.Client{
		Timeout:       f.Timeout,
		Transport:     apiTransport{base: arrTransport},
		CheckRedirect: rejectRedirect,
	}
}

// Base builds a goenvoy BaseClient configured with the factory's timeout and user agent.
func (f BaseFactory) Base(baseURL, apiKey string) (*arr.BaseClient, error) {
	c, err := arr.NewBaseClient(baseURL, apiKey, f.Options()...)
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
