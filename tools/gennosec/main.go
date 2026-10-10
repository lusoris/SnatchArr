// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Command gennosec annotates the gosec findings of generated Go code and checks the annotations.
//
// ogen, sqlc and protoc-gen-go emit code that gosec flags although it is safe, and their output is
// never edited by hand. `gennosec apply` inserts one `// #nosec <rule> -- <reason>` line above
// every line a rule of the table matches. It is idempotent and runs right after each generator
// (the ogen go:generate directive, make proto-gen), so a regeneration keeps the annotations.
// `gennosec check` runs gosec with -track-suppressions over every generated directory of the table
// and fails when a finding has no annotation or an annotation suppresses no finding.
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// rule is one kind of gosec finding that a generator emits, and why it is safe.
type rule struct {
	dir    string         // generated directory, relative to the repository root
	id     string         // gosec rule id
	match  *regexp.Regexp // generated line gosec reports the finding on
	reason string
}

var rules = []rule{
	{
		dir:    "api/internal/store/sqlcgen",
		id:     "G101",
		match:  regexp.MustCompile("^const \\w*(?i:apikey|token|secret|passwd|password|pwd|cred|bearer)\\w* = `"),
		reason: "sqlc query constant named after a table; the value is SQL, not a credential",
	},
	{
		dir:    "api/internal/build/oas",
		id:     "G124",
		match:  regexp.MustCompile(`^\s*req\.AddCookie\(&http\.Cookie\{$`),
		reason: "ogen client adds the session cookie to an outgoing request; Secure, HttpOnly and SameSite are response attributes",
	},
	{
		dir:    "api/internal/gen",
		id:     "G103",
		match:  regexp.MustCompile(`unsafe\.Slice\(unsafe\.StringData\(`),
		reason: "protoc-gen-go views the immutable raw descriptor read-only; see the SAFETY proof above",
	},
}

// maxRootDepth bounds the walk up to the repository root (HISS-02).
const maxRootDepth = 32

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "gennosec:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 1 || (args[0] != "apply" && args[0] != "check") {
		return errors.New("usage: gennosec apply|check")
	}
	root, err := repoRoot()
	if err != nil {
		return err
	}
	if args[0] == "apply" {
		return applyAll(root)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	return checkAll(ctx, root)
}

// repoRoot walks up from the working directory to the directory holding go.mod.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("working directory: %w", err)
	}
	for range maxRootDepth {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", errors.New("no go.mod above the working directory")
}

// applyAll annotates every Go file under each rule's directory.
func applyAll(root string) error {
	fsys := os.DirFS(root)
	for _, r := range rules {
		err := fs.WalkDir(fsys, r.dir, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			return applyFile(fsys, root, path, r)
		})
		if err != nil {
			return fmt.Errorf("annotate %s: %w", r.dir, err)
		}
	}
	return nil
}

func applyFile(fsys fs.FS, root, path string, r rule) error {
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	in := strings.Split(string(data), "\n")
	out := annotate(in, r)
	if len(out) == len(in) {
		return nil
	}
	info, err := fs.Stat(fsys, path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.WriteFile(full, []byte(strings.Join(out, "\n")), info.Mode().Perm()); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// annotation is the comment line apply inserts above a matched line.
func annotation(indent string, r rule) string {
	return indent + "// #nosec " + r.id + " -- " + r.reason
}

// annotate returns lines with an annotation above every line r matches that lacks one.
func annotate(lines []string, r rule) []string {
	out := make([]string, 0, len(lines))
	for i, line := range lines {
		if r.match.MatchString(line) && (i == 0 || !strings.Contains(lines[i-1], "#nosec "+r.id+" ")) {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			out = append(out, annotation(indent, r))
		}
		out = append(out, line)
	}
	return out
}
