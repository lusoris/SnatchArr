// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package tvdbprovider

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
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
			resp := newOriginResponse(http.StatusNoContent, "", tc.in)
			defer resp.Body.Close()
			guard := originGuard{next: staticRoundTripper(resp)}
			got, err := guard.RoundTrip(newRequest(t, http.MethodPost, "/other"))
			if err != nil {
				t.Fatalf("RoundTrip() error = %v", err)
			}
			defer got.Body.Close()
			if values := got.Header.Values("Cache-Control"); !reflect.DeepEqual(values, tc.want) {
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
			resp := newOriginResponse(http.StatusOK, tc.body, nil)
			defer resp.Body.Close()
			guard := originGuard{next: staticRoundTripper(resp), basePath: "/v4"}
			got, err := guard.RoundTrip(newRequest(t, http.MethodGet, tc.path))
			if err != nil {
				t.Fatalf("RoundTrip() error = %v", err)
			}
			defer got.Body.Close()
			assertBody(t, got, tc.body)
			if got.Header.Get("Cache-Control") != "" {
				t.Fatalf("Cache-Control = %q, want empty", got.Header.Get("Cache-Control"))
			}
		})
	}
}

func TestOriginGuardAddsNoStoreForUncacheableResponses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		method string
		path   string
		status int
		body   string
		want   bool
	}{
		{name: "numeric translation name", method: http.MethodGet, path: "/v4/series/81189/translations/deu", status: http.StatusOK, body: `{"status":"success","data":{"name":123}}`, want: true},
		{name: "unknown path", method: http.MethodGet, path: "/v4/unknown", status: http.StatusOK, body: `{"not":"tvdb"}`, want: true},
		{name: "not found", method: http.MethodGet, path: "/v4/series/81189", status: http.StatusNotFound, body: `{"message":"missing"}`},
		{name: "post", method: http.MethodPost, path: "/v4/unknown", status: http.StatusOK, body: `not json`},
		{name: "empty body", method: http.MethodGet, path: "/v4/series/81189", status: http.StatusOK, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resp := newOriginResponse(tc.status, tc.body, []string{"max-age=60"})
			defer resp.Body.Close()
			guard := originGuard{next: staticRoundTripper(resp), basePath: "/v4"}
			got, err := guard.RoundTrip(newRequest(t, tc.method, tc.path))
			if err != nil {
				t.Fatalf("RoundTrip() error = %v", err)
			}
			defer got.Body.Close()
			assertBody(t, got, tc.body)
			hasNoStore := strings.Contains(strings.ToLower(got.Header.Get("Cache-Control")), "no-store")
			if hasNoStore != tc.want {
				t.Fatalf("no-store = %t, want %t; Cache-Control = %q", hasNoStore, tc.want, got.Header.Get("Cache-Control"))
			}
		})
	}
}

func TestOriginGuardReturnsBodyReadError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("planted body read error")
	body := &errorReadCloser{err: wantErr}
	resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body}
	defer resp.Body.Close()
	guard := originGuard{next: staticRoundTripper(resp), basePath: "/v4"}
	got, err := guard.RoundTrip(newRequest(t, http.MethodGet, "/v4/series/81189"))
	if got != nil {
		defer got.Body.Close()
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("RoundTrip() error = %v, want %v", err, wantErr)
	}
	if !body.closed {
		t.Fatal("body was not closed after read error")
	}
}

func staticRoundTripper(resp *http.Response) roundTripFunc {
	return func(*http.Request) (*http.Response, error) { return resp, nil }
}

func newOriginResponse(status int, body string, cacheControl []string) *http.Response {
	resp := &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
	for _, value := range cacheControl {
		resp.Header.Add("Cache-Control", value)
	}
	return resp
}

func newRequest(t *testing.T, method, path string) *http.Request {
	t.Helper()
	u, err := url.Parse("https://example.test" + path)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", path, err)
	}
	return &http.Request{Method: method, URL: u, Header: make(http.Header)}
}

func assertBody(t *testing.T, resp *http.Response, want string) {
	t.Helper()
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(got) != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

type errorReadCloser struct {
	err    error
	closed bool
}

func (b *errorReadCloser) Read([]byte) (int, error) { return 0, b.err }

func (b *errorReadCloser) Close() error {
	b.closed = true
	return nil
}
