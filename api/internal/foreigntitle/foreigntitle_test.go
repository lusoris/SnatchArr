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

// TestLoad checks that the embedded rule files load and that validate refuses malformed rules.
func TestLoad(t *testing.T) {
	t.Parallel()

	t.Run("positive embedded rules", func(t *testing.T) {
		t.Parallel()
		rules, err := Load()
		if err != nil {
			t.Fatalf("Load() unexpected error: %v", err)
		}
		if len(rules) < 2 {
			t.Fatalf("expected at least 2 languages, got %d", len(rules))
		}
		de, ok := rules["de"]
		if !ok || de.Code != "de" || de.Name != "German" {
			t.Fatalf("invalid German rules: %+v", de)
		}
		if len(de.Expansions) == 0 || len(de.AudioTags) == 0 || len(de.StripArticles) == 0 {
			t.Fatalf("German rules missing entries: %+v", de)
		}
		fr, ok := rules["fr"]
		if !ok || fr.Code != "fr" || fr.Name != "French" {
			t.Fatalf("invalid French rules: %+v", fr)
		}
	})

	t.Run("negative validation cases", func(t *testing.T) {
		t.Parallel()
		if _, err := parse([]byte("code: de\nname: German\nunknown_field: true\n")); err == nil {
			t.Fatalf("parse() should reject unknown fields")
		}
		r := Rules{Code: "en"}
		if err := validate(&r, "de"); err == nil {
			t.Fatalf("validate() should fail on code stem mismatch")
		}
		rEmpty := Rules{Code: "de", Expansions: []Expansion{{From: "ä", To: []string{}}}}
		if err := validate(&rEmpty, "de"); err == nil {
			t.Fatalf("validate() should fail on empty to-list")
		}
		rRegex := Rules{Code: "de", AudioTags: []string{"[unclosed"}}
		if err := validate(&rRegex, "de"); err == nil {
			t.Fatalf("validate() should fail on invalid regex")
		}
		if _, err := parse([]byte("")); err == nil {
			t.Fatalf("parse() should fail on empty input")
		}
	})
}

// TestVariants_PositiveGerman checks German diacritic expansion and article stripping.
func TestVariants_PositiveGerman(t *testing.T) {
	t.Parallel()
	rules, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	de := rules["de"]

	tests := []struct {
		name  string
		title string
		max   int
		want  []string
	}{
		{
			name:  "positive de umlaut Käfer",
			title: "Käfer",
			max:   5,
			want:  []string{"Kaefer", "Kafer"},
		},
		{
			name:  "positive de Die Ärzte full and stripped",
			title: "Die Ärzte",
			max:   5,
			want:  []string{"Die Aerzte", "Die Arzte", "Aerzte", "Arzte", "Ärzte"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Variants(tc.title, de, tc.max)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("Variants(%q) = %v, want %v", tc.title, got, tc.want)
			}
		})
	}
}

// TestVariants_PositiveFrench checks French variants, including both elision apostrophes.
func TestVariants_PositiveFrench(t *testing.T) {
	t.Parallel()
	rules, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	fr := rules["fr"]

	tests := []struct {
		name  string
		title string
		max   int
		want  []string
	}{
		{
			name:  "positive fr accent and article",
			title: "Le Fabuleux Destin d'Amélie Poulain",
			max:   5,
			want: []string{
				"Le Fabuleux Destin d'Amelie Poulain",
				"Fabuleux Destin d'Amelie Poulain",
				"Fabuleux Destin d'Amélie Poulain",
			},
		},
		{
			name:  "positive fr elision L'Amour Ouf",
			title: "L'Amour Ouf",
			max:   5,
			want:  []string{"Amour Ouf", "L Amour Ouf"},
		},
		{
			name:  "positive fr elision U+2019 L’Amour Ouf",
			title: "L’Amour Ouf",
			max:   5,
			want:  []string{"Amour Ouf", "L Amour Ouf"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Variants(tc.title, fr, tc.max)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("Variants(%q) = %v, want %v", tc.title, got, tc.want)
			}
		})
	}
}

// TestVariants_Negative checks titles that yield no variant.
func TestVariants_Negative(t *testing.T) {
	t.Parallel()
	rules, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	de := rules["de"]

	tests := []struct {
		name  string
		title string
		rules Rules
		max   int
	}{
		{name: "negative max zero", title: "Käfer", rules: de, max: 0},
		{name: "negative max negative", title: "Käfer", rules: de, max: -2},
		{name: "negative empty title", title: "  ", rules: de, max: 5},
		{name: "negative no variants possible", title: "Inception", rules: de, max: 5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Variants(tc.title, tc.rules, tc.max); got != nil {
				t.Fatalf("Variants(%q) = %v, want nil", tc.title, got)
			}
		})
	}
}

// TestVariants_BoundaryGerman checks the variant cap and a title that is only an article.
func TestVariants_BoundaryGerman(t *testing.T) {
	t.Parallel()
	rules, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	de := rules["de"]

	tests := []struct {
		name     string
		title    string
		max      int
		want     []string
		wantNone bool
	}{
		{
			name:  "boundary max 1 cap",
			title: "Die Ärzte",
			max:   1,
			want:  []string{"Die Aerzte"},
		},
		{
			name:  "boundary max 2 cap",
			title: "Die Ärzte",
			max:   2,
			want:  []string{"Die Aerzte", "Die Arzte"},
		},
		{
			name:     "boundary only article der",
			title:    "Der",
			max:      5,
			wantNone: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Variants(tc.title, de, tc.max)
			if tc.wantNone {
				if got != nil {
					t.Fatalf("Variants(%q) = %v, want nil", tc.title, got)
				}
				return
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("Variants(%q) = %v, want %v", tc.title, got, tc.want)
			}
		})
	}
}

// TestVariants_BoundaryFrench checks the variant cap on elided titles with both apostrophes.
func TestVariants_BoundaryFrench(t *testing.T) {
	t.Parallel()
	rules, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	fr := rules["fr"]

	tests := []struct {
		name  string
		title string
		max   int
		want  []string
	}{
		{
			name:  "boundary fr elision max 1 cap",
			title: "L'Amour Ouf",
			max:   1,
			want:  []string{"Amour Ouf"},
		},
		{
			name:  "boundary fr elision U+2019 max 1 cap",
			title: "L’Amour Ouf",
			max:   1,
			want:  []string{"Amour Ouf"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Variants(tc.title, fr, tc.max)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("Variants(%q) = %v, want %v", tc.title, got, tc.want)
			}
		})
	}
}

// TestRewriteTitle_PositiveSeparators checks rewrites across dot, space, underscore and dash separators.
func TestRewriteTitle_PositiveSeparators(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		release   string
		foreign   string
		canonical string
		want      string
	}{
		{
			name:      "positive dots separator",
			release:   "Alone.Ueberlebe.die.Wildnis.S01E01.720p-TvR",
			foreign:   "Alone Ueberlebe die Wildnis",
			canonical: "Alone Germany",
			want:      "Alone.Germany.S01E01.720p-TvR",
		},
		{
			name:      "positive spaces separator",
			release:   "Alone Ueberlebe die Wildnis S01E01 720p-TvR",
			foreign:   "Alone Ueberlebe die Wildnis",
			canonical: "Alone Germany",
			want:      "Alone Germany S01E01 720p-TvR",
		},
		{
			name:      "positive underscore separator",
			release:   "Alone_Ueberlebe_die_Wildnis_S01E01_720p-TvR",
			foreign:   "Alone Ueberlebe die Wildnis",
			canonical: "Alone Germany",
			want:      "Alone_Germany_S01E01_720p-TvR",
		},
		{
			name:      "positive dash separator",
			release:   "Alone-Ueberlebe-die-Wildnis-S01E01-720p-TvR",
			foreign:   "Alone Ueberlebe die Wildnis",
			canonical: "Alone Germany",
			want:      "Alone-Germany-S01E01-720p-TvR",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := RewriteTitle(tc.release, tc.foreign, tc.canonical)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("RewriteTitle() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRewriteTitle_PositiveWords checks rewrites of multi-word foreign and canonical titles.
func TestRewriteTitle_PositiveWords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		release   string
		foreign   string
		canonical string
		want      string
	}{
		{
			name:      "positive single word foreign title",
			release:   "Kaefer.S01E01.720p-GRP",
			foreign:   "Kaefer",
			canonical: "The Bug",
			want:      "The.Bug.S01E01.720p-GRP",
		},
		{
			name:      "positive case insensitive match",
			release:   "alone.ueberlebe.die.wildnis.S01E01",
			foreign:   "ALONE UEBERLEBE DIE WILDNIS",
			canonical: "Alone Germany",
			want:      "Alone.Germany.S01E01",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := RewriteTitle(tc.release, tc.foreign, tc.canonical)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("RewriteTitle() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRewriteTitle_Negative checks releases that do not start with the foreign title.
func TestRewriteTitle_Negative(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		release   string
		foreign   string
		canonical string
	}{
		{name: "negative prefix mismatch", release: "Other.Show.S01E01", foreign: "Alone", canonical: "Alone Germany"},
		{name: "negative partial word mismatch", release: "Aloneness.S01E01", foreign: "Alone", canonical: "Alone Germany"},
		{name: "negative release too short", release: "Alone", foreign: "Alone Germany", canonical: "Alone"},
		{name: "negative empty release", release: "", foreign: "Alone", canonical: "Alone Germany"},
		{name: "negative empty foreign", release: "Show.S01E01", foreign: "   ", canonical: "Alone"},
		{name: "negative empty canonical", release: "Show.S01E01", foreign: "Show", canonical: "   "},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got, err := RewriteTitle(tc.release, tc.foreign, tc.canonical); err == nil {
				t.Fatalf("expected error, got %q", got)
			}
		})
	}
}

// TestRewriteTitle_Boundary checks a release that is exactly the foreign title.
func TestRewriteTitle_Boundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		release   string
		foreign   string
		canonical string
		want      string
	}{
		{
			name:      "boundary exact title match without suffix",
			release:   "Alone.Ueberlebe",
			foreign:   "Alone Ueberlebe",
			canonical: "Alone Germany",
			want:      "Alone.Germany",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := RewriteTitle(tc.release, tc.foreign, tc.canonical)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("RewriteTitle() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRewriteTitle_KeepsGroup is the planted case: the release group suffix must survive the rewrite.
func TestRewriteTitle_KeepsGroup(t *testing.T) {
	t.Parallel()
	release := "Kaefer.S01E01.720p-GROUP"
	foreign := "Kaefer"
	canonical := "The Bug"
	got, err := RewriteTitle(release, foreign, canonical)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	const wantSuffix = "-GROUP"
	if !strings.HasSuffix(got, wantSuffix) {
		t.Fatalf("RewriteTitle dropped group suffix: got %q, want suffix %q", got, wantSuffix)
	}
}

// TestRewriteTitle_WordLimits checks titles at and one past the word and byte limits.
func TestRewriteTitle_WordLimits(t *testing.T) {
	t.Parallel()

	words100 := make([]string, 0, 100)
	for i := range 100 {
		words100 = append(words100, fmt.Sprintf("w%d", i))
	}
	title100 := strings.Join(words100, ".")
	words101 := make([]string, 0, 101)
	words101 = append(words101, words100...)
	words101 = append(words101, "w100")
	title101 := strings.Join(words101, ".")

	t.Run("boundary 100 words foreign succeeds", func(t *testing.T) {
		t.Parallel()
		rel := title100 + ".S01E01-GRP"
		got, err := RewriteTitle(rel, title100, "Canonical")
		if err != nil {
			t.Fatalf("expected success for 100 words, got error: %v", err)
		}
		if got != "Canonical.S01E01-GRP" {
			t.Fatalf("RewriteTitle() = %q, want Canonical.S01E01-GRP", got)
		}
	})

	t.Run("boundary 101 words foreign fails", func(t *testing.T) {
		t.Parallel()
		rel := title101 + ".S01E01-GRP"
		if _, err := RewriteTitle(rel, title101, "Canonical"); err == nil {
			t.Fatalf("expected error for 101 words in foreign title")
		}
	})

	t.Run("boundary 101 words canonical fails", func(t *testing.T) {
		t.Parallel()
		rel := "Show.S01E01-GRP"
		if _, err := RewriteTitle(rel, "Show", title101); err == nil {
			t.Fatalf("expected error for 101 words in canonical title")
		}
	})
}

// TestClassify_Positive checks releases classified as dubbed or subtitle-only.
func TestClassify_Positive(t *testing.T) {
	t.Parallel()
	rules, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	de := rules["de"]
	fr := rules["fr"]

	tests := []struct {
		name    string
		release string
		rules   Rules
		want    Audio
	}{
		{name: "positive German dubbed tag", release: "Kaefer.S01E01.GERMAN.720p-GRP", rules: de, want: AudioDubbed},
		{name: "positive French dubbed tag", release: "Movie.FRENCH.1080p-GRP", rules: fr, want: AudioDubbed},
		{name: "positive French VOSTFR subtitle only", release: "Movie.VOSTFR.1080p-GRP", rules: fr, want: AudioSubOnly},
		{name: "positive German OmU subtitle only", release: "Movie.OmU.1080p-GRP", rules: de, want: AudioSubOnly},
		{name: "positive VOSTFR with WEB-DL stays sub-only", release: "Le.Film.2020.VOSTFR.1080p.WEB-DL-GRP", rules: fr, want: AudioSubOnly},
		{name: "positive German DL token stays dubbed", release: "Show.S01E01.GERMAN.DL-GRP", rules: de, want: AudioDubbed},
		{name: "positive dash as left boundary", release: "Show.S01E01-GERMAN-GRP", rules: de, want: AudioDubbed},
		{name: "positive untagged group with mkv extension", release: "Show.S01E01.1080p-TvR.mkv", rules: de, want: AudioDubbed},
		{name: "positive untagged group with brackets and mkv", release: "Show.S01E01.1080p.[TvR].mkv", rules: de, want: AudioDubbed},
		{name: "positive untagged group with brackets at start", release: "[TvR] Alone.S01E01.720p", rules: de, want: AudioDubbed},
		{name: "positive untagged group terminal dash", release: "Alone.Ueberlebe.die.Wildnis.S01E01.720p-TvR", rules: de, want: AudioDubbed},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Classify(tc.release, tc.rules); got != tc.want {
				t.Fatalf("Classify(%q) = %v, want %v", tc.release, got, tc.want)
			}
		})
	}
}

// TestClassify_Negative checks releases that must stay unknown, such as WEB-DL and title words.
func TestClassify_Negative(t *testing.T) {
	t.Parallel()
	rules, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	de := rules["de"]
	fr := rules["fr"]

	tests := []struct {
		name    string
		release string
		rules   Rules
		want    Audio
	}{
		{name: "negative WEB-DL source is not DL audio", release: "Show.S01E01.1080p.WEB-DL.x264-GRP", rules: de, want: AudioUnknown},
		{name: "negative Germany in title is not GERMAN tag", release: "Alone.Germany.S01E01.720p.HDTV-GRP", rules: de, want: AudioUnknown},
		{name: "negative unknown audio", release: "Movie.1080p-GRP", rules: de, want: AudioUnknown},
		{name: "negative English audio", release: "Movie.ENGLISH.1080p-GRP", rules: de, want: AudioUnknown},
		{name: "negative MULTi tag alone in fr", release: "Show.S01E01.MULTi.1080p-GRP", rules: fr, want: AudioUnknown},
		{name: "negative DL tag alone in de", release: "Show.S01E01.DL.1080p-GRP", rules: de, want: AudioUnknown},
		{name: "negative WEB.DL tag alone in de", release: "Show.S01E01.WEB.DL-GRP", rules: de, want: AudioUnknown},
		{name: "negative untagged group in title", release: "Show-TvR.Movie.1080p-OTHER", rules: de, want: AudioUnknown},
		{name: "negative title word after group is not an extension", release: "Show.S01E01-TvR.Movie", rules: de, want: AudioUnknown},
		{name: "negative untagged group not in group pos", release: "TvR.Movie.1080p-OTHER", rules: de, want: AudioUnknown},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Classify(tc.release, tc.rules); got != tc.want {
				t.Fatalf("Classify(%q) = %v, want %v", tc.release, got, tc.want)
			}
		})
	}
}

// TestClassify_Boundary checks tag precedence and untagged groups at the edge of the terminal position.
func TestClassify_Boundary(t *testing.T) {
	t.Parallel()
	rules, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	de := rules["de"]

	tests := []struct {
		name    string
		release string
		rules   Rules
		want    Audio
	}{
		{name: "boundary GERMAN plus OmU dubbed wins", release: "Movie.GERMAN.OmU.1080p-GRP", rules: de, want: AudioDubbed},
		{name: "boundary untagged group with brackets no extension", release: "Show.S01E01.1080p.[TvR]", rules: de, want: AudioDubbed},
		{name: "boundary untagged group with torrent extension", release: "Show.S01E01.1080p-TvR.torrent", rules: de, want: AudioDubbed},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Classify(tc.release, tc.rules); got != tc.want {
				t.Fatalf("Classify(%q) = %v, want %v", tc.release, got, tc.want)
			}
		})
	}
}

// TestClassify_TagMutation checks that changed tags are recompiled rather than served from a stale cache.
func TestClassify_TagMutation(t *testing.T) {
	t.Parallel()
	rules, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	de := rules["de"]
	de.AudioTags[0] = "MEINNEUERTAG"

	got := Classify("Show.S01E01.MEINNEUERTAG.1080p-GRP", de)
	if got != AudioDubbed {
		t.Fatalf("Classify with mutated AudioTags[0] = %v, want AudioDubbed", got)
	}
	gotOld := Classify("Show.S01E01.GERMAN.1080p-GRP", de)
	if gotOld != AudioUnknown {
		t.Fatalf("Classify with old tag after mutation = %v, want AudioUnknown", gotOld)
	}
}

// TestClassify_PatternLimitIndex50 checks that a tag past index 49 still matches.
func TestClassify_PatternLimitIndex50(t *testing.T) {
	t.Parallel()
	tags := make([]string, 51)
	for i := range 51 {
		tags[i] = fmt.Sprintf("TAG%02d", i)
	}
	r := Rules{AudioTags: tags}
	got := Classify("Show.S01E01.TAG50.1080p-GRP", r)
	if got != AudioDubbed {
		t.Fatalf("Classify matching tag at index 50 = %v, want AudioDubbed", got)
	}
}

// TestParse_MultipleDocuments checks that a rule file with a second YAML document is refused.
func TestParse_MultipleDocuments(t *testing.T) {
	t.Parallel()
	_, err := parse([]byte("code: de\n---\ncode: fr\n"))
	if err == nil || !strings.Contains(err.Error(), "multiple YAML documents") {
		t.Fatalf("expected 'multiple YAML documents' error, got %v", err)
	}
}

// TestAudio_String checks the names of every Audio value, including out-of-range ones.
func TestAudio_String(t *testing.T) {
	t.Parallel()
	if AudioUnknown.String() != "unknown" {
		t.Fatalf("AudioUnknown.String() = %q, want %q", AudioUnknown.String(), "unknown")
	}
	if AudioDubbed.String() != "dubbed" {
		t.Fatalf("AudioDubbed.String() = %q, want %q", AudioDubbed.String(), "dubbed")
	}
	if AudioSubOnly.String() != "sub_only" {
		t.Fatalf("AudioSubOnly.String() = %q, want %q", AudioSubOnly.String(), "sub_only")
	}
	if Audio(-1).String() != "unknown" {
		t.Fatalf("Audio(-1).String() = %q, want %q", Audio(-1).String(), "unknown")
	}
	if Audio(99).String() != "unknown" {
		t.Fatalf("Audio(99).String() = %q, want %q", Audio(99).String(), "unknown")
	}
}

// TestProperties_RewriteTitle checks that a rewrite keeps every release token after the title.
func TestProperties_RewriteTitle(t *testing.T) {
	t.Parallel()
	f := func(ep uint16, qualIdx uint8) bool {
		suffix := fmt.Sprintf(".S%02dE%02d.720p-GRP%d", ep%30+1, ep%50+1, qualIdx)
		release := "Alone.Ueberlebe.die.Wildnis" + suffix
		foreign := "Alone Ueberlebe die Wildnis"
		canonical := "Alone Germany"
		got, err := RewriteTitle(release, foreign, canonical)
		if err != nil {
			return false
		}
		return strings.HasSuffix(got, suffix)
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Fatalf("property check failed: %v", err)
	}
}

// TestProperties_Variants checks that Variants never exceeds its cap or returns the input.
func TestProperties_Variants(t *testing.T) {
	t.Parallel()
	rulesMap, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	deRules := rulesMap["de"]

	titles := []string{
		"Die Ärzte",
		"Käfer",
		"Übergrößen",
		"Das Boot",
		"Der Herr der Ringe",
		"Schön",
		"Inception",
	}

	f := func(titleIdx uint8, maxRaw int16) bool {
		title := titles[int(titleIdx)%len(titles)]
		maxCount := int(maxRaw % 15)
		variants := Variants(title, deRules, maxCount)
		if maxCount <= 0 {
			return variants == nil
		}
		if len(variants) > maxCount {
			return false
		}
		seen := make(map[string]struct{}, len(variants))
		for _, v := range variants {
			if strings.EqualFold(v, title) {
				return false
			}
			lower := strings.ToLower(v)
			if _, ok := seen[lower]; ok {
				return false
			}
			seen[lower] = struct{}{}
		}
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 300}); err != nil {
		t.Fatalf("property check failed: %v", err)
	}
}
