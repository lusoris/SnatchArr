// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package overlay builds desired Whisparr-to-Jellyfin overlay links. SnatchArr computes the map and the plugin applies it (ADR-0028 amendment, issue 1209). Collisions preserve the lowest non-reserved movie ID's plain folder, so deleting that holder renames the next one. Canonical caseless matching is at least as wide as the equivalence of any case-insensitive or normalization-insensitive target filesystem; it may add a suffix a case-sensitive target would not need, never fewer. Windows device names are reserved. Containment is lexical; the plugin must resolve symlinks before applying links.
package overlay

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/golusoris/goenvoy/arr/whisparr"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"

	"github.com/lusoris/SnatchArr/api/internal/pathsafe"
)

const (
	maxTitleBytes     = 180
	maxRawTitleBytes  = 4096
	maxExtensionBytes = 16 // Includes the leading period.
	// 180 title + 7 year + 11 " [whisparr-" + 20 digits (strconv.Itoa(math.MinInt)) + 1 "]" = 219 folder bytes; + 16 extension = 235 <= 255.
	maxComponentBytes = 255
	// DefaultMaxMovies bounds Build when Options.MaxMovies is zero.
	DefaultMaxMovies = 100_000
	// MaxMoviesCeiling is the largest explicit Options.MaxMovies accepted by Build.
	MaxMoviesCeiling = 1_000_000
)

// ErrInvalidOptions reports an empty or non-absolute source or target root.
var ErrInvalidOptions = errors.New("invalid overlay options")

// ErrTooManyMovies reports that Build received more movies than its configured limit.
var ErrTooManyMovies = errors.New("too many overlay movies")

const (
	// ReasonMissingPath reports a movie with no usable movie-file path.
	ReasonMissingPath = "missing_path"
	// ReasonOutsideSourceRoot reports a path that is not an absolute child of SourceRoot.
	ReasonOutsideSourceRoot = "outside_source_root"
	// ReasonNoExtension reports a movie-file path with no extension.
	ReasonNoExtension = "no_extension"
	// ReasonUnsupportedExtension keeps every Link within one 255-byte path component and free of reserved characters.
	ReasonUnsupportedExtension = "unsupported_extension"
	// ReasonEmptyTitle reports a title that is empty after sanitization.
	ReasonEmptyTitle = "empty_title"
	// ReasonTitleTooLong reports a raw title exceeding maxRawTitleBytes.
	ReasonTitleTooLong = "title_too_long"
	// ReasonDuplicateMovieID reports every copy of a repeated file-bearing movie ID.
	// Whisparr movie IDs are unique, so repeats identify an inconsistent list;
	// refusing every copy keeps the map independent of input order.
	ReasonDuplicateMovieID = "duplicate_movie_id"
)

// Options identifies the source tree reported by Whisparr and its Jellyfin-visible target tree.
type Options struct {
	SourceRoot string
	TargetRoot string
	// MaxMovies bounds the list processed by Build; zero selects DefaultMaxMovies, up to MaxMoviesCeiling.
	MaxMovies int
}

// Entry describes one desired overlay symlink.
type Entry struct {
	MovieID int
	Folder  string
	Link    string
	Target  string
}

// Refusal explains why a file-bearing movie cannot produce an overlay entry.
type Refusal struct {
	MovieID int
	Reason  string
}

// Map contains desired entries and deterministic refusal records.
type Map struct {
	Entries []Entry
	Refused []Refusal
}

// Build computes the complete desired overlay map without reading or writing external state.
func Build(opts Options, movies []whisparr.Movie) (Map, error) {
	if err := validateOptions(opts); err != nil {
		return Map{}, err
	}
	limit := opts.MaxMovies
	if limit == 0 {
		limit = DefaultMaxMovies
	}
	if limit < 0 || limit > MaxMoviesCeiling {
		return Map{}, fmt.Errorf("max movies: %w", ErrInvalidOptions)
	}
	if len(movies) > limit {
		return Map{}, fmt.Errorf("%w", ErrTooManyMovies)
	}
	result := Map{}
	duplicates := duplicateMovieIDs(movies)
	// len(movies) is bounded by limit above.
	for _, movie := range movies {
		if !movie.HasFile {
			continue
		}
		if _, duplicated := duplicates[movie.ID]; duplicated {
			result.Refused = append(result.Refused, Refusal{MovieID: movie.ID, Reason: ReasonDuplicateMovieID})
			continue
		}
		entry, reason := buildEntry(opts, movie)
		if reason != "" {
			result.Refused = append(result.Refused, Refusal{MovieID: movie.ID, Reason: reason})
			continue
		}
		result.Entries = append(result.Entries, entry)
	}
	applyCollisions(result.Entries)
	sortMap(&result)
	return result, nil
}

// duplicateMovieIDs returns the set of movie IDs that appear on more than one file-bearing movie.
func duplicateMovieIDs(movies []whisparr.Movie) map[int]struct{} {
	fileCount := 0
	// len(movies) is bounded by Build's configured limit.
	for index := range movies {
		if movies[index].HasFile {
			fileCount++
		}
	}
	if fileCount < 2 {
		return nil
	}
	ids := make([]int, 0, fileCount)
	// len(movies) is bounded by Build's configured limit.
	for index := range movies {
		if movies[index].HasFile {
			ids = append(ids, movies[index].ID)
		}
	}
	slices.Sort(ids)
	duplicates := make(map[int]struct{})
	// len(ids) equals fileCount, bounded by Build's configured limit.
	for index := 1; index < len(ids); index++ {
		if ids[index] == ids[index-1] {
			duplicates[ids[index]] = struct{}{}
		}
	}
	return duplicates
}

func validateOptions(opts Options) error {
	if opts.SourceRoot == "" || strings.ContainsRune(opts.SourceRoot, '\x00') || !filepath.IsAbs(opts.SourceRoot) {
		return fmt.Errorf("source root: %w", ErrInvalidOptions)
	}
	if opts.TargetRoot == "" || strings.ContainsRune(opts.TargetRoot, '\x00') || !filepath.IsAbs(opts.TargetRoot) {
		return fmt.Errorf("target root: %w", ErrInvalidOptions)
	}
	return nil
}

func buildEntry(opts Options, movie whisparr.Movie) (Entry, string) {
	if len(movie.Title) > maxRawTitleBytes {
		return Entry{}, ReasonTitleTooLong
	}
	path, ok := movieFilePath(movie)
	if !ok {
		return Entry{}, ReasonMissingPath
	}
	if !isAllowedPath(opts.SourceRoot, path) {
		return Entry{}, ReasonOutsideSourceRoot
	}
	ext := filepath.Ext(path)
	if ext == "" || ext == "." {
		return Entry{}, ReasonNoExtension
	}
	if !validExtension(ext) {
		return Entry{}, ReasonUnsupportedExtension
	}
	title := sanitizeTitle(movie.Title)
	if title == "" {
		return Entry{}, ReasonEmptyTitle
	}
	rel, err := filepath.Rel(filepath.Clean(opts.SourceRoot), filepath.Clean(path))
	if err != nil {
		return Entry{}, ReasonOutsideSourceRoot
	}
	folder := titleWithYear(title, movie)
	return Entry{MovieID: movie.ID, Folder: folder, Link: folder + ext, Target: filepath.Join(opts.TargetRoot, rel)}, ""
}

func movieFilePath(movie whisparr.Movie) (string, bool) {
	if movie.MovieFile == nil {
		return "", false
	}
	path := movie.MovieFile.Path
	if path != "" {
		return path, true
	}
	if movie.MovieFile.RelativePath == "" {
		return "", false
	}
	return filepath.Join(movie.Path, movie.MovieFile.RelativePath), true
}

func validExtension(ext string) bool {
	if len(ext) > maxExtensionBytes || len(ext) < 2 || ext[0] != '.' {
		return false
	}
	for index := range len(ext) - 1 {
		if !validExtensionByte(ext[index+1]) {
			return false
		}
	}
	return true
}

func validExtensionByte(char byte) bool {
	return strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-", rune(char))
}

func isAllowedPath(sourceRoot, path string) bool {
	return filepath.IsAbs(path) && !strings.ContainsRune(path, '\x00') && pathsafe.StrictlyWithin(sourceRoot, path)
}

func titleWithYear(title string, movie whisparr.Movie) string {
	year := movieYear(movie)
	if year == 0 {
		return title
	}
	return title + " (" + strconv.Itoa(year) + ")"
}

func movieYear(movie whisparr.Movie) int {
	if isValidYear(movie.Year) {
		return movie.Year
	}
	if len(movie.ReleaseDate) < 4 {
		return 0
	}
	year, err := strconv.Atoi(movie.ReleaseDate[:4])
	if err != nil || !isValidYear(year) {
		return 0
	}
	return year
}

func isValidYear(year int) bool {
	return year >= 1900 && year <= 2099
}

func sanitizeTitle(title string) string {
	var clean strings.Builder
	// buildEntry limits the raw title to maxRawTitleBytes before this loop.
	for _, char := range title {
		switch {
		case strings.ContainsRune("/\\<>:\"|?*", char), unicode.IsControl(char):
			clean.WriteByte(' ')
		case char == '[':
			clean.WriteByte('(')
		case char == ']':
			clean.WriteByte(')')
		default:
			clean.WriteRune(char)
		}
	}
	return trimTitle(truncateTitle(strings.Trim(strings.Join(strings.Fields(clean.String()), " "), " .")))
}

func truncateTitle(title string) string {
	for steps := 0; len(title) > maxTitleBytes && steps < maxRawTitleBytes; steps++ {
		_, size := utf8.DecodeLastRuneInString(title)
		title = title[:len(title)-size]
	}
	return title
}

func trimTitle(title string) string {
	return strings.Trim(title, " .")
}

func applyCollisions(entries []Entry) {
	groups := make(map[string][]int, len(entries))
	caser := cases.Fold()
	// Build caps len(entries) at DefaultMaxMovies; these loops are bounded by that input size.
	for index := range entries {
		folder := entries[index].Folder
		key := norm.NFC.String(caser.String(norm.NFD.String(folder)))
		groups[key] = append(groups[key], index)
	}
	for _, indexes := range groups {
		applyCollision(entries, indexes)
	}
}

func applyCollision(entries []Entry, indexes []int) {
	plain := -1
	for _, index := range indexes {
		if !reservedDeviceFolder(entries[index].Folder) && (plain == -1 || entries[index].MovieID < entries[plain].MovieID) {
			plain = index
		}
	}
	for _, index := range indexes {
		if index != plain || reservedDeviceFolder(entries[index].Folder) {
			addCollisionSuffix(&entries[index])
		}
	}
}

func addCollisionSuffix(entry *Entry) {
	// Sanitization maps square brackets to parentheses and duplicate refusal makes movie IDs unique, so no other folder can contain this suffix, whether placed at the end of a reserved stem before its first period or appended at the end.
	suffix := " [whisparr-" + strconv.Itoa(entry.MovieID) + "]"
	if dot := strings.IndexByte(entry.Folder, '.'); dot >= 0 && reservedDeviceFolder(entry.Folder) {
		entry.Folder = strings.TrimRight(entry.Folder[:dot], " ") + suffix + entry.Folder[dot:]
	} else {
		entry.Folder += suffix
	}
	entry.Link = entry.Folder + filepath.Ext(entry.Target)
}

func reservedDeviceFolder(folder string) bool {
	stem := strings.TrimRight(strings.SplitN(folder, ".", 2)[0], " ")
	// Sources: Go src/internal/filepathlite/path_windows.go (isReservedBaseName) and Microsoft "Naming Files, Paths, and Namespaces" (plus Go's CONIN$ and CONOUT$ console handles).
	reservedNames := [...]string{"CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "COM¹", "COM²", "COM³", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9", "LPT¹", "LPT²", "LPT³"}
	// The reserved name table has a fixed scalar bound.
	for _, name := range reservedNames {
		if asciiEqualFold(stem, name) {
			return true
		}
	}
	return false
}

func asciiEqualFold(value, upper string) bool {
	if len(value) != len(upper) || len(value) > maxComponentBytes {
		return false
	}
	for index := range len(value) {
		char := value[index]
		if char >= 'a' && char <= 'z' {
			char -= 'a' - 'A'
		}
		if char != upper[index] {
			return false
		}
	}
	return true
}

func sortMap(result *Map) {
	sort.Slice(result.Entries, func(left, right int) bool {
		if result.Entries[left].Folder == result.Entries[right].Folder {
			return result.Entries[left].MovieID < result.Entries[right].MovieID
		}
		return result.Entries[left].Folder < result.Entries[right].Folder
	})
	sort.Slice(result.Refused, func(left, right int) bool {
		if result.Refused[left].MovieID == result.Refused[right].MovieID {
			return result.Refused[left].Reason < result.Refused[right].Reason
		}
		return result.Refused[left].MovieID < result.Refused[right].MovieID
	})
}
