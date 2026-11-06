// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package foreigntitle

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"testing/quick"
)

func TestSearchTitles(t *testing.T) {
	t.Parallel()
	rules, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range aliasCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := SearchTitles(tc.in, rules[tc.lang], tc.season, tc.max)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("SearchTitles() = %q; want %q", got, tc.want)
			}
			if tc.max <= 0 && got != nil {
				t.Fatalf("nonpositive maximum must return nil: %q", got)
			}
		})
	}
}

type aliasCase struct {
	name, lang string
	in         AliasInput
	season     *int
	max        int
	want       []string
}

func aliasCases() []aliasCase {
	one, two := 1, 2
	original := AliasInput{Title: "The Lives of Others", OriginalTitle: "Das Leben der Anderen", OriginalLanguage: "gErMaN", Aliases: []AliasEntry{{Title: "Die Ärzte"}}}
	seasonal := AliasInput{Aliases: []AliasEntry{{Title: "Der Arzt", SeasonNumber: &two}, {Title: "Die Ärzte"}}}
	return []aliasCase{
		{"original_first", "de", original, nil, 10, []string{"Das Leben der Anderen", "Die Ärzte"}},
		{"english_original_dropped", "de", AliasInput{OriginalTitle: "Das Leben", OriginalLanguage: "English"}, nil, 10, nil},
		{"umlaut_article_unmarked", "de", AliasInput{Aliases: []AliasEntry{{Title: "Die Ärzte"}, {Title: "Der Tatortreiniger"}, {Title: "Breaking Bad"}}}, nil, 10, []string{"Die Ärzte", "Der Tatortreiniger"}},
		{"french", "fr", AliasInput{Aliases: []AliasEntry{{Title: "Le Bureau des légendes"}}}, nil, 10, []string{"Le Bureau des légendes"}},
		{"french_not_german", "de", AliasInput{Aliases: []AliasEntry{{Title: "Le Bureau des légendes"}}}, nil, 10, nil},
		{"season_match", "de", seasonal, &two, 10, []string{"Der Arzt", "Die Ärzte"}},
		{"season_mismatch", "de", seasonal, &one, 10, []string{"Die Ärzte"}},
		{"season_absent", "de", seasonal, nil, 10, []string{"Die Ärzte"}},
		{"dedupe_whitespace_case", "de", AliasInput{Aliases: []AliasEntry{{Title: " Die  Ärzte\t"}, {Title: "die ärzte"}}}, nil, 10, []string{"Die Ärzte"}},
		{"article_whitespace", "de", AliasInput{Aliases: []AliasEntry{{Title: " Der\tTatortreiniger "}}}, nil, 10, []string{"Der Tatortreiniger"}},
		{"canonical_dropped", "de", AliasInput{Title: " Die\tÄrzte ", OriginalTitle: "die ärzte", OriginalLanguage: "German", Aliases: []AliasEntry{{Title: "Die  Ärzte"}}}, nil, 10, nil},
		{"empty_original_alias", "de", AliasInput{OriginalLanguage: "German", Aliases: []AliasEntry{{Title: " \t "}}}, nil, 10, nil},
		{"whole_word_article", "de", AliasInput{Aliases: []AliasEntry{{Title: "Derry Girls"}, {Title: "Diesel"}, {Title: "Der"}}}, nil, 10, nil},
		{"french_elision", "fr", AliasInput{Aliases: []AliasEntry{{Title: "L'Amour Ouf"}, {Title: "L’Autre"}}}, nil, 10, []string{"L'Amour Ouf", "L’Autre"}},
		{"zero", "de", original, nil, 0, nil},
		{"negative", "de", original, nil, -1, nil},
		{"one", "de", original, nil, 1, []string{"Das Leben der Anderen"}},
		{"exact", "de", original, nil, 2, []string{"Das Leben der Anderen", "Die Ärzte"}},
	}
}

func TestSearchTitlesManyAliases(t *testing.T) {
	t.Parallel()
	in := AliasInput{}
	for i := range 60 {
		in.Aliases = append(in.Aliases, AliasEntry{Title: fmt.Sprintf("Der Titel %d", i)})
	}
	in.Aliases = append(in.Aliases, AliasEntry{Title: "der titel 59"})
	got := SearchTitles(in, Rules{StripArticles: []string{"der"}}, nil, 100)
	if len(got) != 60 {
		t.Fatalf("titles = %d; want 60 unique titles beyond variantCollector's dedupe bound", len(got))
	}
}

func TestSearchTitlesProperties(t *testing.T) {
	t.Parallel()
	rules, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	property := func(title, original string, aliases []string, limit uint8) bool {
		in := AliasInput{Title: title, OriginalTitle: original, OriginalLanguage: "German"}
		for _, alias := range aliases {
			in.Aliases = append(in.Aliases, AliasEntry{Title: "Der " + alias}, AliasEntry{Title: "der  " + alias})
		}
		in.Aliases = append(in.Aliases, AliasEntry{Title: title})
		return validSearchTitles(in, SearchTitles(in, rules["de"], nil, int(limit)), int(limit))
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 1000}); err != nil {
		t.Fatal(err)
	}
}

func validSearchTitles(in AliasInput, titles []string, maxTitles int) bool {
	if len(titles) > maxTitles {
		return false
	}
	canonical := strings.Join(strings.Fields(in.Title), " ")
	for i, title := range titles {
		if strings.EqualFold(title, canonical) || title == "" {
			return false
		}
		for _, previous := range titles[:i] {
			if strings.EqualFold(title, previous) {
				return false
			}
		}
	}
	return true
}
