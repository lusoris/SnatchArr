// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package foreigntitle

import (
	"errors"
	"fmt"
	"strings"
)

// RewriteTitle replaces the leading foreign title portion in release with canonical,
// matching ignoring case and using dots, spaces, underscores, and dashes as separators.
// It formats canonical using release separators and strictly preserves all trailing tokens byte-for-byte.
func RewriteTitle(release, foreign, canonical string) (string, error) {
	if strings.TrimSpace(release) == "" {
		return "", errors.New("empty release name")
	}
	foreignWords := splitWords(foreign)
	if len(foreignWords) == 0 {
		return "", errors.New("empty foreign title")
	}
	canonicalWords := splitWords(canonical)
	if len(canonicalWords) == 0 {
		return "", errors.New("empty canonical title")
	}

	matchEnd, titleSep, err := matchForeignPrefix(release, foreignWords)
	if err != nil {
		return "", err
	}

	sep := pickSeparator(release, matchEnd, titleSep)
	canonicalJoined := strings.Join(canonicalWords, sep)
	releaseSuffix := release[matchEnd:]

	return canonicalJoined + releaseSuffix, nil
}

func matchForeignPrefix(release string, foreignWords []string) (int, byte, error) {
	const maxWords = 100
	pos := 0
	var titleSep byte

	for i := 0; i < len(foreignWords) && i < maxWords; i++ {
		fw := foreignWords[i]
		if i > 0 {
			nextPos, sep, err := advanceSeparator(release, pos)
			if err != nil {
				return 0, 0, err
			}
			pos = nextPos
			if titleSep == 0 {
				titleSep = sep
			}
		}
		nextPos, err := matchWordAt(release, pos, fw)
		if err != nil {
			return 0, 0, err
		}
		pos = nextPos
	}

	return pos, titleSep, nil
}

func advanceSeparator(release string, pos int) (int, byte, error) {
	if pos >= len(release) || !isSep(release[pos]) {
		return 0, 0, errors.New("missing separator between words in release")
	}
	sep := release[pos]
	for pos < len(release) && isSep(release[pos]) {
		pos++
	}
	return pos, sep, nil
}

func matchWordAt(release string, pos int, fw string) (int, error) {
	wordStart := pos
	for pos < len(release) && !isSep(release[pos]) {
		pos++
	}
	releaseWord := release[wordStart:pos]
	if !strings.EqualFold(releaseWord, fw) {
		return 0, fmt.Errorf("release word %q does not match %q", releaseWord, fw)
	}
	return pos, nil
}

func pickSeparator(release string, matchEnd int, titleSep byte) string {
	if titleSep != 0 {
		return string(titleSep)
	}
	if matchEnd < len(release) && isSep(release[matchEnd]) {
		return string(release[matchEnd])
	}
	switch {
	case strings.ContainsRune(release, '.'):
		return "."
	case strings.ContainsRune(release, '_'):
		return "_"
	case strings.ContainsRune(release, '-'):
		return "-"
	case strings.ContainsRune(release, ' '):
		return " "
	default:
		return "."
	}
}

func splitWords(s string) []string {
	const maxWords = 100
	var words []string
	var b strings.Builder
	for i := 0; i < len(s) && i < 1000; i++ {
		c := s[i]
		if isSep(c) {
			if b.Len() > 0 {
				words = append(words, b.String())
				b.Reset()
				if len(words) >= maxWords {
					break
				}
			}
		} else {
			b.WriteByte(c)
		}
	}
	if b.Len() > 0 && len(words) < maxWords {
		words = append(words, b.String())
	}
	return words
}

func isSep(b byte) bool {
	return b == '.' || b == ' ' || b == '_' || b == '-'
}
