// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package queue_test

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/queue"
)

var outputSweepAt = time.Date(2026, 11, 18, 17, 0, 0, 0, time.FixedZone("CET", 60*60))

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
