// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

var testRule = rule{dir: "x", id: "G101", match: regexp.MustCompile("^const \\w*Token\\w* = `"), reason: "test reason"}

func TestAnnotate(t *testing.T) {
	note := annotation("", testRule)
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"match gets an annotation", []string{"package p", "const getToken = `SELECT 1", "`"}, []string{"package p", note, "const getToken = `SELECT 1", "`"}},
		{"no match is unchanged", []string{"package p", "const getUser = `SELECT 1", "`"}, []string{"package p", "const getUser = `SELECT 1", "`"}},
		{"already annotated is unchanged", []string{note, "const getToken = `SELECT 1"}, []string{note, "const getToken = `SELECT 1"}},
		{"match on the first line", []string{"const getToken = `x`"}, []string{note, "const getToken = `x`"}},
		{"empty file", []string{""}, []string{""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := annotate(tt.in, testRule); !slices.Equal(got, tt.want) {
				t.Errorf("annotate() =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
		})
	}
}

func TestAnnotateKeepsIndentation(t *testing.T) {
	r := rule{id: "G103", match: regexp.MustCompile(`unsafe\.Slice`), reason: "r"}
	got := annotate([]string{"\t\tx := unsafe.Slice(p, n)"}, r)
	if want := "\t\t// #nosec G103 -- r"; got[0] != want {
		t.Errorf("annotation = %q, want %q", got[0], want)
	}
}

func TestLineSite(t *testing.T) {
	tests := []struct {
		lines   string
		want    int
		wantErr bool
	}{
		{"72", 72, false},
		{"72-74", 72, false},
		{"1", 1, false},
		{"0", 0, true},
		{"", 0, true},
		{"x-1", 0, true},
	}
	for _, tt := range tests {
		got, err := lineSite("f.go", tt.lines, "G101")
		if (err != nil) != tt.wantErr {
			t.Errorf("lineSite(%q) error = %v, wantErr %v", tt.lines, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && got.line != tt.want {
			t.Errorf("lineSite(%q) line = %d, want %d", tt.lines, got.line, tt.want)
		}
	}
}

func TestCollectCoversTheNextLine(t *testing.T) {
	out := map[site]bool{}
	collect(out, "f.go", []string{"package p", "\t// #nosec G124 -- reason", "\treq.AddCookie(c)"})
	if !out[site{file: "f.go", line: 3, rule: "G124"}] || len(out) != 1 {
		t.Errorf("collect() = %v, want only f.go:3 G124", out)
	}
}

func TestReport(t *testing.T) {
	a := site{file: "f.go", line: 3, rule: "G101"}
	b := site{file: "f.go", line: 9, rule: "G103"}
	tests := []struct {
		name                          string
		found, unannotated, annotated map[site]bool
		want                          int
	}{
		{"every finding annotated", map[site]bool{a: true}, map[site]bool{}, map[site]bool{a: true}, 0},
		{"finding without annotation", map[site]bool{}, map[site]bool{a: true}, map[site]bool{}, 1},
		{"annotation without finding", map[site]bool{a: true}, map[site]bool{}, map[site]bool{a: true, b: true}, 1},
		{"nothing at all", map[site]bool{}, map[site]bool{}, map[site]bool{}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := report(tt.found, tt.unannotated, tt.annotated); len(got) != tt.want {
				t.Errorf("report() = %v, want %d problem(s)", got, tt.want)
			}
		})
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	for _, args := range [][]string{nil, {"bogus"}, {"apply", "extra"}} {
		if err := run(args); err == nil {
			t.Errorf("run(%q) = nil, want a usage error", args)
		}
	}
}
