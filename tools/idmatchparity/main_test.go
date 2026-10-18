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

// snatchLine is one api JSON line as golusoris log writes it.
func snatchLine(at, msg, rule, id, title string) string {
	return fmt.Sprintf(`{"time":%q,"level":"INFO","msg":%q,"instance":"sonarr","rule":%q,"download_id":%q,"title":%q}`, at, msg, rule, id, title)
}

func would(at, id, title string) string { return snatchLine(at, msgWouldImport, "", id, title) }

func struck(at, rule, id string) string { return snatchLine(at, msgStruck, rule, id, "T") }

func imported(at, id, title string) string {
	return fmt.Sprintf("%s [sonarr] IMPORT '%s' downloadId=%s files=1", at, title, id)
}

func skipped(at, title string) string {
	return fmt.Sprintf("%s [sonarr] SKIP '%s': no clean mapped candidate (of 2 file(s)) — leaving for operator", at, title)
}

func runCase(t *testing.T, snatch, unblocker []string) (int, string) {
	t.Helper()
	dir := t.TempDir()
	sp, up := filepath.Join(dir, "api.log"), filepath.Join(dir, "unblocker.log")
	if err := os.WriteFile(sp, []byte(strings.Join(snatch, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(up, []byte(strings.Join(unblocker, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code, err := run([]string{"-day", "2026-10-19", "-snatcharr", sp, "-unblocker", up}, &out, &errOut)
	if err != nil {
		errOut.WriteString(err.Error())
	}
	return code, out.String() + errOut.String()
}

func TestParity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		snatch    []string
		unblocker []string
		wantCode  int
		wantOut   string
	}{
		{"positive/matched", []string{
			"2026-10-19T12:01:00Z startup line without an object",
			struck("2026-10-19T12:10:00Z", "id_matched", "abc"), would("2026-10-19T12:10:00Z", "ABC", "Show"),
		}, []string{
			"2026-10-19T12:38:00Z start (apply=True, max_per_instance=25)",
			imported("2026-10-19T12:38:02.5Z", "ABC", "Show"),
		}, 0, "would=1 unblocker=1 matched=1 skipped=0 unseen=0 mismatches=0 shadow_live=true verdict=green"},
		{"positive/kubectl_prefix_and_dry_run_repeats", []string{
			"2026-10-19T12:10:00.1Z " + would("2026-10-19T12:10:00Z", "abc", "Show"),
		}, []string{
			strings.Replace(imported("2026-10-19T12:38:00Z", "ABC", "Show"), "IMPORT", "WOULD import", 1),
			strings.Replace(imported("2026-10-19T13:38:00Z", "ABC", "Show"), "IMPORT", "WOULD import", 1),
		}, 0, "would=1 unblocker=1 matched=1"},
		{"positive/skipped_by_candidate_check", []string{
			would("2026-10-19T12:10:00Z", "abc", "Show S01E01"),
		}, []string{skipped("2026-10-19T12:38:00Z", "Show S01E01")}, 0, "would=1 unblocker=0 matched=0 skipped=1"},
		{"boundary/across_midnight", []string{
			would("2026-10-19T23:55:00Z", "abc", "Show"),
		}, []string{imported("2026-10-20T00:38:00Z", "abc", "Show")}, 0, "would=1 unblocker=0 matched=1"},
		{"boundary/slack_edge_matches", []string{
			would("2026-10-19T10:00:00Z", "abc", "Show"),
		}, []string{imported("2026-10-19T12:00:00Z", "abc", "Show")}, 0, "matched=1 skipped=0 unseen=0 mismatches=0"},
		{"boundary/one_unseen_one_matched_stays_green", []string{
			would("2026-10-19T12:10:00Z", "abc", "Show"),
		}, []string{
			imported("2026-10-19T12:38:00Z", "abc", "Show"), imported("2026-10-19T13:38:00Z", "def", "Other"),
		}, 0, "unblocker=2 matched=1 skipped=0 unseen=1 mismatches=0 shadow_live=true verdict=green"},
		{"negative/past_slack_is_both_sides", []string{
			would("2026-10-19T10:00:00Z", "abc", "Show"),
		}, []string{imported("2026-10-19T12:00:01Z", "abc", "Show")}, 1, "mismatch side=snatcharr"},
		{"negative/only_snatcharr", []string{
			would("2026-10-19T12:10:00Z", "abc", "Show"),
		}, nil, 1, `mismatch side=snatcharr download_id=abc at=2026-10-19T12:10:00Z reason="the unblocker did not import it"`},
		{"negative/struck_under_other_rule", []string{
			struck("2026-10-19T12:10:00Z", "import_blocked", "abc"),
		}, []string{imported("2026-10-19T12:38:00Z", "abc", "Show")}, 1, `reason="SnatchArr struck it under rule import_blocked"`},
		{"negative/struck_id_matched_without_would", []string{
			struck("2026-10-19T12:10:00Z", "id_matched", "abc"),
		}, []string{imported("2026-10-19T12:38:00Z", "abc", "Show")}, 1, "without a would-import"},
		{"negative/silent_shadow", []string{
			would("2026-10-18T12:00:00Z", "old", "Old"),
		}, []string{imported("2026-10-19T12:38:00Z", "abc", "Show")}, 1, "unseen=1 mismatches=0 shadow_live=false verdict=red"},
		{"negative/more_unseen_than_matched", []string{
			struck("2026-10-19T09:00:00Z", "stalled", "zzz"),
		}, []string{imported("2026-10-19T12:38:00Z", "abc", "Show")}, 1, "unseen=1 mismatches=0 shadow_live=true verdict=red"},
		{"negative/import_without_timestamp", nil, []string{"[sonarr] IMPORT 'Show' downloadId=abc files=1"}, 2, "no timestamp"},
		{"negative/broken_json", []string{`{"time":`}, nil, 2, "line 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			code, out := runCase(t, tt.snatch, tt.unblocker)
			if code != tt.wantCode || !strings.Contains(out, tt.wantOut) {
				t.Fatalf("exit %d, output:\n%s\nwant exit %d and %q", code, out, tt.wantCode, tt.wantOut)
			}
		})
	}
}

func TestRunFlags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"negative/bad_day", []string{"-day", "19.10.2026", "-snatcharr", "a", "-unblocker", "b"}, "-day"},
		{"negative/missing_log_flag", []string{"-day", "2026-10-19"}, "required"},
		{"negative/missing_file", []string{"-day", "2026-10-19", "-snatcharr", "/nonexistent/a", "-unblocker", "b"}, "open log"},
		{"boundary/unknown_flag", []string{"-bogus"}, "flag provided but not defined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var out, errOut bytes.Buffer
			code, err := run(tt.args, &out, &errOut)
			if err != nil {
				errOut.WriteString(err.Error())
			}
			if code != exitInput || !strings.Contains(errOut.String(), tt.want) {
				t.Fatalf("exit %d, stderr %q, want %d and %q", code, errOut.String(), exitInput, tt.want)
			}
		})
	}
}
