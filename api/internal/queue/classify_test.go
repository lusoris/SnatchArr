// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package queue_test

import (
	"math"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/queue"
)

func TestClassifierTaxonomyPositive(t *testing.T) {
	t.Parallel()

	s := queue.DefaultSettings()
	s.Grace = 15 * time.Minute
	now := fixedNow
	oldAdded := now.Add(-1 * time.Hour)

	tests := []struct {
		name      string
		line      string
		wantClass queue.Class
	}{
		// Lidarr fixed specifications.
		{"lidarr/no_tracks_matched", "No tracks matched", queue.ClassBroken},
		{"lidarr/has_missing_tracks", "Has missing tracks", queue.ClassFixable},
		{"lidarr/has_unmatched_tracks", "Has unmatched tracks", queue.ClassFixable},
		{"lidarr/has_fewer_tracks", "Has fewer tracks than existing release", queue.ClassNotWanted},
		{"lidarr/track_file_contains_more", "Track file on disk contains more tracks than this file contains", queue.ClassNotWanted},
		{"lidarr/album_release_not_requested", "Album release not requested", queue.ClassNotWanted},
		{"lidarr/not_upgrade_track", "Not an upgrade for existing track file(s)", queue.ClassNotWanted},
		{"lidarr/not_upgrade_track_with_quality", "Not an upgrade for existing track file(s). New Quality is FLAC", queue.ClassNotWanted},
		{"lidarr/not_upgrade_album", "Not an upgrade for existing album file(s)", queue.ClassNotWanted},
		{"lidarr/couldnt_find_similar_album", "Couldn't find similar album for Artist - Album", queue.ClassBroken},
		{"lidarr/artist_name_mismatch", "Artist name mismatch, automatic import is not possible for Artist", queue.ClassFixable},
		{"lidarr/missing_tracks_expected", "One or more tracks expected in this release were not imported or missing from the release", queue.ClassFixable},

		// Sonarr fixed specifications.
		{"sonarr/not_upgrade_episode", "Not an upgrade for existing episode file(s)", queue.ClassNotWanted},
		{"sonarr/not_upgrade_episode_quality", "Not an upgrade for existing episode file(s). Existing quality: HDTV-720p", queue.ClassNotWanted},
		{"sonarr/not_quality_revision", "Not a quality revision upgrade for existing episode file(s)", queue.ClassNotWanted},
		{"sonarr/not_custom_format_upgrade", "Not a Custom Format upgrade for existing episode file(s)", queue.ClassNotWanted},
		{"sonarr/thexem_mappings", "This show has individual episode mappings on TheXEM", queue.ClassFixable},
		{"sonarr/episode_file_contains_more", "Episode file on disk contains more episodes than this file contains", queue.ClassNotWanted},
		{"sonarr/sample", "Sample", queue.ClassBroken},
		{"sonarr/unable_to_determine_sample", "Unable to determine if file is a sample", queue.ClassUnknown},
		{"sonarr/episodes_not_found_in_grab", "Episode 1 was not found in the grabbed release: Show.S01E01", queue.ClassFixable},
		{"sonarr/episodes_unexpected_folder", "Episode 1 was unexpected considering the Series.Name folder name", queue.ClassFixable},
		{"sonarr/no_audio_tracks", "No audio tracks detected", queue.ClassBroken},
		{"sonarr/full_season_file", "Single episode file contains all episodes in seasons. Review file name or manually import", queue.ClassFixable},
		{"sonarr/episode_tba_title", "Episode has a TBA title and recently aired", queue.ClassUnknown},
		{"sonarr/episode_no_absolute_number", "Episode does not have an absolute episode number and recently aired", queue.ClassUnknown},
		{"sonarr/already_imported", "Episode file already imported at /path/to/ep.mkv", queue.ClassNotWanted},
		{"sonarr/found_matching_series_id", "Found matching series via grab history, but release was matched to series by ID. Automatic import is not possible. See the FAQ for details.", queue.ClassFixable},
		{"sonarr/series_title_mismatch", "Series title mismatch; automatic import is not possible", queue.ClassFixable},
		{"sonarr/missing_episodes_expected", "One or more episodes expected in this release were not imported or missing from the release", queue.ClassBroken},
		{"sonarr/single_episode_split", "Single episode split into multiple files", queue.ClassFixable},
		{"sonarr/multi_season_download", "Multi-season download, unable to import automatically", queue.ClassFixable},

		// Radarr fixed specifications.
		{"radarr/not_upgrade_movie", "Not an upgrade for existing movie file", queue.ClassNotWanted},
		{"radarr/not_upgrade_movie_quality", "Not an upgrade for existing movie file. Existing quality: Bluray-1080p", queue.ClassNotWanted},
		{"radarr/not_quality_revision", "Not a quality revision upgrade for existing movie file(s)", queue.ClassNotWanted},
		{"radarr/not_custom_format_upgrade", "Not a Custom Format upgrade for existing movie file(s)", queue.ClassNotWanted},
		{"radarr/suspected_multipart", "File is suspected multi-part file, Radarr doesn't support this", queue.ClassBroken},
		{"radarr/movie_not_found_in_grab", "Movie Movie.2024 was not found in the grabbed release: Movie.2024.1080p", queue.ClassFixable},
		{"radarr/already_imported", "Movie file already imported at /path/to/movie.mkv", queue.ClassNotWanted},
		{"radarr/found_matching_movie_id", "Found matching movie via grab history, but release was matched to movie by ID. Manual Import required.", queue.ClassFixable},
		{"radarr/movie_title_mismatch", "Movie title mismatch, automatic import is not possible. Manual Import required.", queue.ClassFixable},

		// Readarr fixed specifications.
		{"readarr/not_upgrade_book", "Not an upgrade for existing book file(s)", queue.ClassNotWanted},
		{"readarr/same_filesize", "Has the same filesize as existing file", queue.ClassNotWanted},
		{"readarr/already_imported", "Book already imported at /path/to/book.epub", queue.ClassNotWanted},

		// All-*arr shared specifications.
		{"shared/dangerous_file", "Caution: Found potentially dangerous file with extension: .exe", queue.ClassBroken},
		{"shared/executable_file", "Caution: Found executable file with extension: '.exe'", queue.ClassBroken},
		{"shared/appledouble_invalid_video", "Invalid video file, filename starts with '._'", queue.ClassBroken},
		{"shared/unsupported_extension", "Invalid video file, unsupported extension: '.iso'", queue.ClassBroken},
		{"shared/archive_file", "Found archive file, might need to be extracted", queue.ClassBroken},
		{"shared/unable_to_parse", "Unable to parse download, automatic import is not possible.", queue.ClassFixable},
		{"shared/category_skip", "Download wasn't grabbed by Sonarr and not in a category, Skipping.", queue.ClassUnknown},
		{"shared/remote_path_mapping", "[/data/dl] is not a valid local path. You may need a Remote Path Mapping.", queue.ClassUnknown},
		{"shared/not_enough_free_space", "Not enough free space", queue.ClassUnknown},
		{"shared/still_unpacking", "File is still being unpacked", queue.ClassUnknown},
		{"shared/locked_file", "Locked file, try again later", queue.ClassUnknown},
		{"shared/mapped_to_root", "Import path is mapped to a series folder", queue.ClassUnknown},
		{"shared/destination_not_root", "Destination artist folder /music is not in a Root Folder", queue.ClassUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			item := queue.Item{
				Title:       "Test.Item.Title",
				StatusLines: []string{"Test.Item.Title", tt.line},
				Added:       oldAdded,
			}
			gotClass, _, _ := queue.ClassifyImportItem(item, s, now)
			if gotClass != tt.wantClass {
				t.Errorf("ClassifyImportItem(%q) = %q, want %q", tt.line, gotClass, tt.wantClass)
			}
		})
	}
}

func TestClassifierNegativeAndNearMiss(t *testing.T) {
	t.Parallel()

	s := queue.DefaultSettings()
	now := fixedNow

	nearMisses := []string{
		"Almost not an upgrade",
		"Not an upgrade for something else",
		"This is not a real arr error message",
		"Random indexer message: download failed due to network error",
		"Has missing tracks and broken disc",
		"Sample.mkv is playing fine",
	}

	for _, line := range nearMisses {
		t.Run(line, func(t *testing.T) {
			t.Parallel()
			item := queue.Item{
				Title:       "Title",
				StatusLines: []string{"Title", line},
				Added:       now.Add(-1 * time.Hour),
			}
			c, _, _ := queue.ClassifyImportItem(item, s, now)
			if c != queue.ClassUnknown {
				t.Errorf("ClassifyImportItem(%q) = %q, want %q", line, c, queue.ClassUnknown)
			}
		})
	}
}

func TestClassifierDistanceBoundaries(t *testing.T) {
	t.Parallel()

	s := queue.DefaultSettings()
	now := fixedNow

	tests := []struct {
		name          string
		line          string
		wantClass     queue.Class
		wantSubreason string
		wantPct       float64
	}{
		// Album match: floor 75.0%
		{"album_exact_floor", "Album match is not close enough: 75.0% vs 80%", queue.ClassFixable, "", 75.0},
		{"album_exact_floor_comma", "Album match is not close enough: 75,0% vs 80%", queue.ClassFixable, "", 75.0},
		{"album_above_floor", "Album match is not close enough: 77.5% vs 80%", queue.ClassFixable, "", 77.5},
		{"album_below_floor", "Album match is not close enough: 74.9% vs 80%", queue.ClassUnknown, queue.SubreasonRematchPending, 74.9},
		{"album_below_floor_comma", "Album match is not close enough: 74,9% vs 80%", queue.ClassUnknown, queue.SubreasonRematchPending, 74.9},
		{"album_very_low", "Album match is not close enough: 10.2% vs 80%", queue.ClassUnknown, queue.SubreasonRematchPending, 10.2},

		// Book match: floor 75.0%
		{"book_exact_floor", "Book match is not close enough: 75.0% vs 80%", queue.ClassFixable, "", 75.0},
		{"book_below_floor", "Book match is not close enough: 74.9% vs 80%", queue.ClassUnknown, queue.SubreasonRematchPending, 74.9},

		// Worst track match: floor 50.0%
		{"worst_track_exact_floor", "Worst track match: 50.0% vs 60%", queue.ClassFixable, "", 50.0},
		{"worst_track_below_floor", "Worst track match: 49.9% vs 60%", queue.ClassUnknown, queue.SubreasonRematchPending, 49.9},
		{"worst_track_comma", "Worst track match: 49,9% vs 60%", queue.ClassUnknown, queue.SubreasonRematchPending, 49.9},

		// Track match is not close enough: floor 50.0%
		{"track_exact_floor", "Track match is not close enough: 50.0% vs 60%", queue.ClassFixable, "", 50.0},
		{"track_below_floor", "Track match is not close enough: 49.9% vs 60%", queue.ClassUnknown, queue.SubreasonRematchPending, 49.9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			item := queue.Item{
				Title:       "Album",
				StatusLines: []string{"Album", tt.line},
				Added:       now.Add(-1 * time.Hour),
			}
			c, pat, pct := queue.ClassifyImportItem(item, s, now)
			if c != tt.wantClass {
				t.Errorf("class = %q, want %q", c, tt.wantClass)
			}
			if tt.wantSubreason != "" && pat != tt.wantSubreason {
				t.Errorf("pattern = %q, want subreason %q", pat, tt.wantSubreason)
			}
			if pct == nil || math.Abs(*pct-tt.wantPct) > 0.01 {
				t.Errorf("pct = %v, want %v", pct, tt.wantPct)
			}
		})
	}
}

func TestClassifierPrecedenceAndCombinations(t *testing.T) {
	t.Parallel()

	s := queue.DefaultSettings()
	s.Grace = 15 * time.Minute
	now := fixedNow
	oldAdded := now.Add(-1 * time.Hour)

	t.Run("empty_lines_unknown", func(t *testing.T) {
		t.Parallel()
		item := queue.Item{Title: "Title", StatusLines: []string{}, Added: oldAdded}
		c, _, _ := queue.ClassifyImportItem(item, s, now)
		if c != queue.ClassUnknown {
			t.Errorf("empty lines class = %q, want unknown", c)
		}
	})

	t.Run("title_only_unknown", func(t *testing.T) {
		t.Parallel()
		item := queue.Item{Title: "Title", StatusLines: []string{"Title"}, Added: oldAdded}
		c, _, _ := queue.ClassifyImportItem(item, s, now)
		if c != queue.ClassUnknown {
			t.Errorf("title only class = %q, want unknown", c)
		}
	})

	t.Run("unknown_wins_over_broken", func(t *testing.T) {
		t.Parallel()
		item := queue.Item{
			Title: "Title",
			StatusLines: []string{
				"Title",
				"Not enough free space",
				"Caution: Found executable file with extension: '.exe'",
			},
			Added: oldAdded,
		}
		c, _, _ := queue.ClassifyImportItem(item, s, now)
		if c != queue.ClassUnknown {
			t.Errorf("unknown + broken class = %q, want unknown", c)
		}
	})

	t.Run("broken_wins_over_fixable", func(t *testing.T) {
		t.Parallel()
		item := queue.Item{
			Title: "Title",
			StatusLines: []string{
				"Title",
				"Sample",
				"Has missing tracks",
			},
			Added: oldAdded,
		}
		c, _, _ := queue.ClassifyImportItem(item, s, now)
		if c != queue.ClassBroken {
			t.Errorf("broken + fixable class = %q, want broken", c)
		}
	})

	t.Run("broken_wins_over_not_wanted", func(t *testing.T) {
		t.Parallel()
		item := queue.Item{
			Title: "Title",
			StatusLines: []string{
				"Title",
				"Sample",
				"Has fewer tracks than existing release",
			},
			Added: oldAdded,
		}
		c, _, _ := queue.ClassifyImportItem(item, s, now)
		if c != queue.ClassBroken {
			t.Errorf("broken + not_wanted class = %q, want broken", c)
		}
	})

	t.Run("not_wanted_wins_over_fixable", func(t *testing.T) {
		t.Parallel()
		item := queue.Item{
			Title: "Title",
			StatusLines: []string{
				"Title",
				"Has fewer tracks than existing release",
				"Has missing tracks",
			},
			Added: oldAdded,
		}
		c, _, _ := queue.ClassifyImportItem(item, s, now)
		if c != queue.ClassNotWanted {
			t.Errorf("not_wanted + fixable class = %q, want not_wanted", c)
		}
	})

	t.Run("has_missing_and_unmatched_tracks_fixable", func(t *testing.T) {
		t.Parallel()
		item := queue.Item{
			Title: "Title",
			StatusLines: []string{
				"Title",
				"Has missing tracks",
				"Has unmatched tracks",
				"Album match is not close enough: 76.0% vs 80%",
			},
			Added: oldAdded,
		}
		c, _, _ := queue.ClassifyImportItem(item, s, now)
		if c != queue.ClassFixable {
			t.Errorf("missing/unmatched + fixable distance class = %q, want fixable", c)
		}
	})

	t.Run("worst_percent_carried", func(t *testing.T) {
		t.Parallel()
		item := queue.Item{
			Title: "Title",
			StatusLines: []string{
				"Title",
				"Album match is not close enough: 80.0% vs 80%",
				"Worst track match: 55.0% vs 60%",
			},
			Added: oldAdded,
		}
		c, _, pct := queue.ClassifyImportItem(item, s, now)
		if c != queue.ClassFixable {
			t.Errorf("class = %q, want fixable", c)
		}
		if pct == nil || *pct != 55.0 {
			t.Errorf("pct = %v, want 55.0", pct)
		}
	})
}

func TestClassifierNoFilesEligibleOverrides(t *testing.T) {
	t.Parallel()

	s := queue.DefaultSettings()
	s.Grace = 15 * time.Minute
	now := fixedNow

	t.Run("broken_after_grace", func(t *testing.T) {
		t.Parallel()
		item := queue.Item{
			Title:       "Title",
			StatusLines: []string{"No files found are eligible for import in /downloads/path"},
			Added:       now.Add(-20 * time.Minute), // past 15m grace
		}
		c, _, _ := queue.ClassifyImportItem(item, s, now)
		if c != queue.ClassBroken {
			t.Errorf("class = %q, want broken", c)
		}
	})

	t.Run("unknown_within_grace", func(t *testing.T) {
		t.Parallel()
		item := queue.Item{
			Title:       "Title",
			StatusLines: []string{"No files found are eligible for import in /downloads/path"},
			Added:       now.Add(-5 * time.Minute), // within 15m grace
		}
		c, _, _ := queue.ClassifyImportItem(item, s, now)
		if c != queue.ClassUnknown {
			t.Errorf("class = %q, want unknown", c)
		}
	})

	t.Run("unknown_when_still_unpacking", func(t *testing.T) {
		t.Parallel()
		item := queue.Item{
			Title: "Title",
			StatusLines: []string{
				"No files found are eligible for import in /downloads/path",
				"File is still being unpacked",
			},
			Added: now.Add(-30 * time.Minute),
		}
		c, _, _ := queue.ClassifyImportItem(item, s, now)
		if c != queue.ClassUnknown {
			t.Errorf("class = %q, want unknown", c)
		}
	})

	t.Run("unknown_when_locked_file", func(t *testing.T) {
		t.Parallel()
		item := queue.Item{
			Title: "Title",
			StatusLines: []string{
				"No files found are eligible for import in /downloads/path",
				"Locked file, try again later",
			},
			Added: now.Add(-30 * time.Minute),
		}
		c, _, _ := queue.ClassifyImportItem(item, s, now)
		if c != queue.ClassUnknown {
			t.Errorf("class = %q, want unknown", c)
		}
	})
}

func TestClassifyRejection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		rejection string
		wantClass queue.Class
	}{
		{"Single episode split into multiple files", queue.ClassFixable},
		{"Series title mismatch; automatic import is not possible", queue.ClassFixable},
		{"Not an upgrade for existing episode file(s)", queue.ClassNotWanted},
		{"Sample", queue.ClassBroken},
		{"Not enough free space", queue.ClassUnknown},
		{"Completely unknown error", queue.ClassUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.rejection, func(t *testing.T) {
			t.Parallel()
			got := queue.ClassifyRejection(tt.rejection)
			if got != tt.wantClass {
				t.Errorf("ClassifyRejection(%q) = %q, want %q", tt.rejection, got, tt.wantClass)
			}
		})
	}
}
