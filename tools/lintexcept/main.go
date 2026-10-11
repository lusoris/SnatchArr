// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Command lintexcept decides which golangci-lint findings a dated exception excuses.
//
// `.golangci.yml` excludes no single file. A file that cannot be fixed here (an asset another
// project owns and locks) keeps its findings, and `exceptions.yaml` at the repository root names
// each of them: one linter, one file, a reason and an expiry. `lintexcept check` reads that list
// and a golangci-lint JSON report and fails when
//
//   - an entry is malformed, repeated, names no file, or expires more than 90 days ahead,
//   - a finding has no entry that still holds (an expired entry excuses nothing), or
//   - an entry excuses no finding.
//
// The list has the shape of the `exceptions` list in `.standards.yaml`, which accepts only the
// rules praetor reads itself (cordanaLLM/praetor#1206).
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jonboulle/clockwork"
	"gopkg.in/yaml.v3"

	"github.com/lusoris/SnatchArr/tools/internal/findings"
)

const (
	// rulePrefix starts the rule id of every entry this check reads.
	rulePrefix = "golangci-lint/"
	// maxDays is how far ahead of today an entry may expire, as in praetor's own list.
	maxDays = 90
	// maxReasonBytes bounds one entry's reason.
	maxReasonBytes = 1024
	dateLayout     = "2006-01-02"
	// globCharacters make a path a pattern; an entry names one file.
	globCharacters = "*?[]\\"
)

var linterName = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// entry is one exception: Rule is "golangci-lint/<linter>", Path one file relative to the
// repository root, Expires the last day (UTC, YYYY-MM-DD) the entry holds.
type entry struct {
	Rule    string `yaml:"rule"`
	Path    string `yaml:"path"`
	Reason  string `yaml:"reason"`
	Expires string `yaml:"expires"`
}

// issue is the part of a golangci-lint JSON finding the check reads. The report spells its
// fields as Go spells these, so they carry no tag.
type issue struct {
	FromLinter string
	Text       string
	Pos        struct {
		Filename string
		Line     int
	}
}

// target is what an entry excuses and what a finding is: one linter in one file.
type target struct {
	linter string
	file   string
}

func main() {
	if err := run(os.Args[1:], os.Stdout, clockwork.NewRealClock().Now()); err != nil {
		fmt.Fprintln(os.Stderr, "lintexcept:", err)
		os.Exit(1)
	}
}

const usage = "usage: lintexcept check -list <exceptions.yaml> -report <golangci-lint JSON report>"

func run(args []string, out io.Writer, now time.Time) error {
	if len(args) == 0 || args[0] != "check" {
		return errors.New(usage)
	}
	flags := flag.NewFlagSet("lintexcept check", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	listPath := flags.String("list", "", "exception list")
	reportPath := flags.String("report", "", "golangci-lint JSON report")
	if err := flags.Parse(args[1:]); err != nil {
		return fmt.Errorf("%w; %s", err, usage)
	}
	if *listPath == "" || *reportPath == "" || flags.NArg() != 0 {
		return errors.New(usage)
	}
	return check(*listPath, *reportPath, out, day(now))
}

// check compares the list with the report and writes one summary line to out when they agree.
// The paths of the list are relative to the directory that holds it, the repository root.
func check(listPath, reportPath string, out io.Writer, today time.Time) error {
	entries, err := readList(listPath)
	if err != nil {
		return err
	}
	// #nosec G304 -- the report path is the caller's argument; the file is only decoded
	report, err := os.ReadFile(reportPath)
	if err != nil {
		return fmt.Errorf("read report: %w", err)
	}
	issues, err := findings.Decode[issue](report)
	if err != nil {
		return fmt.Errorf("%s: %w", reportPath, err)
	}
	root := filepath.Dir(listPath)
	exists := func(file string) bool {
		info, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(file)))
		return statErr == nil && info.Mode().IsRegular()
	}
	problems := append(validate(entries, today, exists), compare(entries, issues, today)...)
	if len(problems) > 0 {
		return fmt.Errorf("%d problem(s):\n  %s", len(problems), strings.Join(problems, "\n  "))
	}
	if _, err = fmt.Fprintf(out, "lintexcept: %d finding(s), each excused by one of %d dated exception(s); no stale exception\n",
		len(issues), len(entries)); err != nil {
		return fmt.Errorf("write summary: %w", err)
	}
	return nil
}

// readList decodes the exception list. A key the check does not read is refused, so a
// misspelled field cannot pass as an absent one.
func readList(file string) ([]entry, error) {
	// #nosec G304 -- the list path is the caller's argument; the file is only decoded
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("read list: %w", err)
	}
	var list struct {
		Exceptions *[]entry `yaml:"exceptions"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&list); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	if list.Exceptions == nil {
		return nil, fmt.Errorf("%s: no \"exceptions\" list", file)
	}
	return *list.Exceptions, nil
}

// day returns the UTC calendar day of t, the form an expires date parses to.
func day(t time.Time) time.Time {
	year, month, dayOfMonth := t.UTC().Date()
	return time.Date(year, month, dayOfMonth, 0, 0, 0, 0, time.UTC)
}

// expired reports whether e stopped holding before today; it still holds on its expires day.
// An expires value that is no date counts as expired.
func (e entry) expired(today time.Time) bool {
	expires, err := time.Parse(dateLayout, e.Expires)
	return err != nil || expires.Before(today)
}

func (e entry) target() target {
	return target{linter: strings.TrimPrefix(e.Rule, rulePrefix), file: e.Path}
}

// validate returns one line per entry that is no well-formed exception for one existing file.
// An expired entry is well formed; compare reports the findings it no longer excuses.
func validate(entries []entry, today time.Time, exists func(string) bool) []string {
	var problems []string
	seen := map[target]bool{}
	for index, e := range entries {
		problem := e.problem(today, exists)
		if problem == "" && seen[e.target()] {
			problem = "repeats an earlier entry for the same rule and path"
		}
		seen[e.target()] = true
		if problem != "" {
			problems = append(problems, fmt.Sprintf("exceptions[%d] (%s, %s): %s", index, e.Rule, e.Path, problem))
		}
	}
	return problems
}

func (e entry) problem(today time.Time, exists func(string) bool) string {
	linter, ok := strings.CutPrefix(e.Rule, rulePrefix)
	switch {
	case !ok || !linterName.MatchString(linter):
		return "rule must be " + rulePrefix + "<linter>; no other rule is read from this list"
	case !cleanRelative(e.Path):
		return "path must be one clean file path relative to the repository root, without glob characters"
	case !exists(e.Path):
		return "path names no file"
	case !oneLine(e.Reason):
		return fmt.Sprintf("reason must be one non-empty line of at most %d bytes", maxReasonBytes)
	}
	expires, err := time.Parse(dateLayout, e.Expires)
	switch {
	case err != nil:
		return fmt.Sprintf("expires %q is no YYYY-MM-DD date", e.Expires)
	case expires.After(today.AddDate(0, 0, maxDays)):
		return fmt.Sprintf("expires %s lies more than %d days after %s", e.Expires, maxDays, today.Format(dateLayout))
	}
	return ""
}

// cleanRelative reports whether file names one path below the repository root: no pattern, no
// absolute path, no "..", no trailing slash.
func cleanRelative(file string) bool {
	if file == "" || path.IsAbs(file) || strings.ContainsAny(file, globCharacters) {
		return false
	}
	return path.Clean(file) == file && file != ".." && !strings.HasPrefix(file, "../")
}

func oneLine(reason string) bool {
	return strings.TrimSpace(reason) != "" && !strings.ContainsAny(reason, "\r\n") && len(reason) <= maxReasonBytes
}

// compare returns one line per finding no live entry excuses and per entry that excuses none.
func compare(entries []entry, issues []issue, today time.Time) []string {
	live, lapsed := map[target]bool{}, map[target]string{}
	for _, e := range entries {
		if e.expired(today) {
			lapsed[e.target()] = e.Expires
			continue
		}
		live[e.target()] = true
	}
	var problems []string
	used := map[target]bool{}
	for _, is := range issues {
		t := target{linter: is.FromLinter, file: path.Clean(strings.ReplaceAll(is.Pos.Filename, "\\", "/"))}
		used[t] = true
		if live[t] {
			continue
		}
		cause := "no exception"
		if expires, ok := lapsed[t]; ok {
			cause = "exception expired on " + expires
		}
		problems = append(problems, fmt.Sprintf("%s:%d: %s: %s (%s)", t.file, is.Pos.Line, t.linter, is.Text, cause))
	}
	for _, e := range entries {
		if !used[e.target()] {
			problems = append(problems, fmt.Sprintf("stale exception (%s, %s): the report has no such finding", e.Rule, e.Path))
		}
	}
	sort.Strings(problems)
	return problems
}
