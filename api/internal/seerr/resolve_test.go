// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package seerr

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/seerrclient"
)

func TestResolveInstance(t *testing.T) {
	t.Parallel()
	radarr := domain.Instance{ID: uuid.New(), Kind: domain.KindRadarr, BaseURL: "http://radarr.lan:7878"}
	radarr4k := domain.Instance{ID: uuid.New(), Kind: domain.KindRadarr, BaseURL: "http://radarr4k.lan:7878"}
	sonarr := domain.Instance{ID: uuid.New(), Kind: domain.KindSonarr, BaseURL: "http://sonarr.lan:8989/sonarr"}
	idx := indexInstances([]domain.Instance{radarr, radarr4k, sonarr})
	servers := []seerrclient.Server{
		{Kind: domain.KindRadarr, ID: 0, BaseURL: "http://RADARR.lan:7878/", IsDefault: true},
		{Kind: domain.KindRadarr, ID: 1, BaseURL: "http://radarr4k.lan:7878", Is4K: true, IsDefault: true},
		{Kind: domain.KindSonarr, ID: 0, BaseURL: "http://sonarr.lan:8989/sonarr/", IsDefault: true},
	}
	fallback := domain.Instance{ID: uuid.New()}
	link := domain.SeerrLink{SonarrInstanceID: &fallback.ID, RadarrInstanceID: &fallback.ID}
	one := 1
	cases := []struct {
		name string
		mt   domain.SeerrMediaType
		req  seerrclient.Request
		want uuid.UUID
	}{
		{"movie by server id, case-insensitive url", domain.SeerrMovie, seerrclient.Request{ServerID: new(0)}, radarr.ID},
		{"4k movie by server id", domain.SeerrMovie, seerrclient.Request{ServerID: &one, Is4K: true}, radarr4k.ID},
		{"movie without server id uses the default", domain.SeerrMovie, seerrclient.Request{}, radarr.ID},
		{"tv default server with base path", domain.SeerrTV, seerrclient.Request{}, sonarr.ID},
		{"unknown 4k tv falls back to the link", domain.SeerrTV, seerrclient.Request{Is4K: true}, fallback.ID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := resolveInstance(tc.mt, tc.req, servers, idx, link)
			if got == nil || *got != tc.want {
				t.Fatalf("got %v, want %s", got, tc.want)
			}
		})
	}
	if got := resolveInstance(domain.SeerrMovie, seerrclient.Request{}, nil, idx, domain.SeerrLink{}); got != nil {
		t.Fatalf("no servers and no fallback must stay unresolved, got %v", got)
	}
}

func TestMediaTypeOf(t *testing.T) {
	t.Parallel()
	var r seerrclient.Request
	if _, ok := mediaTypeOf(r); ok {
		t.Fatal("empty request has no media type")
	}
	r.Media.MediaType = "tv"
	if mt, ok := mediaTypeOf(r); !ok || mt != domain.SeerrTV {
		t.Fatalf("got %q %v", mt, ok)
	}
	r.Type = "movie"
	if mt, _ := mediaTypeOf(r); mt != domain.SeerrMovie {
		t.Fatal("the request type wins over the media record")
	}
}

func TestParseSeerrTimeAndSeasons(t *testing.T) {
	t.Parallel()
	if parseSeerrTime("") != nil || parseSeerrTime("yesterday") != nil {
		t.Fatal("unparseable times must be nil")
	}
	if ts := parseSeerrTime("2026-09-20T10:00:00.000Z"); ts == nil || ts.Year() != 2026 {
		t.Fatalf("got %v", ts)
	}
	var r seerrclient.Request
	if err := json.Unmarshal([]byte(`{"seasons":[{"seasonNumber":2},{"seasonNumber":-1}]}`), &r); err != nil {
		t.Fatal(err)
	}
	if got := seasonsOf(r); len(got) != 2 || got[0] != 2 || got[1] != 0 {
		t.Fatalf("got %v", got)
	}
}
