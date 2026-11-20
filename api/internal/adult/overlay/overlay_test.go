// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package overlay_test

import (
	"errors"
	"math"
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
	sameExtensionFile := *after.MovieFile
	sameExtensionFile.Path = "/source/newer/one.mp4"
	sameExtensionFile.ID = 13
	sameExtension.MovieFile = &sameExtensionFile
	sameExtensionEntry := entryByID(t, build(t, sameExtension), 1)
	if newEntry.Link != sameExtensionEntry.Link || newEntry.Target == sameExtensionEntry.Target {
		t.Fatalf("same-extension upgrade entries = %#v and %#v", newEntry, sameExtensionEntry)
	}
}

func TestBuildCollision(t *testing.T) {
	t.Parallel()
	five := movie(5, "Same", 2020, "/source/five/a.mkv")
	nine := movie(9, "Same", 2020, "/source/nine/a.mkv")
	twelve := movie(12, "Same", 2020, "/source/twelve/a.mkv")
	initial := build(t, five, nine)
	assertFoldUnique(t, initial)
	if entryByID(t, initial, 5).Folder != "Same (2020)" || entryByID(t, initial, 9).Folder != "Same (2020) [whisparr-9]" {
		t.Fatalf("initial collision map = %#v", initial)
	}
	if entryByID(t, initial, 5).Link != "Same (2020).mkv" || entryByID(t, initial, 9).Link != "Same (2020) [whisparr-9].mkv" {
		t.Fatalf("initial collision links = %#v", initial)
	}
	withTwelve := build(t, five, nine, twelve)
	assertFoldUnique(t, withTwelve)
	if entryByID(t, initial, 5) != entryByID(t, withTwelve, 5) || entryByID(t, initial, 9) != entryByID(t, withTwelve, 9) {
		t.Fatal("adding a collision changed an existing entry")
	}
	reversed := build(t, nine, five)
	assertFoldUnique(t, reversed)
	if !reflect.DeepEqual(reversed, initial) {
		t.Fatalf("reversed collision map = %#v, want %#v", reversed, initial)
	}
	reordered := build(t, twelve, nine, five)
	assertFoldUnique(t, reordered)
	if !reflect.DeepEqual(reordered, withTwelve) {
		t.Fatalf("reordered collision map = %#v, want %#v", reordered, withTwelve)
	}
	caseVariant := movie(13, "same", 2020, "/source/case/a.mkv")
	caseMap := build(t, five, caseVariant)
	assertFoldUnique(t, caseMap)
	if entryByID(t, caseMap, 13).Folder != "same (2020) [whisparr-13]" {
		t.Fatal("case variants did not collide")
	}
	deleteMap := build(t, nine, twelve)
	assertFoldUnique(t, deleteMap)
	if entryByID(t, deleteMap, 9).Folder != "Same (2020)" {
		t.Fatal("deleting the plain collision holder did not promote movie 9")
	}
}

func TestBuildUnicodeCollision(t *testing.T) {
	t.Parallel()
	result := build(t,
		movie(1, "Σ", 2020, "/source/one/a.mkv"),
		movie(2, "σ", 2020, "/source/two/a.mkv"),
		movie(3, "ς", 2020, "/source/three/a.mkv"),
		movie(4, "Straße", 2020, "/source/four/a.mkv"),
		movie(5, "STRASSE", 2020, "/source/five/a.mkv"),
		movie(6, "é", 2020, "/source/six/a.mkv"),
		movie(7, "e\u0301", 2020, "/source/seven/a.mkv"),
		movie(8, "K", 2020, "/source/eight/a.mkv"),
		movie(9, "k", 2020, "/source/nine/a.mkv"),
	)
	assertFoldUnique(t, result)
	want := map[int]string{
		1: "Σ (2020)", 2: "σ (2020) [whisparr-2]", 3: "ς (2020) [whisparr-3]",
		4: "Straße (2020)", 5: "STRASSE (2020) [whisparr-5]",
		6: "é (2020)", 7: "e\u0301 (2020) [whisparr-7]",
		8: "K (2020)", 9: "k (2020) [whisparr-9]",
	}
	for id, folder := range want {
		if got := entryByID(t, result, id).Folder; got != folder {
			t.Errorf("movie %d folder = %q, want %q", id, got, folder)
		}
	}
}

func TestBuildWindowsNames(t *testing.T) {
	t.Parallel()
	result := build(t,
		movie(1, "Title: Sub*Part?", 2020, "/source/one/a.mkv"),
		movie(2, `a<b>c"d|e`, 0, "/source/two/a.mkv"),
		movie(4, "Con", 0, "/source/four/a.mkv"),
		movie(5, "con.Air", 0, "/source/five/a.mkv"),
		movie(6, "LPT¹", 0, "/source/six/a.mkv"),
		movie(7, "Con", 2020, "/source/seven/a.mkv"),
		movie(8, "Console", 0, "/source/eight/a.mkv"),
		movie(10, "nul", 0, "/source/ten/a.mkv"),
		movie(11, "NUL", 0, "/source/eleven/a.mkv"),
	)
	assertFoldUnique(t, result)
	want := map[int]string{
		1: "Title Sub Part (2020)", 2: "a b c d e",
		4: "Con [whisparr-4]", 5: "con.Air [whisparr-5]", 6: "LPT¹ [whisparr-6]",
		7: "Con (2020)", 8: "Console", 10: "nul [whisparr-10]", 11: "NUL [whisparr-11]",
	}
	for id, folder := range want {
		entry := entryByID(t, result, id)
		if entry.Folder != folder {
			t.Errorf("movie %d folder = %q, want %q", id, entry.Folder, folder)
		}
	}
	if got := entryByID(t, result, 4).Link; got != "Con [whisparr-4].mkv" {
		t.Errorf("reserved-name link = %q", got)
	}
}

func TestBuildSuffixNoClash(t *testing.T) {
	t.Parallel()
	result := build(t,
		movie(5, "Same", 2020, "/source/five/a.mkv"),
		movie(9, "Same", 2020, "/source/nine/a.mkv"),
		movie(3, "Same [whisparr-9]", 2020, "/source/three/a.mkv"),
		movie(4, "Same (2020) [whisparr-9]", 0, "/source/four/a.mkv"),
	)
	assertFoldUnique(t, result)
	want := map[int]string{5: "Same (2020)", 9: "Same (2020) [whisparr-9]", 3: "Same (whisparr-9) (2020)", 4: "Same (2020) (whisparr-9)"}
	for id, folder := range want {
		if got := entryByID(t, result, id).Folder; got != folder {
			t.Errorf("movie %d folder = %q, want %q", id, got, folder)
		}
	}
	assertLinkAndFolderPairwiseDistinct(t, result)
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
	uneven := entryByID(t, build(t, movie(4, "a"+strings.Repeat("é", 150), 0, "/source/a/four.mkv")), 4)
	if len(uneven.Folder) != 179 || !utf8.ValidString(uneven.Folder) {
		t.Fatalf("uneven-width folder is not a 179-byte UTF-8 title: %q", uneven.Folder)
	}
	empty := build(t, movie(3, ".. \t\n", 2020, "/source/a/three.mkv"))
	if len(empty.Refused) != 1 || empty.Refused[0].Reason != overlay.ReasonEmptyTitle {
		t.Fatalf("empty title result = %#v", empty)
	}
	accepted := build(t, movie(5, strings.Repeat("a", 4096), 0, "/source/a/five.mkv"))
	if got := len(entryByID(t, accepted, 5).Folder); got != 180 {
		t.Fatalf("4096-byte title folder length = %d, want 180", got)
	}
	tooLong := build(t, movie(6, strings.Repeat("a", 4097), 0, "/source/a/six.mkv"))
	if len(tooLong.Entries) != 0 || len(tooLong.Refused) != 1 || tooLong.Refused[0].Reason != overlay.ReasonTitleTooLong {
		t.Fatalf("4097-byte title result = %#v", tooLong)
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
	cases := []overlay.Options{{SourceRoot: "source", TargetRoot: "/target"}, {SourceRoot: "/source", TargetRoot: "target"}, {SourceRoot: "", TargetRoot: "/target"}, {SourceRoot: "/source", TargetRoot: ""}, {SourceRoot: "/so\x00urce", TargetRoot: "/target"}, {SourceRoot: "/source", TargetRoot: "/tar\x00get"}}
	for _, opts := range cases {
		if _, err := overlay.Build(opts, nil); !errors.Is(err, overlay.ErrInvalidOptions) {
			t.Fatalf("Build(%#v) error = %v", opts, err)
		}
	}
}

func TestBuildMissingMovieFile(t *testing.T) {
	t.Parallel()
	result := build(t, whisparr.Movie{ID: 1, HasFile: true})
	want := []overlay.Refusal{{MovieID: 1, Reason: overlay.ReasonMissingPath}}
	if len(result.Entries) != 0 || !reflect.DeepEqual(result.Refused, want) {
		t.Fatalf("missing MovieFile result = %#v, want refusals %#v", result, want)
	}
}

func TestBuildMovieLimit(t *testing.T) {
	t.Parallel()
	movies := []whisparr.Movie{
		movie(1, "One", 0, "/source/one/a.mkv"),
		movie(2, "Two", 0, "/source/two/a.mkv"),
		movie(3, "Three", 0, "/source/three/a.mkv"),
	}
	if _, err := overlay.Build(overlay.Options{SourceRoot: "/source", TargetRoot: "/target", MaxMovies: 2}, movies[:2]); err != nil {
		t.Fatalf("limit-sized build error = %v", err)
	}
	if _, err := overlay.Build(overlay.Options{SourceRoot: "/source", TargetRoot: "/target", MaxMovies: 2}, movies); !errors.Is(err, overlay.ErrTooManyMovies) {
		t.Fatalf("over-limit build error = %v", err)
	}
	for _, limit := range []int{-1, overlay.DefaultMaxMovies + 1} {
		if _, err := overlay.Build(overlay.Options{SourceRoot: "/source", TargetRoot: "/target", MaxMovies: limit}, nil); !errors.Is(err, overlay.ErrInvalidOptions) {
			t.Errorf("MaxMovies %d error = %v", limit, err)
		}
	}
	if _, err := overlay.Build(overlay.Options{SourceRoot: "/source", TargetRoot: "/target"}, movies); err != nil {
		t.Fatalf("default limit build error = %v", err)
	}
}

func TestBuildExtensionRules(t *testing.T) {
	t.Parallel()
	accepted := build(t,
		movie(1, "DVR", 0, "/source/a/one.dvr-ms"),
		movie(2, "Long", 0, "/source/a/two.abcdefghijklmno"),
	)
	if entryByID(t, accepted, 1).Link != "DVR.dvr-ms" || entryByID(t, accepted, 2).Link != "Long.abcdefghijklmno" {
		t.Fatalf("accepted extensions = %#v", accepted)
	}
	for _, tc := range []struct{ path, reason string }{
		{"/source/a/one.abcdefghijklmnop", overlay.ReasonUnsupportedExtension},
		{"/source/a/one.mk:v", overlay.ReasonUnsupportedExtension},
		{"/source/a/one.", overlay.ReasonNoExtension},
	} {
		result := build(t, movie(3, "Invalid", 0, tc.path))
		if len(result.Entries) != 0 || len(result.Refused) != 1 || result.Refused[0].Reason != tc.reason {
			t.Errorf("path %q result = %#v, want reason %q", tc.path, result, tc.reason)
		}
	}
	longTitle := strings.Repeat("a", 200)
	boundary := build(t,
		movie(1, longTitle, 2020, "/source/a/one.abcdefghijklmno"),
		movie(math.MaxInt, longTitle, 2020, "/source/b/two.abcdefghijklmno"),
	)
	maxEntry := entryByID(t, boundary, math.MaxInt)
	if len(maxEntry.Folder) > 255 || len(maxEntry.Link) > 255 {
		t.Fatalf("maximum component lengths folder=%d link=%d", len(maxEntry.Folder), len(maxEntry.Link))
	}
}

func TestBuildDuplicateID(t *testing.T) {
	t.Parallel()
	first := movie(7, "First", 2020, "/source/a/one.mkv")
	second := movie(7, "Second", 2020, "/source/a/two.mkv")
	eight := movie(8, "Eight", 2020, "/source/a/eight.mkv")
	result := build(t, first, second, eight)
	if hasEntry(result, 7) || !hasEntry(result, 8) || len(result.Refused) != 2 {
		t.Fatalf("duplicate result = %#v", result)
	}
	for _, refusal := range result.Refused {
		if refusal.MovieID != 7 || refusal.Reason != overlay.ReasonDuplicateMovieID {
			t.Fatalf("duplicate refusal = %#v", refusal)
		}
	}
	if reversed := build(t, eight, second, first); !reflect.DeepEqual(result, reversed) {
		t.Fatalf("reversed duplicate result = %#v, want %#v", reversed, result)
	}
	nilCopy := whisparr.Movie{ID: 7, HasFile: true}
	duplicateNil := build(t, first, nilCopy)
	if len(duplicateNil.Entries) != 0 || len(duplicateNil.Refused) != 2 {
		t.Fatalf("duplicate with missing file result = %#v", duplicateNil)
	}
	for _, refusal := range duplicateNil.Refused {
		if refusal.MovieID != 7 || refusal.Reason != overlay.ReasonDuplicateMovieID {
			t.Errorf("duplicate with missing file refusal = %#v", refusal)
		}
	}
	withoutFile := first
	withoutFile.HasFile = false
	boundary := build(t, withoutFile, second)
	if !hasEntry(boundary, 7) || len(boundary.Entries) != 1 || len(boundary.Refused) != 0 {
		t.Fatalf("fileless duplicate boundary = %#v", boundary)
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

func assertFoldUnique(t *testing.T, result overlay.Map) {
	t.Helper()
	for left := range len(result.Entries) {
		for right := left + 1; right < len(result.Entries); right++ {
			if strings.EqualFold(result.Entries[left].Folder, result.Entries[right].Folder) {
				t.Fatalf("folders are equal under Unicode folding: %q and %q", result.Entries[left].Folder, result.Entries[right].Folder)
			}
		}
	}
}

func assertLinkAndFolderPairwiseDistinct(t *testing.T, result overlay.Map) {
	t.Helper()
	for left := range len(result.Entries) {
		for right := left + 1; right < len(result.Entries); right++ {
			if strings.EqualFold(result.Entries[left].Folder, result.Entries[right].Folder) || strings.EqualFold(result.Entries[left].Link, result.Entries[right].Link) {
				t.Fatalf("entries are not pairwise distinct: %#v and %#v", result.Entries[left], result.Entries[right])
			}
		}
	}
}
