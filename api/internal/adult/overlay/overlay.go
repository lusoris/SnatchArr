// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package overlay builds desired Whisparr-to-Jellyfin overlay links. SnatchArr computes the map and the plugin applies it (ADR-0028 amendment, issue 1209). Collisions preserve the lowest movie ID's plain folder, so deleting that holder renames the next one. Containment is lexical; the plugin must resolve symlinks before applying links.
package overlay

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/golusoris/goenvoy/arr/whisparr"

	"github.com/lusoris/SnatchArr/api/internal/pathsafe"
)

const maxTitleBytes = 180

// ErrInvalidOptions reports an empty or non-absolute source or target root.
var ErrInvalidOptions = errors.New("invalid overlay options")

const (
	// ReasonMissingPath reports a movie with no usable movie-file path.
	ReasonMissingPath = "missing_path"
	// ReasonOutsideSourceRoot reports a path that is not an absolute child of SourceRoot.
	ReasonOutsideSourceRoot = "outside_source_root"
	// ReasonNoExtension reports a movie-file path with no extension.
	ReasonNoExtension = "no_extension"
	// ReasonEmptyTitle reports a title that is empty after sanitization.
	ReasonEmptyTitle = "empty_title"
	// ReasonDuplicateMovieID reports a later duplicate movie ID.
	ReasonDuplicateMovieID = "duplicate_movie_id"
)

// Options identifies the source tree reported by Whisparr and its Jellyfin-visible target tree.
type Options struct {
	SourceRoot string
	TargetRoot string
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
	result := Map{}
	seen := make(map[int]struct{}, len(movies))
	for _, movie := range movies {
		if !movie.HasFile || movie.MovieFile == nil {
			continue
		}
		if _, duplicate := seen[movie.ID]; duplicate {
			result.Refused = append(result.Refused, Refusal{MovieID: movie.ID, Reason: ReasonDuplicateMovieID})
			continue
		}
		seen[movie.ID] = struct{}{}
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

func validateOptions(opts Options) error {
	if opts.SourceRoot == "" || !filepath.IsAbs(opts.SourceRoot) {
		return fmt.Errorf("source root: %w", ErrInvalidOptions)
	}
	if opts.TargetRoot == "" || !filepath.IsAbs(opts.TargetRoot) {
		return fmt.Errorf("target root: %w", ErrInvalidOptions)
	}
	return nil
}

func buildEntry(opts Options, movie whisparr.Movie) (Entry, string) {
	path, ok := movieFilePath(movie)
	if !ok {
		return Entry{}, ReasonMissingPath
	}
	if !isAllowedPath(opts.SourceRoot, path) {
		return Entry{}, ReasonOutsideSourceRoot
	}
	ext := filepath.Ext(path)
	if ext == "" {
		return Entry{}, ReasonNoExtension
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
	path := movie.MovieFile.Path
	if path != "" {
		return path, true
	}
	if movie.MovieFile.RelativePath == "" {
		return "", false
	}
	return filepath.Join(movie.Path, movie.MovieFile.RelativePath), true
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
	for _, char := range title {
		switch {
		case char == '/', char == '\\', unicode.IsControl(char):
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
	for len(title) > maxTitleBytes {
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
	for index := range entries {
		key := strings.ToLower(entries[index].Folder)
		groups[key] = append(groups[key], index)
	}
	for _, indexes := range groups {
		applyCollision(entries, indexes)
	}
}

func applyCollision(entries []Entry, indexes []int) {
	if len(indexes) < 2 {
		return
	}
	plain := indexes[0]
	for _, index := range indexes[1:] {
		if entries[index].MovieID < entries[plain].MovieID {
			plain = index
		}
	}
	for _, index := range indexes {
		if index != plain {
			entries[index].Folder += " [whisparr-" + strconv.Itoa(entries[index].MovieID) + "]"
			entries[index].Link = entries[index].Folder + filepath.Ext(entries[index].Target)
		}
	}
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
