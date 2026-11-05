// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package prowlarr_test

import (
	"slices"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/prowlarr"
	"github.com/lusoris/SnatchArr/api/internal/prowlarrclient"
)

// TestIndexersForLanguage verifies locale fallback, tag precedence and language validation.
func TestIndexersForLanguage(t *testing.T) {
	t.Parallel()
	labels := map[int]string{
		1: "lang-de", 2: "lang-fr", 3: "lang-en", 4: " \tLANG-DE\n",
		5: "lang-", 6: "lang-deu", 7: "lang-d1", 8: "other", 9: "lang-dé",
	}
	tests := []struct {
		name, locale, lang string
		tags               []int
		disabled, want     bool
	}{
		{name: "locale match", locale: "de-DE", lang: "de", want: true},
		{name: "Austrian region", locale: "de-AT", lang: "de", want: true},
		{name: "Swiss region", locale: "de-CH", lang: "de", want: true},
		{name: "underscore region", locale: "de_CH", lang: "de", want: true},
		{name: "bare locale", locale: "de", lang: "de", want: true},
		{name: "locale mismatch", locale: "en-US", lang: "de"},
		{name: "locale primary only", locale: "en-de", lang: "de"},
		{name: "override include", locale: "en-US", tags: []int{1}, lang: "de", want: true},
		{name: "override exclude", locale: "de-DE", tags: []int{3}, lang: "de"},
		{name: "multiple tags German", locale: "en-US", tags: []int{2, 1}, lang: "de", want: true},
		{name: "multiple tags French", locale: "en-US", tags: []int{1, 2}, lang: "fr", want: true},
		{name: "multiple tags exclude locale", locale: "en-US", tags: []int{1, 2}, lang: "en"},
		{name: "uppercase language and locale", locale: "DE-de", lang: "DE", want: true},
		{name: "uppercase trimmed label", locale: "en-US", tags: []int{4}, lang: "DE", want: true},
		{name: "malformed empty tag", locale: "de-DE", tags: []int{5}, lang: "de", want: true},
		{name: "malformed long tag", locale: "de-DE", tags: []int{6}, lang: "de", want: true},
		{name: "malformed digit tag", locale: "de-DE", tags: []int{7}, lang: "de", want: true},
		{name: "malformed unicode tag", locale: "de-DE", tags: []int{9}, lang: "de", want: true},
		{name: "malformed tags do not select", locale: "en-US", tags: []int{5, 6, 7, 9}, lang: "de"},
		{name: "unrelated tag", locale: "de-DE", tags: []int{8}, lang: "de", want: true},
		{name: "unknown tag", locale: "de-DE", tags: []int{99}, lang: "de", want: true},
		{name: "unknown tag with override", locale: "de-DE", tags: []int{99, 3}, lang: "de"},
		{name: "disabled locale", locale: "de-DE", lang: "de", disabled: true},
		{name: "disabled override", tags: []int{1}, lang: "de", disabled: true},
		{name: "empty language", locale: "de-DE", lang: ""},
		{name: "long language", locale: "de-DE", lang: "deu"},
		{name: "short language", locale: "de-DE", lang: "D"},
		{name: "digit language", locale: "d1-DE", tags: []int{7}, lang: "d1"},
		{name: "unicode language", locale: "dé-DE", tags: []int{9}, lang: "dé"},
		{name: "padded language", locale: "de-DE", lang: " de "},
		{name: "empty locale", lang: "de"},
		{name: "empty locale overridden", tags: []int{1}, lang: "de", want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			indexers := []prowlarrclient.Indexer{{ID: 7, Enabled: !tc.disabled, Language: tc.locale, Tags: tc.tags}}
			want := []int{}
			if tc.want {
				want = []int{7}
			}
			got := prowlarr.IndexersForLanguage(indexers, labels, tc.lang)
			if got == nil || !slices.Equal(got, want) {
				t.Fatalf("IndexersForLanguage() = %#v, want %#v", got, want)
			}
		})
	}
}

// TestIndexersForLanguage_Collection verifies nil inputs, ordering and deduplication.
func TestIndexersForLanguage_Collection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		indexers []prowlarrclient.Indexer
		want     []int
	}{
		{name: "nil inputs", want: []int{}},
		{name: "empty inputs", indexers: []prowlarrclient.Indexer{}, want: []int{}},
		{
			name:     "nil labels preserve locale fallback",
			indexers: []prowlarrclient.Indexer{{ID: 4, Enabled: true, Language: "de-DE", Tags: []int{1}}},
			want:     []int{4},
		},
		{
			name: "ascending unique selected IDs",
			indexers: []prowlarrclient.Indexer{
				{ID: 8, Enabled: true, Language: "de-DE"},
				{ID: 2, Enabled: true, Language: "de-AT"},
				{ID: 5, Enabled: true, Language: "en-US"},
				{ID: 8, Enabled: true, Language: "de-CH"},
				{ID: 3, Enabled: true, Language: "de"},
			},
			want: []int{2, 3, 8},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := prowlarr.IndexersForLanguage(tc.indexers, nil, "de")
			if got == nil || !slices.Equal(got, tc.want) {
				t.Fatalf("IndexersForLanguage() = %#v, want %#v", got, tc.want)
			}
		})
	}
}
