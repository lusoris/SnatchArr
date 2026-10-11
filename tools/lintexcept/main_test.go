// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const gateFile = "tools/apicompat/gate/main.go"

var today = time.Date(2026, time.October, 11, 0, 0, 0, 0, time.UTC)

func exists(file string) bool { return file == gateFile || file == "..hidden.go" }

func wrapcheck(expires string) entry {
	return entry{Rule: "golangci-lint/wrapcheck", Path: gateFile, Reason: "locked asset", Expires: expires}
}

func finding(linter, file string, line int) issue {
	is := issue{FromLinter: linter, Text: "text"}
	is.Pos.Filename, is.Pos.Line = file, line
	return is
}

func TestValidate(t *testing.T) {
	t.Parallel()
	with := func(change func(*entry)) []entry {
		e := wrapcheck("2027-01-06")
		change(&e)
		return []entry{e}
	}
	tests := []struct {
		name    string
		entries []entry
		want    string // part of the one problem; empty for none
	}{
		{"well formed", with(func(*entry) {}), ""},
		{"no entry", nil, ""},
		{"expires on the last allowed day", with(func(e *entry) { e.Expires = "2027-01-09" }), ""},
		{"expires one day too far", with(func(e *entry) { e.Expires = "2027-01-10" }), "more than 90 days after 2026-10-11"},
		{"expired entry is well formed", with(func(e *entry) { e.Expires = "2026-10-01" }), ""},
		{"no date", with(func(e *entry) { e.Expires = "" }), "is no YYYY-MM-DD date"},
		{"date in another form", with(func(e *entry) { e.Expires = "06.01.2027" }), "is no YYYY-MM-DD date"},
		{"rule of another tool", with(func(e *entry) { e.Rule = "HISS-10" }), "rule must be golangci-lint/<linter>"},
		{"rule without a linter", with(func(e *entry) { e.Rule = "golangci-lint/" }), "rule must be golangci-lint/<linter>"},
		{"rule naming every linter", with(func(e *entry) { e.Rule = "golangci-lint/*" }), "rule must be golangci-lint/<linter>"},
		{"path widened to the directory", with(func(e *entry) { e.Path = "tools/apicompat/gate" }), "path names no file"},
		{"path widened with a trailing slash", with(func(e *entry) { e.Path = "tools/apicompat/gate/" }), "one clean file path"},
		{"path widened to a pattern", with(func(e *entry) { e.Path = "tools/apicompat/gate/*.go" }), "one clean file path"},
		{"path leaving the repository", with(func(e *entry) { e.Path = "../" + gateFile }), "one clean file path"},
		{"absolute path", with(func(e *entry) { e.Path = "/" + gateFile }), "one clean file path"},
		{"empty path", with(func(e *entry) { e.Path = "" }), "one clean file path"},
		{"file name starting with two dots", with(func(e *entry) { e.Path = "..hidden.go" }), ""},
		{"missing file", with(func(e *entry) { e.Path = "tools/apicompat/gate/gone.go" }), "path names no file"},
		{"empty reason", with(func(e *entry) { e.Reason = " " }), "reason must be one non-empty line"},
		{"reason of two lines", with(func(e *entry) { e.Reason = "a\nb" }), "reason must be one non-empty line"},
		{"reason at the size bound", with(func(e *entry) { e.Reason = strings.Repeat("r", maxReasonBytes) }), ""},
		{"reason over the size bound", with(func(e *entry) { e.Reason = strings.Repeat("r", maxReasonBytes+1) }), "reason must be one non-empty line"},
		{"repeated entry", []entry{wrapcheck("2027-01-06"), wrapcheck("2026-12-01")}, "exceptions[1] (golangci-lint/wrapcheck, " + gateFile + "): repeats"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := validate(tt.entries, today, exists)
			if tt.want == "" {
				if len(got) != 0 {
					t.Fatalf("validate() = %q, want no problem", got)
				}
				return
			}
			if len(got) != 1 || !strings.Contains(got[0], tt.want) {
				t.Fatalf("validate() = %q, want one problem containing %q", got, tt.want)
			}
		})
	}
}

func TestCompare(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		entries []entry
		issues  []issue
		want    []string // one part per problem, in order
	}{
		{"finding with a live entry", []entry{wrapcheck("2027-01-06")}, []issue{finding("wrapcheck", gateFile, 470)}, nil},
		{
			"two findings share one entry",
			[]entry{wrapcheck("2027-01-06")},
			[]issue{finding("wrapcheck", gateFile, 470), finding("wrapcheck", gateFile, 818)},
			nil,
		},
		{"nothing to excuse, nothing excused", nil, nil, nil},
		{"entry holds on its expires day", []entry{wrapcheck("2026-10-11")}, []issue{finding("wrapcheck", gateFile, 470)}, nil},
		{
			"entry lapsed the day before",
			[]entry{wrapcheck("2026-10-10")},
			[]issue{finding("wrapcheck", gateFile, 470)},
			[]string{gateFile + ":470: wrapcheck: text (exception expired on 2026-10-10)"},
		},
		{
			"entry whose date is no date excuses nothing",
			[]entry{wrapcheck("soon")},
			[]issue{finding("wrapcheck", gateFile, 470)},
			[]string{"(exception expired on soon)"},
		},
		{
			"finding without an entry", nil,
			[]issue{finding("wrapcheck", gateFile, 470)},
			[]string{gateFile + ":470: wrapcheck: text (no exception)"},
		},
		{
			"entry for another linter",
			[]entry{wrapcheck("2027-01-06")},
			[]issue{finding("govet", gateFile, 851)},
			[]string{"stale exception (golangci-lint/wrapcheck, " + gateFile + ")", ":851: govet: text (no exception)"},
		},
		{
			"entry for another file",
			[]entry{wrapcheck("2027-01-06")},
			[]issue{finding("wrapcheck", "tools/gennosec/main.go", 7)},
			[]string{"stale exception", "tools/gennosec/main.go:7: wrapcheck: text (no exception)"},
		},
		{"entry without a finding", []entry{wrapcheck("2027-01-06")}, nil, []string{"stale exception (golangci-lint/wrapcheck, " + gateFile + ")"}},
		{
			"report path with backslashes",
			[]entry{wrapcheck("2027-01-06")},
			[]issue{finding("wrapcheck", strings.ReplaceAll(gateFile, "/", "\\"), 470)},
			nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := compare(tt.entries, tt.issues, today)
			if len(got) != len(tt.want) {
				t.Fatalf("compare() = %q, want %d problem(s)", got, len(tt.want))
			}
			for index, part := range tt.want {
				if !strings.Contains(got[index], part) {
					t.Errorf("compare()[%d] = %q, want it to contain %q", index, got[index], part)
				}
			}
		})
	}
}

func TestDayIsTheUTCDay(t *testing.T) {
	t.Parallel()
	berlin := time.FixedZone("CEST", 2*60*60)
	// 00:30 on 12 October in Berlin is still 11 October in UTC.
	if got := day(time.Date(2026, time.October, 12, 0, 30, 0, 0, berlin)); !got.Equal(today) {
		t.Errorf("day() = %s, want %s", got, today)
	}
}

const listHead = "exceptions:\n"

func listEntry(expires string) string {
	return "  - rule: golangci-lint/wrapcheck\n    path: main.go\n    reason: locked asset\n    expires: \"" + expires + "\"\n"
}

const wrapcheckReport = `{"Issues":[{"FromLinter":"wrapcheck","Text":"unwrapped","Pos":{"Filename":"main.go","Line":4}}],"Report":{}}`

func TestRun(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		list    string
		report  string
		wantOut string
		wantErr string
	}{
		{"finding excused", listHead + listEntry("2027-01-06"), wrapcheckReport, "1 finding(s), each excused by one of 1 dated exception(s)", ""},
		{"empty list, clean report", "exceptions: []\n", `{"Issues":[]}`, "0 finding(s)", ""},
		{"finding without an entry", "exceptions: []\n", wrapcheckReport, "", "main.go:4: wrapcheck: unwrapped (no exception)"},
		{
			"entry without an expiry", listHead + "  - rule: golangci-lint/wrapcheck\n    path: main.go\n    reason: locked asset\n", wrapcheckReport,
			"", `expires "" is no YYYY-MM-DD date`,
		},
		{"expired entry", listHead + listEntry("2026-10-10"), wrapcheckReport, "", "exception expired on 2026-10-10"},
		{"entry for a directory", listHead + strings.Replace(listEntry("2027-01-06"), "main.go", ".", 1), wrapcheckReport, "", "path names no file"},
		{"misspelled key", listHead + strings.Replace(listEntry("2027-01-06"), "expires", "expiry", 1), wrapcheckReport, "", "field expiry not found"},
		{"list without the exceptions key", "{}\n", wrapcheckReport, "", `no "exceptions" list`},
		{"empty list file", "", wrapcheckReport, "", "EOF"},
		{"report without findings field", "exceptions: []\n", `{"Report":{}}`, "", `no "Issues" field`},
		{"report that is no JSON", "exceptions: []\n", "level=error msg=typecheck", "", "report: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			write(t, filepath.Join(dir, "main.go"), "package main\n")
			write(t, filepath.Join(dir, "exceptions.yaml"), tt.list)
			write(t, filepath.Join(dir, "report.json"), tt.report)
			var out bytes.Buffer
			err := run([]string{"check", "-list", filepath.Join(dir, "exceptions.yaml"), "-report", filepath.Join(dir, "report.json")}, &out, today)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("run() error = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || !strings.Contains(out.String(), tt.wantOut) {
				t.Fatalf("run() = %q, %v; want output containing %q", out.String(), err, tt.wantOut)
			}
		})
	}
}

func TestRunRejectsBadArguments(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		nil,
		{"bogus"},
		{"check"},
		{"check", "-list", "a"},
		{"check", "-report", "b"},
		{"check", "-list", "a", "-report", "b", "extra"},
		{"check", "-unknown"},
		{"check", "-list", "missing.yaml", "-report", "missing.json"},
	} {
		if err := run(args, &bytes.Buffer{}, today); err == nil {
			t.Errorf("run(%q) = nil, want an error", args)
		}
	}
}

func write(t *testing.T, file, content string) {
	t.Helper()
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", file, err)
	}
}
