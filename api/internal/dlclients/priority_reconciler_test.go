// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package dlclients

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

type prioritySourceFake struct {
	inputs     priorityInputs
	err        error
	secrets    map[uuid.UUID]string
	secretErrs map[uuid.UUID]error
}

func (f prioritySourceFake) Load(context.Context) (priorityInputs, error) { return f.inputs, f.err }
func (f prioritySourceFake) Secret(_ context.Context, id uuid.UUID) (string, error) {
	return f.secrets[id], f.secretErrs[id]
}

func TestPriorityGroupCredentials(t *testing.T) {
	t.Parallel()
	rows := priorityTestInputs(domain.ClientQBittorrent, "observe", "http://qbit", "http://other").Clients[:2]
	first, second := rows[0].ID, rows[1].ID
	failed := errors.New("decrypt failed")
	tests := []struct {
		name       string
		secrets    map[uuid.UUID]string
		secretErrs map[uuid.UUID]error
		wantRow    uuid.UUID
		wantSecret string
		wantErr    bool
	}{
		{name: "secret on later row is used", secrets: map[uuid.UUID]string{second: "s2"}, wantRow: second, wantSecret: "s2"},
		{name: "first row with a secret wins", secrets: map[uuid.UUID]string{first: "s1", second: "s2"}, wantRow: first, wantSecret: "s1"},
		{name: "no secret falls back to first row", wantRow: first},
		{name: "failed lookup skipped when another row has a secret", secrets: map[uuid.UUID]string{second: "s2"},
			secretErrs: map[uuid.UUID]error{first: failed}, wantRow: second, wantSecret: "s2"},
		{name: "failed lookup without any secret fails", secretErrs: map[uuid.UUID]error{second: failed}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			source := prioritySourceFake{secrets: tt.secrets, secretErrs: tt.secretErrs}
			r := newPriorityReconciler(source, nil, clock.NewFake(), slog.New(slog.DiscardHandler))
			row, secret, err := r.groupCredentials(t.Context(), rows)
			if tt.wantErr {
				if !errors.Is(err, failed) {
					t.Fatalf("err = %v, want %v", err, failed)
				}
				return
			}
			if err != nil || row.ID != tt.wantRow || secret != tt.wantSecret {
				t.Fatalf("row = %v, secret = %q, err = %v; want %v, %q", row.ID, secret, err, tt.wantRow, tt.wantSecret)
			}
		})
	}
}

func TestPriorityReconcilerReadOnly(t *testing.T) {
	t.Parallel()
	for _, kind := range []domain.DownloadClientKind{domain.ClientQBittorrent, domain.ClientNZBGet, domain.ClientSABnzbd} {
		for _, mode := range []string{"observe", "act"} {
			t.Run(string(kind)+"/"+mode, func(t *testing.T) {
				t.Parallel()
				good, bad := &priorityFake{}, &priorityFake{fail: true}
				goodServer := httptest.NewServer(http.HandlerFunc(good.serve))
				defer goodServer.Close()
				badServer := httptest.NewServer(http.HandlerFunc(bad.serve))
				defer badServer.Close()
				inputs := priorityTestInputs(kind, mode, goodServer.URL, badServer.URL)
				clk := clock.NewFake()
				var logs bytes.Buffer
				r := newPriorityReconciler(prioritySourceFake{inputs: inputs}, NewGoenvoy(clk), clk, slog.New(slog.NewJSONHandler(&logs, nil)))
				if err := r.Tick(t.Context()); err != nil {
					t.Fatal(err)
				}
				assertPriorityPlan(t, r, inputs, clk)
				if _, err := r.Latest(inputs.Clients[2].ID); !errors.Is(err, domain.ErrNotFound) {
					t.Fatalf("failed client plan: %v", err)
				}
				good.assertReads(t)
				bad.assertReads(t)
				assertPriorityLogs(t, logs.String())
				good.mu.Lock()
				count := len(good.requests)
				good.mu.Unlock()
				if kind != domain.ClientQBittorrent && count != 1 {
					t.Fatalf("shared client read %d times", count)
				}
			})
		}
	}
}

func priorityTestInputs(kind domain.DownloadClientKind, mode, goodURL, badURL string) priorityInputs {
	first, second := uuid.New(), uuid.New()
	return priorityInputs{
		Clients: []domain.DownloadClient{
			{ID: uuid.New(), InstanceID: &first, Category: "movies", PriorityMode: mode, Kind: kind, BaseURL: goodURL, Enabled: true},
			{ID: uuid.New(), InstanceID: &second, Category: "tv", PriorityMode: mode, Kind: kind, BaseURL: goodURL + "/", Enabled: true},
			{ID: uuid.New(), InstanceID: &first, Category: "movies", PriorityMode: mode, Kind: kind, BaseURL: badURL, Enabled: true},
		},
		Instances: []domain.Instance{{ID: first, Kind: domain.KindRadarr}, {ID: second, Kind: domain.KindSonarr, DownloadPriority: new(100)}},
		Preset:    domain.PriorityPreset{domain.KindRadarr: 1},
	}
}

func assertPriorityPlan(t *testing.T, r *Reconciler, inputs priorityInputs, clk clock.Clock) {
	t.Helper()
	for _, row := range inputs.Clients[:2] {
		plan, err := r.Latest(row.ID)
		if err != nil {
			t.Fatal(err)
		}
		metadata := plan
		metadata.Plan = domain.DownloadClientPlan{}
		want := PriorityPlan{ClientID: row.ID, Mode: row.PriorityMode, ObservedAt: clk.Now(), Unattributed: 1}
		if !reflect.DeepEqual(metadata, want) {
			t.Fatalf("metadata = %+v, want %+v", metadata, want)
		}
		if len(plan.Plan.Pause) != 1 || len(plan.Plan.Resume) != 0 {
			t.Fatalf("plan = %+v", plan)
		}
		plan.Plan.Pause[0] = "modified"
		again, err := r.Latest(row.ID)
		if err != nil || again.Plan.Pause[0] == "modified" {
			t.Fatalf("cache aliased: %+v, %v", again, err)
		}
	}
}

func TestPriorityReconcilerSourceFailure(t *testing.T) {
	t.Parallel()
	r := newPriorityReconciler(prioritySourceFake{err: errors.New("failed")}, nil, clock.NewFake(), slog.New(slog.DiscardHandler))
	if err := r.Tick(t.Context()); err == nil {
		t.Fatal("expected load failure")
	}
	if _, err := r.Latest(uuid.New()); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("no observation: %v", err)
	}
}

func TestPriorityGroups(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		clients []domain.DownloadClient
		want    int
	}{
		{"empty", nil, 0},
		{"unsupported", []domain.DownloadClient{{Enabled: true, Kind: domain.ClientDeluge, BaseURL: "http://client"}}, 0},
		{"disabled", []domain.DownloadClient{{Enabled: false, Kind: domain.ClientNZBGet, BaseURL: "http://client"}}, 0},
		{"invalid URL", []domain.DownloadClient{{Enabled: true, Kind: domain.ClientNZBGet, BaseURL: "://"}}, 0},
		{"same URL different kinds", []domain.DownloadClient{{Enabled: true, Kind: domain.ClientNZBGet, BaseURL: "http://client"}, {Enabled: true, Kind: domain.ClientQBittorrent, BaseURL: "http://client"}}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := priorityGroups(tc.clients); len(got) != tc.want {
				t.Fatalf("groups = %+v", got)
			}
		})
	}
}

type priorityObserverFunc func(context.Context, domain.DownloadClient, string) (PriorityObservation, error)

func (f priorityObserverFunc) PriorityItems(ctx context.Context, c domain.DownloadClient, secret string) (PriorityObservation, error) {
	return f(ctx, c, secret)
}

func TestPriorityReconcilerRetainsLatest(t *testing.T) {
	t.Parallel()
	fake := &priorityFake{}
	srv := httptest.NewServer(http.HandlerFunc(fake.serve))
	defer srv.Close()
	inputs := priorityTestInputs(domain.ClientSABnzbd, "observe", srv.URL, srv.URL)
	clk := clock.NewFake()
	source := &prioritySourceFake{inputs: inputs}
	r := newPriorityReconciler(source, NewGoenvoy(clk), clk, slog.New(slog.DiscardHandler))
	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	first, err := r.Latest(inputs.Clients[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	r.observer = priorityObserverFunc(func(context.Context, domain.DownloadClient, string) (PriorityObservation, error) {
		return PriorityObservation{}, errors.New("unavailable")
	})
	clk.Advance(time.Minute)
	if tickErr := r.Tick(t.Context()); tickErr != nil {
		t.Fatal(tickErr)
	}
	latest, err := r.Latest(inputs.Clients[0].ID)
	if err != nil || !latest.ObservedAt.Equal(first.ObservedAt) {
		t.Fatalf("latest = %+v, err = %v", latest, err)
	}
	source.inputs.Clients = nil
	if err := r.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Latest(inputs.Clients[0].ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("removed client: %v", err)
	}
}

func TestPriorityReconcilerWorkerBound(t *testing.T) {
	t.Parallel()
	var active, peak, total atomic.Int32
	var peakMu sync.Mutex
	entered := make(chan struct{}, 12)
	release := make(chan struct{})
	observer := priorityWorkerObserver(t, entered, release, &active, &peak, &total, &peakMu)
	clients := make([]domain.DownloadClient, 12)
	for i := range clients {
		clients[i] = domain.DownloadClient{ID: uuid.New(), Kind: domain.ClientNZBGet, BaseURL: fmt.Sprintf("http://client/%d", i), Enabled: true}
	}
	r := newPriorityReconciler(prioritySourceFake{inputs: priorityInputs{Clients: clients}}, observer, clock.NewFake(), slog.New(slog.DiscardHandler))
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Tick(ctx) }()
	for range maxPriorityWorkers {
		select {
		case <-entered:
		case <-ctx.Done():
			close(release)
			t.Fatal("workers did not start")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if peak.Load() > maxPriorityWorkers || total.Load() != 12 {
		t.Fatalf("peak = %d, total = %d", peak.Load(), total.Load())
	}
}

func assertPriorityLogs(t *testing.T, logs string) {
	t.Helper()
	if strings.Contains(logs, "private title") || strings.Contains(logs, "movies") {
		t.Fatalf("content leaked in logs: %s", logs)
	}
	if strings.Count(logs, `"msg":"download priority plan"`) != 2 || !strings.Contains(logs, "observation failed") {
		t.Fatalf("logs: %s", logs)
	}
}

func priorityWorkerObserver(t *testing.T, entered chan<- struct{}, release <-chan struct{}, active, peak, total *atomic.Int32, peakMu *sync.Mutex) priorityObserverFunc {
	t.Helper()
	return priorityObserverFunc(func(ctx context.Context, _ domain.DownloadClient, _ string) (PriorityObservation, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("read has no deadline")
		}
		now := active.Add(1)
		defer active.Add(-1)
		peakMu.Lock()
		peak.Store(max(peak.Load(), now))
		peakMu.Unlock()
		entered <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return PriorityObservation{}, ctx.Err()
		}
		total.Add(1)
		return PriorityObservation{}, nil
	})
}
