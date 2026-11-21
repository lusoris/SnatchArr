// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package queue_test

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/queue"
)

var outputSweepAt = time.Date(2026, 11, 18, 17, 0, 0, 0, time.FixedZone("CET", 60*60))

const outputCheckLimit = 20

func TestSweepBlindWindowReplay(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	env := newOutputSweepEnvWithLogger(t, []string{"other-job"}, slog.New(slog.NewTextHandler(&logs, nil)))
	env.arr.items[env.key] = blindWindowItems(86)
	runOutputSweeps(t, env, 2)
	if env.arr.removeCalls != 0 || strikeCount(env.store, env.instance.ID) != 0 {
		t.Fatalf("removals/strikes = %d/%d, want 0/0", env.arr.removeCalls, strikeCount(env.store, env.instance.ID))
	}
	assertOutputMetrics(t, env.metrics.outputs, queueOutputNotVisible, 172)
	assertNotVisibleWarnings(t, logs.String(), 86, 2)
}

func TestSweepVisibleEmptyFolderStillStrikes(t *testing.T) {
	t.Parallel()
	env := newOutputSweepEnv(t, blindWindowNames(86))
	env.arr.items[env.key] = blindWindowItems(86)
	runOutputSweeps(t, env, 1)
	assertImportBrokenStrikes(t, env.store, env.instance.ID, 86)
	if env.arr.removeCalls != 0 {
		t.Fatalf("first sweep removals = %d, want 0", env.arr.removeCalls)
	}
	env.clk.Advance(15 * time.Minute)
	runOutputSweeps(t, env, 1)
	if got := env.arr.removeCalls; got != 10 {
		t.Fatalf("second sweep removals = %d, want bounded 10", got)
	}
}

func TestSweepOutputListError(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	env := newOutputSweepEnvWithLogger(t, nil, slog.New(slog.NewTextHandler(&logs, nil)))
	env.arr.folderErr[outputParent] = errors.New("mount unavailable")
	env.arr.items[env.key] = blindWindowItems(1)
	runOutputSweeps(t, env, 1)
	assertOutputMetrics(t, env.metrics.outputs, queueOutputError, 1)
	if got := strikeCount(env.store, env.instance.ID); got != 0 {
		t.Fatalf("strikes = %d, want 0", got)
	}
	if output := logs.String(); !strings.Contains(output, "msg=\"queue: output path check failed\"") || !strings.Contains(output, "instance=sonarr-main") || !strings.Contains(output, "dir="+outputParent) || !strings.Contains(output, "error=\"mount unavailable\"") {
		t.Fatalf("error log = %q", output)
	}
}

func TestSweepOutputUncheckedPreservesImportBroken(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		itemCount  int
		listingErr bool
	}{
		{"outside output window", outputCheckLimit + 1, false},
		{"listing error", 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := newOutputSweepEnv(t, nil)
			settings := env.store.settings[env.instance.ID]
			settings.Rules[queue.RuleImportBroken] = queue.RuleSettings{Mode: queue.ModeObserve, Threshold: 2}
			env.store.settings[env.instance.ID] = settings
			items := outputItemsForSweep(outputSweepAt, tc.itemCount)
			target := items[len(items)-1]
			env.arr.items[env.key] = items
			env.store.strikes[env.instance.ID] = map[string]queue.Strike{target.DownloadID: {Rule: queue.RuleImportBroken, Count: 1}}
			if tc.listingErr {
				env.arr.folderErr["/cursor/00/"] = errors.New("mount unavailable")
			}
			if err := env.service.SweepAll(t.Context()); err != nil {
				t.Fatal(err)
			}
			got := env.store.strikes[env.instance.ID][target.DownloadID]
			if got.Rule != queue.RuleImportBroken || got.Count != 1 {
				t.Fatalf("unchecked strike = %#v, want import_broken count 1", got)
			}
			if tc.listingErr {
				delete(env.arr.folderErr, "/cursor/00/")
			} else {
				env.clk.Advance(15 * time.Minute)
			}
			parent := strings.TrimSuffix(target.OutputPath, target.DownloadID)
			env.arr.folders[parent] = []string{target.DownloadID}
			if err := env.service.SweepAll(t.Context()); err != nil {
				t.Fatal(err)
			}
			got = env.store.strikes[env.instance.ID][target.DownloadID]
			if got.Rule != queue.RuleImportBroken || got.Count != 2 {
				t.Fatalf("checked strike = %#v, want import_broken count 2", got)
			}
		})
	}
}

func TestSweepOutputCursorRestart(t *testing.T) {
	t.Parallel()
	env := newOutputSweepEnv(t, nil)
	items := outputItemsForSweep(outputSweepAt, 45)
	env.arr.items[env.key] = items
	if err := env.service.SweepAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	firstCursor := env.store.cursors[env.instance.ID]
	if firstCursor != "/cursor/19/" {
		t.Fatalf("first cursor = %q, want /cursor/19/", firstCursor)
	}
	env.arr.folderDirs = nil
	second := newTestService(env.store, &fakeInstanceSource{instances: []queue.Instance{env.instance}}, env.arr, env.clk, &fakeEventPublisher{}, env.metrics)
	if err := second.SweepAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(env.arr.folderDirs) != outputCheckLimit || env.arr.folderDirs[0] != "/cursor/20/" {
		t.Fatalf("restart listings = %#v, want start at /cursor/20/", env.arr.folderDirs)
	}
}

func TestSweepOutputCursorStaysOnFailure(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		strikesErr error
		recordErr  error
	}{
		{"strikes", errors.New("strikes unavailable"), nil},
		{"record", nil, errors.New("record unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := newOutputSweepEnv(t, nil)
			env.arr.items[env.key] = outputItemsForSweep(outputSweepAt, 1)
			env.store.cursors[env.instance.ID] = "/saved/"
			env.store.strikesErr = tc.strikesErr
			env.store.recordErr = tc.recordErr
			if err := env.service.SweepAll(t.Context()); err == nil {
				t.Fatal("SweepAll() error = nil, want failure")
			}
			if got := env.store.cursors[env.instance.ID]; got != "/saved/" {
				t.Fatalf("cursor = %q, want unchanged /saved/", got)
			}
		})
	}
}

func TestSweepOutputCursorReadError(t *testing.T) {
	t.Parallel()
	env := newOutputSweepEnv(t, nil)
	env.store.cursorErr = errors.New("cursor unavailable")
	if err := env.service.SweepAll(t.Context()); err == nil || !errors.Is(err, env.store.cursorErr) {
		t.Fatalf("SweepAll() error = %v, want wrapped cursor error", err)
	}
}

func outputItemsForSweep(now time.Time, count int) []queue.Item {
	items := make([]queue.Item, count)
	for i := range items {
		parent := fmt.Sprintf("/cursor/%02d/", i)
		name := fmt.Sprintf("job-%02d", i)
		items[i] = queue.Item{ID: int32(i + 1), DownloadID: name, Added: now.Add(-time.Hour), OutputPath: parent + name, Monitored: true, TrackedDownloadStatus: "warning", TrackedDownloadState: "importPending", ReasonLines: []string{"No files found are eligible for import in " + parent + name}}
	}
	return items
}

func TestSweepNotVisibleSkipsImportBlocked(t *testing.T) {
	t.Parallel()
	env := newOutputSweepEnv(t, []string{"other-job"})
	enableImportBlocked(env.store, env.instance.ID)
	env.arr.items[env.key] = blindWindowItems(1)
	runOutputSweeps(t, env, 2)
	if got := strikeCount(env.store, env.instance.ID); got != 0 {
		t.Fatalf("strikes = %d, want 0", got)
	}
}

func TestSweepPreGraceNoFilesNoStrike(t *testing.T) {
	t.Parallel()
	env := newOutputSweepEnv(t, []string{"job-001"})
	enableImportBlocked(env.store, env.instance.ID)
	item := blindWindowItems(1)[0]
	item.Added = outputSweepAt.Add(-time.Minute)
	item.ReasonLines = append(item.ReasonLines, "unrecognized import status")
	env.arr.items[env.key] = []queue.Item{item}
	for range 2 {
		if err := env.service.SweepAll(t.Context()); err != nil {
			t.Fatal(err)
		}
		env.clk.Advance(time.Minute)
	}
	if got := strikeCount(env.store, env.instance.ID); got != 0 || env.arr.folderCalls != 0 {
		t.Fatalf("strikes/folder calls = %d/%d, want 0/0", got, env.arr.folderCalls)
	}
}

func TestSweepUnpackNoFilesKeepsImportBlocked(t *testing.T) {
	t.Parallel()
	env := newOutputSweepEnv(t, []string{"job-001"})
	enableImportBlocked(env.store, env.instance.ID)
	item := blindWindowItems(1)[0]
	item.ReasonLines = append(item.ReasonLines, "File is still being unpacked")
	env.arr.items[env.key] = []queue.Item{item}
	runOutputSweeps(t, env, 1)
	strike, ok := env.store.strikes[env.instance.ID][item.DownloadID]
	if !ok || strike.Rule != queue.RuleImportBlocked || strike.Count != 1 || env.arr.folderCalls != 0 {
		t.Fatalf("strike/folder calls = %#v/%d, want import_blocked count 1 and no listings", strike, env.arr.folderCalls)
	}
}

func TestSweepVisibilityLostBeforeRemoval(t *testing.T) {
	t.Parallel()
	env := newOutputSweepEnv(t, []string{"job-001"})
	env.arr.items[env.key] = blindWindowItems(1)
	runOutputSweeps(t, env, 1)
	env.arr.folders[outputParent] = []string{"other-job"}
	runOutputSweeps(t, env, 1)
	env.arr.folders[outputParent] = []string{"job-001"}
	runOutputSweeps(t, env, 1)
	strike := env.store.strikes[env.instance.ID]["job-001"]
	if env.arr.removeCalls != 0 || strike.Rule != queue.RuleImportBroken || strike.Count != 1 {
		t.Fatalf("removals/strike = %d/%#v, want 0/import_broken count 1", env.arr.removeCalls, strike)
	}
}

func TestInspectRecordsOutputResults(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	env := newOutputSweepEnvWithLogger(t, []string{"other-job"}, slog.New(slog.NewTextHandler(&logs, nil)))
	env.arr.items[env.key] = blindWindowItems(1)
	if _, err := env.service.Inspect(t.Context(), env.instance.ID); err != nil {
		t.Fatal(err)
	}
	assertOutputMetrics(t, env.metrics.outputs, queueOutputNotVisible, 1)
	timestamp := fmt.Sprintf("sonarr-main:not_visible_at:%d", outputSweepAt.Unix())
	if want := []string{queueOutputNotVisible, timestamp}; !slices.Equal(env.metrics.outputLog, want) {
		t.Errorf("inspect output log = %#v, want %#v", env.metrics.outputLog, want)
	}
	if output := logs.String(); !strings.Contains(output, "msg=\"queue: output path not visible to arr\"") || !strings.Contains(output, "instance=sonarr-main") || !strings.Contains(output, "output_path="+outputParent+"job-001/") {
		t.Fatalf("inspect log = %q", output)
	}
}

func TestInspectListedOutputDoesNotRecordNotVisibleTime(t *testing.T) {
	t.Parallel()
	env := newOutputSweepEnv(t, []string{"job-001"})
	env.arr.items[env.key] = blindWindowItems(1)
	if _, err := env.service.Inspect(t.Context(), env.instance.ID); err != nil {
		t.Fatal(err)
	}
	if want := []string{"sonarr-main:listed"}; !slices.Equal(env.metrics.outputLog, want) {
		t.Errorf("inspect output log = %#v, want %#v", env.metrics.outputLog, want)
	}
}

func TestSweepAllInitsOutputSeries(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	arr := newFakeArrClient()
	metrics := &fakeMetricsRecorder{}
	instances := []queue.Instance{
		{ID: uuid.New(), Name: "radarr-main", Kind: domain.KindRadarr, BaseURL: "http://radarr.local", APIKey: "radarr-key"},
		{ID: uuid.New(), Name: "sonarr-main", Kind: domain.KindSonarr, BaseURL: "http://sonarr.local", APIKey: "sonarr-key"},
	}
	for _, inst := range instances {
		settings := queue.DefaultSettings()
		settings.Enabled = true
		settings.Grace = time.Minute
		store.settings[inst.ID] = settings
		path := "/data/" + inst.Name + "/job-001/"
		arr.items[string(inst.Kind)+":"+inst.BaseURL] = []queue.Item{{DownloadID: "job-001", Added: outputSweepAt.Add(-time.Hour), OutputPath: path, ReasonLines: []string{"No files found are eligible for import in " + path}}}
	}
	service := newTestService(store, &fakeInstanceSource{instances: instances}, arr, clockwork.NewFakeClockAt(outputSweepAt), &fakeEventPublisher{}, metrics)
	if err := service.SweepAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	wantInit := "listed,not_visible,error,no_path,over_budget"
	if len(metrics.outputLog) < 4 || metrics.outputLog[0] != "radarr-main:init:"+wantInit || metrics.outputLog[1] != "sonarr-main:init:"+wantInit {
		t.Fatalf("output log prefix = %#v, want both instance initializers before checks", metrics.outputLog)
	}
	firstCheck := 2
	for i, entry := range metrics.outputLog {
		if strings.Contains(entry, ":init:") {
			continue
		}
		firstCheck = i
		break
	}
	if firstCheck < 2 {
		t.Fatalf("first output check index = %d, want after both initializers", firstCheck)
	}
}

func TestSweepRecordsNotVisibleTime(t *testing.T) {
	t.Parallel()
	timestamp := fmt.Sprintf("sonarr-main:not_visible_at:%d", outputSweepAt.Unix())
	initRecord := "sonarr-main:init:listed,not_visible,error,no_path,over_budget"
	tests := []struct {
		name        string
		folderNames []string
		itemCount   int
		folderError bool
		want        []string
	}{
		{"not visible and listed", []string{"job-002"}, 2, false, []string{initRecord, queueOutputNotVisible, "sonarr-main:listed", timestamp}},
		{"listed only", []string{"job-001"}, 1, false, []string{initRecord, "sonarr-main:listed"}},
		{"error only", nil, 1, true, []string{initRecord, queueOutputError}},
		{"two not visible", nil, 2, false, []string{initRecord, queueOutputNotVisible, queueOutputNotVisible, timestamp}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			env := newOutputSweepEnv(t, tc.folderNames)
			env.arr.items[env.key] = blindWindowItems(tc.itemCount)
			if tc.folderError {
				env.arr.folderErr[outputParent] = errors.New("mount unavailable")
			}
			runOutputSweeps(t, env, 1)
			if !slices.Equal(env.metrics.outputLog, tc.want) {
				t.Errorf("output log = %#v, want %#v", env.metrics.outputLog, tc.want)
			}
		})
	}
}

func TestSweepRotatesOutputWindow(t *testing.T) {
	t.Parallel()
	env := newOutputSweepEnv(t, nil)
	items := make([]queue.Item, 45)
	parents := make([]string, len(items))
	for i := range items {
		name := fmt.Sprintf("job-%02d", i)
		parent := fmt.Sprintf("/rotating/%02d/", i)
		parents[i] = parent
		items[i] = queue.Item{
			ID:          int32(i + 1),
			DownloadID:  name,
			Added:       outputSweepAt.Add(-time.Hour),
			OutputPath:  parent + name,
			ReasonLines: []string{"No files found are eligible for import in " + parent + name},
		}
	}
	env.arr.items[env.key] = items
	for sweep := range 3 {
		env.arr.folderDirs = nil
		if err := env.service.SweepAll(t.Context()); err != nil {
			t.Fatal(err)
		}
		start := sweep * 20
		end := min(start+20, len(parents))
		if !slices.Equal(env.arr.folderDirs, parents[start:end]) {
			t.Fatalf("sweep %d folders = %v, want %v", sweep+1, env.arr.folderDirs, parents[start:end])
		}
		if sweep == 0 {
			env.arr.folderDirs = nil
			if _, err := env.service.Inspect(t.Context(), env.instance.ID); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(env.arr.folderDirs, parents[20:40]) {
				t.Fatalf("inspect folders = %v, want %v", env.arr.folderDirs, parents[20:40])
			}
		}
		env.clk.Advance(15 * time.Minute)
	}
}

func TestSweepListedUnrecognizedKeepsImportBlocked(t *testing.T) {
	t.Parallel()
	env := newOutputSweepEnv(t, []string{"job-001"})
	enableImportBlocked(env.store, env.instance.ID)
	items := blindWindowItems(1)
	items[0].ReasonLines = append(items[0].ReasonLines, "unrecognized import status")
	env.arr.items[env.key] = items
	runOutputSweeps(t, env, 1)
	strike, ok := env.store.strikes[env.instance.ID]["job-001"]
	if !ok || strike.Rule != queue.RuleImportBlocked || strike.Count != 1 {
		t.Fatalf("strike = %#v, present %t; want import_blocked count 1", strike, ok)
	}
}

type outputSweepEnv struct {
	store    *fakeStore
	arr      *fakeArrClient
	metrics  *fakeMetricsRecorder
	instance queue.Instance
	key      string
	service  *queue.Service
	clk      *clockwork.FakeClock
}

const outputParent = "/data/downloads/sabnzbd/complete/sonarr/"

const (
	queueOutputError      = "sonarr-main:error"
	queueOutputNotVisible = "sonarr-main:not_visible"
)

func newOutputSweepEnv(t *testing.T, names []string) outputSweepEnv {
	t.Helper()
	return newOutputSweepEnvWithLogger(t, names, slog.New(slog.DiscardHandler))
}

func newOutputSweepEnvWithLogger(t *testing.T, names []string, logger *slog.Logger) outputSweepEnv {
	t.Helper()
	instance := queue.Instance{ID: uuid.New(), Name: "sonarr-main", Kind: domain.KindSonarr, BaseURL: "http://sonarr.local", APIKey: "test-key"}
	store, arr, metrics := newFakeStore(), newFakeArrClient(), &fakeMetricsRecorder{}
	settings := queue.DefaultSettings()
	settings.Enabled = true
	settings.Rules[queue.RuleImportBroken] = queue.RuleSettings{Mode: queue.ModeAct, Threshold: 2}
	store.settings[instance.ID] = settings
	arr.folders[outputParent] = names
	clk := clockwork.NewFakeClockAt(outputSweepAt)
	service := newTestServiceWithLogger(store, &fakeInstanceSource{instances: []queue.Instance{instance}}, arr, clk, &fakeEventPublisher{}, metrics, logger)
	return outputSweepEnv{store: store, arr: arr, metrics: metrics, instance: instance, key: string(instance.Kind) + ":" + instance.BaseURL, service: service, clk: clk}
}

func enableImportBlocked(store *fakeStore, id uuid.UUID) {
	settings := store.settings[id]
	settings.Rules[queue.RuleImportBlocked] = queue.RuleSettings{Mode: queue.ModeObserve, Threshold: 1}
	store.settings[id] = settings
}

func blindWindowItems(count int) []queue.Item {
	start := time.Date(2026, 11, 18, 9, 47, 0, 0, outputSweepAt.Location())
	items := make([]queue.Item, 0, count)
	for i := range count {
		name := fmt.Sprintf("job-%03d", i+1)
		path := outputParent + name + "/"
		items = append(items, queue.Item{ID: int32(i + 1), DownloadID: name, Added: start.Add(time.Duration(i) * 4 * time.Minute), Status: "completed", TrackedDownloadStatus: "warning", TrackedDownloadState: "importPending", Protocol: "usenet", OutputPath: path, Monitored: true, ReasonLines: []string{"No files found are eligible for import in " + path}})
	}
	return items
}

func blindWindowNames(count int) []string {
	names := make([]string, 0, count)
	for i := range count {
		names = append(names, fmt.Sprintf("job-%03d", i+1))
	}
	return names
}

func runOutputSweeps(t *testing.T, env outputSweepEnv, sweeps int) {
	t.Helper()
	for i := range sweeps {
		if err := env.service.SweepAll(t.Context()); err != nil {
			t.Fatal(err)
		}
		if i+1 < sweeps {
			env.clk.Advance(15 * time.Minute)
		}
	}
}

func assertOutputMetrics(t *testing.T, got []string, want string, count int) {
	t.Helper()
	if len(got) != count {
		t.Fatalf("output metrics = %d, want %d", len(got), count)
	}
	for _, result := range got {
		if result != want {
			t.Fatalf("output metric = %q, want %q", result, want)
		}
	}
}

func assertNotVisibleWarnings(t *testing.T, logs string, jobs, repeats int) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(logs), "\n")
	if len(lines) != jobs*repeats {
		t.Fatalf("warning records = %d, want %d", len(lines), jobs*repeats)
	}
	for _, line := range lines {
		if !strings.Contains(line, "msg=\"queue: output path not visible to arr\"") || !strings.Contains(line, "instance=sonarr-main") || !strings.Contains(line, "download_id=job-") || !strings.Contains(line, "output_path="+outputParent) {
			t.Fatalf("warning record = %q", line)
		}
	}
	for i := range jobs {
		job := fmt.Sprintf("job-%03d", i+1)
		if strings.Count(logs, "download_id="+job+" ") != repeats || strings.Count(logs, "output_path="+outputParent+job+"/") != repeats {
			t.Fatalf("warning records for %s = %d/%d, want %d/%d", job, strings.Count(logs, "download_id="+job+" "), strings.Count(logs, "output_path="+outputParent+job+"/"), repeats, repeats)
		}
	}
}

func strikeCount(store *fakeStore, id uuid.UUID) int {
	count := 0
	for _, strike := range store.strikes[id] {
		if strike.Count > 0 {
			count++
		}
	}
	return count
}

func assertImportBrokenStrikes(t *testing.T, store *fakeStore, id uuid.UUID, count int) {
	t.Helper()
	strikes := store.strikes[id]
	if len(strikes) != count {
		t.Fatalf("first sweep strikes = %d, want %d", len(strikes), count)
	}
	for downloadID, strike := range strikes {
		if strike.Rule != queue.RuleImportBroken || strike.Count != 1 {
			t.Fatalf("strike %q = %#v, want import_broken count 1", downloadID, strike)
		}
	}
}
