// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package foreigntitle_test

import (
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/foreigntitle"
)

func TestRadarrLanguageID(t *testing.T) {
	t.Parallel()

	table := map[string]int{
		"en": 1, "fr": 2, "es": 3, "de": 4, "it": 5, "da": 6, "nl": 7, "ja": 8, "is": 9, "zh": 10,
		"ru": 11, "pl": 12, "vi": 13, "sv": 14, "no": 15, "nb": 15, "fi": 16, "tr": 17, "pt": 18, "el": 20,
		"ko": 21, "hu": 22, "he": 23, "cs": 25, "hi": 26, "th": 28, "bg": 29, "ro": 27, "ar": 31, "uk": 32,
		"fa": 33, "bn": 34, "lt": 24, "sk": 35, "lv": 36, "ca": 38, "hr": 39, "sr": 40, "bs": 41, "et": 42,
		"ta": 43, "id": 44, "te": 45, "mk": 46, "sl": 47, "ml": 48, "kn": 49, "sq": 50, "af": 51, "mr": 52,
		"tl": 53, "ur": 54, "rm": 55, "mn": 56, "ka": 57,
	}

	t.Run("positive all table entries resolve", func(t *testing.T) {
		t.Parallel()
		for code, wantID := range table {
			gotID, ok := foreigntitle.RadarrLanguageID(code)
			if !ok || gotID != wantID {
				t.Errorf("RadarrLanguageID(%q) = (%d, %t), want (%d, true)", code, gotID, ok, wantID)
			}
		}
	})

	t.Run("positive uppercase case insensitivity", func(t *testing.T) {
		t.Parallel()
		gotID, ok := foreigntitle.RadarrLanguageID("DE")
		if !ok || gotID != 4 {
			t.Fatalf("RadarrLanguageID(\"DE\") = (%d, %t), want (4, true)", gotID, ok)
		}
	})

	boundaries := []struct {
		name string
		in   string
	}{
		{"boundary empty string", ""},
		{"boundary single char", "d"},
		{"boundary three chars", "deu"},
		{"boundary digit in input", "d1"},
		{"boundary punctuation", "d-"},
		{"boundary non-ascii", "dä"},
		{"negative unmapped 2-letter", "zz"},
	}

	for _, bc := range boundaries {
		t.Run(bc.name, func(t *testing.T) {
			t.Parallel()
			gotID, ok := foreigntitle.RadarrLanguageID(bc.in)
			if ok || gotID != 0 {
				t.Fatalf("RadarrLanguageID(%q) = (%d, %t), want (0, false)", bc.in, gotID, ok)
			}
		})
	}
}

func TestRadarrProfileVerdict(t *testing.T) {
	t.Parallel()

	anyProfile := &foreigntitle.RadarrLanguage{ID: foreigntitle.RadarrLanguageAny, Name: "Any"}
	originalProfile := &foreigntitle.RadarrLanguage{ID: foreigntitle.RadarrLanguageOriginal, Name: "Original"}
	germanProfile := &foreigntitle.RadarrLanguage{ID: 4, Name: "German"}
	unknownProfile := &foreigntitle.RadarrLanguage{ID: 0, Name: "Unknown"}

	tests := []struct {
		name       string
		profile    *foreigntitle.RadarrLanguage
		target     string
		wantVerd   foreigntitle.Verdict
		wantReason foreigntitle.Reason
	}{
		{
			name:       "boundary nil profile",
			profile:    nil,
			target:     "de",
			wantVerd:   foreigntitle.VerdictSkipped,
			wantReason: foreigntitle.ReasonProfileLanguageMissing,
		},
		{
			name:       "boundary unmapped target",
			profile:    germanProfile,
			target:     "xyz",
			wantVerd:   foreigntitle.VerdictSkipped,
			wantReason: foreigntitle.ReasonLanguageUnmapped,
		},
		{
			name:       "positive any profile allows",
			profile:    anyProfile,
			target:     "de",
			wantVerd:   foreigntitle.VerdictAllowed,
			wantReason: foreigntitle.ReasonProfileAny,
		},
		{
			name:       "positive original profile original only",
			profile:    originalProfile,
			target:     "de",
			wantVerd:   foreigntitle.VerdictOriginalOnly,
			wantReason: foreigntitle.ReasonProfileOriginal,
		},
		{
			name:       "positive language match allows",
			profile:    germanProfile,
			target:     "de",
			wantVerd:   foreigntitle.VerdictAllowed,
			wantReason: foreigntitle.ReasonProfileLanguageMatch,
		},
		{
			name:       "positive language match uppercase target allows",
			profile:    germanProfile,
			target:     "DE",
			wantVerd:   foreigntitle.VerdictAllowed,
			wantReason: foreigntitle.ReasonProfileLanguageMatch,
		},
		{
			name:       "negative language mismatch skipped",
			profile:    germanProfile,
			target:     "fr",
			wantVerd:   foreigntitle.VerdictSkipped,
			wantReason: foreigntitle.ReasonProfileLanguageMismatch,
		},
		{
			name:       "boundary id 0 unknown profile skipped",
			profile:    unknownProfile,
			target:     "de",
			wantVerd:   foreigntitle.VerdictSkipped,
			wantReason: foreigntitle.ReasonProfileLanguageMismatch,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotVerd, gotReason := foreigntitle.RadarrProfileVerdict(tc.profile, tc.target)
			if gotVerd != tc.wantVerd || gotReason != tc.wantReason {
				t.Fatalf("RadarrProfileVerdict(%v, %q) = (%q, %q); want (%q, %q)",
					tc.profile, tc.target, gotVerd, gotReason, tc.wantVerd, tc.wantReason)
			}
		})
	}
}

func TestRadarrMovieAllows(t *testing.T) {
	t.Parallel()

	anyProfile := &foreigntitle.RadarrLanguage{ID: foreigntitle.RadarrLanguageAny, Name: "Any"}
	originalProfile := &foreigntitle.RadarrLanguage{ID: foreigntitle.RadarrLanguageOriginal, Name: "Original"}
	germanProfile := &foreigntitle.RadarrLanguage{ID: 4, Name: "German"}

	origGerman := foreigntitle.RadarrLanguage{ID: 4, Name: "German"}
	origEnglish := foreigntitle.RadarrLanguage{ID: 1, Name: "English"}

	tests := []struct {
		name       string
		profile    *foreigntitle.RadarrLanguage
		orig       foreigntitle.RadarrLanguage
		target     string
		wantAllow  bool
		wantReason foreigntitle.Reason
	}{
		{
			name:       "boundary nil profile",
			profile:    nil,
			orig:       origGerman,
			target:     "de",
			wantAllow:  false,
			wantReason: foreigntitle.ReasonProfileLanguageMissing,
		},
		{
			name:       "boundary unmapped target",
			profile:    germanProfile,
			orig:       origGerman,
			target:     "zz",
			wantAllow:  false,
			wantReason: foreigntitle.ReasonLanguageUnmapped,
		},
		{
			name:       "positive any profile allows",
			profile:    anyProfile,
			orig:       origEnglish,
			target:     "de",
			wantAllow:  true,
			wantReason: foreigntitle.ReasonProfileAny,
		},
		{
			name:       "positive original match allows",
			profile:    originalProfile,
			orig:       origGerman,
			target:     "de",
			wantAllow:  true,
			wantReason: foreigntitle.ReasonOriginalLanguageMatch,
		},
		{
			name:       "negative original mismatch rejects",
			profile:    originalProfile,
			orig:       origEnglish,
			target:     "de",
			wantAllow:  false,
			wantReason: foreigntitle.ReasonOriginalLanguageMismatch,
		},
		{
			name:       "positive profile match allows",
			profile:    germanProfile,
			orig:       origEnglish,
			target:     "de",
			wantAllow:  true,
			wantReason: foreigntitle.ReasonProfileLanguageMatch,
		},
		{
			name:       "negative profile mismatch rejects",
			profile:    germanProfile,
			orig:       origGerman,
			target:     "fr",
			wantAllow:  false,
			wantReason: foreigntitle.ReasonProfileLanguageMismatch,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotAllow, gotReason := foreigntitle.RadarrMovieAllows(tc.profile, tc.orig, tc.target)
			if gotAllow != tc.wantAllow || gotReason != tc.wantReason {
				t.Fatalf("RadarrMovieAllows(%v, %v, %q) = (%t, %q); want (%t, %q)",
					tc.profile, tc.orig, tc.target, gotAllow, gotReason, tc.wantAllow, tc.wantReason)
			}
		})
	}
}
