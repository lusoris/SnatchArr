// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package foreigntitle

import "strings"

// AliasInput supplies locally cached metadata without an upstream client dependency.
type AliasInput struct {
	Title            string
	OriginalTitle    string
	OriginalLanguage string
	Aliases          []AliasEntry
}

// AliasEntry applies to the whole item unless SeasonNumber scopes it to one season.
type AliasEntry struct {
	Title        string
	SeasonNumber *int
}

// SearchTitles selects original and localized aliases in source order, within maxTitles.
// Unmarked aliases are omitted because *arr searches them natively (ADR-0019 section 5).
// Language names are compared case-insensitively; app-specific IDs are never shared.
func SearchTitles(in AliasInput, r Rules, season *int, maxTitles int) []string {
	if maxTitles <= 0 {
		return nil
	}
	c := aliasCollector{canonical: normalizeAlias(in.Title), max: maxTitles}
	if strings.EqualFold(in.OriginalLanguage, r.Name) {
		c.add(in.OriginalTitle)
	}
	for i := range len(in.Aliases) {
		if len(c.titles) >= maxTitles {
			break
		}
		alias := in.Aliases[i]
		alias.Title = normalizeAlias(alias.Title)
		if alias.SeasonNumber != nil && (season == nil || *season != *alias.SeasonNumber) {
			continue
		}
		if markedAlias(alias.Title, r) {
			c.add(alias.Title)
		}
	}
	return c.titles
}

// markedAlias identifies language cues that justify a search beyond native *arr alias matching.
func markedAlias(title string, r Rules) bool {
	for i := range len(r.Expansions) {
		if strings.ContainsAny(title, r.Expansions[i].From) {
			return true
		}
	}
	stripped, _ := stripArticle(title, r.StripArticles)
	return stripped != ""
}

// normalizeAlias collapses whitespace so formatting differences do not create duplicate searches.
func normalizeAlias(title string) string {
	return strings.Join(strings.Fields(title), " ")
}

type aliasCollector struct {
	canonical string
	max       int
	titles    []string
}

// add collects unique noncanonical titles within the limit to avoid redundant searches.
func (c *aliasCollector) add(title string) {
	title = normalizeAlias(title)
	if title == "" || strings.EqualFold(title, c.canonical) || len(c.titles) >= c.max {
		return
	}
	for i := range len(c.titles) {
		if strings.EqualFold(title, c.titles[i]) {
			return
		}
	}
	c.titles = append(c.titles, title)
}
