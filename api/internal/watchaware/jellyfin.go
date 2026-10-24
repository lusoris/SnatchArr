// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package watchaware

import (
	"context"
	"fmt"
	"time"

	"github.com/golusoris/goenvoy/mediaserver/jellyfin/v3"
)

// JellyfinSource adapts goenvoy's Jellyfin client to the WatchSource interface.
type JellyfinSource struct {
	client *jellyfin.Client
}

// NewJellyfin creates a new Jellyfin WatchSource.
func NewJellyfin(baseURL, apiKey string, timeout time.Duration, ua string) (*JellyfinSource, error) {
	cl, err := jellyfin.New(
		baseURL,
		jellyfin.WithAccessToken(apiKey),
		jellyfin.WithTimeout(timeout),
		jellyfin.WithUserAgent(ua),
	)
	if err != nil {
		return nil, fmt.Errorf("watchaware: init jellyfin client: %w", err)
	}
	return &JellyfinSource{client: cl}, nil
}

// Kind returns KindJellyfin.
func (s *JellyfinSource) Kind() Kind {
	return KindJellyfin
}

// Watches fetches watch state across all users from Jellyfin.
func (s *JellyfinSource) Watches(ctx context.Context) ([]Watch, error) {
	return fetchPagedWatches(ctx, KindJellyfin, s.getUsers, s.fetchPage)
}

func (s *JellyfinSource) getUsers(ctx context.Context) ([]userRecord, error) {
	users, err := s.client.GetUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("watchaware: jellyfin get users: %w", err)
	}
	res := make([]userRecord, len(users))
	for i, u := range users {
		res[i] = userRecord{ID: u.ID, Name: u.Name}
	}
	return res, nil
}

func (s *JellyfinSource) fetchPage(ctx context.Context, userID, userName string, startIndex, limit int) ([]Watch, int, error) {
	params := buildItemParams(userID, startIndex, limit)
	result, err := s.client.GetItems(ctx, params)
	if err != nil {
		return nil, 0, fmt.Errorf("watchaware: jellyfin get items: %w", err)
	}
	watches := make([]Watch, len(result.Items))
	for i, it := range result.Items {
		watches[i] = mapJellyfinItem(userID, userName, it)
	}
	return watches, result.TotalRecordCount, nil
}

func mapJellyfinItem(userID, userName string, item jellyfin.BaseItemDto) Watch {
	var (
		played           bool
		playedPercentage *float64
		positionTicks    int64
		lastPlayed       string
	)
	if item.UserData != nil {
		played = item.UserData.Played
		playedPercentage = item.UserData.PlayedPercentage
		positionTicks = item.UserData.PlaybackPositionTicks
		lastPlayed = item.UserData.LastPlayedDate
	}
	return Watch{
		UserID:           userID,
		UserName:         userName,
		ItemID:           item.ID,
		ItemType:         item.Type,
		SeriesID:         item.SeriesID,
		SeasonNumber:     item.ParentIndexNumber,
		EpisodeNumber:    item.IndexNumber,
		Played:           played,
		PlayedPercentage: playedPercentage,
		PositionTicks:    positionTicks,
		LastPlayed:       lastPlayed,
	}
}
