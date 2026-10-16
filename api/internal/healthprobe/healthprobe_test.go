// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package healthprobe

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, listen, want string
		wantErr            bool
	}{
		{name: "default when unset", listen: "", want: "http://127.0.0.1:8080/readyz"},
		{name: "port only", listen: ":9000", want: "http://127.0.0.1:9000/readyz"},
		{name: "ipv4 wildcard", listen: "0.0.0.0:8080", want: "http://127.0.0.1:8080/readyz"},
		{name: "ipv6 wildcard", listen: "[::]:8080", want: "http://[::1]:8080/readyz"},
		{name: "named host kept", listen: "10.0.0.5:80", want: "http://10.0.0.5:80/readyz"},
		{name: "highest port", listen: ":65535", want: "http://127.0.0.1:65535/readyz"},
		{name: "no port", listen: "localhost", wantErr: true},
		{name: "empty port", listen: "127.0.0.1:", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := URL(tc.listen)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("URL(%q) = %q, want an error", tc.listen, got)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("URL(%q) = %q, %v; want %q", tc.listen, got, err, tc.want)
			}
		})
	}
}

func server(t *testing.T, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCheck(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		status     int
		wantErr    bool
		notReadyIs bool
	}{
		{name: "ready", status: http.StatusOK},
		{name: "not ready", status: http.StatusServiceUnavailable, wantErr: true, notReadyIs: true},
		{name: "other 2xx is not ready", status: http.StatusNoContent, wantErr: true, notReadyIs: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := server(t, tc.status)
			err := Check(t.Context(), srv.Client(), srv.URL+"/readyz")
			if (err != nil) != tc.wantErr {
				t.Fatalf("Check() = %v, want error %v", err, tc.wantErr)
			}
			if tc.notReadyIs && !errors.Is(err, ErrNotReady) {
				t.Fatalf("Check() = %v, want ErrNotReady", err)
			}
		})
	}
}

func TestCheckUnreachable(t *testing.T) {
	t.Parallel()
	srv := server(t, http.StatusOK)
	url := srv.URL + "/readyz"
	srv.Close()
	err := Check(t.Context(), srv.Client(), url)
	if err == nil || errors.Is(err, ErrNotReady) {
		t.Fatalf("Check() on a closed port = %v, want a connection error", err)
	}
}

func TestCheckCancelled(t *testing.T) {
	t.Parallel()
	srv := server(t, http.StatusOK)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := Check(ctx, srv.Client(), srv.URL+"/readyz"); err == nil {
		t.Fatal("Check() with a cancelled context passed")
	}
}

func TestRun(t *testing.T) {
	t.Parallel()
	ok := server(t, http.StatusOK)
	down := server(t, http.StatusServiceUnavailable)
	cases := []struct {
		name, addr, stderr string
		want               int
	}{
		{name: "ready", addr: strings.TrimPrefix(ok.URL, "http://"), want: 0},
		{name: "not ready", addr: strings.TrimPrefix(down.URL, "http://"), want: 1, stderr: "answered 503"},
		{name: "bad address", addr: "nope", want: 1, stderr: "listen address"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var stderr bytes.Buffer
			getenv := func(key string) string {
				if key == "APP_HTTP_ADDR" {
					return tc.addr
				}
				return ""
			}
			if got := Run(t.Context(), getenv, &stderr); got != tc.want {
				t.Fatalf("Run() = %d, want %d (stderr %q)", got, tc.want, stderr.String())
			}
			if !strings.Contains(stderr.String(), tc.stderr) {
				t.Fatalf("stderr %q does not name %q", stderr.String(), tc.stderr)
			}
		})
	}
}
