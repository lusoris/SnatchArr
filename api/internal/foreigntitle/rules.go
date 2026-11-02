// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package foreigntitle implements declarative foreign-title expansion, title rewriting, and audio classification.
package foreigntitle

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed rules/languages/*.yaml
var embeddedRulesFS embed.FS

// maxTagPatterns bounds the number of tag patterns parsed, compiled, and matched.
const maxTagPatterns = 100

// Expansion defines a phonetic or diacritic character mapping.
type Expansion struct {
	From string   `yaml:"from"`
	To   []string `yaml:"to"`
}

// Rules holds the localized parsing and classification settings for one language.
type Rules struct {
	Code             string      `yaml:"code"`
	Name             string      `yaml:"name"`
	Expansions       []Expansion `yaml:"expansions"`
	AudioTags        []string    `yaml:"audio_tags"`
	SubtitleOnlyTags []string    `yaml:"subtitle_only_tags"`
	UntaggedGroups   []string    `yaml:"untagged_groups"`
	StripArticles    []string    `yaml:"strip_articles"`

	cachedAudioTags []string
	audioPatterns   []*regexp.Regexp
	cachedSubTags   []string
	subPatterns     []*regexp.Regexp
}

// Audio classifies the audio delivery type of a release.
type Audio int

const (
	// AudioUnknown indicates that no audio or subtitle tags were matched.
	AudioUnknown Audio = iota
	// AudioDubbed indicates that the release provides localized audio dubbing.
	AudioDubbed
	// AudioSubOnly indicates that the release is subtitle-only in the target language.
	AudioSubOnly
)

// String returns a human-readable representation of Audio.
func (a Audio) String() string {
	switch a {
	case AudioDubbed:
		return "dubbed"
	case AudioSubOnly:
		return "sub_only"
	case AudioUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

// parse strictly decodes YAML bytes into Rules, failing on any unknown field or multiple documents.
func parse(data []byte) (Rules, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var r Rules
	if err := dec.Decode(&r); err != nil {
		return Rules{}, fmt.Errorf("decode yaml: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return Rules{}, errors.New("multiple YAML documents")
	}
	return r, nil
}

// validate ensures rule invariants: code matches file stem, regexes compile, and no empty to-lists.
func validate(r *Rules, stem string) error {
	if r.Code != stem {
		return fmt.Errorf("code %q does not match stem %q", r.Code, stem)
	}
	if err := validateExpansions(r.Expansions); err != nil {
		return err
	}
	audioRegexes, err := compileTagPatterns(r.AudioTags)
	if err != nil {
		return fmt.Errorf("audio_tags: %w", err)
	}
	subRegexes, err := compileTagPatterns(r.SubtitleOnlyTags)
	if err != nil {
		return fmt.Errorf("subtitle_only_tags: %w", err)
	}
	r.audioPatterns = audioRegexes
	r.cachedAudioTags = slices.Clone(r.AudioTags)
	r.subPatterns = subRegexes
	r.cachedSubTags = slices.Clone(r.SubtitleOnlyTags)
	return nil
}

// validateExpansions verifies that expansion mappings contain non-empty source and target strings.
func validateExpansions(expansions []Expansion) error {
	const maxExpansions = 100
	limit := min(len(expansions), maxExpansions)
	for i := range limit {
		exp := expansions[i]
		if exp.From == "" {
			return errors.New("expansion from is empty")
		}
		if len(exp.To) == 0 {
			return fmt.Errorf("expansion %q has empty to-list", exp.From)
		}
		limitTo := min(len(exp.To), 10)
		for j := range limitTo {
			if exp.To[j] == "" {
				return fmt.Errorf("expansion %q has empty target at index %d", exp.From, j)
			}
		}
	}
	return nil
}

// compileTagPatterns compiles tag strings into boundary-anchored regular expressions up to maxTagPatterns.
func compileTagPatterns(tags []string) ([]*regexp.Regexp, error) {
	limit := min(len(tags), maxTagPatterns)
	regexes := make([]*regexp.Regexp, 0, limit)
	for i := range limit {
		pattern := tags[i]
		if _, err := regexp.Compile(pattern); err != nil {
			return nil, fmt.Errorf("compile %q: %w", pattern, err)
		}
		rx, err := regexp.Compile(tokenPattern(pattern))
		if err != nil {
			return nil, fmt.Errorf("compile %q: %w", pattern, err)
		}
		regexes = append(regexes, rx)
	}
	return regexes, nil
}

// tokenPattern anchors a tag to release-token boundaries with delimiters including spaces, dots, underscores, brackets, parentheses, and dashes on either boundary.
func tokenPattern(tag string) string {
	return `(?i)(?:^|[ ._\[\](){}-])(?:` + tag + `)(?:[ ._\[\](){}-]|$)`
}

// Load reads and validates all embedded language rule YAML files into a map keyed by language code.
func Load() (map[string]Rules, error) {
	entries, err := fs.ReadDir(embeddedRulesFS, "rules/languages")
	if err != nil {
		return nil, fmt.Errorf("read embedded rules: %w", err)
	}
	const maxFiles = 50
	res := make(map[string]Rules, len(entries))
	limit := min(len(entries), maxFiles)
	for i := range limit {
		entry := entries[i]
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		stem := strings.TrimSuffix(entry.Name(), ".yaml")
		data, err := embeddedRulesFS.ReadFile("rules/languages/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", entry.Name(), err)
		}
		r, err := parse(data)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", entry.Name(), err)
		}
		if err := validate(&r, stem); err != nil {
			return nil, fmt.Errorf("validate %s: %w", entry.Name(), err)
		}
		res[r.Code] = r
	}
	return res, nil
}
