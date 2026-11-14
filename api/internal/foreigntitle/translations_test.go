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
	one, two := 1, 2
	seasonal := []AliasEntry{{Title: "Der Arzt", SeasonNumber: &two}}
	for _, tc := range []struct {
		name   string
		in     AliasInput
		season *int
		max    int
		want   []string
	}{
		{"positive_language_only", AliasInput{Translations: tr}, nil, 10, []string{"Der Titel", "Zweiter Titel"}},
		{"negative_other_language", AliasInput{Translations: tr[2:3]}, nil, 10, nil},
		{"boundary_empty", AliasInput{Translations: []LanguageTitle{}}, nil, 10, nil},
		{"boundary_max_one", AliasInput{Translations: tr}, nil, 1, []string{"Der Titel"}},
		{"dedupe_local_alias_wins", AliasInput{Translations: tr[:1], Aliases: []AliasEntry{{Title: "der titel"}}}, nil, 10, []string{"der titel"}},
		{"cap_keeps_local_alias", AliasInput{Translations: tr, Aliases: local}, nil, 1, []string{"Die Serie"}},
		{"translations_fill_after_alias", AliasInput{Translations: tr, Aliases: local}, nil, 2, []string{"Die Serie", "Der Titel"}},
		{"cap_keeps_season_alias", AliasInput{Translations: tr, Aliases: seasonal}, &two, 1, []string{"Der Arzt"}},
		{"season_alias_then_translation", AliasInput{Translations: tr, Aliases: seasonal}, &two, 2, []string{"Der Arzt", "Der Titel"}},
		{"season_mismatch_translations_fill", AliasInput{Translations: tr, Aliases: seasonal}, &one, 2, []string{"Der Titel", "Zweiter Titel"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := SearchTitles(tc.in, rules["de"], tc.season, tc.max)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("SearchTitles() = %q; want %q", got, tc.want)
			}
		})
	}
}
