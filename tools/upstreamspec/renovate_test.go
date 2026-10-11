// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const (
	// committedManifest is the manifest as the Renovate configuration names it.
	committedManifest = "tools/upstreamspec/specs.yaml"
	customRegex       = "custom.regex"
	// tagSource lists the tags of a repository with the date of each. branchSource resolves a
	// branch to its newest commit and dates it; git-refs would do the first but gives no date,
	// and Renovate holds an update without one for as long as minimumReleaseAge is set.
	tagSource    = "github-tags"
	branchSource = "github-digest"
	// readers is the number of custom managers that read the manifest: one per datasource.
	readers = 2
)

// renovateConfig is the part of renovate.json that decides whether Renovate keeps the pins of the
// manifest current.
//
//nolint:tagliatelle // renovate.json is camelCase
type renovateConfig struct {
	EnabledManagers []string        `json:"enabledManagers"`
	CustomManagers  []customManager `json:"customManagers"`
	PackageRules    []packageRule   `json:"packageRules"`
}

//nolint:tagliatelle // renovate.json is camelCase
type customManager struct {
	CustomType          string   `json:"customType"`
	ManagerFilePatterns []string `json:"managerFilePatterns"`
	MatchStrings        []string `json:"matchStrings"`
	DatasourceTemplate  string   `json:"datasourceTemplate"`
	VersioningTemplate  string   `json:"versioningTemplate"`
}

//nolint:tagliatelle // renovate.json is camelCase
type packageRule struct {
	MatchManagers []string `json:"matchManagers"`
	Automerge     *bool    `json:"automerge"`
}

// reading is what Renovate takes from one manifest entry, and the datasource it asks about it.
type reading struct{ datasource, repository, ref, commit string }

// wanted returns the reading that keeps p current: a tag pin is asked of the tags of its
// repository, a branch pin of the newest commit of its branch.
func wanted(p pin) reading {
	if p.Branch != "" {
		return reading{branchSource, p.Repository, p.Branch, p.Commit}
	}
	return reading{tagSource, p.Repository, p.Tag, p.Commit}
}

// versioningFor returns the versioning a manager of the datasource must name, and whether the
// datasource is one the pins are asked of. Tags have four parts, which only loose compares; a
// branch is compared with nothing, which is the default of its datasource.
func versioningFor(datasource string) (versioning string, known bool) {
	switch datasource {
	case tagSource:
		return "loose", true
	case branchSource:
		return "", true
	default:
		return "", false
	}
}

// renovateProblems returns why Renovate would not keep the pins current, one line per reason:
// custom managers are off, the manifest is not read by one manager for tag pins and one for
// branch pins, a manager asks another datasource or compares with another versioning, the
// patterns do not capture a pin as the manifest has it or capture it for the wrong datasource, or
// a pin update would be merged without a person.
func renovateProblems(config renovateConfig, manifest string, pins []pin) []string {
	var problems []string
	if !slices.Contains(config.EnabledManagers, customRegex) {
		problems = append(problems, "enabledManagers lacks custom.regex, so Renovate runs no custom manager")
	}
	if !config.mergedByHand() {
		problems = append(problems, "the last package rule that sets automerge for custom.regex does not set it to false")
	}
	var reading []customManager
	for _, manager := range config.CustomManagers {
		if slices.Contains(manager.ManagerFilePatterns, committedManifest) {
			reading = append(reading, manager)
		}
	}
	if len(reading) != readers {
		return append(problems, fmt.Sprintf("%d custom manager(s) read %s; want %d, one for tag pins and one for branch pins",
			len(reading), committedManifest, readers))
	}
	var settings []string
	for _, manager := range reading {
		settings = append(settings, manager.settingProblems()...)
	}
	if len(settings) > 0 {
		return append(problems, settings...)
	}
	return append(problems, pinProblems(reading, manifest, pins)...)
}

// mergedByHand reports whether the last package rule that sets automerge and can apply to an
// update of the custom manager sets it to false. Later rules win in Renovate. A rule counts as
// applicable unless it names other managers, which is stricter than Renovate: its other matchers
// are not read here.
func (c renovateConfig) mergedByHand() bool {
	byHand := false
	for _, rule := range c.PackageRules {
		if rule.Automerge == nil {
			continue
		}
		if len(rule.MatchManagers) > 0 && !slices.Contains(rule.MatchManagers, customRegex) {
			continue
		}
		byHand = !*rule.Automerge
	}
	return byHand
}

func (m customManager) settingProblems() []string {
	versioning, known := versioningFor(m.DatasourceTemplate)
	if !known {
		return []string{fmt.Sprintf("datasourceTemplate is %q; want %q for tag pins or %q for branch pins",
			m.DatasourceTemplate, tagSource, branchSource)}
	}
	settings := []struct{ name, got, want string }{
		{"customType", m.CustomType, "regex"},
		{"versioningTemplate", m.VersioningTemplate, versioning},
	}
	var problems []string
	for _, setting := range settings {
		if setting.got != setting.want {
			problems = append(problems, fmt.Sprintf("the %s manager: %s is %q; want %q", m.DatasourceTemplate, setting.name, setting.got, setting.want))
		}
	}
	return problems
}

// readInto applies matchStrings to the manifest text the way the regex manager does (every match
// of every pattern is one dependency), stores what it captures by dependency name and returns the
// number of matches.
func (m customManager) readInto(read map[string]reading, manifest string) (int, error) {
	matches := 0
	for _, pattern := range m.MatchStrings {
		expression, err := regexp.Compile(pattern)
		if err != nil {
			return 0, fmt.Errorf("matchStrings: %w", err)
		}
		for _, match := range expression.FindAllStringSubmatch(manifest, -1) {
			group := func(name string) string {
				if index := expression.SubexpIndex(name); index >= 0 {
					return match[index]
				}
				return ""
			}
			read[group("depName")] = reading{m.DatasourceTemplate, group("packageName"), group("currentValue"), group("currentDigest")}
			matches++
		}
	}
	return matches, nil
}

// pinProblems compares what the managers capture from the manifest text with the pins.
func pinProblems(managers []customManager, manifest string, pins []pin) []string {
	read := map[string]reading{}
	matches := 0
	for _, manager := range managers {
		count, err := manager.readInto(read, manifest)
		if err != nil {
			return []string{err.Error()}
		}
		matches += count
	}
	var problems []string
	for _, p := range pins {
		if got, want := read[p.Name], wanted(p); got != want {
			problems = append(problems, fmt.Sprintf("%s: Renovate reads %+v; the manifest pins %+v", p.Name, got, want))
		}
	}
	if matches != len(pins) {
		problems = append(problems, fmt.Sprintf("matchStrings match %d time(s); the manifest has %d pin(s)", matches, len(pins)))
	}
	return problems
}

// committed returns the Renovate configuration, the manifest text and its pins as committed.
func committed(t *testing.T) (config []byte, manifest string, pins []pin) {
	t.Helper()
	config, err := os.ReadFile("../../renovate.json")
	if err != nil {
		t.Fatalf("read the Renovate configuration: %v", err)
	}
	text, err := os.ReadFile("specs.yaml")
	if err != nil {
		t.Fatalf("read the manifest: %v", err)
	}
	pins, err = readManifest("specs.yaml")
	if err != nil {
		t.Fatalf("readManifest: %v", err)
	}
	return config, string(text), pins
}

// planted returns text with its one occurrence of old replaced.
func planted(t *testing.T, text, old, replacement string) string {
	t.Helper()
	if count := strings.Count(text, old); count != 1 {
		t.Fatalf("%q occurs %d time(s); a planted defect needs exactly one", old, count)
	}
	return strings.Replace(text, old, replacement, 1)
}

func forSpecs(rule packageRule) bool { return slices.Contains(rule.MatchManagers, customRegex) }

// renovateCase is one defect: change plants it in the configuration, of which tags and branches
// are the two managers, manifest (when set) replaces the committed manifest text, and want holds
// a part of each problem it must be reported with.
type renovateCase struct {
	name     string
	change   func(config *renovateConfig, tags, branches *customManager)
	manifest string
	want     []string
}

// managerDefects returns the defects of one of the two managers.
func managerDefects() []renovateCase {
	return []renovateCase{
		{
			name:   "the manager for tag pins reads another file",
			want:   []string{"1 custom manager(s) read"},
			change: func(_ *renovateConfig, tags, _ *customManager) { tags.ManagerFilePatterns = []string{"specs.yaml"} },
		},
		{
			name: "no manager for branch pins",
			want: []string{"1 custom manager(s) read"},
			change: func(config *renovateConfig, _, _ *customManager) {
				config.CustomManagers = slices.DeleteFunc(config.CustomManagers, func(manager customManager) bool {
					return manager.DatasourceTemplate == branchSource
				})
			},
		},
		{
			name:   "a datasource that lists releases only",
			want:   []string{`datasourceTemplate is "github-releases"`},
			change: func(_ *renovateConfig, tags, _ *customManager) { tags.DatasourceTemplate = "github-releases" },
		},
		{
			name:   "a datasource that dates no commit of a branch",
			want:   []string{`datasourceTemplate is "git-refs"`},
			change: func(_ *renovateConfig, _, branches *customManager) { branches.DatasourceTemplate = "git-refs" },
		},
		{
			name:   "the default versioning for tags",
			want:   []string{`the github-tags manager: versioningTemplate is ""`},
			change: func(_ *renovateConfig, tags, _ *customManager) { tags.VersioningTemplate = "" },
		},
		{
			name:   "a versioning that compares a branch",
			want:   []string{`the github-digest manager: versioningTemplate is "loose"`},
			change: func(_ *renovateConfig, _, branches *customManager) { branches.VersioningTemplate = "loose" },
		},
		{
			name:   "a manager of another type",
			want:   []string{`the github-digest manager: customType is "jsonata"`},
			change: func(_ *renovateConfig, _, branches *customManager) { branches.CustomType = "jsonata" },
		},
		{
			name: "branch pins asked of the tags",
			want: []string{": Renovate reads {datasource:github-tags"},
			change: func(_ *renovateConfig, _, branches *customManager) {
				branches.DatasourceTemplate, branches.VersioningTemplate = tagSource, "loose"
			},
		},
		{
			name:   "a pattern that does not compile",
			want:   []string{"matchStrings: "},
			change: func(_ *renovateConfig, tags, _ *customManager) { tags.MatchStrings = []string{"("} },
		},
	}
}

func configurationDefects() []renovateCase {
	return []renovateCase{
		{
			name: "custom managers are off",
			want: []string{"enabledManagers lacks custom.regex"},
			change: func(config *renovateConfig, _, _ *customManager) {
				config.EnabledManagers = slices.DeleteFunc(config.EnabledManagers, func(name string) bool { return name == customRegex })
			},
		},
		{
			name: "no rule keeps the pins from automerge",
			want: []string{"automerge"},
			change: func(config *renovateConfig, _, _ *customManager) {
				config.PackageRules = slices.DeleteFunc(config.PackageRules, forSpecs)
			},
		},
		{
			name: "the rule for the pins comes before the general automerge rule",
			want: []string{"automerge"},
			change: func(config *renovateConfig, _, _ *customManager) {
				index := slices.IndexFunc(config.PackageRules, forSpecs)
				rule := config.PackageRules[index]
				config.PackageRules = slices.Insert(slices.Delete(config.PackageRules, index, index+1), 0, rule)
			},
		},
	}
}

// manifestDefects returns the manifest layouts the patterns of renovate.json must not read as a
// pin, for the first tag pin and the first branch pin: its fields in another order, and a commit
// id that is one digit short.
func manifestDefects(t *testing.T, manifest string, pins []pin) []renovateCase {
	t.Helper()
	kinds := []struct {
		field string
		of    func(pin) string
	}{
		{"tag", func(p pin) string { return p.Tag }},
		{"branch", func(p pin) string { return p.Branch }},
	}
	// Two planted defects per kind of pin.
	cases := make([]renovateCase, 0, 2*len(kinds))
	for _, kind := range kinds {
		index := slices.IndexFunc(pins, func(p pin) bool { return kind.of(p) != "" })
		if index < 0 {
			t.Fatalf("the manifest has no %s pin; the planted defects need one", kind.field)
		}
		first := pins[index]
		inOrder := fmt.Sprintf("    repository: %s\n    %s: %s\n", first.Repository, kind.field, kind.of(first))
		swapped := fmt.Sprintf("    %s: %s\n    repository: %s\n", kind.field, kind.of(first), first.Repository)
		unread := []string{first.Name + ": Renovate reads", fmt.Sprintf("match %d time(s)", len(pins)-1)}
		cases = append(cases,
			renovateCase{name: "the " + kind.field + " before the repository in one entry", want: unread, manifest: planted(t, manifest, inOrder, swapped)},
			renovateCase{name: "a commit id of 39 digits in a " + kind.field + " pin", want: unread, manifest: planted(t, manifest, first.Commit, first.Commit[:39])},
		)
	}
	return cases
}

// decoded returns a fresh copy of the committed configuration.
func decoded(t *testing.T, raw []byte) *renovateConfig {
	t.Helper()
	config := &renovateConfig{}
	if err := json.Unmarshal(raw, config); err != nil {
		t.Fatalf("decode renovate.json: %v", err)
	}
	return config
}

// managers returns the two custom managers a defect is planted in: the one for tag pins and the
// one for branch pins.
func managers(t *testing.T, config *renovateConfig) (tags, branches *customManager) {
	t.Helper()
	reading := 0
	for index := range config.CustomManagers {
		manager := &config.CustomManagers[index]
		if !slices.Contains(manager.ManagerFilePatterns, committedManifest) {
			continue
		}
		reading++
		switch manager.DatasourceTemplate {
		case tagSource:
			tags = manager
		case branchSource:
			branches = manager
		}
	}
	if reading != readers || tags == nil || branches == nil || !slices.ContainsFunc(config.PackageRules, forSpecs) {
		t.Fatalf("renovate.json has %d custom manager(s) for %s and package rules %+v; the planted defects need a manager for %s, one for %s and a rule for them",
			reading, committedManifest, config.PackageRules, tagSource, branchSource)
	}
	return tags, branches
}

// TestRenovateKeepsEveryPinCurrent checks the committed renovate.json against the committed
// manifest, and that each defect that would stop the updates, or merge one unseen, is reported.
func TestRenovateKeepsEveryPinCurrent(t *testing.T) {
	t.Parallel()
	raw, manifest, pins := committed(t)
	cases := slices.Concat([]renovateCase{{name: "the committed files"}}, configurationDefects(), managerDefects(), manifestDefects(t, manifest, pins))
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			config := decoded(t, raw)
			if c.change != nil {
				tags, branches := managers(t, config)
				c.change(config, tags, branches)
			}
			text := manifest
			if c.manifest != "" {
				text = c.manifest
			}
			got := renovateProblems(*config, text, pins)
			if len(got) != len(c.want) {
				t.Fatalf("problems = %q; want %d matching %q", got, len(c.want), c.want)
			}
			for i, want := range c.want {
				if !strings.Contains(got[i], want) {
					t.Errorf("problem %d = %q; want it to contain %q", i, got[i], want)
				}
			}
		})
	}
}
