// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package arrclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
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
