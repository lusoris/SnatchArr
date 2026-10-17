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

	"gopkg.in/yaml.v3"
)

const (
	// committedManifest is the manifest as the Renovate configuration names it.
	committedManifest = "tools/upstreamspec/specs.yaml"
	customRegex       = "custom.regex"
	// sourceDatasource resolves a branch to its newest commit and dates it; git-refs gives no date,
	// and Renovate holds an update without one for as long as minimumReleaseAge is set.
	sourceDatasource = "github-digest"
	// sourceBranch is the goenvoy branch the source pin follows.
	sourceBranch = "main"
	// readers is the number of custom managers that read the manifest: the one for the source pin.
	readers = 1
)

// renovateConfig is the part of renovate.json that decides whether Renovate keeps the source pin of
// the manifest current.
//
//nolint:tagliatelle // renovate.json is camelCase
type renovateConfig struct {
	EnabledManagers []string        `json:"enabledManagers"`
	CustomManagers  []customManager `json:"customManagers"`
	PackageRules    []packageRule   `json:"packageRules"`
}

//nolint:tagliatelle // renovate.json is camelCase
type customManager struct {
	CustomType           string   `json:"customType"`
	ManagerFilePatterns  []string `json:"managerFilePatterns"`
	MatchStrings         []string `json:"matchStrings"`
	CurrentValueTemplate string   `json:"currentValueTemplate"`
	DatasourceTemplate   string   `json:"datasourceTemplate"`
	VersioningTemplate   string   `json:"versioningTemplate"`
}

//nolint:tagliatelle // renovate.json is camelCase
type packageRule struct {
	MatchManagers []string `json:"matchManagers"`
	Automerge     *bool    `json:"automerge"`
}

// reading is what Renovate takes from the manifest source block, and the datasource it asks about it.
type reading struct{ datasource, repository, branch, commit string }

// wanted returns the reading that keeps the source pin current: github-digest on branch main of the
// upstream repository at its pinned commit.
func wanted(s manifestSource) reading {
	return reading{sourceDatasource, s.Repository, sourceBranch, s.Commit}
}

// renovateProblems returns why Renovate would not keep the source pin current, one line per reason:
// custom managers are off, the manifest is not read by exactly one manager for the source manifest,
// the manager asks another datasource, tracks another branch or compares with a versioning template,
// the patterns do not capture the source pin as the manifest has it, or the update would be merged
// without a person.
func renovateProblems(config renovateConfig, manifest string, source manifestSource) []string {
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
		return append(problems, fmt.Sprintf("%d custom manager(s) read %s; want %d for the source manifest",
			len(reading), committedManifest, readers))
	}
	manager := reading[0]
	if settings := manager.settingProblems(); len(settings) > 0 {
		return append(problems, settings...)
	}
	return append(problems, pinProblems(manager, manifest, source)...)
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

// settingProblems names each setting of the manager that differs from the one the source pin
// needs.
func (m customManager) settingProblems() []string {
	var problems []string
	if m.CustomType != "regex" {
		problems = append(problems, fmt.Sprintf("the %s manager: customType is %q; want \"regex\"", m.DatasourceTemplate, m.CustomType))
	}
	if m.DatasourceTemplate != sourceDatasource {
		problems = append(problems, fmt.Sprintf("datasourceTemplate is %q; want %q", m.DatasourceTemplate, sourceDatasource))
	}
	if m.CurrentValueTemplate != sourceBranch {
		problems = append(problems, fmt.Sprintf("currentValueTemplate is %q; want %q", m.CurrentValueTemplate, sourceBranch))
	}
	if m.VersioningTemplate != "" {
		problems = append(problems, fmt.Sprintf("the %s manager: versioningTemplate is %q; want \"\"", m.DatasourceTemplate, m.VersioningTemplate))
	}
	return problems
}

// readInto applies matchStrings to the manifest text the way the regex manager does (every match
// of every pattern is one dependency), stores what it captures by dependency name and returns the
// number of matches. A value the pattern does not capture comes from its template, and the package
// name defaults to the dependency name, as in Renovate.
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
			branch := group("currentValue")
			if branch == "" {
				branch = m.CurrentValueTemplate
			}
			pkg := group("packageName")
			if pkg == "" {
				pkg = group("depName")
			}
			read[group("depName")] = reading{
				datasource: m.DatasourceTemplate,
				repository: pkg,
				branch:     branch,
				commit:     group("currentDigest"),
			}
			matches++
		}
	}
	return matches, nil
}

// pinProblems compares what the manager captures from the manifest text with the source pin.
func pinProblems(manager customManager, manifest string, source manifestSource) []string {
	read := map[string]reading{}
	matches, err := manager.readInto(read, manifest)
	if err != nil {
		return []string{err.Error()}
	}
	var problems []string
	if got, want := read[source.Repository], wanted(source); got != want {
		problems = append(problems, fmt.Sprintf("%s: Renovate reads %+v; the manifest pins %+v", source.Repository, got, want))
	}
	if matches != 1 {
		problems = append(problems, fmt.Sprintf("matchStrings match %d time(s); want 1", matches))
	}
	return problems
}

// committed returns the Renovate configuration, the manifest text and its source pin as committed.
func committed(t *testing.T) (config []byte, manifest string, source manifestSource) {
	t.Helper()
	config, err := os.ReadFile("../../renovate.json")
	if err != nil {
		t.Fatalf("read the Renovate configuration: %v", err)
	}
	text, err := os.ReadFile("specs.yaml")
	if err != nil {
		t.Fatalf("read the manifest: %v", err)
	}
	var local localManifest
	if err := yaml.Unmarshal(text, &local); err != nil {
		t.Fatalf("unmarshal specs.yaml: %v", err)
	}
	return config, string(text), local.Source
}

// planted returns text with its one occurrence of old replaced.
func planted(t *testing.T, text, old, replacement string) string {
	t.Helper()
	if count := strings.Count(text, old); count != 1 {
		t.Fatalf("%q occurs %d time(s); a planted defect needs exactly one", old, count)
	}
	return strings.Replace(text, old, replacement, 1)
}

// forSpecs reports whether rule applies to the custom managers.
func forSpecs(rule packageRule) bool { return slices.Contains(rule.MatchManagers, customRegex) }

// renovateCase is one defect: change plants it in the configuration and in the manager that reads
// the manifest, manifest (when set) replaces the committed manifest text, and want holds a part of
// each problem it must be reported with.
type renovateCase struct {
	name     string
	change   func(config *renovateConfig, manager *customManager)
	manifest string
	want     []string
}

// managerDefects returns the defects of the manager that reads the source pin.
func managerDefects() []renovateCase {
	return []renovateCase{
		{
			name:   "the manager reads another file",
			want:   []string{"0 custom manager(s) read"},
			change: func(_ *renovateConfig, m *customManager) { m.ManagerFilePatterns = []string{"specs.yaml"} },
		},
		{
			name: "no manager for the source manifest",
			want: []string{"0 custom manager(s) read"},
			change: func(config *renovateConfig, _ *customManager) {
				config.CustomManagers = slices.DeleteFunc(config.CustomManagers, func(m customManager) bool {
					return slices.Contains(m.ManagerFilePatterns, committedManifest)
				})
			},
		},
		{
			name:   "a datasource that dates no commit",
			want:   []string{`datasourceTemplate is "git-refs"`},
			change: func(_ *renovateConfig, m *customManager) { m.DatasourceTemplate = "git-refs" },
		},
		{
			name:   "a branch other than main",
			want:   []string{`currentValueTemplate is "develop"`},
			change: func(_ *renovateConfig, m *customManager) { m.CurrentValueTemplate = "develop" },
		},
		{
			name:   "a versioning template",
			want:   []string{`the github-digest manager: versioningTemplate is "loose"`},
			change: func(_ *renovateConfig, m *customManager) { m.VersioningTemplate = "loose" },
		},
		{
			name:   "a manager of another type",
			want:   []string{`the github-digest manager: customType is "jsonata"`},
			change: func(_ *renovateConfig, m *customManager) { m.CustomType = "jsonata" },
		},
		{
			name:   "a pattern that does not compile",
			want:   []string{"matchStrings: "},
			change: func(_ *renovateConfig, m *customManager) { m.MatchStrings = []string{"("} },
		},
	}
}

// configurationDefects returns the defects of the configuration around the manager.
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
// source pin: its fields in another order, and a commit id that is one digit short.
func manifestDefects(t *testing.T, manifest string, source manifestSource) []renovateCase {
	t.Helper()
	inOrder := fmt.Sprintf("  repository: %s\n  commit: %s\n", source.Repository, source.Commit)
	swapped := fmt.Sprintf("  commit: %s\n  repository: %s\n", source.Commit, source.Repository)
	unread := []string{source.Repository + ": Renovate reads", "matchStrings match 0 time(s)"}
	return []renovateCase{
		{
			name:     "the commit before the repository in the source block",
			want:     unread,
			manifest: planted(t, manifest, inOrder, swapped),
		},
		{
			name:     "a commit id of 39 digits in the source block",
			want:     unread,
			manifest: planted(t, manifest, source.Commit, source.Commit[:39]),
		},
	}
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

// managerOf returns the one custom manager that reads the manifest.
func managerOf(t *testing.T, config *renovateConfig) *customManager {
	t.Helper()
	reading := 0
	var target *customManager
	for index := range config.CustomManagers {
		manager := &config.CustomManagers[index]
		if slices.Contains(manager.ManagerFilePatterns, committedManifest) {
			reading++
			target = manager
		}
	}
	if reading != readers || target == nil || !slices.ContainsFunc(config.PackageRules, forSpecs) {
		t.Fatalf("renovate.json has %d custom manager(s) for %s and package rules %+v; want %d",
			reading, committedManifest, config.PackageRules, readers)
	}
	return target
}

// TestRenovateKeepsEveryPinCurrent checks the committed renovate.json against the committed
// manifest, and that each defect that would stop the updates, or merge one unseen, is reported.
func TestRenovateKeepsEveryPinCurrent(t *testing.T) {
	t.Parallel()
	raw, manifest, source := committed(t)
	cases := slices.Concat([]renovateCase{{name: "the committed files"}}, configurationDefects(), managerDefects(), manifestDefects(t, manifest, source))
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			config := decoded(t, raw)
			if c.change != nil {
				m := managerOf(t, config)
				c.change(config, m)
			}
			text := manifest
			if c.manifest != "" {
				text = c.manifest
			}
			got := renovateProblems(*config, text, source)
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
