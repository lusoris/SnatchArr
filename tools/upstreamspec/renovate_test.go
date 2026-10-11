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

// reading is what Renovate takes from one manifest entry.
type reading struct{ repository, tag, commit string }

// renovateProblems returns why Renovate would not keep the pins current, one line per reason:
// custom managers are off, no manager or more than one reads the manifest, the manager asks
// another datasource or compares the four-part tags with another versioning, its pattern does not
// capture a pin as the manifest has it, or a pin update would be merged without a person.
func renovateProblems(config renovateConfig, manifest string, pins []pin) []string {
	var problems []string
	if !slices.Contains(config.EnabledManagers, customRegex) {
		problems = append(problems, "enabledManagers lacks custom.regex, so Renovate runs no custom manager")
	}
	if !config.mergedByHand() {
		problems = append(problems, "the last package rule that sets automerge for custom.regex does not set it to false")
	}
	var readers []customManager
	for _, manager := range config.CustomManagers {
		if slices.Contains(manager.ManagerFilePatterns, committedManifest) {
			readers = append(readers, manager)
		}
	}
	if len(readers) != 1 {
		return append(problems, fmt.Sprintf("%d custom manager(s) read %s; want 1", len(readers), committedManifest))
	}
	problems = append(problems, readers[0].settingProblems()...)
	return append(problems, readers[0].pinProblems(manifest, pins)...)
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
	settings := []struct{ name, got, want string }{
		{"customType", m.CustomType, "regex"},
		{"datasourceTemplate", m.DatasourceTemplate, "github-tags"},
		{"versioningTemplate", m.VersioningTemplate, "loose"},
	}
	var problems []string
	for _, setting := range settings {
		if setting.got != setting.want {
			problems = append(problems, fmt.Sprintf("%s is %q; want %q", setting.name, setting.got, setting.want))
		}
	}
	return problems
}

// pinProblems applies matchStrings to the manifest text the way the regex manager does (every
// match of every pattern is one dependency) and compares what it captures with the pins.
func (m customManager) pinProblems(manifest string, pins []pin) []string {
	read := map[string]reading{}
	matches := 0
	for _, pattern := range m.MatchStrings {
		expression, err := regexp.Compile(pattern)
		if err != nil {
			return []string{"matchStrings: " + err.Error()}
		}
		for _, match := range expression.FindAllStringSubmatch(manifest, -1) {
			group := func(name string) string {
				if index := expression.SubexpIndex(name); index >= 0 {
					return match[index]
				}
				return ""
			}
			read[group("depName")] = reading{group("packageName"), group("currentValue"), group("currentDigest")}
			matches++
		}
	}
	var problems []string
	for _, p := range pins {
		if got, want := read[p.Name], (reading{p.Repository, p.Tag, p.Commit}); got != want {
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

// renovateCase is one defect: change plants it in the configuration, manifest (when set) replaces
// the committed manifest text, and want holds a part of each problem it must be reported with.
type renovateCase struct {
	name     string
	change   func(config *renovateConfig, manager *customManager)
	manifest string
	want     []string
}

func configurationDefects() []renovateCase {
	return []renovateCase{
		{
			name: "custom managers are off",
			want: []string{"enabledManagers lacks custom.regex"},
			change: func(config *renovateConfig, _ *customManager) {
				config.EnabledManagers = slices.DeleteFunc(config.EnabledManagers, func(name string) bool { return name == customRegex })
			},
		},
		{
			name:   "the manager reads another file",
			want:   []string{"0 custom manager(s) read"},
			change: func(_ *renovateConfig, manager *customManager) { manager.ManagerFilePatterns = []string{"specs.yaml"} },
		},
		{
			name:   "a datasource that lists releases only",
			want:   []string{`datasourceTemplate is "github-releases"`},
			change: func(_ *renovateConfig, manager *customManager) { manager.DatasourceTemplate = "github-releases" },
		},
		{
			name:   "the default versioning",
			want:   []string{`versioningTemplate is ""`},
			change: func(_ *renovateConfig, manager *customManager) { manager.VersioningTemplate = "" },
		},
		{
			name:   "a pattern that does not compile",
			want:   []string{"matchStrings: "},
			change: func(_ *renovateConfig, manager *customManager) { manager.MatchStrings = []string{"("} },
		},
		{
			name: "no rule keeps the pins from automerge",
			want: []string{"automerge"},
			change: func(config *renovateConfig, _ *customManager) {
				config.PackageRules = slices.DeleteFunc(config.PackageRules, forSpecs)
			},
		},
		{
			name: "the rule for the pins comes before the general automerge rule",
			want: []string{"automerge"},
			change: func(config *renovateConfig, _ *customManager) {
				index := slices.IndexFunc(config.PackageRules, forSpecs)
				rule := config.PackageRules[index]
				config.PackageRules = slices.Insert(slices.Delete(config.PackageRules, index, index+1), 0, rule)
			},
		},
	}
}

// manifestDefects returns the manifest layouts the pattern of renovate.json must not read as the
// first pin: its fields in another order, and a commit id that is one digit short.
func manifestDefects(t *testing.T, manifest string, pins []pin) []renovateCase {
	t.Helper()
	first := pins[0]
	inOrder := fmt.Sprintf("    repository: %s\n    tag: %s\n", first.Repository, first.Tag)
	swapped := fmt.Sprintf("    tag: %s\n    repository: %s\n", first.Tag, first.Repository)
	unread := []string{first.Name + ": Renovate reads", fmt.Sprintf("match %d time(s)", len(pins)-1)}
	return []renovateCase{
		{name: "the tag before the repository in one entry", want: unread, manifest: planted(t, manifest, inOrder, swapped)},
		{name: "a commit id of 39 digits", want: unread, manifest: planted(t, manifest, first.Commit, first.Commit[:39])},
	}
}

// decoded returns a fresh copy of the committed configuration and its one custom manager.
func decoded(t *testing.T, raw []byte) (*renovateConfig, *customManager) {
	t.Helper()
	var config renovateConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("decode renovate.json: %v", err)
	}
	if len(config.CustomManagers) != 1 || !slices.ContainsFunc(config.PackageRules, forSpecs) {
		t.Fatalf("renovate.json has %d custom manager(s) and package rules %+v; the planted defects need one manager and a rule for it",
			len(config.CustomManagers), config.PackageRules)
	}
	return &config, &config.CustomManagers[0]
}

// TestRenovateKeepsEveryPinCurrent checks the committed renovate.json against the committed
// manifest, and that each defect that would stop the updates, or merge one unseen, is reported.
func TestRenovateKeepsEveryPinCurrent(t *testing.T) {
	t.Parallel()
	raw, manifest, pins := committed(t)
	cases := slices.Concat([]renovateCase{{name: "the committed files"}}, configurationDefects(), manifestDefects(t, manifest, pins))
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			config, manager := decoded(t, raw)
			if c.change != nil {
				c.change(config, manager)
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
