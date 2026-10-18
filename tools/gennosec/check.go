// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/lusoris/SnatchArr/tools/internal/findings"
)

// issue is the part of a gosec JSON finding the check reads.
type issue struct {
	RuleID       string            `json:"rule_id"`
	File         string            `json:"file"`
	Line         string            `json:"line"`
	Suppressions []json.RawMessage `json:"suppressions"`
}

// site is a finding or an annotation: file (relative to the root), the line the finding is
// reported on, and the rule.
type site struct {
	file string
	line int
	rule string
}

var annotationLine = regexp.MustCompile(`^\s*// #nosec (G\d{3}) -- `)

// gosecFlags come before the directories: a JSON report that also lists suppressed findings.
var gosecFlags = []string{"-quiet", "-fmt=json", "-track-suppressions", "-conf", ".gosec.json"}

// checkAll runs gosec over every generated directory and compares findings with annotations.
// It writes one summary line to out when they agree.
func checkAll(ctx context.Context, root string, out io.Writer) error {
	report, err := runGosec(ctx, root)
	if err != nil {
		return err
	}
	return compare(root, report, out)
}

// compare decodes gosec's JSON report and checks its findings against the annotations under root.
func compare(root string, gosecReport []byte, out io.Writer) error {
	issues, err := findings.Decode[issue](gosecReport)
	if err != nil {
		return fmt.Errorf("gosec output: %w", err)
	}
	found, unannotated, err := classify(root, issues)
	if err != nil {
		return err
	}
	annotated, err := annotations(root)
	if err != nil {
		return err
	}
	problems := report(found, unannotated, annotated)
	if len(problems) > 0 {
		return fmt.Errorf("%d problem(s):\n  %s", len(problems), strings.Join(problems, "\n  "))
	}
	if _, err = fmt.Fprintf(out, "gennosec: %d generated finding(s), each annotated; no stale annotation\n", len(found)); err != nil {
		return fmt.Errorf("write summary: %w", err)
	}
	return nil
}

// runGosec returns gosec's JSON report over every generated directory of the rule table.
func runGosec(ctx context.Context, root string) ([]byte, error) {
	args := append(make([]string, 0, len(gosecFlags)+len(rules)), gosecFlags...)
	for _, r := range rules {
		args = append(args, "./"+r.dir+"/...")
	}
	// #nosec G204 -- fixed binary; the arguments are the constant flags and the rule table's directories
	cmd := exec.CommandContext(ctx, "gosec", args...)
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run() // gosec exits 1 when it finds anything; the JSON decides
	if _, err := findings.Decode[issue](stdout.Bytes()); err != nil {
		return nil, fmt.Errorf("gosec output (stderr %q): %w", stderr.String(), errors.Join(err, runErr))
	}
	return stdout.Bytes(), nil
}

// classify splits gosec's issues into the suppressed sites and the unannotated ones.
func classify(root string, issues []issue) (found, unannotated map[site]bool, err error) {
	found, unannotated = map[site]bool{}, map[site]bool{}
	for _, is := range issues {
		s, sErr := toSite(root, is)
		if sErr != nil {
			return nil, nil, sErr
		}
		if len(is.Suppressions) == 0 {
			unannotated[s] = true
			continue
		}
		found[s] = true
	}
	return found, unannotated, nil
}

func toSite(root string, is issue) (site, error) {
	rel, err := filepath.Rel(root, is.File)
	if err != nil {
		return site{}, fmt.Errorf("relative path of %s: %w", is.File, err)
	}
	return lineSite(filepath.ToSlash(rel), is.Line, is.RuleID)
}

// lineSite parses gosec's line field ("72" or "72-74") into a site at the first line.
func lineSite(file, lines, rule string) (site, error) {
	first, _, _ := strings.Cut(lines, "-")
	n, err := strconv.Atoi(first)
	if err != nil || n < 1 {
		return site{}, fmt.Errorf("%s: gosec line %q is not a line number", file, lines)
	}
	return site{file: file, line: n, rule: rule}, nil
}

// annotations lists every annotation as the site it covers: the line below it.
func annotations(root string) (map[site]bool, error) {
	out := map[site]bool{}
	fsys := os.DirFS(root)
	for _, r := range rules {
		err := fs.WalkDir(fsys, r.dir, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return walkErr
			}
			data, err := fs.ReadFile(fsys, path)
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			collect(out, path, strings.Split(string(data), "\n"))
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("scan %s: %w", r.dir, err)
		}
	}
	return out, nil
}

func collect(out map[site]bool, path string, lines []string) {
	for i, line := range lines {
		if m := annotationLine.FindStringSubmatch(line); m != nil {
			out[site{file: path, line: i + 2, rule: m[1]}] = true
		}
	}
}

// report names every unannotated finding and every annotation that suppresses no finding.
func report(found, unannotated, annotated map[site]bool) []string {
	var problems []string
	for s := range unannotated {
		problems = append(problems, fmt.Sprintf("%s:%d %s: finding without annotation", s.file, s.line, s.rule))
	}
	for s := range annotated {
		if !found[s] {
			problems = append(problems, fmt.Sprintf("%s:%d %s: annotation without finding", s.file, s.line, s.rule))
		}
	}
	sort.Strings(problems)
	return problems
}
