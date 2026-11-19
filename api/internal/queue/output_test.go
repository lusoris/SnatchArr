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
	checked, results, _ := checkOutputs(context.Background(), arr, inst, s, items, now, 0)
	if len(results) != len(items) {
		t.Fatalf("results = %d, want %d", len(results), len(items))
	}
	if !checked[0].OutputListed || checked[1].OutputListed || results[0].result != outputListed || results[1].result != outputNotVisible {
		t.Fatalf("exact output names not respected: %#v %#v", checked, results)
	}
	arr.folders["/case/"] = []string{"Job"}
	checked, results, _ = checkOutputs(context.Background(), arr, inst, s, []Item{outputItem("case", "/case/job", now)}, now, 0)
	if checked[0].OutputListed || results[0].result != outputNotVisible {
		t.Fatalf("case-mismatched output = %#v / %#v, want not visible", checked[0], results[0])
	}
	arr.errs["/error/"] = errors.New("unavailable")
	checked, results, _ = checkOutputs(context.Background(), arr, inst, s, []Item{outputItem("error", "/error/job", now), outputItem("empty", "", now)}, now, 0)
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if checked[0].OutputListed || results[0].result != outputError || results[1].result != outputNoPath {
		t.Fatalf("fail-closed results = %#v %#v", checked, results)
	}
	assertOutputCheckBounds(t, arr, inst, s, now)
}

func TestCheckOutputsRotates(t *testing.T) {
	t.Parallel()
	now, s := time.Date(2026, 11, 18, 17, 0, 0, 0, time.UTC), DefaultSettings()
	arr := &outputArr{folders: make(map[string][]string), errs: map[string]error{}}
	items := make([]Item, 45)
	for i := range items {
		parent := fmt.Sprintf("/parent-%02d/", i)
		name := fmt.Sprintf("job-%02d", i)
		items[i] = outputItem(name, parent+name, now)
		arr.folders[parent] = []string{name}
	}
	cursor := 0
	for sweep := range 3 {
		_, results, next := checkOutputs(context.Background(), arr, Instance{Kind: domain.KindSonarr}, s, items, now, cursor)
		if sweep == 0 && len(arr.calls) != maxOutputChecks {
			t.Fatalf("first sweep calls = %d, want %d", len(arr.calls), maxOutputChecks)
		}
		wantListed := min(maxOutputChecks, len(items)-sweep*maxOutputChecks)
		listed := 0
		for _, result := range results {
			if result.result == outputListed {
				listed++
			}
		}
		if listed != wantListed {
			t.Fatalf("listed results = %d, want %d", listed, wantListed)
		}
		cursor = next
	}
	if len(arr.calls) != len(items) {
		t.Fatalf("calls = %d, want %d", len(arr.calls), len(items))
	}
	seen := make(map[string]int, len(arr.calls))
	for _, parent := range arr.calls {
		seen[parent]++
	}
	for i := range items {
		if got := seen[fmt.Sprintf("/parent-%02d/", i)]; got != 1 {
			t.Fatalf("parent %d listed %d times, want once", i, got)
		}
	}
	arr.calls = nil
	twenty := make([]Item, maxOutputChecks)
	for i := range twenty {
		name := fmt.Sprintf("job-%02d", i)
		twenty[i] = outputItem(name, fmt.Sprintf("/exact-%02d/", i)+name, now)
	}
	_, _, next := checkOutputs(context.Background(), arr, Instance{Kind: domain.KindSonarr}, s, twenty, now, 0)
	if len(arr.calls) != maxOutputChecks || next != 0 {
		t.Fatalf("boundary calls/cursor = %d/%d, want %d/0", len(arr.calls), next, maxOutputChecks)
	}
	arr.calls = nil
	_, _, next = checkOutputs(context.Background(), arr, Instance{Kind: domain.KindSonarr}, s, nil, now, 7)
	if len(arr.calls) != 0 || next != 7 {
		t.Fatalf("empty calls/cursor = %d/%d, want 0/7", len(arr.calls), next)
	}
}

func TestCheckOutputsBudget(t *testing.T) {
	t.Parallel()
	now, s := time.Date(2026, 11, 18, 17, 0, 0, 0, time.UTC), DefaultSettings()
	inst := Instance{Kind: domain.KindSonarr}
	arr := &outputArr{folders: map[string][]string{}, errs: map[string]error{}}
	_, _, _ = checkOutputs(context.Background(), arr, inst, s, []Item{outputItem("a", "/a/job", now)}, now, 0)
	if !arr.deadlineSeen {
		t.Fatal("ListFolder did not receive a deadline")
	}
	ctx, cancel := context.WithCancel(context.Background())
	arr.calls, arr.deadlineSeen, arr.cancelOnFirst = nil, false, cancel
	items := []Item{outputItem("a", "/a/job", now), outputItem("b", "/b/job", now), outputItem("c", "/c/job", now)}
	_, results, _ := checkOutputs(ctx, arr, inst, s, items, now, 0)
	if len(arr.calls) != 1 {
		t.Fatalf("calls after cancellation = %d, want 1", len(arr.calls))
	}
	if results[0].result == outputError {
		t.Fatalf("first result = %q, want completed listing", results[0].result)
	}
	for _, result := range results[1:] {
		if result.result != outputError {
			t.Fatalf("result = %q, want error", result.result)
		}
	}
}

func assertOutputCheckBounds(t *testing.T, arr *outputArr, inst Instance, s Settings, now time.Time) {
	t.Helper()
	arr.calls = nil
	items := make([]Item, 0, maxOutputChecks+1)
	for i := 0; i <= maxOutputChecks; i++ {
		items = append(items, outputItem(fmt.Sprintf("job-%d", i), fmt.Sprintf("/p-%02d/job", i), now))
	}
	_, results, _ := checkOutputs(context.Background(), arr, inst, s, items, now, 0)
	if len(arr.calls) != maxOutputChecks || results[maxOutputChecks].result != outputOverBudget {
		t.Fatalf("calls/results = %d/%s, want %d/over_budget", len(arr.calls), results[maxOutputChecks].result, maxOutputChecks)
	}
	arr.calls = nil
	shared := make([]Item, 86)
	for i := range shared {
		shared[i] = outputItem(fmt.Sprintf("shared-%d", i), "/shared/job", now)
	}
	_, _, _ = checkOutputs(context.Background(), arr, inst, s, shared, now, 0)
	if len(arr.calls) != 1 {
		t.Fatalf("shared parent calls = %d, want 1", len(arr.calls))
	}
}

func outputItem(id, path string, now time.Time) Item {
	return Item{DownloadID: id, OutputPath: path, Added: now.Add(-time.Hour), ReasonLines: []string{"No files found are eligible for import in " + path}}
}

type outputArr struct {
	folders       map[string][]string
	errs          map[string]error
	calls         []string
	deadlineSeen  bool
	cancelOnFirst context.CancelFunc
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

func (a *outputArr) ListFolder(ctx context.Context, _ domain.AppKind, _, _, dir string) ([]string, error) {
	_, a.deadlineSeen = ctx.Deadline()
	a.calls = append(a.calls, dir)
	if a.cancelOnFirst != nil {
		a.cancelOnFirst()
		a.cancelOnFirst = nil
	}
	return a.folders[dir], a.errs[dir]
}
