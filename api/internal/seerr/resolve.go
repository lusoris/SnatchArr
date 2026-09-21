// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package seerr turns Seerr requests into snatch priorities: approved-but-unavailable
// requests are cached, mapped onto SnatchArr instances and library entities, and the
// worker snatches them first. SnatchArr never writes to Seerr.
package seerr

import (
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/seerrclient"
)

// mediaTypeOf reads the media type from the request, falling back to the media record.
func mediaTypeOf(r seerrclient.Request) (domain.SeerrMediaType, bool) {
	for _, t := range []string{r.Type, r.Media.MediaType} {
		switch t {
		case string(domain.SeerrMovie):
			return domain.SeerrMovie, true
		case string(domain.SeerrTV):
			return domain.SeerrTV, true
		}
	}
	return "", false
}

func kindFor(mt domain.SeerrMediaType) domain.AppKind {
	if mt == domain.SeerrMovie {
		return domain.KindRadarr
	}
	return domain.KindSonarr
}

// instanceIndex maps normalised base URLs to instances of a kind family.
type instanceIndex map[string]domain.Instance

func indexInstances(list []domain.Instance) instanceIndex {
	idx := make(instanceIndex, len(list))
	for _, inst := range list {
		if u, err := domain.NormalizeBaseURL(inst.BaseURL); err == nil {
			idx[u] = inst
		}
	}
	return idx
}

// lookup returns the instance behind a Seerr server URL when it has the wanted family.
func (idx instanceIndex) lookup(baseURL string, kind domain.AppKind) *uuid.UUID {
	u, err := domain.NormalizeBaseURL(baseURL)
	if err != nil {
		return nil
	}
	inst, ok := idx[u]
	if !ok || !sameFamily(inst.Kind, kind) {
		return nil
	}
	id := inst.ID
	return &id
}

// sameFamily accepts Whisparr instances that share a Sonarr/Radarr API shape.
func sameFamily(have, want domain.AppKind) bool {
	switch want {
	case domain.KindRadarr:
		return have == domain.KindRadarr || have == domain.KindWhisparrV3
	case domain.KindSonarr:
		return have == domain.KindSonarr || have == domain.KindWhisparrV2
	case domain.KindLidarr, domain.KindReadarr, domain.KindWhisparrV2, domain.KindWhisparrV3:
		return have == want
	default:
		return false
	}
}

// resolveInstance picks the SnatchArr instance a request belongs to: the Seerr server
// the request names (matched by URL), else Seerr's default server for that family and
// 4K flag, else the link's fallback instance.
func resolveInstance(mt domain.SeerrMediaType, r seerrclient.Request, servers []seerrclient.Server, idx instanceIndex, link domain.SeerrLink) *uuid.UUID {
	kind := kindFor(mt)
	if r.ServerID != nil {
		named := func(s seerrclient.Server) bool { return s.ID == *r.ServerID }
		if id := matchServer(servers, idx, kind, r.Is4K, named); id != nil {
			return id
		}
	}
	isDefault := func(s seerrclient.Server) bool { return s.IsDefault }
	if id := matchServer(servers, idx, kind, r.Is4K, isDefault); id != nil {
		return id
	}
	if kind == domain.KindRadarr {
		return link.RadarrInstanceID
	}
	return link.SonarrInstanceID
}

// matchServer returns the instance behind the first server of the family and 4K flag that
// satisfies pick and whose URL is a known instance.
func matchServer(servers []seerrclient.Server, idx instanceIndex, kind domain.AppKind, is4k bool, pick func(seerrclient.Server) bool) *uuid.UUID {
	for _, s := range servers {
		if s.Kind != kind || s.Is4K != is4k || !pick(s) {
			continue
		}
		if id := idx.lookup(s.BaseURL, kind); id != nil {
			return id
		}
	}
	return nil
}

// parseSeerrTime accepts Seerr's ISO timestamps; unknown formats yield nil.
func parseSeerrTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}

func seasonsOf(r seerrclient.Request) []int32 {
	out := make([]int32, 0, len(r.Seasons))
	for _, s := range r.Seasons {
		out = append(out, int32(min(max(s.SeasonNumber, 0), 1<<20))) // #nosec G115 -- clamped
	}
	return out
}
