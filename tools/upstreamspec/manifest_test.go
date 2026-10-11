// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	someCommit = "0123456789abcdef0123456789abcdef01234567"
	someSHA256 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

// entry is a well-formed manifest entry; change replaces "<key>: <value>" lines by key.
func entry(change map[string]string) string {
	lines := []string{
		"name: sample",
		"repository: Owner/Project",
		"tag: v1.2.3",
		"commit: " + someCommit,
		"path: src/openapi.json",
		"sha256: " + someSHA256,
		"license: GPL-3.0",
		"prefix: /api/v1",
		"operations: [GET /api/v1/status, 'POST /api/v1/command/{id}']",
	}
	var out strings.Builder
	out.WriteString("specs:\n")
	for i, line := range lines {
		key, _, _ := strings.Cut(line, ":")
		if replaced, ok := change[key]; ok {
			line = replaced
		}
		if line == "" {
			continue
		}
		lead := "    "
		if i == 0 {
			lead = "  - "
		}
		out.WriteString(lead + line + "\n")
	}
	return out.String()
}

func writeFile(t *testing.T, file, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", file, err)
	}
}

func TestReadManifest(t *testing.T) {
	t.Parallel()
	with := func(key, line string) string { return entry(map[string]string{key: line}) }
	tests := []struct {
		name     string
		manifest string
		want     string // part of the error; empty for none
	}{
		{"well formed", entry(nil), ""},
		{"no prefix", entry(map[string]string{"prefix": "", "operations": "operations: [GET /status]"}), ""},
		{"a tag with four numbers", with("tag", "tag: v4.0.20.3014"), ""},
		{"a branch in the place of the tag", with("tag", "branch: develop"), ""},
		{"a branch with a slash", with("tag", "branch: release/v1"), ""},
		{"no entry", "specs: []\n", `no "specs" entry`},
		{"an empty file", "", "EOF"},
		{"a misspelled key", with("sha256", "sha265: "+someSHA256), "field sha265 not found"},
		{"a name with a path", with("name", "name: ../sample"), "name must be"},
		{"a repository with a host", with("repository", "repository: github.com/Owner/Project"), "repository must be <owner>/<name>"},
		{"a tag with a space", with("tag", "tag: v1 beta"), "tag must name one release tag"},
		{"neither tag nor branch", with("tag", ""), "exactly one of tag and branch"},
		{"a tag and a branch", with("tag", "tag: v1.2.3\n    branch: develop"), "exactly one of tag and branch"},
		{"a branch with a space", with("tag", "branch: my branch"), "branch must name one branch"},
		{"a branch that ends with a slash", with("tag", "branch: release/"), "branch must name one branch"},
		{"a short commit id", with("commit", "commit: 0123456"), "full 40-digit commit id"},
		{"a branch for a commit", with("commit", "commit: main"), "full 40-digit commit id"},
		{"an upper-case commit id", with("commit", "commit: "+strings.ToUpper(someCommit)), "full 40-digit commit id"},
		{"a path leaving the repository", with("path", "path: ../openapi.json"), "one clean file path"},
		{"an absolute path", with("path", "path: /etc/passwd"), "one clean file path"},
		{"no checksum", with("sha256", ""), "sha256 must be 64"},
		{"a checksum one digit short", with("sha256", "sha256: "+someSHA256[1:]), "sha256 must be 64"},
		{"no license", with("license", ""), "license must be the SPDX id"},
		{"a prefix without a slash", with("prefix", "prefix: api/v1"), "prefix must be empty or a path"},
		{"no operation", with("operations", "operations: []"), "operations lists nothing"},
		{"an operation without a method", with("operations", "operations: [/api/v1/status]"), `must be "GET <path>" or "POST <path>"`},
		{"an operation with another method", with("operations", "operations: [DELETE /api/v1/status]"), `must be "GET <path>" or "POST <path>"`},
		{"an operation with a query", with("operations", "operations: ['GET /api/v1/status?x=1']"), `must be "GET <path>" or "POST <path>"`},
		{"an operation outside the prefix", with("operations", "operations: [GET /api/v3/status]"), `does not start with the prefix "/api/v1"`},
		{"an operation listed twice", with("operations", "operations: [GET /api/v1/status, GET /api/v1/status]"), "is listed twice"},
		{"a name used twice", entry(nil) + strings.TrimPrefix(entry(nil), "specs:\n"), "specs[1] (sample): repeats the name"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			file := filepath.Join(t.TempDir(), "specs.yaml")
			writeFile(t, file, test.manifest)
			pins, err := readManifest(file)
			switch {
			case test.want == "" && err != nil:
				t.Fatalf("readManifest: %v", err)
			case test.want == "" && len(pins) != 1:
				t.Fatalf("readManifest returned %d pin(s), want 1", len(pins))
			case test.want != "" && (err == nil || !strings.Contains(err.Error(), test.want)):
				t.Fatalf("readManifest = %v, want an error with %q", err, test.want)
			}
		})
	}
	if _, err := readManifest(filepath.Join(t.TempDir(), "absent.yaml")); err == nil {
		t.Error("an absent manifest was accepted")
	}
}

// TestCommittedManifest binds the manifest of this repository: it must be well formed and name
// the specs the contract tests load.
func TestCommittedManifest(t *testing.T) {
	t.Parallel()
	pins, err := readManifest("specs.yaml")
	if err != nil {
		t.Fatalf("readManifest: %v", err)
	}
	names := make([]string, 0, len(pins))
	for _, p := range pins {
		names = append(names, p.Name)
	}
	want := []string{"sonarr", "radarr", "lidarr", "seerr", "bookshelf", "chaptarr", "readarr-faustvii"}
	if !slices.Equal(names, want) {
		t.Errorf("pinned specs = %q, want %q", names, want)
	}
}

func TestPinSource(t *testing.T) {
	t.Parallel()
	p := pin{Name: "sample", Repository: "Owner/Project", Commit: someCommit, Path: "src/openapi.json"}
	want := "https://example.test/Owner/Project/" + someCommit + "/src/openapi.json"
	for _, base := range []string{"https://example.test", "https://example.test/"} {
		if got := p.source(base); got != want {
			t.Errorf("source(%q) = %q, want %q", base, got, want)
		}
	}
	if got := p.specFile(); got != "sample.spec" {
		t.Errorf("specFile = %q", got)
	}
}

func TestPinRef(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		pin  pin
		want string
	}{
		{pin{Tag: "v1.2.3"}, "v1.2.3"},
		{pin{Branch: "develop"}, "branch develop"},
	} {
		if got := test.pin.ref(); got != test.want {
			t.Errorf("ref of %+v = %q, want %q", test.pin, got, test.want)
		}
	}
}
