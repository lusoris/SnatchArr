// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package foreigntitle

import (
	"slices"
	"testing"
)

// TestSearchTitlesTranslations checks translations feed the search list only for the rule language.
func TestSearchTitlesTranslations(t *testing.T) {
	t.Parallel()
	rules, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	tr := []LanguageTitle{
		{Language: "de", Title: "Der Titel", Source: "tvdb"},
		{Language: "DE", Title: "Zweiter Titel", Source: "tvdb"},
		{Language: "fr", Title: "Le Titre", Source: "tvdb"},
		{Language: "de", Title: " ", Source: "tvdb"},
		{Language: "de", Title: "der  titel", Source: "tvdb"},
	}
	for _, tc := range []struct {
		name string
		in   AliasInput
		max  int
		want []string
	}{
		{"positive_language_only", AliasInput{Translations: tr}, 10, []string{"Der Titel", "Zweiter Titel"}},
		{"negative_other_language", AliasInput{Translations: tr[2:3]}, 10, nil},
		{"boundary_empty", AliasInput{Translations: []LanguageTitle{}}, 10, nil},
		{"boundary_max_one", AliasInput{Translations: tr}, 1, []string{"Der Titel"}},
		{"dedupe_with_alias", AliasInput{Translations: tr[:1], Aliases: []AliasEntry{{Title: "der titel"}}}, 10, []string{"Der Titel"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := SearchTitles(tc.in, rules["de"], nil, tc.max)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("SearchTitles() = %q; want %q", got, tc.want)
			}
		})
	}
}
