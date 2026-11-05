// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Radarr language mappings and preflight compatibility logic.
//
// Source: Radarr v6.4.4.10685 (GPL-3.0)
// Credit Radarr (GPL-3.0) as source of the language specification and id table:
//   - src/NzbDrone.Core/DecisionEngine/Specifications/LanguageSpecification.cs
//   - src/NzbDrone.Core/Languages/Language.cs
//   - src/NzbDrone.Core/Parser/IsoLanguages.cs

package foreigntitle

import (
	"strings"
)

// RadarrLanguage identifies a language in Radarr quality profile configurations.
type RadarrLanguage struct {
	ID   int
	Name string
}

const (
	// RadarrLanguageAny indicates a quality profile accepting any audio/release language.
	RadarrLanguageAny = -1
	// RadarrLanguageOriginal indicates a quality profile accepting releases matching the original language.
	RadarrLanguageOriginal = -2
)

// Verdict indicates whether a quality profile allows searches in a foreign target language.
type Verdict string

const (
	// VerdictAllowed indicates searches in the language can pass Radarr's quality profile language check.
	VerdictAllowed Verdict = "allowed"
	// VerdictOriginalOnly indicates searches pass only when the target language matches the original language.
	VerdictOriginalOnly Verdict = "original_only"
	// VerdictSkipped indicates searches in the language are rejected by the quality profile language check.
	VerdictSkipped Verdict = "skipped"
)

// Reason provides diagnostic context for a Verdict.
type Reason string

const (
	// ReasonProfileAny indicates the profile language is Any (-1).
	ReasonProfileAny Reason = "profile_any"
	// ReasonProfileLanguageMatch indicates the profile language matches the target language.
	ReasonProfileLanguageMatch Reason = "profile_language_match"
	// ReasonProfileOriginal indicates the profile language is Original (-2).
	ReasonProfileOriginal Reason = "profile_original"
	// ReasonProfileLanguageMismatch indicates the profile language differs from the target language.
	ReasonProfileLanguageMismatch Reason = "profile_language_mismatch"
	// ReasonProfileLanguageMissing indicates the profile has no associated language.
	ReasonProfileLanguageMissing Reason = "profile_language_missing"
	// ReasonLanguageUnmapped indicates the target language code cannot be mapped to a Radarr language ID.
	ReasonLanguageUnmapped Reason = "language_unmapped"
	// ReasonOriginalLanguageMatch indicates the target language matches the movie's original language.
	ReasonOriginalLanguageMatch Reason = "original_language_match"
	// ReasonOriginalLanguageMismatch indicates the target language differs from the movie's original language.
	ReasonOriginalLanguageMismatch Reason = "original_language_mismatch"
)

// isoToRadarrID maps two-letter ISO 639-1 language codes to Radarr language IDs (first entry wins).
var isoToRadarrID = map[string]int{
	"en": 1, "fr": 2, "es": 3, "de": 4, "it": 5, "da": 6, "nl": 7, "ja": 8, "is": 9, "zh": 10,
	"ru": 11, "pl": 12, "vi": 13, "sv": 14, "no": 15, "nb": 15, "fi": 16, "tr": 17, "pt": 18, "el": 20,
	"ko": 21, "hu": 22, "he": 23, "cs": 25, "hi": 26, "th": 28, "bg": 29, "ro": 27, "ar": 31, "uk": 32,
	"fa": 33, "bn": 34, "lt": 24, "sk": 35, "lv": 36, "ca": 38, "hr": 39, "sr": 40, "bs": 41, "et": 42,
	"ta": 43, "id": 44, "te": 45, "mk": 46, "sl": 47, "ml": 48, "kn": 49, "sq": 50, "af": 51, "mr": 52,
	"tl": 53, "ur": 54, "rm": 55, "mn": 56, "ka": 57,
}

func isASCIILetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// RadarrLanguageID maps a two-letter ISO language code to its Radarr Language ID.
// The lookup is case-insensitive and requires exactly 2 ASCII letters.
func RadarrLanguageID(iso string) (int, bool) {
	if len(iso) != 2 || !isASCIILetter(iso[0]) || !isASCIILetter(iso[1]) {
		return 0, false
	}
	id, ok := isoToRadarrID[strings.ToLower(iso)]
	return id, ok
}

// RadarrProfileVerdict determines whether a Radarr quality profile allows a foreign-title search in target language.
func RadarrProfileVerdict(profile *RadarrLanguage, target string) (Verdict, Reason) {
	if profile == nil {
		return VerdictSkipped, ReasonProfileLanguageMissing
	}
	targetID, ok := RadarrLanguageID(target)
	if !ok {
		return VerdictSkipped, ReasonLanguageUnmapped
	}
	if profile.ID == RadarrLanguageAny {
		return VerdictAllowed, ReasonProfileAny
	}
	if profile.ID == RadarrLanguageOriginal {
		return VerdictOriginalOnly, ReasonProfileOriginal
	}
	if profile.ID == targetID {
		return VerdictAllowed, ReasonProfileLanguageMatch
	}
	return VerdictSkipped, ReasonProfileLanguageMismatch
}

// RadarrMovieAllows evaluates whether a movie with a given original language satisfies the profile for a target language.
func RadarrMovieAllows(profile *RadarrLanguage, original RadarrLanguage, target string) (bool, Reason) {
	if profile == nil {
		return false, ReasonProfileLanguageMissing
	}
	targetID, ok := RadarrLanguageID(target)
	if !ok {
		return false, ReasonLanguageUnmapped
	}
	if profile.ID == RadarrLanguageAny {
		return true, ReasonProfileAny
	}
	if profile.ID == RadarrLanguageOriginal {
		if original.ID == targetID {
			return true, ReasonOriginalLanguageMatch
		}
		return false, ReasonOriginalLanguageMismatch
	}
	if profile.ID == targetID {
		return true, ReasonProfileLanguageMatch
	}
	return false, ReasonProfileLanguageMismatch
}
