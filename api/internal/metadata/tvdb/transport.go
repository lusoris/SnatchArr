// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package tvdbprovider

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/time/rate"
)

// errBodyTooLarge reports a TVDB response past the byte cap.
var errBodyTooLarge = errors.New("tvdb: response body exceeds byte cap")

// limitTransport waits for the limiter, with the request context, before every real HTTP
// request: logins and retries count against the rate ceiling too.
type limitTransport struct {
	next    http.RoundTripper
	limiter *rate.Limiter
}

func (t *limitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.limiter.Wait(req.Context()); err != nil {
		return nil, fmt.Errorf("tvdb: rate wait: %w", err)
	}
	resp, err := t.next.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("tvdb: round trip: %w", err)
	}
	return resp, nil
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
