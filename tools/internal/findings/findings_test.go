// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package findings_test

import (
	"strings"
	"testing"

	"github.com/lusoris/SnatchArr/tools/internal/findings"
)

type finding struct {
	RuleID string   `json:"rule_id"`
	Line   string   `json:"line"`
	Notes  []string `json:"notes"`
}

func TestDecode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		report  string
		want    int
		wantErr string
	}{
		{"one finding", `{"Issues":[{"rule_id":"G101","line":"3"}],"Stats":{}}`, 1, ""},
		{"two findings", `{"Issues":[{"rule_id":"G101"},{"rule_id":"G103"}]}`, 2, ""},
		{"empty list", `{"Issues":[]}`, 0, ""},
		{"null list", `{"Issues":null}`, 0, ""},
		{"no Issues field", `{"Stats":{}}`, 0, `no "Issues" field`},
		{"lower-case field is another field", `{"issues":[]}`, 0, `no "Issues" field`},
		{"Issues is not a list", `{"Issues":{"rule_id":"G101"}}`, 0, `report field "Issues"`},
		{"not JSON", `gosec: no packages`, 0, "report: "},
		{"empty output", ``, 0, "report: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := findings.Decode[finding]([]byte(tt.report))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Decode() error = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || len(got) != tt.want {
				t.Fatalf("Decode() = %d finding(s), %v; want %d", len(got), err, tt.want)
			}
		})
	}
}

func TestDecodeKeepsTheFields(t *testing.T) {
	t.Parallel()
	got, err := findings.Decode[finding]([]byte(`{"Issues":[{"rule_id":"G124","line":"7-9","notes":["a","b"]}]}`))
	if err != nil || len(got) != 1 {
		t.Fatalf("Decode() = %v, %v; want one finding", got, err)
	}
	if f := got[0]; f.RuleID != "G124" || f.Line != "7-9" || len(f.Notes) != 2 {
		t.Errorf("finding = %+v, want rule G124 at lines 7-9 with two notes", f)
	}
}
