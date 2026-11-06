// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package arrclient_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golusoris/goenvoy/arr/v2"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/foreigntitle"
)

var _ arrclient.ItemTitler = (*arrclient.GoenvoyProber)(nil)

func titleServer(t *testing.T, path, body string, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != path || req.Method != http.MethodGet || req.Header.Get("X-Api-Key") != "test-key" {
			t.Errorf("request = %s %s, API key = %q", req.Method, req.URL.Path, req.Header.Get("X-Api-Key"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestItemTitlesFetch(t *testing.T) {
	t.Parallel()
	one, two := 1, 2
	for _, tc := range []struct {
		name, path, body string
		want             arrclient.ItemTitles
	}{
		{
			"sonarr", "/api/v3/series/7", `{"title":"The Doctors","year":2005,"originalLanguage":{"id":4,"name":"German"},"alternateTitles":[{"title":"Die Ärzte","seasonNumber":1,"sceneSeasonNumber":9},{"title":"Der Arzt","sceneSeasonNumber":2}]}`,
			arrclient.ItemTitles{Kind: domain.KindSonarr, ItemID: 7, Title: "The Doctors", Year: 2005, OriginalLanguage: "German", Aliases: []arrclient.Alias{{Title: "Die Ärzte", SeasonNumber: &one}, {Title: "Der Arzt", SeasonNumber: &two}}},
		},
		{
			"radarr", "/api/v3/movie/603", `{"title":"The Lives of Others","year":2006,"originalTitle":"Das Leben der Anderen","originalLanguage":{"id":4,"name":"German"},"alternateTitles":[{"title":"Das Leben der Anderen","sourceType":"tmdb"},{"title":"La Vie des autres"},{"title":"Lives of Others"}]}`,
			arrclient.ItemTitles{Kind: domain.KindRadarr, ItemID: 603, Title: "The Lives of Others", Year: 2006, OriginalTitle: "Das Leben der Anderen", OriginalLanguage: "German", Aliases: []arrclient.Alias{{Title: "Das Leben der Anderen"}, {Title: "La Vie des autres"}, {Title: "Lives of Others"}}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := titleServer(t, tc.path, tc.body, http.StatusOK)
			p := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: 2 * time.Second})
			got, err := p.ItemTitles(t.Context(), tc.want.Kind, srv.URL, "test-key", tc.want.ItemID)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ItemTitles() = %+v, %v; want %+v", got, err, tc.want)
			}
			assertAliasInput(t, got, tc.want)
		})
	}
}

func assertAliasInput(t *testing.T, got, want arrclient.ItemTitles) {
	t.Helper()
	in := got.AliasInput()
	expected := foreigntitle.AliasInput{Title: want.Title, OriginalTitle: want.OriginalTitle, OriginalLanguage: want.OriginalLanguage}
	for _, alias := range want.Aliases {
		expected.Aliases = append(expected.Aliases, foreigntitle.AliasEntry{Title: alias.Title, SeasonNumber: alias.SeasonNumber})
	}
	if !reflect.DeepEqual(in, expected) {
		t.Fatalf("AliasInput() = %+v; want %+v", in, expected)
	}
}

func TestItemTitlesInvalid(t *testing.T) {
	t.Parallel()
	var count atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	for _, tc := range []struct {
		kind domain.AppKind
		id   int
	}{
		{domain.KindSonarr, 0},
		{domain.KindRadarr, -1},
		{domain.KindSonarr, -1},
		{domain.KindRadarr, 0},
		{domain.KindLidarr, 1},
		{domain.KindReadarr, 1},
		{domain.KindStash, 1},
		{domain.KindWhisparrV2, 1},
		{domain.KindWhisparrV3, 1},
		{domain.AppKind("unknown"), 1},
	} {
		t.Run(fmt.Sprintf("%s_%d", tc.kind, tc.id), func(t *testing.T) {
			t.Parallel()
			p := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: time.Second})
			_, err := p.ItemTitles(t.Context(), tc.kind, srv.URL, "test-key", tc.id)
			if !errors.Is(err, domain.ErrInvalid) || count.Load() != 0 {
				t.Fatalf("error = %v, requests = %d; want ErrInvalid, zero requests", err, count.Load())
			}
		})
	}
}

func TestItemTitlesErrors(t *testing.T) {
	t.Parallel()
	for _, kind := range []domain.AppKind{domain.KindSonarr, domain.KindRadarr} {
		for _, tc := range []struct {
			name, body string
			status     int
			want       error
		}{
			{"server", `{}`, http.StatusInternalServerError, domain.ErrUnreachable},
			{"missing", `{"message":"not found"}`, http.StatusNotFound, domain.ErrNotFound},
			{"empty404", "", http.StatusNotFound, domain.ErrNotFound},
		} {
			t.Run(string(kind)+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				path := "/api/v3/movie/7"
				if kind == domain.KindSonarr {
					path = "/api/v3/series/7"
				}
				srv := titleServer(t, path, tc.body, tc.status)
				p := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: time.Second})
				_, err := p.ItemTitles(t.Context(), kind, srv.URL, "test-key", 7)
				apiErr, ok := errors.AsType[*arr.APIError](err)
				if !errors.Is(err, tc.want) || !ok || apiErr.StatusCode != tc.status {
					t.Fatalf("error = %v; want %v and API status %d", err, tc.want, tc.status)
				}
				if tc.name == "empty404" && !strings.Contains(err.Error(), "base URL does not point at "+string(kind)) {
					t.Fatalf("missing empty-404 hint: %v", err)
				}
			})
		}
	}
}

func TestItemTitlesBodyCap(t *testing.T) {
	t.Parallel()
	const capBytes = 4 << 20
	for _, kind := range []domain.AppKind{domain.KindSonarr, domain.KindRadarr} {
		for _, size := range []int{capBytes - 1, capBytes, capBytes + 1} {
			t.Run(fmt.Sprintf("%s/%d", kind, size), func(t *testing.T) {
				t.Parallel()
				const item = `{"title":"Canonical"}`
				body := item + strings.Repeat(" ", size-len(item))
				path := "/api/v3/movie/7"
				if kind == domain.KindSonarr {
					path = "/api/v3/series/7"
				}
				srv := titleServer(t, path, body, http.StatusOK)
				p := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: 5 * time.Second})
				got, err := p.ItemTitles(t.Context(), kind, srv.URL, "test-key", 7)
				if size > capBytes {
					if !errors.Is(err, domain.ErrUnreachable) {
						t.Fatalf("error = %v; want ErrUnreachable for oversized body", err)
					}
				} else if err != nil || got.Title != "Canonical" {
					t.Fatalf("ItemTitles() = %+v, %v", got, err)
				}
			})
		}
	}
}

func TestItemTitlesAliasBounds(t *testing.T) {
	t.Parallel()
	for _, kind := range []domain.AppKind{domain.KindSonarr, domain.KindRadarr} {
		for _, count := range []int{0, 199, 200, 201} {
			t.Run(fmt.Sprintf("%s/%d", kind, count), func(t *testing.T) {
				t.Parallel()
				aliases := make([]string, 0, count+1)
				aliases = append(aliases, `{"title":" \t "}`)
				for i := range count {
					aliases = append(aliases, fmt.Sprintf(`{"title":" Alias %d "}`, i))
				}
				body := `{"alternateTitles":[` + strings.Join(aliases, ",") + `]}`
				path := "/api/v3/movie/7"
				if kind == domain.KindSonarr {
					path = "/api/v3/series/7"
				}
				srv := titleServer(t, path, body, http.StatusOK)
				p := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: time.Second})
				got, err := p.ItemTitles(t.Context(), kind, srv.URL, "test-key", 7)
				if err != nil || len(got.Aliases) != min(count, 200) {
					t.Fatalf("aliases = %d, %v; want %d", len(got.Aliases), err, min(count, 200))
				}
				for i, alias := range got.Aliases {
					if alias.Title != fmt.Sprintf("Alias %d", i) || alias.SeasonNumber != nil {
						t.Fatalf("alias %d = %+v", i, alias)
					}
				}
			})
		}
	}
}

func TestItemTitlesSonarrLanguageName(t *testing.T) {
	t.Parallel()
	// Sonarr Thai is 32, while Radarr Thai is 28: cross-app ID lookup loses this name.
	srv := titleServer(t, "/api/v3/series/7", `{"title":"Series","originalLanguage":{"id":32,"name":"Thai"}}`, http.StatusOK)
	p := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: time.Second})
	got, err := p.ItemTitles(t.Context(), domain.KindSonarr, srv.URL, "test-key", 7)
	if err != nil || !strings.EqualFold(got.OriginalLanguage, (foreigntitle.Rules{Name: "Thai"}).Name) {
		t.Fatalf("original language = %q, %v; want Thai by name", got.OriginalLanguage, err)
	}
	assertAliasInput(t, got, arrclient.ItemTitles{Title: "Series", OriginalLanguage: "Thai"})
}
