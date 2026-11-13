// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package foreigntitle

import (
	"slices"
	"testing"
)

// TestSearchTitlesTranslations checks translations feed the search list only for the rule
// language, and only into the capacity local aliases leave.
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
	// A marked local alias (German article) that the provider list must never push out.
	local := []AliasEntry{{Title: "Die Serie"}}
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
		{"dedupe_local_alias_wins", AliasInput{Translations: tr[:1], Aliases: []AliasEntry{{Title: "der titel"}}}, 10, []string{"der titel"}},
		{"cap_keeps_local_alias", AliasInput{Translations: tr, Aliases: local}, 1, []string{"Die Serie"}},
		{"translations_fill_after_alias", AliasInput{Translations: tr, Aliases: local}, 2, []string{"Die Serie", "Der Titel"}},
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
