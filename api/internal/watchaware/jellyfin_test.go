// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package watchaware_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golusoris/goenvoy/mediaserver/jellyfin/v3"

	"github.com/lusoris/SnatchArr/api/internal/watchaware"
)

func TestJellyfinAdapter_TwoPagesAndMapping(t *testing.T) {
	t.Parallel()

	var userParamChecked atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/Users":
			_ = json.NewEncoder(w).Encode([]jellyfin.UserDto{
				{ID: "user-1", Name: "Alice"},
			})
		case "/Items":
			userID := r.URL.Query().Get("userId")
			if userID != "user-1" {
				t.Errorf("userId = %q, want user-1", userID)
			} else {
				userParamChecked.Store(true)
			}
			if got := r.URL.Query().Get("IncludeItemTypes"); got != "Episode,Movie,Series" {
				t.Errorf("IncludeItemTypes = %q, want Episode,Movie,Series", got)
			}
			if got := r.URL.Query().Get("Fields"); got != "ProviderIds" {
				t.Errorf("Fields = %q, want ProviderIds", got)
			}

			startStr := r.URL.Query().Get("StartIndex")
			startIndex, _ := strconv.Atoi(startStr)
			if startIndex == 0 {
				items := make([]jellyfin.BaseItemDto, 200)
				for i := range items {
					items[i] = jellyfin.BaseItemDto{
						ID:                fmt.Sprintf("ep-%d", i),
						Type:              "Episode",
						SeriesID:          "series-100",
						ParentIndexNumber: 2,
						IndexNumber:       i + 1,
						UserData: &jellyfin.UserItemDataDto{
							Played:                true,
							PlaybackPositionTicks: int64(1000 + i),
							LastPlayedDate:        "2026-10-24T04:00:00Z",
						},
					}
				}
				_ = json.NewEncoder(w).Encode(jellyfin.ItemsResult{
					TotalRecordCount: 202,
					StartIndex:       0,
					Items:            items,
				})
				return
			}

			// Page 2
			pct := 92.5
			_ = json.NewEncoder(w).Encode(jellyfin.ItemsResult{
				TotalRecordCount: 202,
				StartIndex:       200,
				Items: []jellyfin.BaseItemDto{
					{
						ID:                "movie-201",
						Type:              "Movie",
						SeriesID:          "",
						ParentIndexNumber: 0,
						IndexNumber:       0,
						UserData: &jellyfin.UserItemDataDto{
							Played:                false,
							PlayedPercentage:      &pct,
							PlaybackPositionTicks: 5555,
							LastPlayedDate:        "2026-10-23T22:00:00Z",
						},
					},
					{
						ID:          "series-100",
						Type:        "Series",
						ProviderIDs: map[string]string{"Tvdb": "12345"},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	src, err := watchaware.NewJellyfin(srv.URL, "test-token", 5*time.Second, "SnatchArr-test")
	if err != nil {
		t.Fatalf("NewJellyfin: %v", err)
	}

	if kind := src.Kind(); kind != watchaware.KindJellyfin {
		t.Errorf("Kind() = %q, want %q", kind, watchaware.KindJellyfin)
	}

	ctx := context.Background()
	watches, err := src.Watches(ctx)
	if err != nil {
		t.Fatalf("Watches(): %v", err)
	}

	if !userParamChecked.Load() {
		t.Fatal("userId query parameter was not verified")
	}

	if len(watches) != 202 {
		t.Fatalf("len(watches) = %d, want 202", len(watches))
	}

	// Verify mapped episode
	w0 := watches[0]
	if w0.UserID != "user-1" || w0.UserName != "Alice" || w0.ItemID != "ep-0" || w0.ItemType != "Episode" {
		t.Errorf("w0 identification = %+v", w0)
	}
	if w0.TvdbID != "" {
		t.Errorf("w0 TvdbID = %q, want empty", w0.TvdbID)
	}
	if w0.SeriesID != "series-100" || w0.SeasonNumber != 2 || w0.EpisodeNumber != 1 {
		t.Errorf("w0 season/episode = series:%s s%02de%02d, want series-100 s02e01", w0.SeriesID, w0.SeasonNumber, w0.EpisodeNumber)
	}
	if !w0.Played || w0.PositionTicks != 1000 || w0.LastPlayed != "2026-10-24T04:00:00Z" {
		t.Errorf("w0 played state = %+v", w0)
	}

	// Verify mapped movie
	w200 := watches[200]
	if w200.ItemID != "movie-201" || w200.ItemType != "Movie" || w200.Played {
		t.Errorf("w200 = %+v", w200)
	}
	if w200.TvdbID != "" {
		t.Errorf("w200 TvdbID = %q, want empty", w200.TvdbID)
	}
	if w200.PlayedPercentage == nil || *w200.PlayedPercentage != 92.5 {
		t.Errorf("w200 PlayedPercentage = %v, want 92.5", w200.PlayedPercentage)
	}
	if w200.PositionTicks != 5555 || w200.LastPlayed != "2026-10-23T22:00:00Z" {
		t.Errorf("w200 playback = %+v", w200)
	}

	// Verify mapped series with Tvdb ID
	w201 := watches[201]
	if w201.ItemID != "series-100" || w201.ItemType != "Series" || w201.TvdbID != "12345" {
		t.Errorf("w201 = %+v, want series-100 Series tvdb 12345", w201)
	}
}

func TestJellyfinAdapter_MaxPagesBoundHonoured(t *testing.T) {
	t.Parallel()

	var reqCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/Users":
			_ = json.NewEncoder(w).Encode([]jellyfin.UserDto{
				{ID: "user-1", Name: "Alice"},
			})
		case "/Items":
			reqCount.Add(1)
			items := make([]jellyfin.BaseItemDto, 200)
			for i := range items {
				items[i] = jellyfin.BaseItemDto{
					ID:   fmt.Sprintf("ep-%d", i),
					Type: "Episode",
				}
			}
			// Fake reports a huge total: 1,000,000 items
			_ = json.NewEncoder(w).Encode(jellyfin.ItemsResult{
				TotalRecordCount: 1_000_000,
				StartIndex:       0,
				Items:            items,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	src, err := watchaware.NewJellyfin(srv.URL, "test-token", 5*time.Second, "SnatchArr-test")
	if err != nil {
		t.Fatalf("NewJellyfin: %v", err)
	}

	ctx := context.Background()
	watches, err := src.Watches(ctx)
	if !errors.Is(err, watchaware.ErrTruncated) {
		t.Fatalf("Watches() error = %v, want ErrTruncated", err)
	}
	if watches != nil {
		t.Errorf("Watches() returned %d partial watches, want none", len(watches))
	}

	gotReqs := int(reqCount.Load())
	const maxPages = 50
	if gotReqs > maxPages {
		t.Fatalf("requests = %d, want <= %d", gotReqs, maxPages)
	}
	if gotReqs != maxPages {
		t.Errorf("requests = %d, want exactly %d", gotReqs, maxPages)
	}
}
