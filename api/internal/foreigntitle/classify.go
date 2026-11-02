// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package foreigntitle

import (
	"regexp"
	"strings"
)

// Classify determines the audio type of a release based on embedded rules.
// Untagged groups and audio tags yield AudioDubbed. Dubbed takes precedence over
// subtitle-only tags (e.g. DL + OmU). Subtitle-only tags yield AudioSubOnly.
func Classify(release string, r Rules) Audio {
	if isUntaggedGroup(release, r.UntaggedGroups) {
		return AudioDubbed
	}
	if matchPatterns(release, r.audioPatterns, r.AudioTags) {
		return AudioDubbed
	}
	if matchPatterns(release, r.subPatterns, r.SubtitleOnlyTags) {
		return AudioSubOnly
	}
	return AudioUnknown
}

func matchPatterns(release string, compiled []*regexp.Regexp, raw []string) bool {
	if len(compiled) == len(raw) && len(compiled) > 0 {
		return matchCompiledPatterns(release, compiled)
	}
	return matchRawPatterns(release, raw)
}

func matchCompiledPatterns(release string, compiled []*regexp.Regexp) bool {
	const maxCompiled = 50
	for i := 0; i < len(compiled) && i < maxCompiled; i++ {
		if compiled[i].MatchString(release) {
			return true
		}
	}
	return false
}

func matchRawPatterns(release string, raw []string) bool {
	compiled, err := compileTagPatterns(raw)
	if err != nil {
		return false
	}
	return matchCompiledPatterns(release, compiled)
}

func isUntaggedGroup(release string, untagged []string) bool {
	const maxGroups = 50
	lowerRel := strings.ToLower(release)
	for i := 0; i < len(untagged) && i < maxGroups; i++ {
		grp := strings.ToLower(strings.TrimSpace(untagged[i]))
		if grp == "" {
			continue
		}
		dashGrp := "-" + grp
		if strings.HasSuffix(lowerRel, dashGrp) {
			return true
		}
		if idx := strings.LastIndex(lowerRel, dashGrp); idx >= 0 {
			rem := lowerRel[idx+len(dashGrp):]
			if strings.HasPrefix(rem, ".") || strings.HasPrefix(rem, "-") {
				return true
			}
		}
		bracketGrp := "[" + grp + "]"
		if strings.HasPrefix(lowerRel, bracketGrp) || strings.HasSuffix(lowerRel, bracketGrp) {
			return true
		}
	}
	return false
}
