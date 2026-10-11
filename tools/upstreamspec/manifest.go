// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// pin is one upstream OpenAPI spec, fixed to the bytes of one file at one commit.
//
// Tag is the upstream release the commit belongs to. Renovate reads Repository, Tag and Commit
// (renovate.json) and proposes the next stable release; SHA256 then decides whether the spec file
// changed with it.
type pin struct {
	Name       string   `yaml:"name"`
	Repository string   `yaml:"repository"`
	Tag        string   `yaml:"tag"`
	Commit     string   `yaml:"commit"`
	Path       string   `yaml:"path"`
	SHA256     string   `yaml:"sha256"`
	License    string   `yaml:"license"`
	Prefix     string   `yaml:"prefix"`
	Operations []string `yaml:"operations"`
}

var (
	pinName    = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	repository = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9._-]+$`)
	tagName    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	commitID   = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sha256Hex  = regexp.MustCompile(`^[0-9a-f]{64}$`)
	licenseID  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+-]*$`)
	prefixPath = regexp.MustCompile(`^(/[A-Za-z0-9._-]+)*$`)
	operation  = regexp.MustCompile(`^(GET|POST) (/[A-Za-z0-9._{}-]+)+$`)
)

// readManifest decodes the manifest and refuses one that is not well formed. A key the tool does
// not read is refused, so a misspelled field cannot pass as an absent one.
func readManifest(file string) ([]pin, error) {
	// #nosec G304 -- the manifest path is the caller's argument; the file is only decoded
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var manifest struct {
		Specs []pin `yaml:"specs"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	if len(manifest.Specs) == 0 {
		return nil, fmt.Errorf("%s: no \"specs\" entry", file)
	}
	seen := map[string]bool{}
	for index, p := range manifest.Specs {
		problem := p.problem()
		if problem == "" && seen[p.Name] {
			problem = "repeats the name of an earlier entry"
		}
		seen[p.Name] = true
		if problem != "" {
			return nil, fmt.Errorf("%s: specs[%d] (%s): %s", file, index, p.Name, problem)
		}
	}
	return manifest.Specs, nil
}

func (p pin) problem() string {
	checks := []struct {
		ok   bool
		text string
	}{
		{pinName.MatchString(p.Name), "name must be lower-case letters, digits and hyphens"},
		{repository.MatchString(p.Repository), "repository must be <owner>/<name>"},
		{tagName.MatchString(p.Tag), "tag must name one release tag"},
		{commitID.MatchString(p.Commit), "commit must be a full 40-digit commit id"},
		{cleanRelative(p.Path), "path must be one clean file path inside the upstream repository"},
		{sha256Hex.MatchString(p.SHA256), "sha256 must be 64 lower-case hex digits"},
		{licenseID.MatchString(p.License), "license must be the SPDX id of the upstream repository"},
		{prefixPath.MatchString(p.Prefix), "prefix must be empty or a path like /api/v1"},
		{len(p.Operations) > 0, "operations lists nothing, so the contract would check nothing"},
	}
	for _, check := range checks {
		if !check.ok {
			return check.text
		}
	}
	return p.operationProblem()
}

func (p pin) operationProblem() string {
	seen := map[string]bool{}
	for _, entry := range p.Operations {
		_, route, _ := strings.Cut(entry, " ")
		switch {
		case !operation.MatchString(entry):
			return fmt.Sprintf("operation %q must be \"GET <path>\" or \"POST <path>\"", entry)
		case !strings.HasPrefix(route, p.Prefix+"/"):
			return fmt.Sprintf("operation %q does not start with the prefix %q", entry, p.Prefix)
		case seen[entry]:
			return fmt.Sprintf("operation %q is listed twice", entry)
		}
		seen[entry] = true
	}
	return ""
}

// cleanRelative reports whether file names one path below a root: not absolute, no "..", no
// trailing slash, no "." segment.
func cleanRelative(file string) bool {
	if file == "" || path.IsAbs(file) || strings.ContainsAny(file, "\\ \t\r\n") {
		return false
	}
	return path.Clean(file) == file && file != "." && file != ".." && !strings.HasPrefix(file, "../")
}

// source is where the pinned file is read from below base.
func (p pin) source(base string) string {
	return strings.TrimSuffix(base, "/") + "/" + p.Repository + "/" + p.Commit + "/" + p.Path
}

// specFile is the name of the cached spec inside the cache directory.
func (p pin) specFile() string { return p.Name + ".spec" }
