// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"bytes"
	"errors"
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

func logFiles(t *testing.T, snatch, unblocker []string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	sp, up := filepath.Join(dir, "api.log"), filepath.Join(dir, "unblocker.log")
	if err := os.WriteFile(sp, []byte(strings.Join(snatch, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(up, []byte(strings.Join(unblocker, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	return sp, up
}

func runCase(t *testing.T, snatch, unblocker []string) (int, string) {
	t.Helper()
	snatch = append(append([]string{}, snatch...), `{"time":"2026-10-19T00:00:00Z","msg":"startup"}`)
	unblocker = append(append([]string{}, unblocker...), "2026-10-19T00:00:00Z startup")
	sp, up := logFiles(t, snatch, unblocker)
	var out, errOut bytes.Buffer
	code, err := run([]string{"-day", "2026-10-19", "-snatcharr", sp, "-unblocker", up}, &out, &errOut)
	if err != nil {
		errOut.WriteString(err.Error())
	}
	return code, out.String() + errOut.String()
}

func TestRunDayCoverage(t *testing.T) {
	t.Parallel()
	inside := `{"time":"2026-10-19T00:00:00Z","msg":"startup"}`
	stamp := "2026-10-19T00:00:00Z startup"
	tests := []struct {
		name      string
		snatch    string
		unblocker string
		wantCode  int
		wantFlag  string
	}{
		{"negative/empty_snatcharr", "", stamp, exitInput, "-snatcharr"},
		{"negative/empty_unblocker", inside, "", exitInput, "-unblocker"},
		{"negative/both_empty_snatcharr_first", "", "", exitInput, "-snatcharr"},
		{"negative/snatcharr_other_day", `{"time":"2026-10-18T12:00:00Z"}`, stamp, exitInput, "-snatcharr"},
		{"negative/unblocker_other_day", inside, "2026-10-18T12:00:00Z startup", exitInput, "-unblocker"},
		{"negative/untimestamped_snatcharr", `{"msg":"startup"}`, stamp, exitInput, "-snatcharr"},
		{"negative/untimestamped_unblocker", inside, "startup", exitInput, "-unblocker"},
		{"negative/snatcharr_prefix_only", "2026-10-19T00:00:00Z " + `{"msg":"startup"}`, stamp, exitInput, "-snatcharr"},
		{"positive/ignored_unblocker_bad_stamp", inside, "not-a-timestamp startup\n" + stamp, 0, ""},
		{"boundary/day_start", inside, stamp, 0, ""},
		{"boundary/snatcharr_next_day", `{"time":"2026-10-20T00:00:00Z"}`, stamp, exitInput, "-snatcharr"},
		{"boundary/unblocker_next_day", inside, "2026-10-20T00:00:00Z startup", exitInput, "-unblocker"},
		{
			"boundary/span_overlaps_day", "{\"time\":\"2026-10-20T00:00:00Z\"}\n{\"time\":\"2026-10-18T23:59:59Z\"}",
			"2026-10-20T00:00:00Z startup\n2026-10-18T23:59:59Z startup", 0, "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sp, up := logFiles(t, []string{tt.snatch}, []string{tt.unblocker})
			var out, errOut bytes.Buffer
			code, err := run([]string{"-day", "2026-10-19", "-snatcharr", sp, "-unblocker", up}, &out, &errOut)
			if tt.wantFlag == "" {
				if code != tt.wantCode || err != nil || !strings.Contains(out.String(), "verdict=green") {
					t.Fatalf("exit %d, err %v, stdout %q", code, err, out.String())
				}
				return
			}
			path := sp
			if tt.wantFlag == "-unblocker" {
				path = up
			}
			want := fmt.Sprintf("%s log %s: no lines on 2026-10-19", tt.wantFlag, path)
			if code != tt.wantCode || err == nil || err.Error() != want || out.Len() != 0 {
				t.Fatalf("exit %d, err %v, stdout %q; want exit %d, err %q", code, err, out.String(), tt.wantCode, want)
			}
		})
	}
}

func TestRunOutFailure(t *testing.T) {
	t.Parallel()
	sp, up := logFiles(t, []string{`{"time":"2026-10-19T00:00:00Z"}`}, []string{"2026-10-19T00:00:00Z startup"})
	path := filepath.Join(t.TempDir(), "missing", "day.txt")
	var out, errOut bytes.Buffer
	code, err := run([]string{"-day", "2026-10-19", "-snatcharr", sp, "-unblocker", up, "-out", path}, &out, &errOut)
	if code != exitInput || !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), path) {
		t.Fatalf("exit %d, err %v; want exit 2 and wrapped file error for %s", code, err, path)
	}
	if !strings.Contains(out.String(), "verdict=green") {
		t.Fatalf("stdout %q, want unchanged row", out.String())
	}
}

func TestRunOut(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		unblocker string
		wantCode  int
	}{
		{"positive/green", imported("2026-10-19T12:00:00Z", "abc", "Show"), 0},
		{"positive/red_with_mismatch", "2026-10-19T12:00:00Z startup", exitRed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sp, up := logFiles(t, []string{would("2026-10-19T12:00:00Z", "abc", "Show")}, []string{tt.unblocker})
			path := filepath.Join(t.TempDir(), "day.txt")
			args := []string{"-day", "2026-10-19", "-snatcharr", sp, "-unblocker", up}
			var out, plain, errOut bytes.Buffer
			if code, err := run(args, &plain, &errOut); code != tt.wantCode || err != nil {
				t.Fatalf("without -out: exit %d, err %v", code, err)
			}
			if code, err := run(append(args, "-out", path), &out, &errOut); code != tt.wantCode || err != nil {
				t.Fatalf("with -out: exit %d, err %v", code, err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != out.String() || out.String() != plain.String() {
				t.Fatalf("file %q, stdout %q, plain stdout %q", data, out.String(), plain.String())
			}
		})
	}
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
