// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package overlay_test

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/golusoris/goenvoy/arr/whisparr"
	"golang.org/x/text/unicode/norm"

	"github.com/lusoris/SnatchArr/api/internal/adult/overlay"
)

var (
	sourceRoot     = testRoot("source")
	defaultOptions = overlay.Options{SourceRoot: sourceRoot, TargetRoot: sourceRoot}
)

func TestBuildImport(t *testing.T) {
	t.Parallel()
	a := movie(1, "Alpha", 2020, rootPath(sourceRoot, "a", "one.mkv"))
	b := movie(2, "Beta", 2021, rootPath(sourceRoot, "b", "two.mkv"))
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
	before := build(t, movie(1, "Alpha", 2020, rootPath(sourceRoot, "a", "one.mkv")))
	after := build(t, movie(1, "Alpha", 2020, rootPath(sourceRoot, "new", "one.mkv")))
	oldEntry := entryByID(t, before, 1)
	newEntry := entryByID(t, after, 1)
	if oldEntry.Folder != newEntry.Folder || oldEntry.Link != newEntry.Link || oldEntry.Target == newEntry.Target {
		t.Fatalf("rename entries = %#v and %#v", oldEntry, newEntry)
	}
}

func TestBuildDelete(t *testing.T) {
	t.Parallel()
	a := movie(1, "Alpha", 2020, rootPath(sourceRoot, "a", "one.mkv"))
	b := movie(2, "Beta", 2020, rootPath(sourceRoot, "b", "two.mkv"))
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
	before := movie(1, "Alpha", 2020, rootPath(sourceRoot, "a", "one.mkv"))
	after := movie(1, "Alpha", 2020, rootPath(sourceRoot, "new", "one.mp4"))
	after.MovieFile.ID = 12
	oldEntry := entryByID(t, build(t, before), 1)
	newEntry := entryByID(t, build(t, after), 1)
	if oldEntry.Folder != newEntry.Folder || oldEntry.Link == newEntry.Link || oldEntry.Target == newEntry.Target {
		t.Fatalf("upgrade entries = %#v and %#v", oldEntry, newEntry)
	}
	sameExtension := after
	sameExtensionFile := *after.MovieFile
	sameExtensionFile.Path = rootPath(sourceRoot, "newer", "one.mp4")
	sameExtensionFile.ID = 13
	sameExtension.MovieFile = &sameExtensionFile
	sameExtensionEntry := entryByID(t, build(t, sameExtension), 1)
	if newEntry.Link != sameExtensionEntry.Link || newEntry.Target == sameExtensionEntry.Target {
		t.Fatalf("same-extension upgrade entries = %#v and %#v", newEntry, sameExtensionEntry)
	}
}

func TestBuildCollision(t *testing.T) {
	t.Parallel()
	five := movie(5, "Same", 2020, rootPath(sourceRoot, "five", "a.mkv"))
	nine := movie(9, "Same", 2020, rootPath(sourceRoot, "nine", "a.mkv"))
	twelve := movie(12, "Same", 2020, rootPath(sourceRoot, "twelve", "a.mkv"))
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
	caseVariant := movie(13, "same", 2020, rootPath(sourceRoot, "case", "a.mkv"))
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
		movie(1, "Σ", 2020, rootPath(sourceRoot, "one", "a.mkv")),
		movie(2, "σ", 2020, rootPath(sourceRoot, "two", "a.mkv")),
		movie(3, "ς", 2020, rootPath(sourceRoot, "three", "a.mkv")),
		movie(4, "Straße", 2020, rootPath(sourceRoot, "four", "a.mkv")),
		movie(5, "STRASSE", 2020, rootPath(sourceRoot, "five", "a.mkv")),
		movie(6, "é", 2020, rootPath(sourceRoot, "six", "a.mkv")),
		movie(7, "e\u0301", 2020, rootPath(sourceRoot, "seven", "a.mkv")),
		movie(8, "K", 2020, rootPath(sourceRoot, "eight", "a.mkv")),
		movie(9, "k", 2020, rootPath(sourceRoot, "nine", "a.mkv")),
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

func TestBuildHFSIgnorableCollision(t *testing.T) {
	t.Parallel()
	result := build(t,
		movie(1, "AB", 0, rootPath(sourceRoot, "one", "a.mkv")),
		movie(2, "A\u200dB", 0, rootPath(sourceRoot, "two", "a.mkv")),
		movie(3, "A\ufeffB\u200c", 0, rootPath(sourceRoot, "three", "a.mkv")),
		movie(4, "A\u200bB", 0, rootPath(sourceRoot, "four", "a.mkv")),
	)
	want := map[int]string{
		1: "AB", 2: "A\u200dB [whisparr-2]", 3: "A\ufeffB\u200c [whisparr-3]",
		4: "A\u200bB",
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
		movie(1, "Title: Sub*Part?", 2020, rootPath(sourceRoot, "one", "a.mkv")),
		movie(2, `a<b>c"d|e`, 0, rootPath(sourceRoot, "two", "a.mkv")),
		movie(4, "Con", 0, rootPath(sourceRoot, "four", "a.mkv")),
		movie(5, "con.Air", 0, rootPath(sourceRoot, "five", "a.mkv")),
		movie(6, "LPT¹", 0, rootPath(sourceRoot, "six", "a.mkv")),
		movie(7, "Con", 2020, rootPath(sourceRoot, "seven", "a.mkv")),
		movie(8, "Console", 0, rootPath(sourceRoot, "eight", "a.mkv")),
		movie(10, "nul", 0, rootPath(sourceRoot, "ten", "a.mkv")),
		movie(11, "NUL", 0, rootPath(sourceRoot, "eleven", "a.mkv")),
		movie(12, "Con.Air", 2020, rootPath(sourceRoot, "twelve", "a.mkv")),
		movie(13, "COM1.foo", 0, rootPath(sourceRoot, "thirteen", "a.mkv")),
		movie(14, "CONIN$", 0, rootPath(sourceRoot, "fourteen", "a.mkv")),
		movie(15, "conout$.log", 0, rootPath(sourceRoot, "fifteen", "a.mkv")),
		movie(16, "COM0", 0, rootPath(sourceRoot, "sixteen", "a.mkv")),
		movie(17, "LPT0.x", 0, rootPath(sourceRoot, "seventeen", "a.mkv")),
	)
	assertFoldUnique(t, result)
	assertLinkAndFolderPairwiseDistinct(t, result)
	want := map[int]string{
		1:  "Title Sub Part (2020)",
		2:  "a b c d e",
		4:  "Con [whisparr-4]",
		5:  "con [whisparr-5].Air",
		6:  "LPT¹ [whisparr-6]",
		7:  "Con (2020)",
		8:  "Console",
		10: "nul [whisparr-10]",
		11: "NUL [whisparr-11]",
		12: "Con [whisparr-12].Air (2020)",
		13: "COM1 [whisparr-13].foo",
		14: "CONIN$ [whisparr-14]",
		15: "conout$ [whisparr-15].log",
		16: "COM0",
		17: "LPT0.x",
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
	if got := entryByID(t, result, 5).Link; got != "con [whisparr-5].Air.mkv" {
		t.Errorf("reserved-stem period link = %q", got)
	}
}

func TestBuildSuffixNoClash(t *testing.T) {
	t.Parallel()
	result := build(t,
		movie(5, "Same", 2020, rootPath(sourceRoot, "five", "a.mkv")),
		movie(9, "Same", 2020, rootPath(sourceRoot, "nine", "a.mkv")),
		movie(3, "Same [whisparr-9]", 2020, rootPath(sourceRoot, "three", "a.mkv")),
		movie(4, "Same (2020) [whisparr-9]", 0, rootPath(sourceRoot, "four", "a.mkv")),
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
	sep := string(filepath.Separator)
	cases := []struct {
		id   int
		path string
	}{
		{id: 1, path: sourceRoot + sep + ".." + sep + "other" + sep + "one.mkv"},
		{id: 2, path: rootPath(testRoot("source2"), "two.mkv")},
		{id: 3, path: filepath.Join("relative", "three.mkv")},
		{id: 4, path: sourceRoot},
		{id: 5, path: rootPath(sourceRoot, "\x00", "five.mkv")},
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

func TestBuildPrefixedDotDotSegment(t *testing.T) {
	t.Parallel()
	result := build(t, movie(1, "Prefixed", 2020, rootPath(sourceRoot, "..name", "a.mkv")))
	if len(result.Refused) != 0 || entryByID(t, result, 1).Target != filepath.Join(sourceRoot, "..name", "a.mkv") {
		t.Fatalf("prefixed segment result = %#v", result)
	}
}

func TestBuildRootMapping(t *testing.T) {
	t.Parallel()
	targetRoot := testRoot("jellyfin-view")
	opts := overlay.Options{SourceRoot: sourceRoot, TargetRoot: targetRoot}
	result, err := overlay.Build(opts, []whisparr.Movie{movie(1, "Alpha", 2020, rootPath(sourceRoot, "a", "one.mkv"))})
	if err != nil {
		t.Fatal(err)
	}
	if got := entryByID(t, result, 1).Target; got != filepath.Join(targetRoot, "a", "one.mkv") {
		t.Fatalf("target = %q", got)
	}
}

func TestBuildTitleSanitize(t *testing.T) {
	t.Parallel()
	sanitized := entryByID(t, build(t, movie(1, ".. Alpha/Beta\\[Tag]\x01 ..", 2020, rootPath(sourceRoot, "a", "one.mkv"))), 1)
	if sanitized.Folder != "Alpha Beta (Tag) (2020)" {
		t.Fatalf("sanitized folder = %q", sanitized.Folder)
	}
	long := entryByID(t, build(t, movie(2, strings.Repeat("é", 150), 0, rootPath(sourceRoot, "a", "two.mkv"))), 2)
	if len(long.Folder) > 180 || !utf8.ValidString(long.Folder) {
		t.Fatalf("long folder is not a <=180-byte UTF-8 title: %q", long.Folder)
	}
	uneven := entryByID(t, build(t, movie(4, "a"+strings.Repeat("é", 150), 0, rootPath(sourceRoot, "a", "four.mkv"))), 4)
	if len(uneven.Folder) != 179 || !utf8.ValidString(uneven.Folder) {
		t.Fatalf("uneven-width folder is not a 179-byte UTF-8 title: %q", uneven.Folder)
	}
	empty := build(t, movie(3, ".. \t\n", 2020, rootPath(sourceRoot, "a", "three.mkv")))
	if len(empty.Refused) != 1 || empty.Refused[0].Reason != overlay.ReasonEmptyTitle {
		t.Fatalf("empty title result = %#v", empty)
	}
	accepted := build(t, movie(5, strings.Repeat("a", 4096), 0, rootPath(sourceRoot, "a", "five.mkv")))
	if got := len(entryByID(t, accepted, 5).Folder); got != 180 {
		t.Fatalf("4096-byte title folder length = %d, want 180", got)
	}
	tooLong := build(t, movie(6, strings.Repeat("a", 4097), 0, rootPath(sourceRoot, "a", "six.mkv")))
	if len(tooLong.Entries) != 0 || len(tooLong.Refused) != 1 || tooLong.Refused[0].Reason != overlay.ReasonTitleTooLong {
		t.Fatalf("4097-byte title result = %#v", tooLong)
	}
}

// nfdUnits counts the UTF-16 units of the NFD form, the decomposed UTF-16 that HFS+ stores for these characters.
func nfdUnits(s string) int {
	return len(utf16.Encode([]rune(norm.NFD.String(s))))
}

func TestBuildTitleHFSUnits(t *testing.T) {
	t.Parallel()
	alpha := "\u1F82"      // 3 UTF-8 bytes; NFD alpha, psili, varia, ypogegrammeni = 4 UTF-16 units.
	compat := "\U0002F800" // 4 UTF-8 bytes, 2 UTF-16 units; NFD U+4E3D = 1 unit, so only the larger count keeps 2.
	for _, tc := range []struct{ name, title, want string }{
		{"ascii-untouched", strings.Repeat("a", 180), strings.Repeat("a", 180)},
		{"units-boundary-kept", strings.Repeat(alpha, 45), strings.Repeat(alpha, 45)},
		{"units-boundary-plus-one", strings.Repeat(alpha, 45) + "a", strings.Repeat(alpha, 45)},
		{"units-boundary-trimmed", strings.Repeat(alpha, 46), strings.Repeat(alpha, 45)},
		{"units-trimmed-inside-byte-budget", strings.Repeat(alpha, 60), strings.Repeat(alpha, 45)},
		{"undecomposed-length-counted", strings.Repeat(alpha, 40) + strings.Repeat(compat, 15), strings.Repeat(alpha, 40) + strings.Repeat(compat, 10)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := entryByID(t, build(t, movie(1, tc.title, 0, rootPath(sourceRoot, "a", "one.mkv"))), 1).Folder
			if got != tc.want {
				t.Fatalf("folder = %+q (%d bytes, %d NFD units), want %+q", got, len(got), nfdUnits(got), tc.want)
			}
		})
	}
}

func TestBuildHFSUnitsWithSuffixAndExtension(t *testing.T) {
	t.Parallel()
	title := strings.Repeat("\u1F82", 60) // 180 bytes, 240 HFS+ units before the fix.
	result := build(t,
		movie(1, title, 2020, rootPath(sourceRoot, "a", "one.abcdefghijklmno")),
		movie(math.MaxInt, title, 2020, rootPath(sourceRoot, "b", "two.abcdefghijklmno")),
	)
	maxEntry := entryByID(t, result, math.MaxInt)
	if !strings.Contains(maxEntry.Folder, " [whisparr-") {
		t.Fatalf("collision suffix missing: %+q", maxEntry.Folder)
	}
	if folder, link := nfdUnits(maxEntry.Folder), nfdUnits(maxEntry.Link); folder > 255 || link > 255 {
		t.Fatalf("HFS+ units folder=%d link=%d, want <= 255", folder, link)
	}
}

func TestBuildYear(t *testing.T) {
	t.Parallel()
	result := build(t,
		movie(1, "Low", 1899, rootPath(sourceRoot, "a", "one.mkv")),
		movie(2, "First", 1900, rootPath(sourceRoot, "a", "two.mkv")),
		movie(3, "Last", 2099, rootPath(sourceRoot, "a", "three.mkv")),
		movie(4, "High", 2100, rootPath(sourceRoot, "a", "four.mkv")),
		withReleaseDate(movie(5, "Release", 0, rootPath(sourceRoot, "a", "five.mkv")), "2001-02-03"),
		withReleaseDate(movie(6, "Bad", 0, rootPath(sourceRoot, "a", "six.mkv")), "nope"),
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
	forward := []whisparr.Movie{movie(2, "Zulu", 2020, rootPath(sourceRoot, "z", "two.mkv")), movie(1, "Alpha", 2020, rootPath(sourceRoot, "a", "one.mkv"))}
	reverse := []whisparr.Movie{forward[1], forward[0]}
	first := build(t, forward...)
	if !reflect.DeepEqual(first, build(t, reverse...)) || !reflect.DeepEqual(first, build(t, forward...)) {
		t.Fatal("Build is not deterministic across input order and repeated calls")
	}
}

func TestBuildOptions(t *testing.T) {
	t.Parallel()
	targetRoot := testRoot("target")
	cases := []overlay.Options{
		{SourceRoot: "source", TargetRoot: targetRoot},
		{SourceRoot: sourceRoot, TargetRoot: "target"},
		{SourceRoot: "", TargetRoot: targetRoot},
		{SourceRoot: sourceRoot, TargetRoot: ""},
		{SourceRoot: testRoot("so\x00urce"), TargetRoot: targetRoot},
		{SourceRoot: sourceRoot, TargetRoot: testRoot("tar\x00get")},
	}
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
	targetRoot := testRoot("target")
	movies := []whisparr.Movie{
		movie(1, "One", 0, rootPath(sourceRoot, "one", "a.mkv")),
		movie(2, "Two", 0, rootPath(sourceRoot, "two", "a.mkv")),
		movie(3, "Three", 0, rootPath(sourceRoot, "three", "a.mkv")),
	}
	if _, err := overlay.Build(overlay.Options{SourceRoot: sourceRoot, TargetRoot: targetRoot, MaxMovies: 2}, movies[:2]); err != nil {
		t.Fatalf("limit-sized build error = %v", err)
	}
	if _, err := overlay.Build(overlay.Options{SourceRoot: sourceRoot, TargetRoot: targetRoot, MaxMovies: 2}, movies); !errors.Is(err, overlay.ErrTooManyMovies) {
		t.Fatalf("over-limit build error = %v", err)
	}
	for _, limit := range []int{-1, overlay.MaxMoviesCeiling + 1} {
		if _, err := overlay.Build(overlay.Options{SourceRoot: sourceRoot, TargetRoot: targetRoot, MaxMovies: limit}, nil); !errors.Is(err, overlay.ErrInvalidOptions) {
			t.Errorf("MaxMovies %d error = %v", limit, err)
		}
	}
	if _, err := overlay.Build(overlay.Options{SourceRoot: sourceRoot, TargetRoot: targetRoot}, movies); err != nil {
		t.Fatalf("default limit build error = %v", err)
	}
}

func TestBuildDefaultLimitIsOneHundredThousand(t *testing.T) {
	t.Parallel()
	const wantDefaultLimit = 100_000
	if overlay.DefaultMaxMovies != wantDefaultLimit {
		t.Errorf("DefaultMaxMovies = %d, want %d", overlay.DefaultMaxMovies, wantDefaultLimit)
	}
	opts := overlay.Options{SourceRoot: sourceRoot, TargetRoot: testRoot("target")}
	movies := make([]whisparr.Movie, wantDefaultLimit+1)
	if _, err := overlay.Build(opts, movies[:wantDefaultLimit]); err != nil {
		t.Fatalf("default-limit build error = %v", err)
	}
	if _, err := overlay.Build(opts, movies); !errors.Is(err, overlay.ErrTooManyMovies) {
		t.Fatalf("over-default-limit build error = %v, want %v", err, overlay.ErrTooManyMovies)
	}
}

func TestBuildMaxMoviesCeilingBoundary(t *testing.T) {
	t.Parallel()
	opts := overlay.Options{SourceRoot: sourceRoot, TargetRoot: testRoot("target"), MaxMovies: overlay.MaxMoviesCeiling}
	if _, err := overlay.Build(opts, nil); err != nil {
		t.Fatalf("MaxMoviesCeiling build error = %v", err)
	}
}

func TestBuildExtensionRules(t *testing.T) {
	t.Parallel()
	accepted := build(t,
		movie(1, "DVR", 0, rootPath(sourceRoot, "a", "one.dvr-ms")),
		movie(2, "Long", 0, rootPath(sourceRoot, "a", "two.abcdefghijklmno")),
	)
	if entryByID(t, accepted, 1).Link != "DVR.dvr-ms" || entryByID(t, accepted, 2).Link != "Long.abcdefghijklmno" {
		t.Fatalf("accepted extensions = %#v", accepted)
	}
	for _, tc := range []struct{ path, reason string }{
		{rootPath(sourceRoot, "a", "one.abcdefghijklmnop"), overlay.ReasonUnsupportedExtension},
		{rootPath(sourceRoot, "a", "one.mk:v"), overlay.ReasonUnsupportedExtension},
		{rootPath(sourceRoot, "a", "one."), overlay.ReasonNoExtension},
	} {
		result := build(t, movie(3, "Invalid", 0, tc.path))
		if len(result.Entries) != 0 || len(result.Refused) != 1 || result.Refused[0].Reason != tc.reason {
			t.Errorf("path %q result = %#v, want reason %q", tc.path, result, tc.reason)
		}
	}
	longTitle := strings.Repeat("a", 200)
	boundary := build(t,
		movie(1, longTitle, 2020, rootPath(sourceRoot, "a", "one.abcdefghijklmno")),
		movie(math.MaxInt, longTitle, 2020, rootPath(sourceRoot, "b", "two.abcdefghijklmno")),
	)
	maxEntry := entryByID(t, boundary, math.MaxInt)
	if len(maxEntry.Folder) > 255 || len(maxEntry.Link) > 255 {
		t.Fatalf("maximum component lengths folder=%d link=%d", len(maxEntry.Folder), len(maxEntry.Link))
	}
}

func TestBuildDuplicateID(t *testing.T) {
	t.Parallel()
	first := movie(7, "First", 2020, rootPath(sourceRoot, "a", "one.mkv"))
	second := movie(7, "Second", 2020, rootPath(sourceRoot, "a", "two.mkv"))
	eight := movie(8, "Eight", 2020, rootPath(sourceRoot, "a", "eight.mkv"))
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
	fallback.Path = rootPath(sourceRoot, "fallback")
	fallback.MovieFile.RelativePath = "one.mkv"
	if got := entryByID(t, build(t, fallback), 1).Target; got != filepath.Join(sourceRoot, "fallback", "one.mkv") {
		t.Fatalf("fallback target = %q", got)
	}
	missing := movie(2, "Missing", 2020, "")
	if got := build(t, missing).Refused[0].Reason; got != overlay.ReasonMissingPath {
		t.Fatalf("missing path reason = %q", got)
	}
	if got := build(t, movie(3, "No Extension", 2020, rootPath(sourceRoot, "no-extension"))).Refused[0].Reason; got != overlay.ReasonNoExtension {
		t.Fatalf("extension reason = %q", got)
	}
}

func testRoot(name string) string {
	return filepath.Join(filepath.VolumeName(os.TempDir())+string(filepath.Separator), name)
}

func rootPath(root string, parts ...string) string {
	elems := make([]string, 0, 1+len(parts))
	elems = append(elems, root)
	elems = append(elems, parts...)
	return filepath.Join(elems...)
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
