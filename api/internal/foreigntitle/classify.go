// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package foreigntitle

import (
	"regexp"
	"slices"
	"strings"
)

// Classify determines the audio type of a release based on embedded rules.
// Untagged groups and audio tags yield AudioDubbed. Dubbed takes precedence over
// subtitle-only tags (e.g. GERMAN + OmU). Subtitle-only tags yield AudioSubOnly.
func Classify(release string, r Rules) Audio {
	if isUntaggedGroup(release, r.UntaggedGroups) {
		return AudioDubbed
	}
	if matchPatterns(release, r.audioPatterns, r.cachedAudioTags, r.AudioTags) {
		return AudioDubbed
	}
	if matchPatterns(release, r.subPatterns, r.cachedSubTags, r.SubtitleOnlyTags) {
		return AudioSubOnly
	}
	return AudioUnknown
}

// matchPatterns matches release against compiled patterns if cache is fresh, falling back to compiling raw tags.
func matchPatterns(release string, compiled []*regexp.Regexp, cached []string, current []string) bool {
	if len(compiled) > 0 && slices.Equal(cached, current) {
		return matchCompiledPatterns(release, compiled)
	}
	return matchRawPatterns(release, current)
}

// matchCompiledPatterns tests release against precompiled regular expressions bounded by maxTagPatterns.
func matchCompiledPatterns(release string, compiled []*regexp.Regexp) bool {
	limit := min(len(compiled), maxTagPatterns)
	for i := range limit {
		if compiled[i].MatchString(release) {
			return true
		}
	}
	return false
}

// matchRawPatterns compiles raw tags on the fly and matches them against release.
func matchRawPatterns(release string, raw []string) bool {
	compiled, err := compileTagPatterns(raw)
	if err != nil {
		return false
	}
	return matchCompiledPatterns(release, compiled)
}

// isUntaggedGroup returns true if release contains an untagged group token at the start or terminal position.
func isUntaggedGroup(release string, untagged []string) bool {
	const maxGroups = 50
	lowerRel := strings.ToLower(release)
	limit := min(len(untagged), maxGroups)
	for i := range limit {
		grp := strings.ToLower(strings.TrimSpace(untagged[i]))
		if grp == "" {
			continue
		}
		if hasGroupToken(lowerRel, grp) {
			return true
		}
	}
	return false
}

// hasGroupToken tests if lowerRel matches group at start or as a terminal token with optional extension.
func hasGroupToken(lowerRel, grp string) bool {
	if strings.HasPrefix(lowerRel, "["+grp+"]") {
		return true
	}
	if matchesTerminalGroup(lowerRel, grp) {
		return true
	}
	if base, ok := stripOneExtension(lowerRel); ok {
		return matchesTerminalGroup(base, grp)
	}
	return false
}

// matchesTerminalGroup tests if s ends with -grp or [grp].
func matchesTerminalGroup(s, grp string) bool {
	return strings.HasSuffix(s, "-"+grp) || strings.HasSuffix(s, "["+grp+"]")
}

// stripOneExtension strips a trailing dot-separated file extension if it is recognized.
func stripOneExtension(s string) (string, bool) {
	idx := strings.LastIndexByte(s, '.')
	if idx < 0 {
		return "", false
	}
	ext := s[idx+1:]
	if isExtension(ext) {
		return s[:idx], true
	}
	return "", false
}

// isExtension reports whether ext is a release or download file extension that may follow
// a terminal group token. A closed list keeps title words such as "Movie" from counting.
func isExtension(ext string) bool {
	switch ext {
	case "mkv", "mp4", "m4v", "avi", "ts", "wmv", "nzb", "torrent":
		return true
	}
	return false
}
