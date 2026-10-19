// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package queue

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Class categorizes a queue import failure reason.
type Class string

// Queue import failure classes.
const (
	ClassFixable   Class = "fixable"
	ClassNotWanted Class = "not_wanted"
	ClassBroken    Class = "broken"
	ClassUnknown   Class = "unknown"
)

// SubreasonRematchPending marks a Lidarr/Readarr distance message below the fixable floor.
const SubreasonRematchPending = "rematch_pending"

type patternRule struct {
	re    *regexp.Regexp
	class Class
}

var (
	// Lidarr distance specifications.
	// src/NzbDrone.Core/MediaFiles/TrackImport/Specifications/CloseAlbumMatchSpecification.cs:34.
	reAlbumMatch = regexp.MustCompile(`(?i)^Album match is not close enough:\s*([0-9]+(?:[.,][0-9]+)?)\s*%\s*vs\s*[0-9]+(?:[.,][0-9]+)?\s*%`)
	// src/NzbDrone.Core/MediaFiles/TrackImport/Specifications/CloseAlbumMatchSpecification.cs:50.
	reWorstTrackMatch = regexp.MustCompile(`(?i)^Worst track match:\s*([0-9]+(?:[.,][0-9]+)?)\s*%\s*vs\s*[0-9]+(?:[.,][0-9]+)?\s*%`)
	// src/NzbDrone.Core/MediaFiles/TrackImport/Specifications/CloseTrackMatchSpecification.cs:26.
	reTrackMatch = regexp.MustCompile(`(?i)^Track match is not close enough:\s*([0-9]+(?:[.,][0-9]+)?)\s*%\s*vs\s*[0-9]+(?:[.,][0-9]+)?\s*%`)
	// Readarr distance specification.
	// src/NzbDrone.Core/MediaFiles/BookImport/Specifications/CloseAlbumMatchSpecification.cs:32,45.
	reBookMatch = regexp.MustCompile(`(?i)^Book match is not close enough:\s*([0-9]+(?:[.,][0-9]+)?)\s*%\s*vs\s*[0-9]+(?:[.,][0-9]+)?\s*%`)

	// All-apps special unpack/lock markers.
	// src/NzbDrone.Core/MediaFiles/EpisodeImport/Specifications/NotUnpackingSpecification.cs:43.
	reStillUnpacking = regexp.MustCompile(`(?i)^File is still being unpacked$`)
	// src/NzbDrone.Core/MediaFiles/DownloadedEpisodesImportService.cs:354.
	reLockedFile = regexp.MustCompile(`(?i)^Locked file, try again later$`)
	// src/NzbDrone.Core/Download/CompletedDownloadService.cs.
	reNoFilesFound = regexp.MustCompile(`(?i)^No files found are eligible for import in .*`)
)

var staticPatterns = []patternRule{
	// Lidarr fixed specifications.
	// src/NzbDrone.Core/MediaFiles/TrackImport/Specifications/CloseAlbumMatchSpecification.cs:41
	{re: regexp.MustCompile(`(?i)^No tracks matched$`), class: ClassBroken},
	// src/NzbDrone.Core/MediaFiles/TrackImport/Specifications/NoMissingOrUnmatchedTracksSpecification.cs:28
	{re: regexp.MustCompile(`(?i)^Has missing tracks$`), class: ClassFixable},
	// src/NzbDrone.Core/MediaFiles/TrackImport/Specifications/NoMissingOrUnmatchedTracksSpecification.cs:22
	{re: regexp.MustCompile(`(?i)^Has unmatched tracks$`), class: ClassFixable},
	// src/NzbDrone.Core/MediaFiles/TrackImport/Specifications/MoreTracksSpecification.cs:26
	{re: regexp.MustCompile(`(?i)^Has fewer tracks than existing release$`), class: ClassNotWanted},
	// src/NzbDrone.Core/MediaFiles/TrackImport/Specifications/SameTracksImportSpecification.cs:30
	{re: regexp.MustCompile(`(?i)^Track file on disk contains more tracks than this file contains$`), class: ClassNotWanted},
	// src/NzbDrone.Core/MediaFiles/TrackImport/Specifications/ReleaseWantedSpecification.cs:25
	{re: regexp.MustCompile(`(?i)^Album release not requested$`), class: ClassNotWanted},
	// src/NzbDrone.Core/MediaFiles/TrackImport/Specifications/UpgradeSpecification.cs:53,60
	{re: regexp.MustCompile(`(?i)^Not an upgrade for existing track file\(s\)(\. New Quality is .*)?$`), class: ClassNotWanted},
	// src/NzbDrone.Core/MediaFiles/TrackImport/Specifications/AlbumUpgradeSpecification.cs:43
	{re: regexp.MustCompile(`(?i)^Not an upgrade for existing album file\(s\)$`), class: ClassNotWanted},
	// src/NzbDrone.Core/MediaFiles/TrackImport/ImportDecisionMaker.cs:219
	{re: regexp.MustCompile(`(?i)^Couldn't find similar album for .*`), class: ClassBroken},
	// src/NzbDrone.Core/Download/CompletedDownloadService.cs:96
	{re: regexp.MustCompile(`(?i)^Artist name mismatch, automatic import is not possible.*`), class: ClassFixable},
	// src/NzbDrone.Core/Download/CompletedDownloadService.cs:156
	{re: regexp.MustCompile(`(?i)^One or more tracks expected in this release were not imported or missing from the release$`), class: ClassFixable},

	// Sonarr fixed specifications.
	// src/NzbDrone.Core/MediaFiles/EpisodeImport/Specifications/UpgradeSpecification.cs:48
	{re: regexp.MustCompile(`(?i)^Not an upgrade for existing episode file\(s\)(\. Existing quality: .*)?$`), class: ClassNotWanted},
	// src/NzbDrone.Core/MediaFiles/EpisodeImport/Specifications/UpgradeSpecification.cs:58
	{re: regexp.MustCompile(`(?i)^Not a quality revision upgrade for existing episode file\(s\)$`), class: ClassNotWanted},
	// src/NzbDrone.Core/MediaFiles/EpisodeImport/Specifications/UpgradeSpecification.cs:78,88
	{re: regexp.MustCompile(`(?i)^Not a Custom Format upgrade for existing episode file\(s\).*`), class: ClassNotWanted},
	// src/NzbDrone.Core/MediaFiles/EpisodeImport/Specifications/UnverifiedSceneNumberingSpecification.cs:27
	{re: regexp.MustCompile(`(?i)^This show has individual episode mappings on TheXEM.*`), class: ClassFixable},
	// src/NzbDrone.Core/MediaFiles/EpisodeImport/Specifications/SameEpisodesImportSpecification.cs:30
	{re: regexp.MustCompile(`(?i)^Episode file on disk contains more episodes than this file contains$`), class: ClassNotWanted},
	// src/NzbDrone.Core/MediaFiles/EpisodeImport/Specifications/NotSampleSpecification.cs:34
	{re: regexp.MustCompile(`(?i)^Sample$`), class: ClassBroken},
	// src/NzbDrone.Core/MediaFiles/EpisodeImport/Specifications/NotSampleSpecification.cs:38 (override: unknown)
	{re: regexp.MustCompile(`(?i)^Unable to determine if file is a sample$`), class: ClassUnknown},
	// src/NzbDrone.Core/MediaFiles/EpisodeImport/Specifications/MatchesGrabSpecification.cs:42,45
	{re: regexp.MustCompile(`(?i)^Episodes? .* (?:was|were) not found in the grabbed release: .*`), class: ClassFixable},
	// src/NzbDrone.Core/MediaFiles/EpisodeImport/Specifications/MatchesFolderSpecification.cs:72,75
	{re: regexp.MustCompile(`(?i)^Episodes? .* (?:was|were) unexpected considering the .* folder name$`), class: ClassFixable},
	// src/NzbDrone.Core/MediaFiles/EpisodeImport/Specifications/HasAudioTrackSpecification.cs:28
	{re: regexp.MustCompile(`(?i)^No audio tracks detected$`), class: ClassBroken},
	// src/NzbDrone.Core/MediaFiles/EpisodeImport/Specifications/FullSeasonSpecification.cs:26
	{re: regexp.MustCompile(`(?i)^Single episode file contains all episodes in seasons\. Review file name or manually import$`), class: ClassFixable},
	// src/NzbDrone.Core/MediaFiles/EpisodeImport/Specifications/EpisodeTitleSpecification.cs:84,91
	{re: regexp.MustCompile(`(?i)^Episode (?:does not have a title|has a TBA title) and recently aired$`), class: ClassUnknown},
	// src/NzbDrone.Core/MediaFiles/EpisodeImport/Specifications/AbsoluteEpisodeNumberSpecification.cs:51
	{re: regexp.MustCompile(`(?i)^Episode does not have an absolute episode number and recently aired$`), class: ClassUnknown},
	// src/NzbDrone.Core/MediaFiles/EpisodeImport/Specifications/AlreadyImportedSpecification.cs:67,73 (override: not_wanted)
	{re: regexp.MustCompile(`(?i)^Episode file already imported at .*`), class: ClassNotWanted},
	// src/NzbDrone.Core/Download/CompletedDownloadService.cs:124
	{re: regexp.MustCompile(`(?i)^Found matching series via grab history, but release was matched to series by ID\. Automatic import is not possible\..*`), class: ClassFixable},
	// src/NzbDrone.Core/Download/CompletedDownloadService.cs:112
	{re: regexp.MustCompile(`(?i)^Series title mismatch; automatic import is not possible.*`), class: ClassFixable},
	// src/NzbDrone.Core/Download/CompletedDownloadService.cs:202
	{re: regexp.MustCompile(`(?i)^One or more episodes expected in this release were not imported or missing from the release$`), class: ClassBroken},
	// src/NzbDrone.Core/MediaFiles/EpisodeImport/Specifications/SplitEpisodeSpecification.cs:26
	{re: regexp.MustCompile(`(?i)^Single episode split into multiple files$`), class: ClassFixable},
	// src/NzbDrone.Core/MediaFiles/DownloadedEpisodesImportService.cs:215
	{re: regexp.MustCompile(`(?i)^Multi-season download, unable to import automatically$`), class: ClassFixable},

	// Radarr fixed specifications.
	// src/NzbDrone.Core/MediaFiles/MovieImport/Specifications/UpgradeSpecification.cs:48
	{re: regexp.MustCompile(`(?i)^Not an upgrade for existing movie file(\. Existing quality: .*)?$`), class: ClassNotWanted},
	// src/NzbDrone.Core/MediaFiles/MovieImport/Specifications/UpgradeSpecification.cs:58
	{re: regexp.MustCompile(`(?i)^Not a quality revision upgrade for existing movie file\(s\)$`), class: ClassNotWanted},
	// src/NzbDrone.Core/MediaFiles/MovieImport/Specifications/UpgradeSpecification.cs:75
	{re: regexp.MustCompile(`(?i)^Not a Custom Format upgrade for existing movie file\(s\).*`), class: ClassNotWanted},
	// src/NzbDrone.Core/MediaFiles/MovieImport/Specifications/NotMultiPartSpecification.cs:40 (override: broken)
	{re: regexp.MustCompile(`(?i)^File is suspected multi-part file, Radarr doesn't support this$`), class: ClassBroken},
	// src/NzbDrone.Core/MediaFiles/MovieImport/Specifications/MatchesGrabSpecification.cs:36
	{re: regexp.MustCompile(`(?i)^Movie .* was not found in the grabbed release: .*`), class: ClassFixable},
	// src/NzbDrone.Core/MediaFiles/MovieImport/Specifications/AlreadyImportedSpecification.cs:67,73 (override: not_wanted)
	{re: regexp.MustCompile(`(?i)^Movie file already imported at .*`), class: ClassNotWanted},
	// src/NzbDrone.Core/Download/CompletedDownloadService.cs:112
	{re: regexp.MustCompile(`(?i)^Found matching movie via grab history, but release was matched to movie by ID\. Manual Import required\.$`), class: ClassFixable},
	// src/NzbDrone.Core/Download/CompletedDownloadService.cs:100
	{re: regexp.MustCompile(`(?i)^Movie title mismatch, automatic import is not possible\. Manual Import required\.$`), class: ClassFixable},

	// Readarr fixed specifications.
	// src/NzbDrone.Core/MediaFiles/BookImport/Specifications/UpgradeSpecification.cs:46,53
	{re: regexp.MustCompile(`(?i)^Not an upgrade for existing book file\(s\)$`), class: ClassNotWanted},
	// src/NzbDrone.Core/MediaFiles/BookImport/Specifications/SameFileSpecification.cs:41
	{re: regexp.MustCompile(`(?i)^Has the same filesize as existing file$`), class: ClassNotWanted},
	// src/NzbDrone.Core/MediaFiles/BookImport/Specifications/AlreadyImportedSpecification.cs:60 (override: not_wanted)
	{re: regexp.MustCompile(`(?i)^Book already imported at .*`), class: ClassNotWanted},

	// All-*arr shared specifications.
	// src/NzbDrone.Core/MediaFiles/RejectedFileExtensionService.cs:34
	{re: regexp.MustCompile(`(?i)^Caution: Found potentially dangerous file(?: with extension: .*)?$`), class: ClassBroken},
	// src/NzbDrone.Core/MediaFiles/RejectedFileExtensionService.cs:41
	{re: regexp.MustCompile(`(?i)^Caution: Found executable file(?: with extension: '.*')?$`), class: ClassBroken},
	// src/NzbDrone.Core/MediaFiles/DownloadedEpisodesImportService.cs:275
	{re: regexp.MustCompile(`(?i)^Invalid (?:video|music) file, filename starts with '._'`), class: ClassBroken},
	// src/NzbDrone.Core/MediaFiles/DownloadedEpisodesImportService.cs:321
	{re: regexp.MustCompile(`(?i)^Invalid (?:video|audio) file, unsupported extension: '.*'`), class: ClassBroken},
	// src/NzbDrone.Core/MediaFiles/DownloadedEpisodesImportService.cs:381
	{re: regexp.MustCompile(`(?i)^Found archive file, might need to be extracted$`), class: ClassBroken},
	// src/NzbDrone.Core/Download/CompletedDownloadService.cs:145
	{re: regexp.MustCompile(`(?i)^Unable to parse download, automatic import is not possible\.$`), class: ClassFixable},
	// src/NzbDrone.Core/Download/CompletedDownloadService.cs:92
	{re: regexp.MustCompile(`(?i)^Download wasn't grabbed by .* and not in a category, [sS]kipping\.$`), class: ClassUnknown},
	// src/NzbDrone.Core/Download/CompletedDownloadService.cs:383
	{re: regexp.MustCompile(`(?i)^\[.*\] is not a valid local path\. You may need a Remote Path Mapping\..*`), class: ClassUnknown},
	// src/NzbDrone.Core/MediaFiles/EpisodeImport/Specifications/FreeSpaceSpecification.cs:53
	{re: regexp.MustCompile(`(?i)^Not enough free space$`), class: ClassUnknown},
	// src/NzbDrone.Core/MediaFiles/EpisodeImport/Specifications/NotUnpackingSpecification.cs:43
	{re: reStillUnpacking, class: ClassUnknown},
	// src/NzbDrone.Core/MediaFiles/DownloadedEpisodesImportService.cs:354
	{re: reLockedFile, class: ClassUnknown},
	// src/NzbDrone.Core/MediaFiles/DownloadedEpisodesImportService.cs:187
	{re: regexp.MustCompile(`(?i)^Import path is mapped to (?:a series|a movie|an artist) folder$`), class: ClassUnknown},
	// src/NzbDrone.Core/MediaFiles/TrackImport/Specifications/ArtistPathInRootFolderSpecification.cs:34
	{re: regexp.MustCompile(`(?i)^Destination .* folder .* is not in a Root Folder$`), class: ClassUnknown},
}

type lineClassification struct {
	class     Class
	pattern   string
	subreason string
	pct       *float64
}

func parsePercent(raw string) (float64, bool) {
	norm := strings.ReplaceAll(raw, ",", ".")
	val, err := strconv.ParseFloat(norm, 64)
	if err != nil {
		return 0, false
	}
	return val, true
}

func classifyDistanceLine(line string, s Settings) (lineClassification, bool) {
	if m := reAlbumMatch.FindStringSubmatch(line); len(m) > 1 {
		return evalDistance(m[0], m[1], s.MatchFixableMinPercent)
	}
	if m := reBookMatch.FindStringSubmatch(line); len(m) > 1 {
		return evalDistance(m[0], m[1], s.MatchFixableMinPercent)
	}
	if m := reWorstTrackMatch.FindStringSubmatch(line); len(m) > 1 {
		return evalDistance(m[0], m[1], s.TrackFixableMinPercent)
	}
	if m := reTrackMatch.FindStringSubmatch(line); len(m) > 1 {
		return evalDistance(m[0], m[1], s.TrackFixableMinPercent)
	}
	return lineClassification{}, false
}

func evalDistance(match, numStr string, fixableFloor float64) (lineClassification, bool) {
	p, ok := parsePercent(numStr)
	if !ok {
		return lineClassification{class: ClassUnknown}, true
	}
	if p >= fixableFloor {
		return lineClassification{class: ClassFixable, pattern: match, pct: &p}, true
	}
	return lineClassification{
		class:     ClassUnknown,
		pattern:   SubreasonRematchPending,
		subreason: SubreasonRematchPending,
		pct:       &p,
	}, true
}

func classifyLine(line string, s Settings, hasUnpackOrLocked, pastGrace bool) lineClassification {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return lineClassification{class: ClassUnknown}
	}
	if res, ok := classifyDistanceLine(trimmed, s); ok {
		return res
	}
	if reNoFilesFound.MatchString(trimmed) {
		if !hasUnpackOrLocked && pastGrace {
			return lineClassification{class: ClassBroken, pattern: reNoFilesFound.String()}
		}
		return lineClassification{class: ClassUnknown}
	}
	for _, p := range staticPatterns {
		if p.re.MatchString(trimmed) {
			return lineClassification{class: p.class, pattern: p.re.String()}
		}
	}
	return lineClassification{class: ClassUnknown}
}

// ClassifyRejection classifies a single rejection reason string using the shared pattern table.
// Unrecognized strings classify as unknown.
func ClassifyRejection(rejection string) Class {
	c := classifyLine(rejection, DefaultSettings(), false, true)
	return c.class
}

func resolveSettingsFloors(s Settings) Settings {
	if s.MatchFixableMinPercent <= 0 && s.MatchBrokenBelowPercent <= 0 {
		defs := DefaultSettings()
		s.MatchFixableMinPercent = defs.MatchFixableMinPercent
		s.MatchBrokenBelowPercent = defs.MatchBrokenBelowPercent
		s.TrackFixableMinPercent = defs.TrackFixableMinPercent
		s.TrackBrokenBelowPercent = defs.TrackBrokenBelowPercent
	}
	return s
}

type importCollector struct {
	sawUnknown   bool
	rematchOnly  bool
	brokenPat    string
	notWantedPat string
	fixablePat   string
	minMatchPct  *float64
}

func (c *importCollector) record(res lineClassification) {
	if res.pct != nil {
		c.minMatchPct = updateWorstPercent(c.minMatchPct, *res.pct)
	}
	switch res.class {
	case ClassUnknown:
		c.sawUnknown = true
		if res.subreason != SubreasonRematchPending {
			c.rematchOnly = false
		}
	case ClassBroken:
		c.rematchOnly = false
		if c.brokenPat == "" {
			c.brokenPat = res.pattern
		}
	case ClassNotWanted:
		c.rematchOnly = false
		if c.notWantedPat == "" {
			c.notWantedPat = res.pattern
		}
	case ClassFixable:
		if c.fixablePat == "" {
			c.fixablePat = res.pattern
		}
	}
}

// ClassifyImportItem classifies an item's status messages, considering grace period and returning any distance match percent.
func ClassifyImportItem(item Item, s Settings, now time.Time) (Class, string, *float64) {
	s = resolveSettingsFloors(s)
	lines := filterMeaningfulLines(item.StatusLines, item.Title)
	if len(lines) == 0 {
		return ClassUnknown, "", nil
	}

	hasUnpackOrLocked := checkUnpackOrLocked(lines)
	pastGrace := isPastGrace(item.Added, s.Grace, now)

	c := &importCollector{rematchOnly: true}
	for _, l := range lines {
		res := classifyLine(l, s, hasUnpackOrLocked, pastGrace)
		c.record(res)
	}

	return resolveClassPrecedence(c.sawUnknown, c.rematchOnly, c.brokenPat, c.notWantedPat, c.fixablePat, c.minMatchPct)
}

func resolveClassPrecedence(unknown, rematchOnly bool, broken, notWanted, fixable string, pct *float64) (Class, string, *float64) {
	if unknown {
		if rematchOnly && pct != nil {
			return ClassUnknown, SubreasonRematchPending, pct
		}
		return ClassUnknown, "", pct
	}
	if broken != "" {
		return ClassBroken, broken, pct
	}
	if notWanted != "" {
		return ClassNotWanted, notWanted, pct
	}
	if fixable != "" {
		return ClassFixable, fixable, pct
	}
	return ClassUnknown, "", pct
}

func filterMeaningfulLines(lines []string, title string) []string {
	cleanTitle := strings.TrimSpace(title)
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			continue
		}
		if cleanTitle != "" && strings.EqualFold(trimmed, cleanTitle) {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func checkUnpackOrLocked(lines []string) bool {
	for _, l := range lines {
		if reStillUnpacking.MatchString(l) || reLockedFile.MatchString(l) {
			return true
		}
	}
	return false
}

func isPastGrace(added time.Time, grace time.Duration, now time.Time) bool {
	if added.IsZero() || grace <= 0 {
		return true
	}
	if now.IsZero() {
		return true
	}
	return now.Sub(added) >= grace
}

func updateWorstPercent(curr *float64, next float64) *float64 {
	if curr == nil {
		v := next
		return &v
	}
	v := math.Min(*curr, next)
	return &v
}
