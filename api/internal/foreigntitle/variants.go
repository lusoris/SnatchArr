// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package foreigntitle

import (
	"strings"
)

type variantCollector struct {
	targetTitle string
	max         int
	items       []string
}

// add appends a non-duplicate, non-target title variant to collector, returning whether max capacity is reached.
func (c *variantCollector) add(val string) bool {
	if len(c.items) >= c.max || val == "" {
		return len(c.items) >= c.max
	}
	if strings.EqualFold(val, c.targetTitle) {
		return false
	}
	const maxDedup = 50
	limit := min(len(c.items), maxDedup)
	for i := range limit {
		if strings.EqualFold(c.items[i], val) {
			return false
		}
	}
	c.items = append(c.items, val)
	return len(c.items) >= c.max
}

// addStrippedVariants appends article-stripped and elision-spaced variants to collector.
func (c *variantCollector) addStrippedVariants(title string, r Rules) {
	stripped, spaced := stripArticle(title, r.StripArticles)
	if stripped != "" && !strings.EqualFold(stripped, title) {
		if c.add(expand(stripped, r.Expansions, 0)) {
			return
		}
		if c.add(expand(stripped, r.Expansions, 1)) {
			return
		}
		if c.add(stripped) {
			return
		}
	}
	if spaced != "" && !strings.EqualFold(spaced, title) {
		if c.add(expand(spaced, r.Expansions, 0)) {
			return
		}
		if c.add(expand(spaced, r.Expansions, 1)) {
			return
		}
		c.add(spaced)
	}
}

// Variants generates deterministic, search-ready title variants for a localized title.
// It deduplicates case-insensitively, never returns the input title, and caps results at maxVariants.
func Variants(title string, r Rules, maxVariants int) []string {
	if maxVariants <= 0 || strings.TrimSpace(title) == "" {
		return nil
	}
	c := variantCollector{
		targetTitle: title,
		max:         maxVariants,
		items:       make([]string, 0, min(maxVariants, 10)),
	}
	if c.add(expand(title, r.Expansions, 0)) {
		return c.items
	}
	if c.add(expand(title, r.Expansions, 1)) {
		return c.items
	}
	c.addStrippedVariants(title, r)
	if len(c.items) == 0 {
		return nil
	}
	return c.items
}

// expand applies expansion character substitutions for the given choice index.
func expand(s string, expansions []Expansion, choiceIdx int) string {
	const maxExp = 100
	pairs := make([]string, 0, len(expansions)*2)
	limit := min(len(expansions), maxExp)
	for i := range limit {
		e := expansions[i]
		if len(e.To) == 0 {
			continue
		}
		to := e.To[0]
		if choiceIdx < len(e.To) {
			to = e.To[choiceIdx]
		}
		pairs = append(pairs, e.From, to)
	}
	if len(pairs) == 0 {
		return s
	}
	return strings.NewReplacer(pairs...).Replace(s)
}

// stripArticle searches for a leading article and returns the stripped remainder and optional spaced variant.
func stripArticle(title string, articles []string) (string, string) {
	const maxArticles = 50
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return "", ""
	}
	lower := strings.ToLower(trimmed)
	limit := min(len(articles), maxArticles)
	for i := range limit {
		art := articles[i]
		if rem, spaced, ok := matchArticlePrefix(trimmed, lower, art); ok {
			return rem, spaced
		}
	}
	return "", ""
}

// matchArticlePrefix matches an article against trimmed, returning the stripped remainder and spaced elision variant.
func matchArticlePrefix(trimmed, lower, art string) (string, string, bool) {
	artLower := strings.ToLower(art)
	if strings.HasSuffix(artLower, "'") || strings.HasSuffix(artLower, "’") {
		return matchElidedArticle(trimmed, lower, artLower)
	}
	if rem, ok := matchStandardArticle(trimmed, lower, artLower); ok {
		return rem, "", true
	}
	return "", "", false
}

// matchElidedArticle matches an elided article against lower and builds remainder and spaced variant.
func matchElidedArticle(trimmed, lower, artLower string) (string, string, bool) {
	baseArt := strings.TrimSuffix(strings.TrimSuffix(artLower, "'"), "’")
	matchLen := elisionPrefixLen(lower, baseArt)
	if matchLen == 0 || len(trimmed) <= matchLen {
		return "", "", false
	}
	rem := strings.TrimSpace(trimmed[matchLen:])
	if rem == "" {
		return "", "", false
	}
	spaced := trimmed[:len(baseArt)] + " " + rem
	return rem, spaced, true
}

// elisionPrefixLen returns the prefix match length if lower starts with baseArt followed by an apostrophe.
func elisionPrefixLen(lower, baseArt string) int {
	if strings.HasPrefix(lower, baseArt+"'") {
		return len(baseArt) + len("'")
	}
	if strings.HasPrefix(lower, baseArt+"’") {
		return len(baseArt) + len("’")
	}
	return 0
}

// matchStandardArticle matches a non-elided article against lower followed by a separator.
func matchStandardArticle(trimmed, lower, artLower string) (string, bool) {
	if !strings.HasPrefix(lower, artLower) || len(trimmed) <= len(artLower) {
		return "", false
	}
	if !isSep(trimmed[len(artLower)]) {
		return "", false
	}
	rem := strings.TrimLeft(trimmed[len(artLower):], " ._-")
	return rem, rem != ""
}
