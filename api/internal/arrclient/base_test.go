// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package arrclient_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golusoris/goenvoy/arr/v2"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/prowlarrclient"
	"github.com/lusoris/SnatchArr/api/internal/seerrclient"
)

func TestBaseFactory_Base(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		factory arrclient.BaseFactory
		baseURL string
		apiKey  string
		wantErr bool
	}{
		{
			name:    "positive: valid configuration",
			factory: arrclient.BaseFactory{Timeout: 5 * time.Second, UserAgent: "TestAgent/1.0"},
			baseURL: "http://localhost:8989",
			apiKey:  "secret",
			wantErr: false,
		},
		{
			name:    "negative: invalid URL format",
			factory: arrclient.BaseFactory{Timeout: 5 * time.Second, UserAgent: "TestAgent/1.0"},
			baseURL: "://invalid-url",
			apiKey:  "secret",
			wantErr: true,
		},
		{
			name:    "boundary: zero timeout and empty UA",
			factory: arrclient.BaseFactory{Timeout: 0, UserAgent: ""},
			baseURL: "http://localhost:8989/",
			apiKey:  "",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, err := tt.factory.Base(tt.baseURL, tt.apiKey)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Base() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && c == nil {
				t.Fatal("Base() returned nil client")
			}
		})
	}
}

func TestBaseFactory_Options(t *testing.T) {
	t.Parallel()

	f := arrclient.BaseFactory{Timeout: 5 * time.Second, UserAgent: "CustomUA/2.0"}
	if opts := f.Options(); len(opts) != 2 {
		t.Fatalf("Options() len = %d, want 2", len(opts))
	}

	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	c, err := f.Base(srv.URL, "key")
	if err != nil {
		t.Fatalf("Base() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var dummy map[string]any
	if err := c.Get(ctx, "/", &dummy); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if gotUA != "CustomUA/2.0" {
		t.Errorf("got User-Agent %q, want %q", gotUA, "CustomUA/2.0")
	}
}

func TestBaseFactory_Timeout(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	f := arrclient.BaseFactory{Timeout: 10 * time.Millisecond, UserAgent: "TimeoutTest/1.0"}
	c, err := f.Base(srv.URL, "key")
	if err != nil {
		t.Fatalf("Base() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var dummy map[string]any
	if err := c.Get(ctx, "/", &dummy); err == nil {
		t.Fatal("expected error from client timeout, got nil")
	}
}

func TestClientBaseURLDiagnostics(t *testing.T) {
	t.Parallel()
	clients := []struct {
		name   string
		status func(context.Context, string) (string, error)
	}{
		{"sonarr", func(ctx context.Context, baseURL string) (string, error) {
			out, err := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: time.Second}).Probe(ctx, domain.KindSonarr, baseURL, "key")
			return out.Version, err
		}},
		{"Seerr", func(ctx context.Context, baseURL string) (string, error) {
			out, err := seerrclient.New(time.Second, "test").Status(ctx, baseURL, "key")
			return out.Version, err
		}},
		{"Prowlarr", func(ctx context.Context, baseURL string) (string, error) {
			out, err := prowlarrclient.New(time.Second, "test").Status(ctx, baseURL, "key")
			return out.Version, err
		}},
	}
	for _, client := range clients {
		t.Run(client.name, func(t *testing.T) {
			t.Parallel()
			for _, tc := range []struct {
				name        string
				status      int
				contentType string
				body        string
				want        string
			}{
				{"negative auth proxy", 302, "text/html", "", "redirects to"},
				{"positive JSON", 200, "application/json", `{"version":"1.2.3"}`, ""},
				{"negative HTML", 200, "text/html; charset=utf-8", "<html>login</html>", "HTML page"},
				{"boundary HTML media type case", 201, "Text/HTML", "<html>login</html>", "HTML page"},
				{"negative empty 404", 404, "text/plain", "", "empty 404: base URL does not point at " + client.name},
				{"boundary JSON 404", 404, "application/json", `{"error":"not found"}`, "returned 404"},
				{"boundary empty 401", 401, "text/plain", "", "returned 401"},
				{"boundary HTML 404", 404, "text/html", "<html>not found</html>", "returned 404"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()
					srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if tc.status == http.StatusFound {
							if r.URL.Path == "/login" {
								w.Header().Set("Content-Type", "text/html")
								_, _ = w.Write([]byte("<html>login</html>"))
								return
							}
							http.Redirect(w, r, "/login?rd=private-return", tc.status)
							return
						}
						w.Header().Set("Content-Type", tc.contentType)
						w.WriteHeader(tc.status)
						_, _ = w.Write([]byte(tc.body))
					}))
					t.Cleanup(srv.Close)
					version, err := client.status(t.Context(), srv.URL)
					if tc.want == "" {
						if err != nil || version != "1.2.3" {
							t.Fatalf("status = %q, %v", version, err)
						}
						return
					}
					if !errors.Is(err, domain.ErrUnreachable) || !strings.Contains(err.Error(), tc.want) {
						t.Fatalf("status error = %v, want unreachable containing %q", err, tc.want)
					}
					if tc.body != "" && strings.Contains(err.Error(), "empty 404") {
						t.Fatalf("nonempty response got empty-404 hint: %v", err)
					}
				})
			}
		})
	}
}

func TestBaseFactoryRejectsRedirects(t *testing.T) {
	t.Parallel()
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			var followed atomic.Int32
			login := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				followed.Add(1)
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte("<html>login</html>"))
			}))
			t.Cleanup(login.Close)
			for _, location := range []string{login.URL + "/outpost.goauthentik.io/start?rd=private-return#private-fragment", "/login?rd=private-return"} {
				t.Run(location, func(t *testing.T) {
					t.Parallel()
					srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						http.Redirect(w, r, location, status)
					}))
					t.Cleanup(srv.Close)
					c, err := (arrclient.BaseFactory{Timeout: time.Second}).Base(srv.URL, "key")
					if err != nil {
						t.Fatal(err)
					}
					var dst map[string]any
					err = c.Get(t.Context(), "/api/status", &dst)
					if err == nil {
						t.Fatal("expected redirect error")
					}
					destination := login.URL + "/outpost.goauthentik.io/start"
					if strings.HasPrefix(location, "/") {
						destination = srv.URL + "/login"
					}
					redirect, ok := errors.AsType[*arrclient.RedirectError](err)
					if !ok || redirect.Location != destination {
						t.Fatalf("redirect = %+v, error = %v, want %q", redirect, err, destination)
					}
					for _, want := range []string{"redirects to " + destination, "use the app's direct API address"} {
						if !strings.Contains(err.Error(), want) {
							t.Errorf("error = %v, want %q", err, want)
						}
					}
					for _, unwanted := range []string{"private-return", "private-fragment", "invalid character"} {
						if strings.Contains(err.Error(), unwanted) {
							t.Errorf("error contains %q: %v", unwanted, err)
						}
					}
					if followed.Load() != 0 {
						t.Fatal("redirect target was contacted")
					}
				})
			}
		})
	}
}

func TestHintAPIError(t *testing.T) {
	t.Parallel()
	apiErr := &arr.APIError{StatusCode: http.StatusNotFound}
	for _, tc := range []struct {
		name string
		err  error
		hint bool
	}{
		{"positive empty 404", apiErr, true},
		{"positive wrapped empty 404", fmt.Errorf("request: %w", apiErr), true},
		{"negative non-API error", errors.New("refused"), false},
		{"boundary nil", nil, false},
		{"boundary whitespace body", &arr.APIError{StatusCode: 404, Body: []byte(" ")}, false},
		{"boundary empty 500", &arr.APIError{StatusCode: 500}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := arrclient.HintAPIError(tc.err, "Seerr")
			if tc.err == nil {
				if got != nil {
					t.Fatalf("nil error gained a hint: %v", got)
				}
				return
			}
			if !errors.Is(got, tc.err) {
				t.Fatalf("error chain lost: %v", got)
			}
			if strings.Contains(got.Error(), "empty 404: base URL does not point at Seerr") != tc.hint {
				t.Fatalf("hint = %v, want %v: %v", !tc.hint, tc.hint, got)
			}
			if tc.hint {
				original, ok := errors.AsType[*arr.APIError](got)
				if !ok || original != apiErr {
					t.Fatalf("API error lost: %v", got)
				}
			}
		})
	}
}

func TestBaseFactoryRejectsHTMLForEveryMethod(t *testing.T) {
	t.Parallel()
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte("<html>login</html>"))
			}))
			t.Cleanup(srv.Close)
			c, err := (arrclient.BaseFactory{Timeout: time.Second}).Base(srv.URL, "key")
			if err != nil {
				t.Fatal(err)
			}
			calls := map[string]func() error{
				http.MethodGet:    func() error { return c.Get(t.Context(), "/api/items?offset=1", nil) },
				http.MethodHead:   func() error { return c.Head(t.Context(), "/api/items?offset=1") },
				http.MethodPost:   func() error { return c.Post(t.Context(), "/api/items?offset=1", nil, nil) },
				http.MethodPut:    func() error { return c.Put(t.Context(), "/api/items?offset=1", nil, nil) },
				http.MethodPatch:  func() error { return c.Patch(t.Context(), "/api/items?offset=1", nil, nil) },
				http.MethodDelete: func() error { return c.Delete(t.Context(), "/api/items?offset=1", nil, nil) },
			}
			err = calls[method]()
			if err == nil || !strings.Contains(err.Error(), method+" /api/items returned an HTML page") {
				t.Fatalf("error = %v, want method and path in HTML diagnostic", err)
			}
		})
	}
}
