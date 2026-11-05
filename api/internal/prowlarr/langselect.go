// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package prowlarr

import (
	"slices"
	"strings"

	"github.com/lusoris/SnatchArr/api/internal/prowlarrclient"
)

// IndexersForLanguage returns sorted, unique IDs of enabled indexers accepting lang.
// Valid lang-xx tags override the definition locale; otherwise its primary subtag
// decides. Invalid language keys and no matches return a non-nil empty slice.
func IndexersForLanguage(indexers []prowlarrclient.Indexer, tagLabels map[int]string, lang string) []int {
	out := make([]int, 0)
	lang = strings.ToLower(lang)
	if !validLanguageKey(lang) {
		return out
	}
	for _, indexer := range indexers {
		if indexer.Enabled && indexerAcceptsLanguage(indexer, tagLabels, lang) {
			out = append(out, indexer.ID)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// validLanguageKey reports whether lang consists of exactly two lowercase ASCII letters.
func validLanguageKey(lang string) bool {
	if len(lang) != 2 {
		return false
	}
	for _, letter := range lang {
		if letter < 'a' || letter > 'z' {
			return false
		}
	}
	return true
}

// indexerAcceptsLanguage applies valid language tags before falling back to the locale.
func indexerAcceptsLanguage(indexer prowlarrclient.Indexer, tagLabels map[int]string, lang string) bool {
	hasLanguageTag := false
	for _, id := range indexer.Tags {
		label := strings.ToLower(strings.TrimSpace(tagLabels[id]))
		tagLang, ok := strings.CutPrefix(label, "lang-")
		if !ok || !validLanguageKey(tagLang) {
			continue
		}
		hasLanguageTag = true
		if tagLang == lang {
			return true
		}
	}
	locale := strings.ToLower(indexer.Language)
	if separator := strings.IndexAny(locale, "-_"); separator >= 0 {
		locale = locale[:separator]
	}
	return !hasLanguageTag && locale == lang
}
