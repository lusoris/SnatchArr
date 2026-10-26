// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package adult

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golusoris/goenvoy/metadata"
	"github.com/golusoris/goenvoy/metadata/adult/stashbox/v2"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestIsAuthOrGraphQLError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "401 unauthorized",
			err:  &stashbox.APIError{StatusCode: http.StatusUnauthorized},
			want: true,
		},
		{
			name: "403 forbidden",
			err:  &stashbox.APIError{StatusCode: http.StatusForbidden},
			want: true,
		},
		{
			name: "500 internal error",
			err:  &stashbox.APIError{StatusCode: http.StatusInternalServerError},
			want: false,
		},
		{
			name: "graphql error",
			err:  &stashbox.GraphQLError{Message: "unauthorized field access"},
			want: true,
		},
		{
			name: "wrapped graphql error",
			err:  fmt.Errorf("request failed: %w", &stashbox.GraphQLError{Message: "syntax error"}),
			want: true,
		},
		{
			name: "standard error",
			err:  errors.New("connection reset"),
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isAuthOrGraphQLError(tc.err)
			if got != tc.want {
				t.Errorf("isAuthOrGraphQLError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestFetchFavoritePerformers_BoundaryEmpty(t *testing.T) {
	t.Parallel()
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"query_performers": map[string]any{
					"count":      0,
					"performers": []any{},
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	client, err := stashbox.New(srv.URL, "key", metadata.WithTimeout(5*time.Second))
	if err != nil {
		t.Fatalf("stashbox.New unexpected error: %v", err)
	}

	ctx := context.Background()
	performers, truncated, err := fetchFavoritePerformers(ctx, client)
	if err != nil {
		t.Fatalf("fetchFavoritePerformers error: %v", err)
	}
	if len(performers) != 0 {
		t.Errorf("expected 0 performers, got %d", len(performers))
	}
	if truncated {
		t.Errorf("expected truncated=false for empty response")
	}
	if got := requestCount.Load(); got != 1 {
		t.Errorf("expected exactly 1 request for empty response, got %d", got)
	}
}

func TestFetchFavoritePerformers_Boundary500Cap(t *testing.T) {
	t.Parallel()
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqNum := requestCount.Add(1)
		if reqNum > 10 {
			t.Errorf("boundary violated: request %d made beyond 500 cap", reqNum)
		}
		pageItems := make([]map[string]any, 0, 50)
		for i := range 50 {
			pageItems = append(pageItems, map[string]any{
				"id":   fmt.Sprintf("performer-%d-%d", reqNum, i),
				"name": fmt.Sprintf("Performer %d %d", reqNum, i),
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"query_performers": map[string]any{
					"count":      1000,
					"performers": pageItems,
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	client, err := stashbox.New(srv.URL, "key", metadata.WithTimeout(5*time.Second))
	if err != nil {
		t.Fatalf("stashbox.New unexpected error: %v", err)
	}

	ctx := context.Background()
	performers, truncated, err := fetchFavoritePerformers(ctx, client)
	if err != nil {
		t.Fatalf("fetchFavoritePerformers error: %v", err)
	}
	if len(performers) != MaxItemsPerSweep {
		t.Errorf("expected exactly %d performers at cap, got %d", MaxItemsPerSweep, len(performers))
	}
	if !truncated {
		t.Errorf("expected truncated=true when total count (1000) exceeds cap (500)")
	}
	if got := requestCount.Load(); got != 10 {
		t.Errorf("expected exactly 10 requests to reach 500 cap, got %d", got)
	}
}

func TestFetchFavoriteStudios_Boundary500Cap(t *testing.T) {
	t.Parallel()
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqNum := requestCount.Add(1)
		if reqNum > 10 {
			t.Errorf("boundary violated: request %d made beyond 500 cap", reqNum)
		}
		pageItems := make([]map[string]any, 0, 50)
		for i := range 50 {
			pageItems = append(pageItems, map[string]any{
				"id":   fmt.Sprintf("studio-%d-%d", reqNum, i),
				"name": fmt.Sprintf("Studio %d %d", reqNum, i),
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"query_studios": map[string]any{
					"count":   800,
					"studios": pageItems,
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	client, err := stashbox.New(srv.URL, "key", metadata.WithTimeout(5*time.Second))
	if err != nil {
		t.Fatalf("stashbox.New unexpected error: %v", err)
	}

	ctx := context.Background()
	studios, truncated, err := fetchFavoriteStudios(ctx, client)
	if err != nil {
		t.Fatalf("fetchFavoriteStudios error: %v", err)
	}
	if len(studios) != MaxItemsPerSweep {
		t.Errorf("expected exactly %d studios at cap, got %d", MaxItemsPerSweep, len(studios))
	}
	if !truncated {
		t.Errorf("expected truncated=true when total count (800) exceeds cap (500)")
	}
	if got := requestCount.Load(); got != 10 {
		t.Errorf("expected exactly 10 requests to reach 500 cap, got %d", got)
	}
}

func TestSyncErrorWrapping_401AndGraphQL(t *testing.T) {
	t.Parallel()
	srv401 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}))
	t.Cleanup(srv401.Close)

	srvGQL := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errors": []any{
				map[string]any{"message": "Invalid API key or token expired"},
			},
		})
	}))
	t.Cleanup(srvGQL.Close)

	client401, _ := stashbox.New(srv401.URL, "bad-key", metadata.WithTimeout(5*time.Second))
	clientGQL, _ := stashbox.New(srvGQL.URL, "bad-key", metadata.WithTimeout(5*time.Second))

	ctx := context.Background()
	_, _, err := fetchFavoritePerformers(ctx, client401)
	if err == nil {
		t.Fatal("expected error on 401, got nil")
	}
	if !isAuthOrGraphQLError(err) {
		t.Errorf("expected isAuthOrGraphQLError(err)=true for 401, got false (%v)", err)
	}
	wrapped401 := fmt.Errorf("%w: %w", domain.ErrInvalid, err)
	if !errors.Is(wrapped401, domain.ErrInvalid) {
		t.Errorf("expected errors.Is(wrapped401, domain.ErrInvalid)")
	}

	_, _, err = fetchFavoritePerformers(ctx, clientGQL)
	if err == nil {
		t.Fatal("expected error on GraphQL error, got nil")
	}
	if !isAuthOrGraphQLError(err) {
		t.Errorf("expected isAuthOrGraphQLError(err)=true for GraphQL error, got false (%v)", err)
	}
	wrappedGQL := fmt.Errorf("%w: %w", domain.ErrInvalid, err)
	if !errors.Is(wrappedGQL, domain.ErrInvalid) {
		t.Errorf("expected errors.Is(wrappedGQL, domain.ErrInvalid)")
	}
}
