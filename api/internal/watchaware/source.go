// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package watchaware provides media-server watch-state synchronization and
// predicates for SnatchArr's watch-aware modes (ADR-0024). It defines the
// WatchSource interface, adapters for Jellyfin and Emby, and the sync engine.
package watchaware

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
)

// Bounds (HISS-02).
const (
	pageSize   = 200
	maxPages   = 50
	maxUsers   = 100
	maxSources = 32
)

// ErrTruncated reports a user library larger than maxPages*pageSize items. The sync
// fails closed for that source instead of storing a partial watch set, which the
// season gate would read as unwatched episodes.
var ErrTruncated = errors.New("watchaware: item listing exceeds page bound")

// Kind identifies the media server provider.
type Kind string

const (
	// KindJellyfin identifies a Jellyfin media server.
	KindJellyfin Kind = "jellyfin"
	// KindEmby identifies an Emby media server.
	KindEmby Kind = "emby"
)

// Watch is one user's watch state for a media item.
type Watch struct {
	UserID           string
	UserName         string
	ItemID           string
	ItemType         string // "Episode" | "Movie"
	SeriesID         string
	SeasonNumber     int
	EpisodeNumber    int
	Played           bool
	PlayedPercentage *float64
	PositionTicks    int64
	LastPlayed       string
}

// WatchSource fetches watch state from a media server.
type WatchSource interface {
	Kind() Kind
	Watches(ctx context.Context) ([]Watch, error)
}

// userRecord holds basic identification for a media-server user.
type userRecord struct {
	ID   string
	Name string
}

// userData mirrors goenvoy's UserItemDataDto, which Jellyfin and Emby define with the
// same fields; each adapter converts its own pointer type to *userData.
type userData struct {
	PlayCount             int
	Played                bool
	PlayedPercentage      *float64
	IsFavorite            bool
	PlaybackPositionTicks int64
	LastPlayedDate        string
}

// itemRef is the part of a media-server item a Watch reads.
type itemRef struct {
	ID, Type, SeriesID string
	Season, Episode    int
	UserData           *userData
}

func newWatch(userID, userName string, it itemRef) Watch {
	w := Watch{
		UserID:        userID,
		UserName:      userName,
		ItemID:        it.ID,
		ItemType:      it.Type,
		SeriesID:      it.SeriesID,
		SeasonNumber:  it.Season,
		EpisodeNumber: it.Episode,
	}
	if ud := it.UserData; ud != nil {
		w.Played = ud.Played
		w.PlayedPercentage = ud.PlayedPercentage
		w.PositionTicks = ud.PlaybackPositionTicks
		w.LastPlayed = ud.LastPlayedDate
	}
	return w
}

func buildItemParams(userID string, startIndex, limit int) url.Values {
	return url.Values{
		"userId":           {userID},
		"Recursive":        {"true"},
		"IncludeItemTypes": {"Episode,Movie"},
		"EnableUserData":   {"true"},
		"StartIndex":       {strconv.Itoa(startIndex)},
		"Limit":            {strconv.Itoa(limit)},
	}
}

// fetchPagedWatches iterates through all users (bounded by maxUsers) and pages
// through their media items (bounded by maxPages per user).
//
// In accordance with HISS-02:
// - Paging stops at maxPages; when more items remain the call returns ErrTruncated
// and no partial result.
// - Every I/O call obeys the caller-supplied ctx deadline.
func fetchPagedWatches(
	ctx context.Context,
	kind Kind,
	getUsers func(ctx context.Context) ([]userRecord, error),
	fetchPage func(ctx context.Context, userID, userName string, startIndex, limit int) ([]Watch, int, error),
) ([]Watch, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("watchaware: %s context: %w", kind, err)
	}
	users, err := getUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("watchaware: %s get users: %w", kind, err)
	}
	if len(users) > maxUsers {
		users = users[:maxUsers]
	}

	var allWatches []Watch
	for _, u := range users {
		ws, err := fetchUserWatches(ctx, kind, u, fetchPage)
		if err != nil {
			return nil, err
		}
		allWatches = append(allWatches, ws...)
	}
	return allWatches, nil
}

func fetchUserWatches(
	ctx context.Context,
	kind Kind,
	u userRecord,
	fetchPage func(ctx context.Context, userID, userName string, startIndex, limit int) ([]Watch, int, error),
) ([]Watch, error) {
	var userWatches []Watch
	for page := range maxPages {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("watchaware: %s get items user %s: %w", kind, u.ID, err)
		}
		startIndex := page * pageSize
		ws, total, err := fetchPage(ctx, u.ID, u.Name, startIndex, pageSize)
		if err != nil {
			return nil, fmt.Errorf("watchaware: %s get items user %s: %w", kind, u.ID, err)
		}
		userWatches = append(userWatches, ws...)
		if len(ws) < pageSize || startIndex+len(ws) >= total {
			return userWatches, nil
		}
	}
	return nil, fmt.Errorf("watchaware: %s user %s: %w", kind, u.ID, ErrTruncated)
}
