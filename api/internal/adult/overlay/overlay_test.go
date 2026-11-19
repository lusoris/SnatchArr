// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package overlay_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/golusoris/goenvoy/arr/whisparr"

	"github.com/lusoris/SnatchArr/api/internal/adult/overlay"
)

var defaultOptions = overlay.Options{SourceRoot: "/source", TargetRoot: "/source"}

func TestBuildImport(t *testing.T) {
	t.Parallel()
	a := movie(1, "Alpha", 2020, "/source/a/one.mkv")
	b := movie(2, "Beta", 2021, "/source/b/two.mkv")
	before := build(t, a)
	after := build(t, a, b)
	if entryByID(t, before, 1) != entryByID(t, after, 1) {
		t.Fatal("adding movie B changed movie A")
	}
	if got := entryByID(t, after, 2); got.Folder != "Beta (2021)" {
		t.Fatalf("movie B folder = %q", got.Folder)
	}
}

func TestBuildRename(t *testing.T) {
	t.Parallel()
	before := build(t, movie(1, "Alpha", 2020, "/source/a/one.mkv"))
	after := build(t, movie(1, "Alpha", 2020, "/source/new/one.mkv"))
	oldEntry := entryByID(t, before, 1)
	newEntry := entryByID(t, after, 1)
	if oldEntry.Folder != newEntry.Folder || oldEntry.Link != newEntry.Link || oldEntry.Target == newEntry.Target {
		t.Fatalf("rename entries = %#v and %#v", oldEntry, newEntry)
	}
}

func TestBuildDelete(t *testing.T) {
	t.Parallel()
	a := movie(1, "Alpha", 2020, "/source/a/one.mkv")
	b := movie(2, "Beta", 2020, "/source/b/two.mkv")
	before := build(t, a, b)
	deleteCases := []struct {
		name   string
		movies []whisparr.Movie
	}{
		{name: "has file false", movies: []whisparr.Movie{{ID: 1, HasFile: false}, b}},
		{name: "movie file nil", movies: []whisparr.Movie{{ID: 1, HasFile: true}, b}},
		{name: "movie absent", movies: []whisparr.Movie{b}},
	}
	for _, tc := range deleteCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			after := build(t, tc.movies...)
			if hasEntry(after, 1) || entryByID(t, before, 2) != entryByID(t, after, 2) {
				t.Fatalf("delete map = %#v", after)
			}
		})
	}
}

func TestBuildUpgrade(t *testing.T) {
	t.Parallel()
	before := movie(1, "Alpha", 2020, "/source/a/one.mkv")
	after := movie(1, "Alpha", 2020, "/source/new/one.mp4")
	after.MovieFile.ID = 12
	oldEntry := entryByID(t, build(t, before), 1)
	newEntry := entryByID(t, build(t, after), 1)
	if oldEntry.Folder != newEntry.Folder || oldEntry.Link == newEntry.Link || oldEntry.Target == newEntry.Target {
		t.Fatalf("upgrade entries = %#v and %#v", oldEntry, newEntry)
	}
	sameExtension := after
	sameExtension.MovieFile.Path = "/source/newer/one.mp4"
	sameExtension.MovieFile.ID = 13
	if entryByID(t, build(t, after), 1).Link != entryByID(t, build(t, sameExtension), 1).Link {
		t.Fatal("same-extension upgrade changed link")
	}
}

func TestBuildCollision(t *testing.T) {
	t.Parallel()
	five := movie(5, "Same", 2020, "/source/five/a.mkv")
	nine := movie(9, "Same", 2020, "/source/nine/a.mkv")
	twelve := movie(12, "Same", 2020, "/source/twelve/a.mkv")
	initial := build(t, five, nine)
	if entryByID(t, initial, 5).Folder != "Same (2020)" || entryByID(t, initial, 9).Folder != "Same (2020) [whisparr-9]" {
		t.Fatalf("initial collision map = %#v", initial)
	}
	withTwelve := build(t, five, nine, twelve)
	if entryByID(t, initial, 5) != entryByID(t, withTwelve, 5) || entryByID(t, initial, 9) != entryByID(t, withTwelve, 9) {
		t.Fatal("adding a collision changed an existing entry")
	}
	caseVariant := movie(13, "same", 2020, "/source/case/a.mkv")
	if entryByID(t, build(t, five, caseVariant), 13).Folder != "same (2020) [whisparr-13]" {
		t.Fatal("case variants did not collide")
	}
	if entryByID(t, build(t, nine, twelve), 9).Folder != "Same (2020)" {
		t.Fatal("deleting the plain collision holder did not promote movie 9")
	}
}

func TestBuildOutsideRoot(t *testing.T) {
	t.Parallel()
	cases := []struct {
		id   int
		path string
	}{
		{id: 1, path: "/source/../other/one.mkv"},
		{id: 2, path: "/source2/two.mkv"},
		{id: 3, path: "relative/three.mkv"},
		{id: 4, path: "/source"},
		{id: 5, path: "/source/\x00/five.mkv"},
	}
	movies := make([]whisparr.Movie, 0, len(cases))
	for _, tc := range cases {
		movies = append(movies, movie(tc.id, "Title", 2020, tc.path))
	}
	result := build(t, movies...)
	if len(result.Entries) != 0 || len(result.Refused) != len(cases) {
		t.Fatalf("outside-root result = %#v", result)
	}
	for _, refusal := range result.Refused {
		if refusal.Reason != overlay.ReasonOutsideSourceRoot {
			t.Fatalf("refusal = %#v, want containment reason", refusal)
		}
	}
}

func TestBuildRootMapping(t *testing.T) {
	t.Parallel()
	opts := overlay.Options{SourceRoot: "/source", TargetRoot: "/jellyfin-view"}
	result, err := overlay.Build(opts, []whisparr.Movie{movie(1, "Alpha", 2020, "/source/a/one.mkv")})
	if err != nil {
		t.Fatal(err)
	}
	if got := entryByID(t, result, 1).Target; got != "/jellyfin-view/a/one.mkv" {
		t.Fatalf("target = %q", got)
	}
}

func TestBuildTitleSanitize(t *testing.T) {
	t.Parallel()
	sanitized := entryByID(t, build(t, movie(1, ".. Alpha/Beta\\[Tag]\x01 ..", 2020, "/source/a/one.mkv")), 1)
	if sanitized.Folder != "Alpha Beta (Tag) (2020)" {
		t.Fatalf("sanitized folder = %q", sanitized.Folder)
	}
	long := entryByID(t, build(t, movie(2, strings.Repeat("é", 150), 0, "/source/a/two.mkv")), 2)
	if len(long.Folder) > 180 || !utf8.ValidString(long.Folder) {
		t.Fatalf("long folder is not a <=180-byte UTF-8 title: %q", long.Folder)
	}
	empty := build(t, movie(3, ".. \t\n", 2020, "/source/a/three.mkv"))
	if len(empty.Refused) != 1 || empty.Refused[0].Reason != overlay.ReasonEmptyTitle {
		t.Fatalf("empty title result = %#v", empty)
	}
}

func TestBuildYear(t *testing.T) {
	t.Parallel()
	result := build(t,
		movie(1, "Low", 1899, "/source/a/one.mkv"),
		movie(2, "First", 1900, "/source/a/two.mkv"),
		movie(3, "Last", 2099, "/source/a/three.mkv"),
		movie(4, "High", 2100, "/source/a/four.mkv"),
		withReleaseDate(movie(5, "Release", 0, "/source/a/five.mkv"), "2001-02-03"),
		withReleaseDate(movie(6, "Bad", 0, "/source/a/six.mkv"), "nope"),
	)
	want := map[int]string{1: "Low", 2: "First (1900)", 3: "Last (2099)", 4: "High", 5: "Release (2001)", 6: "Bad"}
	for id, folder := range want {
		if got := entryByID(t, result, id).Folder; got != folder {
			t.Fatalf("movie %d folder = %q, want %q", id, got, folder)
		}
	}
}

func TestBuildDeterministic(t *testing.T) {
	t.Parallel()
	forward := []whisparr.Movie{movie(2, "Zulu", 2020, "/source/z/two.mkv"), movie(1, "Alpha", 2020, "/source/a/one.mkv")}
	reverse := []whisparr.Movie{forward[1], forward[0]}
	first := build(t, forward...)
	if !reflect.DeepEqual(first, build(t, reverse...)) || !reflect.DeepEqual(first, build(t, forward...)) {
		t.Fatal("Build is not deterministic across input order and repeated calls")
	}
}

func TestBuildOptions(t *testing.T) {
	t.Parallel()
	cases := []overlay.Options{{SourceRoot: "source", TargetRoot: "/target"}, {SourceRoot: "/source", TargetRoot: "target"}, {SourceRoot: "", TargetRoot: "/target"}, {SourceRoot: "/source", TargetRoot: ""}}
	for _, opts := range cases {
		if _, err := overlay.Build(opts, nil); !errors.Is(err, overlay.ErrInvalidOptions) {
			t.Fatalf("Build(%#v) error = %v", opts, err)
		}
	}
}

func TestBuildDuplicateID(t *testing.T) {
	t.Parallel()
	first := movie(7, "First", 2020, "/source/a/one.mkv")
	second := movie(7, "Second", 2020, "/source/a/two.mkv")
	result := build(t, first, second)
	if entryByID(t, result, 7).Folder != "First (2020)" || len(result.Refused) != 1 || result.Refused[0].Reason != overlay.ReasonDuplicateMovieID {
		t.Fatalf("duplicate result = %#v", result)
	}
}

func TestBuildFilePathRules(t *testing.T) {
	t.Parallel()
	fallback := movie(1, "Fallback", 2020, "")
	fallback.Path = "/source/fallback"
	fallback.MovieFile.RelativePath = "one.mkv"
	if got := entryByID(t, build(t, fallback), 1).Target; got != "/source/fallback/one.mkv" {
		t.Fatalf("fallback target = %q", got)
	}
	missing := movie(2, "Missing", 2020, "")
	if got := build(t, missing).Refused[0].Reason; got != overlay.ReasonMissingPath {
		t.Fatalf("missing path reason = %q", got)
	}
	if got := build(t, movie(3, "No Extension", 2020, "/source/no-extension")).Refused[0].Reason; got != overlay.ReasonNoExtension {
		t.Fatalf("extension reason = %q", got)
	}
}

func movie(id int, title string, year int, path string) whisparr.Movie {
	return whisparr.Movie{ID: id, Title: title, Year: year, HasFile: true, MovieFile: &whisparr.MovieFile{ID: id, MovieID: id, Path: path}}
}

func withReleaseDate(value whisparr.Movie, date string) whisparr.Movie {
	value.ReleaseDate = date
	return value
}

func build(t *testing.T, movies ...whisparr.Movie) overlay.Map {
	t.Helper()
	result, err := overlay.Build(defaultOptions, movies)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func entryByID(t *testing.T, result overlay.Map, id int) overlay.Entry {
	t.Helper()
	for _, entry := range result.Entries {
		if entry.MovieID == id {
			return entry
		}
	}
	t.Fatalf("movie %d missing from %#v", id, result)
	return overlay.Entry{}
}

func hasEntry(result overlay.Map, id int) bool {
	for _, entry := range result.Entries {
		if entry.MovieID == id {
			return true
		}
	}
	return false
}
