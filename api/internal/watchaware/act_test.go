// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package watchaware_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync"
	"testing"

	"github.com/golusoris/goenvoy/arr/sonarr"
	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
	"github.com/lusoris/SnatchArr/api/internal/watchaware"
)

type fakeSeasonWriter struct {
	mu    sync.Mutex
	calls []seasonWriterCall
	err   error
}

type seasonWriterCall struct {
	inst            domain.Instance
	seriesID        int
	seriesMonitored bool
	seasons         map[int]bool
}

func (f *fakeSeasonWriter) SetSeasonsMonitored(
	_ context.Context,
	inst domain.Instance,
	seriesID int,
	seriesMonitored bool,
	seasons map[int]bool,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	copied := make(map[int]bool, len(seasons))
	maps.Copy(copied, seasons)
	f.calls = append(f.calls, seasonWriterCall{
		inst:            inst,
		seriesID:        seriesID,
		seriesMonitored: seriesMonitored,
		seasons:         copied,
	})
	return nil
}

func (f *fakeSeasonWriter) getCalls() []seasonWriterCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]seasonWriterCall(nil), f.calls...)
}

func (f *fakeSeasonWriter) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func TestGoenvoySeasonWriter_Unit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		seriesID        int
		seriesMonitored bool
		seasons         map[int]bool
		seasonPassCode  int
		episodesCode    int
		monitorCode     int
		wantErr         bool
	}{
		{
			name:            "positive: updates season pass and monitors episodes",
			seriesID:        42,
			seriesMonitored: true,
			seasons:         map[int]bool{2: false, 3: true},
			seasonPassCode:  http.StatusOK,
			episodesCode:    http.StatusOK,
			monitorCode:     http.StatusOK,
			wantErr:         false,
		},
		{
			name:            "negative: seasonpass fails with server error",
			seriesID:        42,
			seriesMonitored: false,
			seasons:         map[int]bool{2: false},
			seasonPassCode:  http.StatusInternalServerError,
			episodesCode:    http.StatusOK,
			monitorCode:     http.StatusOK,
			wantErr:         true,
		},
		{
			name:            "negative: get episodes fails with server error",
			seriesID:        42,
			seriesMonitored: true,
			seasons:         map[int]bool{2: false},
			seasonPassCode:  http.StatusOK,
			episodesCode:    http.StatusInternalServerError,
			monitorCode:     http.StatusOK,
			wantErr:         true,
		},
		{
			name:            "negative: monitor episodes fails with server error",
			seriesID:        42,
			seriesMonitored: true,
			seasons:         map[int]bool{2: false},
			seasonPassCode:  http.StatusOK,
			episodesCode:    http.StatusOK,
			monitorCode:     http.StatusInternalServerError,
			wantErr:         true,
		},
		{
			name:            "boundary: empty seasons map succeeds without writes",
			seriesID:        42,
			seriesMonitored: true,
			seasons:         map[int]bool{},
			seasonPassCode:  http.StatusOK,
			episodesCode:    http.StatusOK,
			monitorCode:     http.StatusOK,
			wantErr:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var seasonPassCalled bool
			var seasonPassSeriesMonitored bool
			var seasonPassSeasonMap map[int]bool
			var episodesCalled bool
			var monitorCalls []sonarr.EpisodesMonitoredResource
			var mu sync.Mutex

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()

				switch {
				case r.Method == http.MethodPost && r.URL.Path == "/api/v3/seasonpass":
					seasonPassCalled = true
					if tt.seasonPassCode != http.StatusOK {
						http.Error(w, "server error", tt.seasonPassCode)
						return
					}
					var pass sonarr.SeasonPassResource
					b, _ := io.ReadAll(r.Body)
					_ = json.Unmarshal(b, &pass)
					if len(pass.Series) > 0 {
						seasonPassSeriesMonitored = pass.Series[0].Monitored
						seasonPassSeasonMap = make(map[int]bool)
						for _, s := range pass.Series[0].Seasons {
							seasonPassSeasonMap[s.SeasonNumber] = s.Monitored
						}
					}
					w.WriteHeader(http.StatusOK)

				case r.Method == http.MethodGet && r.URL.Path == "/api/v3/episode":
					episodesCalled = true
					if tt.episodesCode != http.StatusOK {
						http.Error(w, "server error", tt.episodesCode)
						return
					}
					episodes := []sonarr.Episode{
						{ID: 101, SeasonNumber: 1, EpisodeNumber: 1, Monitored: true},
						{ID: 102, SeasonNumber: 2, EpisodeNumber: 1, Monitored: true},
						{ID: 103, SeasonNumber: 2, EpisodeNumber: 2, Monitored: true},
						{ID: 104, SeasonNumber: 3, EpisodeNumber: 1, Monitored: false},
					}
					_ = json.NewEncoder(w).Encode(episodes)

				case r.Method == http.MethodPut && r.URL.Path == "/api/v3/episode/monitor":
					if tt.monitorCode != http.StatusOK {
						http.Error(w, "server error", tt.monitorCode)
						return
					}
					var req sonarr.EpisodesMonitoredResource
					b, _ := io.ReadAll(r.Body)
					_ = json.Unmarshal(b, &req)
					monitorCalls = append(monitorCalls, req)
					w.WriteHeader(http.StatusOK)

				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			instID := uuid.New()
			inst := domain.Instance{
				ID:      instID,
				Kind:    domain.KindSonarr,
				BaseURL: server.URL,
				Enabled: true,
			}
			credsMgr := &fakeInstanceManager{
				creds: map[uuid.UUID]instances.Credentials{
					instID: {BaseURL: server.URL, APIKey: "test-key"},
				},
			}

			writer := watchaware.NewSeasonWriter(credsMgr, arrclient.DefaultOptions())
			ctx := t.Context()
			err := writer.SetSeasonsMonitored(ctx, inst, tt.seriesID, tt.seriesMonitored, tt.seasons)

			if (err != nil) != tt.wantErr {
				t.Fatalf("SetSeasonsMonitored() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && len(tt.seasons) > 0 {
				mu.Lock()
				defer mu.Unlock()

				if !seasonPassCalled {
					t.Error("expected POST /api/v3/seasonpass call")
				}
				if seasonPassSeriesMonitored != tt.seriesMonitored {
					t.Errorf("seasonpass series monitored = %v, want %v", seasonPassSeriesMonitored, tt.seriesMonitored)
				}
				for sn, expectedMon := range tt.seasons {
					if gotMon, ok := seasonPassSeasonMap[sn]; !ok || gotMon != expectedMon {
						t.Errorf("seasonpass season %d monitored = %v, want %v", sn, gotMon, expectedMon)
					}
				}
				if !episodesCalled {
					t.Error("expected GET /api/v3/episode call")
				}

				// Check monitor calls match affected episodes only
				for _, mc := range monitorCalls {
					if mc.Monitored {
						if !slices.Contains(mc.EpisodeIDs, 104) {
							t.Errorf("monitored true calls = %v, want episode 104", mc.EpisodeIDs)
						}
					} else {
						if !slices.Contains(mc.EpisodeIDs, 102) || !slices.Contains(mc.EpisodeIDs, 103) {
							t.Errorf("monitored false calls = %v, want episodes 102, 103", mc.EpisodeIDs)
						}
					}
				}
			}
		})
	}
}

func TestActPending_Positive(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := setupTestEnv(ctx, t)

	instID := uuid.New()
	createTestInstance(ctx, t, env.st, instID, "Sonarr Act")

	gateCfgJSON := `{"gate":{"enabled":true,"mode":"act","seasons_on_add":1,"look_ahead":1,"promote_whose":"any","exempt_tag":"snatcharr-no-gate"}}`
	rawMsg := json.RawMessage(gateCfgJSON)
	if _, err := env.modulesSvc.Update(ctx, domain.ModuleWatchAware, true, &rawMsg); err != nil {
		t.Fatalf("Update module config: %v", err)
	}

	reader := newFakeSeriesReader()
	env.svc.SetSeriesReader(reader)
	writer := &fakeSeasonWriter{}
	env.svc.SetSeasonWriter(writer)

	instMgr := &fakeInstanceManager{
		instances: []domain.Instance{
			{ID: instID, Kind: domain.KindSonarr, Enabled: true, Name: "Sonarr Act"},
		},
		creds: map[uuid.UUID]instances.Credentials{
			instID: {BaseURL: "http://sonarr.local", APIKey: "k"},
		},
	}
	env.svc.SetInstanceManager(instMgr)

	// Baseline pass
	reader.series[instID] = []watchaware.GateSeries{
		{ID: 10, TvdbID: 1010, Title: "Existing Show", Monitored: true},
	}
	if err := env.svc.GatePass(ctx); err != nil {
		t.Fatalf("baseline GatePass: %v", err)
	}
	if len(writer.getCalls()) != 0 {
		t.Fatalf("baseline should not write to sonarr, got %d calls", len(writer.getCalls()))
	}

	// Incremental pass: new gated series with 3 seasons
	reader.series[instID] = append(reader.series[instID], watchaware.GateSeries{
		ID:        42,
		TvdbID:    1042,
		Title:     "New Show",
		Monitored: true,
		Seasons: []watchaware.GateSeason{
			{Number: 1, Monitored: true, FileCount: 0},
			{Number: 2, Monitored: true, FileCount: 0},
			{Number: 3, Monitored: true, FileCount: 0},
		},
	})

	if err := env.svc.GatePass(ctx); err != nil {
		t.Fatalf("incremental GatePass: %v", err)
	}

	calls := writer.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 writer call, got %d", len(calls))
	}
	if calls[0].seriesID != 42 || !calls[0].seriesMonitored {
		t.Errorf("writer call series = %d, monitored = %v; want 42, true", calls[0].seriesID, calls[0].seriesMonitored)
	}
	if calls[0].seasons[2] != false || calls[0].seasons[3] != false {
		t.Errorf("writer call seasons = %v, want 2:false, 3:false", calls[0].seasons)
	}

	// Check decisions in DB have applied_at set
	decisions, err := env.svc.ListGateDecisions(ctx, &instID, 100)
	if err != nil {
		t.Fatalf("ListGateDecisions: %v", err)
	}
	if len(decisions) != 2 {
		t.Fatalf("expected 2 decisions, got %d", len(decisions))
	}
	for _, d := range decisions {
		if !d.WouldAct {
			t.Errorf("decision %d season %d WouldAct = false, want true", d.SeriesID, d.SeasonNumber)
		}
		if d.AppliedAt == nil {
			t.Errorf("decision %d season %d AppliedAt is nil, want set", d.SeriesID, d.SeasonNumber)
		}
	}

	// Watch season 1 -> promotes season 2
	srcID := uuid.New()
	seedMediaServerWatch(ctx, t, env.st, srcID, 1042, 1)

	err = env.svc.GatePass(ctx)
	if err != nil {
		t.Fatalf("promotion GatePass: %v", err)
	}

	allCalls := writer.getCalls()
	if len(allCalls) != 2 {
		t.Fatalf("expected 2 writer calls after promotion, got %d", len(allCalls))
	}
	promoteCall := allCalls[1]
	if promoteCall.seriesID != 42 || promoteCall.seasons[2] != true {
		t.Errorf("promote writer call = %v, want series 42 season 2: true", promoteCall)
	}

	// Verify promote decision has applied_at
	latestDecs, err := env.svc.ListGateDecisions(ctx, &instID, 100)
	if err != nil {
		t.Fatalf("ListGateDecisions after promotion: %v", err)
	}
	var promoteFound bool
	for _, d := range latestDecs {
		if d.Action == watchaware.GateActionPromote && d.SeasonNumber == 2 {
			promoteFound = true
			if d.AppliedAt == nil {
				t.Error("promote decision AppliedAt is nil, want set")
			}
		}
	}
	if !promoteFound {
		t.Error("expected promote decision for season 2")
	}
}

func TestActPending_Negative_ObserveMode(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := setupTestEnv(ctx, t)

	instID := uuid.New()
	createTestInstance(ctx, t, env.st, instID, "Sonarr Observe")

	gateCfgJSON := `{"gate":{"enabled":true,"mode":"observe","seasons_on_add":1,"look_ahead":1,"promote_whose":"any","exempt_tag":"snatcharr-no-gate"}}`
	rawMsg := json.RawMessage(gateCfgJSON)
	if _, err := env.modulesSvc.Update(ctx, domain.ModuleWatchAware, true, &rawMsg); err != nil {
		t.Fatalf("Update module config: %v", err)
	}

	reader := newFakeSeriesReader()
	env.svc.SetSeriesReader(reader)
	writer := &fakeSeasonWriter{}
	env.svc.SetSeasonWriter(writer)

	instMgr := &fakeInstanceManager{
		instances: []domain.Instance{
			{ID: instID, Kind: domain.KindSonarr, Enabled: true, Name: "Sonarr Observe"},
		},
		creds: map[uuid.UUID]instances.Credentials{
			instID: {BaseURL: "http://sonarr.local", APIKey: "k"},
		},
	}
	env.svc.SetInstanceManager(instMgr)

	// Pre-seed a series that already has a pending decision with would_act=true
	// (e.g. leftover from prior act mode run).
	now := env.clk.Now()
	if err := env.st.Q().UpsertGateSeries(ctx, sqlcgen.UpsertGateSeriesParams{
		InstanceID:      instID,
		SeriesID:        99,
		TvdbID:          1099,
		Title:           "Prior Act Show",
		GateSeason:      1,
		State:           watchaware.GateStateGated,
		AddedBeforeGate: false,
		HeldSeasons:     []int32{2},
		FirstSeenAt:     now,
		UpdatedAt:       now,
	}); err != nil {
		t.Fatalf("UpsertGateSeries: %v", err)
	}
	if err := env.st.Q().UpsertGateDecision(ctx, sqlcgen.UpsertGateDecisionParams{
		ID:           uuid.New(),
		InstanceID:   instID,
		SeriesID:     99,
		SeasonNumber: 2,
		Action:       watchaware.GateActionUnmonitor,
		Reason:       "held by season gate",
		WouldAct:     true,
		DecidedAt:    now,
	}); err != nil {
		t.Fatalf("UpsertGateDecision: %v", err)
	}

	// Baseline
	reader.series[instID] = []watchaware.GateSeries{
		{ID: 1, TvdbID: 1001, Title: "Existing Show", Monitored: true},
		{ID: 99, TvdbID: 1099, Title: "Prior Act Show", Monitored: true},
	}
	if err := env.svc.GatePass(ctx); err != nil {
		t.Fatalf("baseline GatePass: %v", err)
	}

	// New series
	reader.series[instID] = append(reader.series[instID], watchaware.GateSeries{
		ID:        42,
		TvdbID:    1042,
		Title:     "New Show",
		Monitored: true,
		Seasons: []watchaware.GateSeason{
			{Number: 1, Monitored: true},
			{Number: 2, Monitored: true},
		},
	})

	if err := env.svc.GatePass(ctx); err != nil {
		t.Fatalf("GatePass: %v", err)
	}

	if len(writer.getCalls()) != 0 {
		t.Fatalf("observe mode must produce zero writer calls, got %d", len(writer.getCalls()))
	}

	decisions, err := env.svc.ListGateDecisions(ctx, &instID, 100)
	if err != nil {
		t.Fatalf("ListGateDecisions: %v", err)
	}
	for _, d := range decisions {
		if d.SeriesID == 42 && d.WouldAct {
			t.Errorf("observe mode decision for series 42 WouldAct = true, want false")
		}
		if d.AppliedAt != nil {
			t.Errorf("observe mode decision %d season %d AppliedAt = %v, want nil", d.SeriesID, d.SeasonNumber, d.AppliedAt)
		}
	}
}

func TestActPending_Negative_ReleasedSeriesNeverNarrowed(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := setupTestEnv(ctx, t)

	instID := uuid.New()
	createTestInstance(ctx, t, env.st, instID, "Sonarr Released")

	gateCfgJSON := `{"gate":{"enabled":true,"mode":"act","seasons_on_add":1,"look_ahead":1,"promote_whose":"any","exempt_tag":"snatcharr-no-gate"}}`
	rawMsg := json.RawMessage(gateCfgJSON)
	if _, err := env.modulesSvc.Update(ctx, domain.ModuleWatchAware, true, &rawMsg); err != nil {
		t.Fatalf("Update module config: %v", err)
	}

	reader := newFakeSeriesReader()
	env.svc.SetSeriesReader(reader)
	writer := &fakeSeasonWriter{}
	env.svc.SetSeasonWriter(writer)

	instMgr := &fakeInstanceManager{
		instances: []domain.Instance{
			{ID: instID, Kind: domain.KindSonarr, Enabled: true, Name: "Sonarr Released"},
		},
		creds: map[uuid.UUID]instances.Credentials{
			instID: {BaseURL: "http://sonarr.local", APIKey: "k"},
		},
	}
	env.svc.SetInstanceManager(instMgr)

	// Baseline
	now := env.clk.Now()
	reader.series[instID] = []watchaware.GateSeries{
		{ID: 1, TvdbID: 1001, Title: "Existing Show", Monitored: true},
	}
	if err := env.svc.GatePass(ctx); err != nil {
		t.Fatalf("baseline GatePass: %v", err)
	}

	// Add series and release it
	reader.series[instID] = append(reader.series[instID], watchaware.GateSeries{
		ID:        42,
		TvdbID:    1042,
		Title:     "Released Show",
		Monitored: true,
		Seasons: []watchaware.GateSeason{
			{Number: 1, Monitored: true},
			{Number: 2, Monitored: true},
		},
	})
	if err := env.svc.GatePass(ctx); err != nil {
		t.Fatalf("GatePass: %v", err)
	}

	// Now release it
	if _, err := env.svc.ReleaseGateSeries(ctx, instID, 42); err != nil {
		t.Fatalf("ReleaseGateSeries: %v", err)
	}

	// Plant an unmonitor decision with would_act=true and applied_at=nil for series 42
	uID := uuid.New()
	_ = env.st.Q().UpsertGateDecision(ctx, sqlcgen.UpsertGateDecisionParams{
		ID:           uID,
		InstanceID:   instID,
		SeriesID:     42,
		SeasonNumber: 3,
		Action:       watchaware.GateActionUnmonitor,
		WouldAct:     true,
		Reason:       "planted unmonitor",
		DecidedAt:    now,
	})

	// Reset writer calls
	writer.mu.Lock()
	writer.calls = nil
	writer.mu.Unlock()

	// Run pass
	if err := env.svc.GatePass(ctx); err != nil {
		t.Fatalf("GatePass after release: %v", err)
	}

	// Assert no unmonitor calls reached the writer for the released series
	for _, call := range writer.getCalls() {
		if call.seriesID == 42 {
			for sn, mon := range call.seasons {
				if !mon {
					t.Errorf("released series must never be unmonitored: got season %d = false in call %v", sn, call)
				}
			}
		}
	}
}

func TestActPending_Boundary_WriterErrorRetries(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := setupTestEnv(ctx, t)

	instID := uuid.New()
	createTestInstance(ctx, t, env.st, instID, "Sonarr Retry")

	gateCfgJSON := `{"gate":{"enabled":true,"mode":"act","seasons_on_add":1,"look_ahead":1,"promote_whose":"any","exempt_tag":"snatcharr-no-gate"}}`
	rawMsg := json.RawMessage(gateCfgJSON)
	if _, err := env.modulesSvc.Update(ctx, domain.ModuleWatchAware, true, &rawMsg); err != nil {
		t.Fatalf("Update module config: %v", err)
	}

	reader := newFakeSeriesReader()
	env.svc.SetSeriesReader(reader)
	writer := &fakeSeasonWriter{}
	env.svc.SetSeasonWriter(writer)

	instMgr := &fakeInstanceManager{
		instances: []domain.Instance{
			{ID: instID, Kind: domain.KindSonarr, Enabled: true, Name: "Sonarr Retry"},
		},
		creds: map[uuid.UUID]instances.Credentials{
			instID: {BaseURL: "http://sonarr.local", APIKey: "k"},
		},
	}
	env.svc.SetInstanceManager(instMgr)

	// Baseline
	reader.series[instID] = []watchaware.GateSeries{
		{ID: 1, TvdbID: 1001, Title: "Existing Show", Monitored: true},
	}
	if err := env.svc.GatePass(ctx); err != nil {
		t.Fatalf("baseline GatePass: %v", err)
	}

	// Add new series, but writer fails
	reader.series[instID] = append(reader.series[instID], watchaware.GateSeries{
		ID:        42,
		TvdbID:    1042,
		Title:     "Retry Show",
		Monitored: true,
		Seasons: []watchaware.GateSeason{
			{Number: 1, Monitored: true},
			{Number: 2, Monitored: true},
		},
	})
	writer.setErr(errors.New("network timeout"))

	if err := env.svc.GatePass(ctx); err != nil {
		t.Fatalf("GatePass: %v", err)
	}

	// Decision must remain pending (applied_at == nil)
	decs, err := env.svc.ListGateDecisions(ctx, &instID, 100)
	if err != nil {
		t.Fatalf("ListGateDecisions: %v", err)
	}
	if len(decs) == 0 {
		t.Fatal("expected decisions recorded")
	}
	for _, d := range decs {
		if d.AppliedAt != nil {
			t.Errorf("decision AppliedAt = %v, want nil after failure", d.AppliedAt)
		}
	}

	// Clear error and run next pass -> should retry and apply
	writer.setErr(nil)
	err = env.svc.GatePass(ctx)
	if err != nil {
		t.Fatalf("GatePass retry: %v", err)
	}

	decsRetry, err := env.svc.ListGateDecisions(ctx, &instID, 100)
	if err != nil {
		t.Fatalf("ListGateDecisions after retry: %v", err)
	}
	for _, d := range decsRetry {
		if d.AppliedAt == nil {
			t.Errorf("decision %d season %d AppliedAt is nil after retry, want set", d.SeriesID, d.SeasonNumber)
		}
	}
}

func TestReleaseGateSeries(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	env := setupTestEnv(ctx, t)

	instID := uuid.New()
	createTestInstance(ctx, t, env.st, instID, "Sonarr Release")
	now := env.clk.Now()

	// Seed gated series with held seasons [2, 3]
	if err := env.st.Q().UpsertGateSeries(ctx, sqlcgen.UpsertGateSeriesParams{
		InstanceID:      instID,
		SeriesID:        42,
		TvdbID:          1042,
		Title:           "Gated Show",
		GateSeason:      1,
		State:           watchaware.GateStateGated,
		AddedBeforeGate: false,
		HeldSeasons:     []int32{2, 3},
		FirstSeenAt:     now,
		UpdatedAt:       now,
	}); err != nil {
		t.Fatalf("UpsertGateSeries: %v", err)
	}

	// 1. Positive: release gated series
	rec, err := env.svc.ReleaseGateSeries(ctx, instID, 42)
	if err != nil {
		t.Fatalf("ReleaseGateSeries() error = %v", err)
	}
	if rec.State != watchaware.GateStateReleased {
		t.Errorf("rec.State = %q, want %q", rec.State, watchaware.GateStateReleased)
	}
	if len(rec.HeldSeasons) != 0 {
		t.Errorf("rec.HeldSeasons = %v, want empty", rec.HeldSeasons)
	}
	if rec.ReleasedAt == nil {
		t.Error("rec.ReleasedAt is nil, want timestamp")
	}

	// Verify promote decisions were inserted
	decs, err := env.svc.ListGateDecisions(ctx, &instID, 100)
	if err != nil {
		t.Fatalf("ListGateDecisions: %v", err)
	}
	var promoteCount int
	for _, d := range decs {
		if d.SeriesID == 42 && d.Action == watchaware.GateActionPromote {
			promoteCount++
			if d.Reason != "released by admin" {
				t.Errorf("decision reason = %q, want 'released by admin'", d.Reason)
			}
		}
	}
	if promoteCount != 2 {
		t.Errorf("promote decisions count = %d, want 2", promoteCount)
	}

	// 2. Idempotent: call ReleaseGateSeries on already released series
	rec2, err := env.svc.ReleaseGateSeries(ctx, instID, 42)
	if err != nil {
		t.Fatalf("ReleaseGateSeries() second call error = %v", err)
	}
	if rec2.State != watchaware.GateStateReleased {
		t.Errorf("second call rec.State = %q, want %q", rec2.State, watchaware.GateStateReleased)
	}

	// 3. Conflict: call ReleaseGateSeries on series with state 'exempt_tag'
	err = env.st.Q().UpsertGateSeries(ctx, sqlcgen.UpsertGateSeriesParams{
		InstanceID:      instID,
		SeriesID:        43,
		TvdbID:          1043,
		Title:           "Exempt Show",
		GateSeason:      0,
		State:           watchaware.GateStateExemptTag,
		AddedBeforeGate: false,
		HeldSeasons:     []int32{},
		FirstSeenAt:     now,
		UpdatedAt:       now,
	})
	if err != nil {
		t.Fatalf("UpsertGateSeries 43: %v", err)
	}
	_, err = env.svc.ReleaseGateSeries(ctx, instID, 43)
	if !errors.Is(err, domain.ErrConflict) {
		t.Errorf("ReleaseGateSeries(exempt) err = %v, want ErrConflict", err)
	}

	// 4. NotFound: call ReleaseGateSeries on non-existent series
	_, err = env.svc.ReleaseGateSeries(ctx, instID, 99999)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("ReleaseGateSeries(missing) err = %v, want ErrNotFound", err)
	}
}

func seedMediaServerWatch(
	ctx context.Context,
	t *testing.T,
	st *store.Store,
	srcID uuid.UUID,
	tvdbID int,
	seasonNum int,
) {
	t.Helper()
	_, err := st.Pool().Exec(ctx, `
		INSERT INTO watchaware_sources (id, kind, name, url, api_key_enc, enabled, created_at, updated_at)
		VALUES ($1, 'jellyfin', 'jf-promo', 'http://jf.local', '\x0102', true, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT (id) DO NOTHING
	`, srcID)
	if err != nil {
		t.Fatalf("insert watch source: %v", err)
	}

	seriesItemID := fmt.Sprintf("jf-series-%d", tvdbID)
	_, err = st.Pool().Exec(ctx, `
		INSERT INTO watchaware_item_watches (
			source_id, user_id, item_id, user_name, item_type, series_id,
			season_number, episode_number, played, provider_tvdb_id, synced_at
		) VALUES ($1, 'user-1', $2, 'Alice', 'Series', '', 0, 0, false, $3, CURRENT_TIMESTAMP)
		ON CONFLICT (source_id, user_id, item_id) DO NOTHING
	`, srcID, seriesItemID, strconv.Itoa(tvdbID))
	if err != nil {
		t.Fatalf("insert series watch row: %v", err)
	}

	epItemID := fmt.Sprintf("jf-ep-%d-%d-1", tvdbID, seasonNum)
	_, err = st.Pool().Exec(ctx, `
		INSERT INTO watchaware_item_watches (
			source_id, user_id, item_id, user_name, item_type, series_id,
			season_number, episode_number, played, provider_tvdb_id, synced_at
		) VALUES ($1, 'user-1', $2, 'Alice', 'Episode', $3, $4, 1, true, '', CURRENT_TIMESTAMP)
		ON CONFLICT (source_id, user_id, item_id) DO NOTHING
	`, srcID, epItemID, seriesItemID, seasonNum)
	if err != nil {
		t.Fatalf("insert episode watch row: %v", err)
	}
}
