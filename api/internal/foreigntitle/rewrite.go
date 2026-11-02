// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package foreigntitle

import (
	"errors"
	"fmt"
	"strings"
)

// maxTitleWords limits the number of words allowed in foreign or canonical titles.
const maxTitleWords = 100

// RewriteTitle replaces the leading foreign title portion in release with canonical,
// matching ignoring case and using dots, spaces, underscores, and dashes as separators.
// It formats canonical using release separators and strictly preserves all trailing tokens byte-for-byte.
func RewriteTitle(release, foreign, canonical string) (string, error) {
	if strings.TrimSpace(release) == "" {
		return "", errors.New("empty release name")
	}
	foreignWords, foreignTrunc := splitWords(foreign)
	if foreignTrunc {
		return "", errors.New("foreign title exceeds word limit")
	}
	if len(foreignWords) == 0 {
		return "", errors.New("empty foreign title")
	}
	canonicalWords, canonicalTrunc := splitWords(canonical)
	if canonicalTrunc {
		return "", errors.New("canonical title exceeds word limit")
	}
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

// matchForeignPrefix matches words of foreign title sequentially against release starting at index 0.
func matchForeignPrefix(release string, foreignWords []string) (int, byte, error) {
	pos := 0
	var titleSep byte

	limit := min(len(foreignWords), maxTitleWords)
	for i := range limit {
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

// advanceSeparator scans past a sequence of separators in release and returns the next character index and primary separator.
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

// matchWordAt compares the word at pos in release against fw case-insensitively.
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

// pickSeparator determines the separator character to use when formatting canonical title words.
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

// splitWords splits a title into individual word tokens separated by release separators, returning words and whether truncation occurred.
func splitWords(s string) ([]string, bool) {
	const maxScanBytes = 2000
	var words []string
	var b strings.Builder
	truncated := len(s) > maxScanBytes
	limit := min(len(s), maxScanBytes)
	for i := range limit {
		c := s[i]
		if !isSep(c) {
			b.WriteByte(c)
			continue
		}
		if b.Len() == 0 {
			continue
		}
		var hitLimit bool
		words, hitLimit = appendTitleWord(words, b.String())
		b.Reset()
		if hitLimit {
			truncated = true
			break
		}
	}
	if b.Len() > 0 {
		var hitLimit bool
		words, hitLimit = appendTitleWord(words, b.String())
		if hitLimit {
			truncated = true
		}
	}
	return words, truncated
}

// appendTitleWord adds word to words if below maxTitleWords, returning the updated slice and whether the limit was exceeded.
// appendTitleWord appends word unless words already holds maxTitleWords, and reports
// whether the limit was hit.
func appendTitleWord(words []string, word string) ([]string, bool) {
	if len(words) >= maxTitleWords {
		return words, true
	}
	return append(words, word), false
}

// isSep checks whether byte b is a title word separator (dot, space, underscore, or dash).
func isSep(b byte) bool {
	return b == '.' || b == ' ' || b == '_' || b == '-'
}
