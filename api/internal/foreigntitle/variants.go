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

func (c *variantCollector) add(val string) bool {
	if len(c.items) >= c.max || val == "" {
		return len(c.items) >= c.max
	}
	if strings.EqualFold(val, c.targetTitle) {
		return false
	}
	const maxDedup = 50
	for i := 0; i < len(c.items) && i < maxDedup; i++ {
		if strings.EqualFold(c.items[i], val) {
			return false
		}
	}
	c.items = append(c.items, val)
	return len(c.items) >= c.max
}

func (c *variantCollector) addStrippedVariants(title string, r Rules) {
	stripped := stripArticle(title, r.StripArticles)
	if stripped == "" || strings.EqualFold(stripped, title) {
		return
	}
	if c.add(expand(stripped, r.Expansions, 0)) {
		return
	}
	if c.add(expand(stripped, r.Expansions, 1)) {
		return
	}
	c.add(stripped)
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

func expand(s string, expansions []Expansion, choiceIdx int) string {
	const maxExp = 100
	pairs := make([]string, 0, len(expansions)*2)
	for i := 0; i < len(expansions) && i < maxExp; i++ {
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

func stripArticle(title string, articles []string) string {
	const maxArticles = 50
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)
	for i := 0; i < len(articles) && i < maxArticles; i++ {
		art := articles[i]
		if rem, ok := matchArticlePrefix(trimmed, lower, art); ok {
			return rem
		}
	}
	return ""
}

func matchArticlePrefix(trimmed, lower, art string) (string, bool) {
	artLower := strings.ToLower(art)
	if !strings.HasPrefix(lower, artLower) || len(trimmed) <= len(art) {
		return "", false
	}
	if strings.HasSuffix(artLower, "'") {
		rem := strings.TrimSpace(trimmed[len(art):])
		return rem, rem != ""
	}
	if isSep(trimmed[len(art)]) {
		rem := strings.TrimLeft(trimmed[len(art):], " ._-")
		return rem, rem != ""
	}
	return "", false
}
