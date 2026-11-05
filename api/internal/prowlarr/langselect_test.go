// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package prowlarr_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/prowlarr"
	"github.com/lusoris/SnatchArr/api/internal/prowlarrclient"
)

// TestIndexersForLanguage verifies locale fallback, tag precedence and language validation.
func TestIndexersForLanguage(t *testing.T) {
	t.Parallel()
	labels := map[int]string{
		1: "lang-de", 2: "lang-fr", 3: "lang-en", 4: " \tLANG-DE\n",
		5: "lang-", 6: "lang-deu", 7: "lang-d1", 8: "other", 9: "lang-dé", 10: "lang-Ko",
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
		{name: "Kelvin sign tag preserves locale", locale: "de-DE", tags: []int{10}, lang: "de", want: true},
		{name: "Kelvin sign tag does not select Korean", locale: "en-US", tags: []int{10}, lang: "ko"},
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
		{name: "Kelvin sign language", locale: "ko-KR", lang: "Ko"},
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
			assertLanguageSelection(t, indexers, labels, tc.lang, want)
		})
	}
}

// assertLanguageSelection checks successful selection and non-nil empty results.
func assertLanguageSelection(t *testing.T, indexers []prowlarrclient.Indexer, labels map[int]string, lang string, want []int) {
	t.Helper()
	got, err := prowlarr.IndexersForLanguage(indexers, labels, lang)
	if err != nil {
		t.Fatalf("IndexersForLanguage() error = %v", err)
	}
	if got == nil || !slices.Equal(got, want) {
		t.Fatalf("IndexersForLanguage() = %#v, want %#v", got, want)
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
			assertLanguageSelection(t, tc.indexers, nil, "de", tc.want)
		})
	}
}

// TestIndexersForLanguage_IndexerLimit verifies inclusive bounds without truncation.
func TestIndexersForLanguage_IndexerLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		count   int
		wantErr error
	}{
		{name: "at limit", count: 1000},
		{name: "over limit", count: 1001, wantErr: domain.ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			indexers := make([]prowlarrclient.Indexer, tc.count)
			indexers[tc.count-1] = prowlarrclient.Indexer{ID: 7, Enabled: true, Language: "de-DE"}
			got, err := prowlarr.IndexersForLanguage(indexers, nil, "de")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("IndexersForLanguage() error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				if got != nil || !strings.Contains(err.Error(), "1001 indexers exceed the limit of 1000") {
					t.Fatalf("IndexersForLanguage() = %v, %v; want no partial result and limit diagnostic", got, err)
				}
			} else if !slices.Equal(got, []int{7}) {
				t.Fatalf("IndexersForLanguage() = %v, want [7]", got)
			}
		})
	}
}

// TestIndexersForLanguage_TagLimit verifies bounds even for disabled or unselected indexers.
func TestIndexersForLanguage_TagLimit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		count    int
		disabled bool
		lang     string
		wantErr  error
	}{
		{name: "at limit", count: 100, lang: "de"},
		{name: "over limit", count: 101, lang: "de", wantErr: domain.ErrInvalid},
		{name: "disabled over limit", count: 101, disabled: true, lang: "de", wantErr: domain.ErrInvalid},
		{name: "invalid language over limit", count: 101, lang: "Ko", wantErr: domain.ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tags := make([]int, tc.count)
			tags[tc.count-1] = 1
			indexers := []prowlarrclient.Indexer{
				{ID: 2, Enabled: true, Language: "de-DE"},
				{ID: 7, Enabled: !tc.disabled, Language: "en-US", Tags: tags},
			}
			got, err := prowlarr.IndexersForLanguage(indexers, map[int]string{1: "LANG-DE"}, tc.lang)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("IndexersForLanguage() error = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				if got != nil || !strings.Contains(err.Error(), "indexer 7: 101 tags exceed the limit of 100") {
					t.Fatalf("IndexersForLanguage() = %v, %v; want no partial result and indexer limit diagnostic", got, err)
				}
			} else if !slices.Equal(got, []int{2, 7}) {
				t.Fatalf("IndexersForLanguage() = %v, want [2 7]", got)
			}
		})
	}
}
