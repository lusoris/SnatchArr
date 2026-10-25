// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package arrclient_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golusoris/goenvoy/arr/v2"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// fakeArr answers /api/v{1,3}/system/status like a real *arr when the key matches.
func fakeArr(t *testing.T, version string, wantKey string) *httptest.Server {
	t.Helper()
	return fakeStatus(t, arr.StatusResponse{AppName: "Fake", Version: version}, wantKey)
}

// fakeStatus is fakeArr with the whole status answer chosen by the test.
func fakeStatus(t *testing.T, status arr.StatusResponse, wantKey string) *httptest.Server {
	t.Helper()
	body, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("encode the status answer: %v", err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != wantKey {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"Unauthorized"}`))
			return
		}
		if r.URL.Path != "/api/v3/system/status" && r.URL.Path != "/api/v1/system/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
}

func TestProbe(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		kind    domain.AppKind
		version string
		key     string
		wantErr error
	}{
		{"positive sonarr", domain.KindSonarr, "4.0.10.2544", "k", nil},
		{"positive radarr", domain.KindRadarr, "5.14.0", "k", nil},
		{"positive lidarr v1 api", domain.KindLidarr, "2.8.2", "k", nil},
		{"positive readarr v1 api", domain.KindReadarr, "0.4.9", "k", nil},
		{"positive whisparr v2", domain.KindWhisparrV2, "2.0.0.1", "k", nil},
		{"positive whisparr v3", domain.KindWhisparrV3, "3.1.0", "k", nil},
		{"negative wrong key", domain.KindSonarr, "4.0.0", "wrong", domain.ErrUnreachable},
		{"negative whisparr generation mismatch", domain.KindWhisparrV2, "3.0.0", "k", domain.ErrInvalid},
		{"boundary whisparr v3 reports v2", domain.KindWhisparrV3, "2.9.9", "k", domain.ErrInvalid},
		{"negative unknown kind", "plex", "1", "k", domain.ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := fakeArr(t, tc.version, "k")
			defer srv.Close()
			p := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: 2 * time.Second})
			res, err := p.Probe(context.Background(), tc.kind, srv.URL, tc.key)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if res.Version != tc.version || res.AppName != "Fake" {
					t.Fatalf("result = %+v", res)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestProbeHonoursTimeout(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	p := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: 200 * time.Millisecond})
	start := time.Now()
	_, err := p.Probe(context.Background(), domain.KindSonarr, srv.URL, "k")
	if !errors.Is(err, domain.ErrUnreachable) {
		t.Fatalf("expected unreachable, got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("probe did not respect its timeout")
	}
}

func TestDefaultOptions(t *testing.T) {
	t.Parallel()

	opts := arrclient.DefaultOptions()
	if opts.Timeout != 15*time.Second {
		t.Errorf("Timeout = %v, want 15s", opts.Timeout)
	}
	if opts.ImportTimeout != 120*time.Second {
		t.Errorf("ImportTimeout = %v, want 120s", opts.ImportTimeout)
	}
	if opts.UserAgent != "SnatchArr/1.0 (https://github.com/lusoris/SnatchArr)" {
		t.Errorf("UserAgent = %q, want SnatchArr default", opts.UserAgent)
	}
}

func TestNewGoenvoyProberOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		in                arrclient.Options
		wantTimeout       time.Duration
		wantImportTimeout time.Duration
		wantUserAgent     string
	}{
		{
			name:              "positive: explicit options preserved",
			in:                arrclient.Options{Timeout: 5 * time.Second, ImportTimeout: 30 * time.Second, UserAgent: "CustomUA/1.0"},
			wantTimeout:       5 * time.Second,
			wantImportTimeout: 30 * time.Second,
			wantUserAgent:     "CustomUA/1.0",
		},
		{
			name:              "negative: zero values default",
			in:                arrclient.Options{},
			wantTimeout:       15 * time.Second,
			wantImportTimeout: 120 * time.Second,
			wantUserAgent:     "SnatchArr/1.0 (https://github.com/lusoris/SnatchArr)",
		},
		{
			name:              "boundary: import timeout raised to timeout",
			in:                arrclient.Options{Timeout: 30 * time.Second, ImportTimeout: 10 * time.Second},
			wantTimeout:       30 * time.Second,
			wantImportTimeout: 30 * time.Second,
			wantUserAgent:     "SnatchArr/1.0 (https://github.com/lusoris/SnatchArr)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := arrclient.NewGoenvoyProber(tt.in)
			opts := p.ProberOpts()
			if opts.Timeout != tt.wantTimeout {
				t.Errorf("Timeout = %v, want %v", opts.Timeout, tt.wantTimeout)
			}
			if opts.ImportTimeout != tt.wantImportTimeout {
				t.Errorf("ImportTimeout = %v, want %v", opts.ImportTimeout, tt.wantImportTimeout)
			}
			if opts.UserAgent != tt.wantUserAgent {
				t.Errorf("UserAgent = %q, want %q", opts.UserAgent, tt.wantUserAgent)
			}
		})
	}
}
