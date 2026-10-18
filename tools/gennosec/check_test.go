// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// plantedSqlc is a sqlc query constant named after a table with a credential in its name: gosec
// reports it as G101 although the value is SQL.
const plantedSqlc = "package sqlcgen\n\nconst getAPIKeyByInstance = `SELECT api_key FROM instance`\n"

// plantedRoot writes a repository with every generated directory of the rule table and the
// planted sqlc file, annotated by apply when annotated is true. It returns the root and the
// planted file's path.
func plantedRoot(t *testing.T, annotated bool) (root, planted string) {
	t.Helper()
	root = t.TempDir()
	for _, r := range rules {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(r.dir)), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	planted = filepath.Join(root, "api", "internal", "store", "sqlcgen", "planted.go")
	if err := os.WriteFile(planted, []byte(plantedSqlc), 0o600); err != nil {
		t.Fatal(err)
	}
	if annotated {
		if err := applyAll(root); err != nil {
			t.Fatalf("applyAll() = %v", err)
		}
	}
	return root, planted
}

// gosecReport is a gosec JSON report with one G101 finding on file at lines.
func gosecReport(file, lines string, suppressed bool) []byte {
	suppressions := "null"
	if suppressed {
		suppressions = `[{"kind":"inSource","justification":"sqlc"}]`
	}
	return fmt.Appendf(nil, `{"Issues":[{"rule_id":"G101","file":%q,"line":%q,"suppressions":%s}],"Stats":{}}`,
		file, lines, suppressions)
}

func TestCompare_PlantedG101InGeneratedSqlc(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		annotated bool
		report    func(planted string) []byte
		want      string // substring of the error, or of the summary when empty is wanted
		wantErr   bool
	}{
		{
			"annotated by apply, gosec suppressed it", true,
			func(p string) []byte { return gosecReport(p, "4", true) }, "1 generated finding(s), each annotated", false,
		},
		{
			"finding reported over a line range", true,
			func(p string) []byte { return gosecReport(p, "4-5", true) }, "1 generated finding(s)", false,
		},
		{
			"apply not run: finding without annotation", false,
			func(p string) []byte { return gosecReport(p, "3", false) }, "planted.go:3 G101: finding without annotation", true,
		},
		{
			"finding moved off the annotated line", true,
			func(p string) []byte { return gosecReport(p, "5", true) }, "planted.go:4 G101: annotation without finding", true,
		},
		{
			"annotation left, gosec reports nothing", true,
			func(string) []byte { return []byte(`{"Issues":[]}`) }, "planted.go:4 G101: annotation without finding", true,
		},
		{
			"report without an Issues field", true,
			func(string) []byte { return []byte(`{"Stats":{}}`) }, `no "Issues" field`, true,
		},
		{
			"line field not a number", true,
			func(p string) []byte { return gosecReport(p, "x", true) }, `gosec line "x" is not a line number`, true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root, planted := plantedRoot(t, tt.annotated)
			var out bytes.Buffer
			err := compare(root, tt.report(planted), &out)
			if (err != nil) != tt.wantErr {
				t.Fatalf("compare() error = %v, wantErr %v", err, tt.wantErr)
			}
			got := out.String()
			if err != nil {
				got = err.Error()
			}
			if !strings.Contains(got, tt.want) {
				t.Errorf("compare() = %q, want it to contain %q", got, tt.want)
			}
		})
	}
}

func TestApplyAll_AnnotatesThePlantedConstantOnce(t *testing.T) {
	t.Parallel()
	root, planted := plantedRoot(t, true)
	first, err := os.ReadFile(planted) // #nosec G304 -- path inside the test's own temporary directory
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(first), "\n")
	if want := annotation("", rules[0]); lines[2] != want {
		t.Fatalf("line 3 = %q, want the annotation %q", lines[2], want)
	}
	if err = applyAll(root); err != nil {
		t.Fatalf("second applyAll() = %v", err)
	}
	second, err := os.ReadFile(planted) // #nosec G304 -- path inside the test's own temporary directory
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Errorf("second apply changed the file:\n%s", second)
	}
}

func TestApplyAll_MissingGeneratedDirectory(t *testing.T) {
	t.Parallel()
	if err := applyAll(t.TempDir()); err == nil {
		t.Error("applyAll() on a root without the generated directories = nil, want an error")
	}
}
