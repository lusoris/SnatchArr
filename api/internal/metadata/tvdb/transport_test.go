// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package tvdbprovider

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestOriginGuardRemovesStaleWhileRevalidate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "mixed case with value",
			in:   []string{"public, StAlE-WhIlE-ReVaLiDaTe=3600, max-age=60"},
			want: []string{"public, max-age=60"},
		},
		{
			name: "only directive",
			in:   []string{"stale-while-revalidate"},
		},
		{
			name: "empty directives dropped",
			in:   []string{"public,, stale-while-revalidate=60,"},
			want: []string{"public"},
		},
		{
			name: "multiple lines",
			in:   []string{"public, stale-while-revalidate=60", "max-age=30, STALE-WHILE-REVALIDATE"},
			want: []string{"public", "max-age=30"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			guard := originGuard{next: newOriginRoundTripper(t, http.StatusNoContent, "", tc.in)}
			got, err := roundTripGuard(t, guard, newRequest(t, http.MethodPost, "/other"))
			if err != nil {
				t.Fatalf("RoundTrip() error = %v", err)
			}
			if got.status != http.StatusNoContent {
				t.Fatalf("status = %d, want %d", got.status, http.StatusNoContent)
			}
			if values := got.header.Values("Cache-Control"); !reflect.DeepEqual(values, tc.want) {
				t.Fatalf("Cache-Control = %q, want %q", values, tc.want)
			}
		})
	}
}

func TestOriginGuardValidTVDBPaths(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		path string
		body string
	}{
		{name: "series", path: "/v4/series/81189", body: `{"status":"success","data":{}}`},
		{name: "series translation", path: "/v4/series/81189/translations/deu", body: `{"status":"success","data":{"name":"Breaking Bad"}}`},
		{name: "movie", path: "/v4/movies/439", body: `{"status":"success","data":{}}`},
		{name: "movie translation", path: "/v4/movies/439/translations/deu", body: `{"status":"success","data":{"name":"Die Verurteilten"}}`},
		{name: "remote id search", path: "/v4/search/remoteid/tt0111161", body: `{"status":"success","data":[]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			guard := originGuard{next: newOriginRoundTripper(t, http.StatusOK, tc.body, nil), basePath: "/v4"}
			got, err := roundTripGuard(t, guard, newRequest(t, http.MethodGet, tc.path))
			if err != nil {
				t.Fatalf("RoundTrip() error = %v", err)
			}
			if got.status != http.StatusOK {
				t.Fatalf("status = %d, want %d", got.status, http.StatusOK)
			}
			assertBody(t, got.body, tc.body)
			if got.header.Get("Cache-Control") != "" {
				t.Fatalf("Cache-Control = %q, want empty", got.header.Get("Cache-Control"))
			}
		})
	}
}

func TestOriginGuardAddsNoStoreForUncacheableResponses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		method       string
		path         string
		status       int
		cacheControl []string
		body         string
		want         bool
	}{
		{name: "numeric translation name", method: http.MethodGet, path: "/v4/series/81189/translations/deu", status: http.StatusOK, body: `{"status":"success","data":{"name":123}}`, want: true},
		{name: "unknown path", method: http.MethodGet, path: "/v4/unknown", status: http.StatusOK, body: `{"not":"tvdb"}`, want: true},
		{name: "not found", method: http.MethodGet, path: "/v4/series/81189", status: http.StatusNotFound, body: `{"message":"missing"}`},
		{name: "lookalike directive", method: http.MethodGet, path: "/v4/series/81189", status: http.StatusNotFound, cacheControl: []string{"x-no-store, max-age=no-store"}, body: `{"message":"missing"}`},
		{name: "post", method: http.MethodPost, path: "/v4/unknown", status: http.StatusOK, body: `not json`},
		{name: "empty body", method: http.MethodGet, path: "/v4/series/81189", status: http.StatusOK, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			inCC := tc.cacheControl
			if inCC == nil {
				inCC = []string{"max-age=60"}
			}
			guard := originGuard{next: newOriginRoundTripper(t, tc.status, tc.body, inCC), basePath: "/v4"}
			got, err := roundTripGuard(t, guard, newRequest(t, tc.method, tc.path))
			if err != nil {
				t.Fatalf("RoundTrip() error = %v", err)
			}
			if got.status != tc.status {
				t.Fatalf("status = %d, want %d", got.status, tc.status)
			}
			assertBody(t, got.body, tc.body)
			hasNoStore := slices.Contains(cacheControlDirectiveNames(got.header), "no-store")
			if hasNoStore != tc.want {
				t.Fatalf("no-store = %t, want %t; Cache-Control = %q", hasNoStore, tc.want, got.header.Values("Cache-Control"))
			}
		})
	}
}

func TestOriginGuardReturnsBodyReadError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("planted body read error")
	body := &errorReadCloser{err: wantErr}
	assertOriginBodyClosedOnce(t, &body.closes)
	resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body}
	guard := originGuard{next: staticRoundTripper(resp), basePath: "/v4"}
	_, err := roundTripGuard(t, guard, newRequest(t, http.MethodGet, "/v4/series/81189"))
	if !errors.Is(err, wantErr) {
		t.Fatalf("RoundTrip() error = %v, want %v", err, wantErr)
	}
	if !body.closed {
		t.Fatal("body was not closed after read error")
	}
}

func TestOriginGuardBoundsResponseBody(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		size    int
		wantErr bool
	}{
		{name: "at cap", size: maxBodyBytes},
		{name: "over cap", size: maxBodyBytes + 1, wantErr: true},
		{name: "far over cap", size: 2 * maxBodyBytes, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := &trackingBody{Reader: bytes.NewReader(bytes.Repeat([]byte{'x'}, tc.size))}
			assertOriginBodyClosedOnce(t, &body.closes)
			resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body}
			guard := originGuard{next: staticRoundTripper(resp), basePath: "/v4"}
			got, err := roundTripGuard(t, guard, newRequest(t, http.MethodGet, "/v4/series/81189"))
			if tc.wantErr {
				if !errors.Is(err, errBodyTooLarge) {
					t.Fatalf("RoundTrip() error = %v, want errBodyTooLarge", err)
				}
				if !body.closed {
					t.Fatal("oversized body was not closed")
				}
				if read := tc.size - body.Len(); read > maxBodyBytes+1 {
					t.Fatalf("origin guard read %d bytes, want at most %d", read, maxBodyBytes+1)
				}
				return
			}
			if err != nil {
				t.Fatalf("RoundTrip() error = %v", err)
			}
			assertBody(t, got.body, string(bytes.Repeat([]byte{'x'}, tc.size)))
			if !body.closed {
				t.Fatal("body was not closed")
			}
		})
	}
}

type guardResponse struct {
	status int
	header http.Header
	body   []byte
}

func roundTripGuard(t *testing.T, guard originGuard, req *http.Request) (guardResponse, error) {
	t.Helper()
	resp, err := guard.RoundTrip(req)
	if resp == nil {
		return guardResponse{}, err
	}
	var body []byte
	var readErr error
	if err == nil {
		body, readErr = io.ReadAll(resp.Body)
	}
	if closeErr := resp.Body.Close(); closeErr != nil {
		t.Errorf("Close() error = %v", closeErr)
	}
	if err != nil {
		return guardResponse{}, err
	}
	if readErr != nil {
		t.Fatalf("ReadAll() error = %v", readErr)
	}
	return guardResponse{
		status: resp.StatusCode,
		header: resp.Header,
		body:   body,
	}, nil
}

func staticRoundTripper(resp *http.Response) roundTripFunc {
	return func(*http.Request) (*http.Response, error) { return resp, nil }
}

func newOriginRoundTripper(t *testing.T, status int, body string, cacheControl []string) roundTripFunc {
	t.Helper()
	origin := &trackingBody{Reader: bytes.NewReader([]byte(body))}
	assertOriginBodyClosedOnce(t, &origin.closes)
	header := make(http.Header)
	for _, value := range cacheControl {
		header.Add("Cache-Control", value)
	}
	return staticRoundTripper(&http.Response{StatusCode: status, Header: header, Body: origin})
}

func assertOriginBodyClosedOnce(t *testing.T, closes *int) {
	t.Helper()
	t.Cleanup(func() {
		if *closes != 1 {
			t.Errorf("origin body Close count = %d, want 1", *closes)
		}
	})
}

func cacheControlDirectiveNames(header http.Header) []string {
	var names []string
	for _, value := range header.Values("Cache-Control") {
		for directive := range strings.SplitSeq(value, ",") {
			name, _, _ := strings.Cut(directive, "=")
			name = strings.ToLower(strings.TrimSpace(name))
			if name != "" {
				names = append(names, name)
			}
		}
	}
	return names
}

func newRequest(t *testing.T, method, path string) *http.Request {
	t.Helper()
	u, err := url.Parse("https://example.test" + path)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", path, err)
	}
	return &http.Request{Method: method, URL: u, Header: make(http.Header)}
}

func assertBody(t *testing.T, got []byte, want string) {
	t.Helper()
	if string(got) != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

type errorReadCloser struct {
	err    error
	closed bool
	closes int
}

func (b *errorReadCloser) Read([]byte) (int, error) { return 0, b.err }

func (b *errorReadCloser) Close() error {
	b.closed = true
	b.closes++
	return nil
}

type trackingBody struct {
	*bytes.Reader
	closed bool
	closes int
}

func (b *trackingBody) Close() error {
	b.closed = true
	b.closes++
	return nil
}
