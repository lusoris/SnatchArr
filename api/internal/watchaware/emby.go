// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package watchaware

import (
	"context"
	"fmt"
	"time"

	"github.com/golusoris/goenvoy/mediaserver/emby/v3"
)

// EmbySource adapts goenvoy's Emby client to the WatchSource interface.
type EmbySource struct {
	client *emby.Client
}

// NewEmby creates a new Emby WatchSource.
func NewEmby(baseURL, apiKey string, timeout time.Duration, ua string) (*EmbySource, error) {
	cl, err := emby.New(
		baseURL,
		emby.WithAccessToken(apiKey),
		emby.WithTimeout(timeout),
		emby.WithUserAgent(ua),
	)
	if err != nil {
		return nil, fmt.Errorf("watchaware: init emby client: %w", err)
	}
	return &EmbySource{client: cl}, nil
}

// Kind returns KindEmby.
func (s *EmbySource) Kind() Kind {
	return KindEmby
}

// Watches fetches watch state across all users from Emby.
func (s *EmbySource) Watches(ctx context.Context) ([]Watch, error) {
	return fetchPagedWatches(ctx, KindEmby, s.getUsers, s.fetchPage)
}

func (s *EmbySource) getUsers(ctx context.Context) ([]userRecord, error) {
	users, err := s.client.GetUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("watchaware: emby get users: %w", err)
	}
	res := make([]userRecord, len(users))
	for i, u := range users {
		res[i] = userRecord{ID: u.ID, Name: u.Name}
	}
	return res, nil
}

func (s *EmbySource) fetchPage(ctx context.Context, userID, userName string, startIndex, limit int) ([]Watch, int, error) {
	params := buildItemParams(userID, startIndex, limit)
	result, err := s.client.GetItems(ctx, params)
	if err != nil {
		return nil, 0, fmt.Errorf("watchaware: emby get items: %w", err)
	}
	watches := make([]Watch, len(result.Items))
	for i, it := range result.Items {
		watches[i] = newWatch(userID, userName, itemRef{
			ID: it.ID, Type: it.Type, SeriesID: it.SeriesID,
			Season: it.ParentIndexNumber, Episode: it.IndexNumber,
			UserData: (*userData)(it.UserData),
			TvdbID:   extractTvdbID(it.ProviderIDs),
		})
	}
	return watches, result.TotalRecordCount, nil
}
