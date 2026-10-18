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
	radarrDefault := domain.Instance{ID: uuid.New(), Kind: domain.KindRadarr, BaseURL: "http://radarr-main.lan:7878"}
	sonarr := domain.Instance{ID: uuid.New(), Kind: domain.KindSonarr, BaseURL: "http://sonarr.lan:8989/sonarr"}
	idx := indexInstances([]domain.Instance{radarr, radarr4k, radarrDefault, sonarr})
	servers := []seerrclient.Server{
		// Server 0 is not the default, so a request naming it only resolves through its id.
		{Kind: domain.KindRadarr, ID: 0, BaseURL: "http://RADARR.lan:7878/"},
		{Kind: domain.KindRadarr, ID: 2, BaseURL: "http://radarr-main.lan:7878", IsDefault: true},
		{Kind: domain.KindRadarr, ID: 1, BaseURL: "http://radarr4k.lan:7878", Is4K: true, IsDefault: true},
		{Kind: domain.KindSonarr, ID: 0, BaseURL: "http://sonarr.lan:8989/sonarr/", IsDefault: true},
	}
	fallback := domain.Instance{ID: uuid.New()}
	link := domain.SeerrLink{SonarrInstanceID: &fallback.ID, RadarrInstanceID: &fallback.ID}
	cases := []struct {
		name string
		mt   domain.SeerrMediaType
		req  seerrclient.Request
		want uuid.UUID
	}{
		{"movie by server id, case-insensitive url", domain.SeerrMovie, seerrclient.Request{ServerAssigned: true}, radarr.ID},
		{"4k movie by server id", domain.SeerrMovie, seerrclient.Request{ServerID: 1, ServerAssigned: true, Is4k: true}, radarr4k.ID},
		{"movie without server id uses the default", domain.SeerrMovie, seerrclient.Request{}, radarrDefault.ID},
		{"server id 0 without the assigned flag uses the default", domain.SeerrMovie, seerrclient.Request{ServerID: 0}, radarrDefault.ID},
		{"tv default server with base path", domain.SeerrTV, seerrclient.Request{}, sonarr.ID},
		{"unknown 4k tv falls back to the link", domain.SeerrTV, seerrclient.Request{Is4k: true}, fallback.ID},
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

// Positive, negative, boundary: only the exact Seerr values "movie" and "tv" map, the request
// type wins over the media record, and an unknown request type falls back to the record.
func TestMediaTypeOf(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		reqType string
		media   *seerrclient.Media
		want    domain.SeerrMediaType
		wantOK  bool
	}{
		{name: "movie request", reqType: "movie", want: domain.SeerrMovie, wantOK: true},
		{name: "tv from the media record", media: &seerrclient.Media{MediaType: "tv"}, want: domain.SeerrTV, wantOK: true},
		{name: "request type wins", reqType: "movie", media: &seerrclient.Media{MediaType: "tv"}, want: domain.SeerrMovie, wantOK: true},
		{name: "unknown request type falls back", reqType: "anime", media: &seerrclient.Media{MediaType: "movie"}, want: domain.SeerrMovie, wantOK: true},
		// Negative.
		{name: "empty request"},
		{name: "unknown type", reqType: "anime", media: &seerrclient.Media{MediaType: "music"}},
		// Boundary: the match is exact, not case- or space-folded.
		{name: "upper case", reqType: "Movie"},
		{name: "padded", reqType: " tv"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mt, ok := mediaTypeOf(seerrclient.Request{Type: tc.reqType, Media: tc.media})
			if mt != tc.want || ok != tc.wantOK {
				t.Fatalf("mediaTypeOf = %q %v, want %q %v", mt, ok, tc.want, tc.wantOK)
			}
		})
	}
}

// A movie request goes to the Radarr family, a tv request to the Sonarr family.
func TestKindFor(t *testing.T) {
	t.Parallel()
	if got := kindFor(domain.SeerrMovie); got != domain.KindRadarr {
		t.Fatalf("kindFor(movie) = %q, want radarr", got)
	}
	if got := kindFor(domain.SeerrTV); got != domain.KindSonarr {
		t.Fatalf("kindFor(tv) = %q, want sonarr", got)
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
	if got := seasonsOf(seerrclient.Request{}); got == nil || len(got) != 0 {
		t.Fatalf("a request without seasons must give an empty list, got %#v", got)
	}
}
