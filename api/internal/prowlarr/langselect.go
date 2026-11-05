// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package prowlarr

import (
	"fmt"
	"slices"
	"strings"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/prowlarrclient"
)

// maxIndexers bounds the number of indexers considered for language selection.
const maxIndexers = 1000

// maxIndexerTags bounds the tags inspected on each indexer.
const maxIndexerTags = 100

// IndexersForLanguage returns sorted, unique IDs of enabled indexers accepting lang.
// Valid lang-xx tags override the definition locale; otherwise its primary subtag
// decides. Invalid language keys and no matches return a non-nil empty slice.
// More than maxIndexers indexers or maxIndexerTags tags on any indexer returns
// an error wrapping domain.ErrInvalid, without partial results.
func IndexersForLanguage(indexers []prowlarrclient.Indexer, tagLabels map[int]string, lang string) ([]int, error) {
	if err := validateLanguageIndexers(indexers); err != nil {
		return nil, err
	}
	out := make([]int, 0)
	if !validLanguageKey(lang) {
		return out, nil
	}
	lang = strings.ToLower(lang)
	for _, indexer := range indexers {
		if indexer.Enabled && indexerAcceptsLanguage(indexer, tagLabels, lang) {
			out = append(out, indexer.ID)
		}
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// validateLanguageIndexers rejects oversized collections before selection begins.
func validateLanguageIndexers(indexers []prowlarrclient.Indexer) error {
	if len(indexers) > maxIndexers {
		return fmt.Errorf("%w: prowlarr language selection: %d indexers exceed the limit of %d", domain.ErrInvalid, len(indexers), maxIndexers)
	}
	for _, indexer := range indexers {
		if len(indexer.Tags) > maxIndexerTags {
			return fmt.Errorf("%w: prowlarr language selection: indexer %d: %d tags exceed the limit of %d", domain.ErrInvalid, indexer.ID, len(indexer.Tags), maxIndexerTags)
		}
	}
	return nil
}

// validLanguageKey reports whether lang consists of exactly two ASCII letters.
func validLanguageKey(lang string) bool {
	if len(lang) != 2 {
		return false
	}
	for i := range 2 {
		letter := lang[i]
		if (letter < 'a' || letter > 'z') && (letter < 'A' || letter > 'Z') {
			return false
		}
	}
	return true
}

// indexerAcceptsLanguage applies valid language tags before falling back to the locale.
func indexerAcceptsLanguage(indexer prowlarrclient.Indexer, tagLabels map[int]string, lang string) bool {
	hasLanguageTag := false
	for _, id := range indexer.Tags {
		label := strings.TrimSpace(tagLabels[id])
		if len(label) != 7 || !strings.EqualFold(label[:5], "lang-") {
			continue
		}
		tagLang := label[5:]
		if !validLanguageKey(tagLang) {
			continue
		}
		hasLanguageTag = true
		if strings.ToLower(tagLang) == lang {
			return true
		}
	}
	locale := strings.ToLower(indexer.Language)
	if separator := strings.IndexAny(locale, "-_"); separator >= 0 {
		locale = locale[:separator]
	}
	return !hasLanguageTag && locale == lang
}
