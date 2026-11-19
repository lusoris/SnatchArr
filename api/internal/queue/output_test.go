// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package queue

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func TestSplitOutputPath(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, path, parent, namePart string
		ok                           bool
	}{
		{"trailing slash", "/data/downloads/job/", "/data/downloads/", "job", true},
		{"no trailing slash", "/data/downloads/job", "/data/downloads/", "job", true},
		{"backslash", `C:\dl\complete\job`, `C:\dl\complete\`, "job", true},
		{"root", "/", "", "", false},
		{"empty", "", "", "", false},
		{"no separator", "job", "", "", false},
		{"double trailing", "/data/job//", "/data/", "job", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			parent, name, ok := splitOutputPath(tc.path)
			if parent != tc.parent || name != tc.namePart || ok != tc.ok {
				t.Fatalf("splitOutputPath(%q) = (%q, %q, %t)", tc.path, parent, name, ok)
			}
		})
	}
}

func TestNeedsOutputCheck(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 18, 17, 0, 0, 0, time.UTC)
	s := DefaultSettings()
	line := "No files found are eligible for import in /data/job"
	for _, tc := range []struct {
		name  string
		lines []string
		added time.Time
		want  bool
	}{
		{"past grace", []string{line}, now.Add(-s.Grace), true},
		{"before grace", []string{line}, now.Add(-s.Grace).Add(time.Nanosecond), false},
		{"unpacking", []string{line, "File is still being unpacked"}, now.Add(-s.Grace), false},
		{"other line", []string{"other"}, now.Add(-s.Grace), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := needsOutputCheck(Item{ReasonLines: tc.lines, Added: tc.added}, s, now); got != tc.want {
				t.Fatalf("needsOutputCheck() = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestClassifyNoFilesNeedsListedOutput(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 18, 17, 0, 0, 0, time.UTC)
	item := Item{ReasonLines: []string{"No files found are eligible for import in /data/job"}, Added: now.Add(-time.Hour)}
	for _, listed := range []bool{true, false} {
		item.OutputListed = listed
		class, pattern, _ := ClassifyImportItem(item, DefaultSettings(), now)
		if listed && (class != ClassBroken || pattern == "") {
			t.Fatalf("listed = (%s, %q), want broken pattern", class, pattern)
		}
		if !listed && (class != ClassUnknown || pattern != "") {
			t.Fatalf("not listed = (%s, %q), want unknown empty", class, pattern)
		}
	}
}

func TestCheckOutputs(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 18, 17, 0, 0, 0, time.UTC)
	s := DefaultSettings()
	arr := &outputArr{folders: map[string][]string{"/data/": {"job", "job.1"}}, errs: map[string]error{}}
	inst := Instance{Kind: domain.KindSonarr}
	items := []Item{outputItem("listed", "/data/job/", now), outputItem("prefix", "/data/jo", now)}
	checked, results := checkOutputs(context.Background(), arr, inst, s, items, now)
	if len(results) != len(items) {
		t.Fatalf("results = %d, want %d", len(results), len(items))
	}
	if !checked[0].OutputListed || checked[0].outputChecked || checked[1].OutputListed || !checked[1].outputChecked || results[1].result != outputNotVisible {
		t.Fatalf("exact output names not respected: %#v %#v", checked, results)
	}
	arr.errs["/error/"] = errors.New("unavailable")
	checked, results = checkOutputs(context.Background(), arr, inst, s, []Item{outputItem("error", "/error/job", now), outputItem("empty", "", now)}, now)
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if checked[0].OutputListed || !checked[0].outputChecked || results[0].result != outputError || !checked[1].outputChecked || results[1].result != outputNoPath {
		t.Fatalf("fail-closed results = %#v %#v", checked, results)
	}
	assertOutputCheckBounds(t, arr, inst, s, now)
}

func assertOutputCheckBounds(t *testing.T, arr *outputArr, inst Instance, s Settings, now time.Time) {
	t.Helper()
	arr.calls = nil
	items := make([]Item, 0, maxOutputChecks+1)
	for i := 0; i <= maxOutputChecks; i++ {
		items = append(items, outputItem(fmt.Sprintf("job-%d", i), fmt.Sprintf("/p-%d/job", i), now))
	}
	_, results := checkOutputs(context.Background(), arr, inst, s, items, now)
	if len(arr.calls) != maxOutputChecks || results[maxOutputChecks].result != outputOverBudget {
		t.Fatalf("calls/results = %d/%s, want %d/over_budget", len(arr.calls), results[maxOutputChecks].result, maxOutputChecks)
	}
	arr.calls = nil
	shared := make([]Item, 86)
	for i := range shared {
		shared[i] = outputItem(fmt.Sprintf("shared-%d", i), "/shared/job", now)
	}
	_, _ = checkOutputs(context.Background(), arr, inst, s, shared, now)
	if len(arr.calls) != 1 {
		t.Fatalf("shared parent calls = %d, want 1", len(arr.calls))
	}
}

func outputItem(id, path string, now time.Time) Item {
	return Item{DownloadID: id, OutputPath: path, Added: now.Add(-time.Hour), ReasonLines: []string{"No files found are eligible for import in " + path}}
}

type outputArr struct {
	folders map[string][]string
	errs    map[string]error
	calls   []string
}

func (a *outputArr) Queue(context.Context, domain.AppKind, string, string, int, int) ([]Item, error) {
	return nil, nil
}

func (a *outputArr) RemoveQueueItem(context.Context, domain.AppKind, string, string, int32, bool, bool) error {
	return nil
}

func (a *outputArr) ForceImport(context.Context, domain.AppKind, string, string, string, ImportMode) (int, error) {
	return 0, nil
}

func (a *outputArr) ListFolder(_ context.Context, _ domain.AppKind, _, _, dir string) ([]string, error) {
	a.calls = append(a.calls, dir)
	return a.folders[dir], a.errs[dir]
}
