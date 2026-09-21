// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package arrclient

import (
	"context"
	"fmt"

	"github.com/golusoris/goenvoy/arr/v2"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

// Resolver finds the library entity an external id refers to: a Radarr movie by TMDB id
// or a Sonarr series by TVDB id. found is false when the instance does not have it.
type Resolver interface {
	EntityByExternalID(ctx context.Context, kind domain.AppKind, baseURL, apiKey string, tmdbID, tvdbID int) (id int64, found bool, err error)
}

type idOnly struct {
	ID int64 `json:"id"`
}

// EntityByExternalID implements Resolver with the *arr lookup filters
// (`GET movie?tmdbId=` and `GET series?tvdbId=`), which return the library entry only.
func (p *GoenvoyProber) EntityByExternalID(ctx context.Context, kind domain.AppKind, baseURL, apiKey string, tmdbID, tvdbID int) (int64, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, p.opts.Timeout)
	defer cancel()
	path, err := lookupPath(kind, tmdbID, tvdbID)
	if err != nil {
		return 0, false, err
	}
	c, err := arr.NewBaseClient(baseURL, apiKey, p.clientOptions()...)
	if err != nil {
		return 0, false, fmt.Errorf("arrclient: base client: %w", err)
	}
	var list []idOnly
	if err := c.Get(ctx, path, &list); err != nil {
		return 0, false, fmt.Errorf("%w: %w", domain.ErrUnreachable, err)
	}
	if len(list) == 0 || list[0].ID == 0 {
		return 0, false, nil
	}
	return list[0].ID, true, nil
}

func lookupPath(kind domain.AppKind, tmdbID, tvdbID int) (string, error) {
	switch kind {
	case domain.KindRadarr, domain.KindWhisparrV3:
		if tmdbID <= 0 {
			return "", fmt.Errorf("%w: a TMDB id is required for %s", domain.ErrInvalid, kind)
		}
		return fmt.Sprintf("/api/v3/movie?tmdbId=%d", tmdbID), nil
	case domain.KindSonarr, domain.KindWhisparrV2:
		if tvdbID <= 0 {
			return "", fmt.Errorf("%w: a TVDB id is required for %s", domain.ErrInvalid, kind)
		}
		return fmt.Sprintf("/api/v3/series?tvdbId=%d", tvdbID), nil
	case domain.KindLidarr, domain.KindReadarr:
		return "", fmt.Errorf("%w: %s has no TMDB/TVDB lookup", domain.ErrInvalid, kind)
	default:
		return "", fmt.Errorf("%w: unsupported kind %q", domain.ErrInvalid, kind)
	}
}
