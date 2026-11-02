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

	t.Run("negative unknown field", func(t *testing.T) {
		t.Parallel()
		invalid := []byte("code: de\nname: German\nunknown_field: true\n")
		if _, err := parse(invalid); err == nil {
			t.Fatalf("parse() should reject unknown fields")
		}
	})

	t.Run("negative code mismatch", func(t *testing.T) {
		t.Parallel()
		r := Rules{Code: "en"}
		if err := validate(&r, "de"); err == nil {
			t.Fatalf("validate() should fail on code stem mismatch")
		}
	})

	t.Run("negative empty to-list", func(t *testing.T) {
		t.Parallel()
		r := Rules{Code: "de", Expansions: []Expansion{{From: "ä", To: []string{}}}}
		if err := validate(&r, "de"); err == nil {
			t.Fatalf("validate() should fail on empty to-list")
		}
	})

	t.Run("negative invalid regex", func(t *testing.T) {
		t.Parallel()
		r := Rules{Code: "de", AudioTags: []string{"[unclosed"}}
		if err := validate(&r, "de"); err == nil {
			t.Fatalf("validate() should fail on invalid regex")
		}
	})

	t.Run("boundary empty data", func(t *testing.T) {
		t.Parallel()
		if _, err := parse([]byte("")); err == nil {
			t.Fatalf("parse() should fail on empty input")
		}
	})
}

func TestVariants(t *testing.T) {
	t.Parallel()
	rules, err := Load()
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	de := rules["de"]
	fr := rules["fr"]

	tests := []struct {
		name     string
		title    string
		rules    Rules
		max      int
		want     []string
		wantNone bool
	}{
		{
			name:  "positive de umlaut Käfer",
			title: "Käfer",
			rules: de,
			max:   5,
			want:  []string{"Kaefer", "Kafer"},
		},
		{
			name:  "positive de Die Ärzte full and stripped",
			title: "Die Ärzte",
			rules: de,
			max:   5,
			want:  []string{"Die Aerzte", "Die Arzte", "Aerzte", "Arzte", "Ärzte"},
		},
		{
			name:  "positive fr accent and article",
			title: "Le Fabuleux Destin d'Amélie Poulain",
			rules: fr,
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
			rules: fr,
			max:   5,
			want:  []string{"Amour Ouf"},
		},
		{
			name:     "negative max zero",
			title:    "Käfer",
			rules:    de,
			max:      0,
			wantNone: true,
		},
		{
			name:     "negative max negative",
			title:    "Käfer",
			rules:    de,
			max:      -2,
			wantNone: true,
		},
		{
			name:     "negative empty title",
			title:    "  ",
			rules:    de,
			max:      5,
			wantNone: true,
		},
		{
			name:     "negative no variants possible",
			title:    "Inception",
			rules:    de,
			max:      5,
			wantNone: true,
		},
		{
			name:  "boundary max 1 cap",
			title: "Die Ärzte",
			rules: de,
			max:   1,
			want:  []string{"Die Aerzte"},
		},
		{
			name:  "boundary max 2 cap",
			title: "Die Ärzte",
			rules: de,
			max:   2,
			want:  []string{"Die Aerzte", "Die Arzte"},
		},
		{
			name:     "boundary only article der",
			title:    "Der",
			rules:    de,
			max:      5,
			wantNone: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Variants(tc.title, tc.rules, tc.max)
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

func TestRewriteTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		release   string
		foreign   string
		canonical string
		want      string
		wantErr   bool
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
		{
			name:      "negative prefix mismatch",
			release:   "Other.Show.S01E01",
			foreign:   "Alone",
			canonical: "Alone Germany",
			wantErr:   true,
		},
		{
			name:      "negative partial word mismatch",
			release:   "Aloneness.S01E01",
			foreign:   "Alone",
			canonical: "Alone Germany",
			wantErr:   true,
		},
		{
			name:      "negative release too short",
			release:   "Alone",
			foreign:   "Alone Germany",
			canonical: "Alone",
			wantErr:   true,
		},
		{
			name:      "negative empty release",
			release:   "",
			foreign:   "Alone",
			canonical: "Alone Germany",
			wantErr:   true,
		},
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
			if tc.wantErr {
				if err == nil {
					t.Fatalf("RewriteTitle() expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("RewriteTitle() unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("RewriteTitle() = %q, want %q", got, tc.want)
			}
		})
	}
}

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

func TestClassify(t *testing.T) {
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
		{
			name:    "negative WEB-DL source is not the DL audio tag",
			release: "Show.S01E01.1080p.WEB-DL.x264-GRP",
			rules:   de,
			want:    AudioUnknown,
		},
		{
			name:    "negative Germany in title is not the GERMAN tag",
			release: "Alone.Germany.S01E01.720p.HDTV-GRP",
			rules:   de,
			want:    AudioUnknown,
		},
		{
			name:    "positive DL token before group dash",
			release: "Show.S01E01.GERMAN.DL-GRP",
			rules:   de,
			want:    AudioDubbed,
		},
		{
			name:    "positive VOSTFR with WEB-DL stays sub-only",
			release: "Le.Film.2020.VOSTFR.1080p.WEB-DL-GRP",
			rules:   fr,
			want:    AudioSubOnly,
		},
		{
			name:    "positive German dubbed tag",
			release: "Kaefer.S01E01.GERMAN.720p-GRP",
			rules:   de,
			want:    AudioDubbed,
		},
		{
			name:    "positive French dubbed tag",
			release: "Movie.FRENCH.1080p-GRP",
			rules:   fr,
			want:    AudioDubbed,
		},
		{
			name:    "positive French VOSTFR subtitle only",
			release: "Movie.VOSTFR.1080p-GRP",
			rules:   fr,
			want:    AudioSubOnly,
		},
		{
			name:    "positive German OmU subtitle only",
			release: "Movie.OmU.1080p-GRP",
			rules:   de,
			want:    AudioSubOnly,
		},
		{
			name:    "negative unknown audio",
			release: "Movie.1080p-GRP",
			rules:   de,
			want:    AudioUnknown,
		},
		{
			name:    "negative English audio",
			release: "Movie.ENGLISH.1080p-GRP",
			rules:   de,
			want:    AudioUnknown,
		},
		{
			name:    "boundary DL plus OmU dubbed wins",
			release: "Movie.DL.OmU.1080p-GRP",
			rules:   de,
			want:    AudioDubbed,
		},
		{
			name:    "boundary untagged group TvR dubbed",
			release: "Alone.Ueberlebe.die.Wildnis.S01E01.720p-TvR",
			rules:   de,
			want:    AudioDubbed,
		},
		{
			name:    "boundary untagged group with mkv extension",
			release: "Alone.S01E01.720p-TvR.mkv",
			rules:   de,
			want:    AudioDubbed,
		},
		{
			name:    "boundary untagged group with brackets",
			release: "[TvR] Alone.S01E01.720p",
			rules:   de,
			want:    AudioDubbed,
		},
		{
			name:    "boundary untagged group not in group position",
			release: "TvR.Movie.1080p-OTHER",
			rules:   de,
			want:    AudioUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Classify(tc.release, tc.rules)
			if got != tc.want {
				t.Fatalf("Classify(%q) = %v, want %v", tc.release, got, tc.want)
			}
		})
	}
}

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
}

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
