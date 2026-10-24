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

	"github.com/golusoris/goenvoy/mediaserver/emby/v3"

	"github.com/lusoris/SnatchArr/api/internal/watchaware"
)

func TestEmbyAdapter_TwoPagesAndMapping(t *testing.T) {
	t.Parallel()

	var userParamChecked atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/emby/Users":
			_ = json.NewEncoder(w).Encode([]emby.UserDto{
				{ID: "user-emby-1", Name: "Bob"},
			})
		case "/emby/Items":
			userID := r.URL.Query().Get("userId")
			if userID != "user-emby-1" {
				t.Errorf("userId = %q, want user-emby-1", userID)
			} else {
				userParamChecked.Store(true)
			}

			startStr := r.URL.Query().Get("StartIndex")
			startIndex, _ := strconv.Atoi(startStr)
			if startIndex == 0 {
				items := make([]emby.BaseItemDto, 200)
				for i := range items {
					items[i] = emby.BaseItemDto{
						ID:                fmt.Sprintf("ep-%d", i),
						Type:              "Episode",
						SeriesID:          "series-200",
						ParentIndexNumber: 1,
						IndexNumber:       i + 1,
						UserData: &emby.UserItemDataDto{
							Played:                true,
							PlaybackPositionTicks: int64(2000 + i),
							LastPlayedDate:        "2026-10-24T03:00:00Z",
						},
					}
				}
				_ = json.NewEncoder(w).Encode(emby.ItemsResult{
					TotalRecordCount: 201,
					StartIndex:       0,
					Items:            items,
				})
				return
			}

			// Page 2
			pct := 94.0
			_ = json.NewEncoder(w).Encode(emby.ItemsResult{
				TotalRecordCount: 201,
				StartIndex:       200,
				Items: []emby.BaseItemDto{
					{
						ID:                "movie-301",
						Type:              "Movie",
						SeriesID:          "",
						ParentIndexNumber: 0,
						IndexNumber:       0,
						UserData: &emby.UserItemDataDto{
							Played:                false,
							PlayedPercentage:      &pct,
							PlaybackPositionTicks: 8888,
							LastPlayedDate:        "2026-10-23T21:00:00Z",
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	src, err := watchaware.NewEmby(srv.URL, "test-token", 5*time.Second, "SnatchArr-test")
	if err != nil {
		t.Fatalf("NewEmby: %v", err)
	}

	if kind := src.Kind(); kind != watchaware.KindEmby {
		t.Errorf("Kind() = %q, want %q", kind, watchaware.KindEmby)
	}

	ctx := context.Background()
	watches, err := src.Watches(ctx)
	if err != nil {
		t.Fatalf("Watches(): %v", err)
	}

	if !userParamChecked.Load() {
		t.Fatal("userId query parameter was not verified")
	}

	if len(watches) != 201 {
		t.Fatalf("len(watches) = %d, want 201", len(watches))
	}

	// Verify mapped episode
	w0 := watches[0]
	if w0.UserID != "user-emby-1" || w0.UserName != "Bob" || w0.ItemID != "ep-0" || w0.ItemType != "Episode" {
		t.Errorf("w0 identification = %+v", w0)
	}
	if w0.SeriesID != "series-200" || w0.SeasonNumber != 1 || w0.EpisodeNumber != 1 {
		t.Errorf("w0 season/episode = series:%s s%02de%02d, want series-200 s01e01", w0.SeriesID, w0.SeasonNumber, w0.EpisodeNumber)
	}
	if !w0.Played || w0.PositionTicks != 2000 || w0.LastPlayed != "2026-10-24T03:00:00Z" {
		t.Errorf("w0 played state = %+v", w0)
	}

	// Verify mapped movie
	w200 := watches[200]
	if w200.ItemID != "movie-301" || w200.ItemType != "Movie" || w200.Played {
		t.Errorf("w200 = %+v", w200)
	}
	if w200.PlayedPercentage == nil || *w200.PlayedPercentage != 94.0 {
		t.Errorf("w200 PlayedPercentage = %v, want 94.0", w200.PlayedPercentage)
	}
	if w200.PositionTicks != 8888 || w200.LastPlayed != "2026-10-23T21:00:00Z" {
		t.Errorf("w200 playback = %+v", w200)
	}
}

func TestEmbyAdapter_MaxPagesBoundHonoured(t *testing.T) {
	t.Parallel()

	var reqCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/emby/Users":
			_ = json.NewEncoder(w).Encode([]emby.UserDto{
				{ID: "user-emby-1", Name: "Bob"},
			})
		case "/emby/Items":
			reqCount.Add(1)
			items := make([]emby.BaseItemDto, 200)
			for i := range items {
				items[i] = emby.BaseItemDto{
					ID:   fmt.Sprintf("ep-%d", i),
					Type: "Episode",
				}
			}
			// Fake reports a huge total: 500,000 items
			_ = json.NewEncoder(w).Encode(emby.ItemsResult{
				TotalRecordCount: 500_000,
				StartIndex:       0,
				Items:            items,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	src, err := watchaware.NewEmby(srv.URL, "test-token", 5*time.Second, "SnatchArr-test")
	if err != nil {
		t.Fatalf("NewEmby: %v", err)
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
