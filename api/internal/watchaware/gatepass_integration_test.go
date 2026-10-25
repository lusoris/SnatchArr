// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package watchaware_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	arr "github.com/golusoris/goenvoy/arr/v2"
	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
	"github.com/lusoris/SnatchArr/api/internal/watchaware"
)

func createTestInstance(ctx context.Context, t *testing.T, st *store.Store, id uuid.UUID, name string) {
	t.Helper()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	_, err := st.Q().CreateInstance(ctx, sqlcgen.CreateInstanceParams{
		ID:        id,
		Kind:      "sonarr",
		Name:      name,
		BaseUrl:   "http://sonarr.local",
		ApiKeyEnc: []byte("encrypted"),
		Enabled:   true,
		Source:    "manual",
		CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}
}

type fakeInstanceManager struct {
	instances []domain.Instance
	creds     map[uuid.UUID]instances.Credentials
	listErr   error
}

func (m *fakeInstanceManager) List(ctx context.Context) ([]domain.Instance, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.instances, nil
}

func (m *fakeInstanceManager) Credentials(ctx context.Context, instID uuid.UUID) (instances.Credentials, error) {
	if c, ok := m.creds[instID]; ok {
		return c, nil
	}
	return instances.Credentials{}, fmt.Errorf("no credentials for %s", instID)
}

type fakeSeriesReader struct {
	mu        sync.Mutex
	callCount int
	tags      map[uuid.UUID][]arr.Tag
	series    map[uuid.UUID][]watchaware.GateSeries
	tagsErr   map[uuid.UUID]error
	seriesErr map[uuid.UUID]error
}

func newFakeSeriesReader() *fakeSeriesReader {
	return &fakeSeriesReader{
		tags:      make(map[uuid.UUID][]arr.Tag),
		series:    make(map[uuid.UUID][]watchaware.GateSeries),
		tagsErr:   make(map[uuid.UUID]error),
		seriesErr: make(map[uuid.UUID]error),
	}
}

func (f *fakeSeriesReader) Tags(ctx context.Context, inst domain.Instance) ([]arr.Tag, error) {
	f.mu.Lock()
	f.callCount++
	f.mu.Unlock()
	if err, ok := f.tagsErr[inst.ID]; ok && err != nil {
		return nil, err
	}
	if tags, ok := f.tags[inst.ID]; ok {
		return tags, nil
	}
	return nil, nil
}

func (f *fakeSeriesReader) Series(ctx context.Context, inst domain.Instance) ([]watchaware.GateSeries, error) {
	f.mu.Lock()
	f.callCount++
	f.mu.Unlock()
	if err, ok := f.seriesErr[inst.ID]; ok && err != nil {
		return nil, err
	}
	if s, ok := f.series[inst.ID]; ok {
		return s, nil
	}
	return nil, nil
}

func (f *fakeSeriesReader) getCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.callCount
}

type fakeEventRecorder struct {
	mu     sync.Mutex
	events []domain.Event
	err    error
}

func (f *fakeEventRecorder) Record(_ context.Context, e domain.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.events = append(f.events, e)
	return nil
}

func (f *fakeEventRecorder) getEvents() []domain.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.Event(nil), f.events...)
}

func TestGatePass_BaselineAndIncremental(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := setupTestEnv(ctx, t)

	inst1ID := uuid.New()
	createTestInstance(ctx, t, env.st, inst1ID, "Sonarr 1")
	instMgr := &fakeInstanceManager{
		instances: []domain.Instance{
			{ID: inst1ID, Kind: domain.KindSonarr, Name: "Sonarr 1", Enabled: true},
		},
		creds: map[uuid.UUID]instances.Credentials{
			inst1ID: {BaseURL: "http://sonarr.local", APIKey: "key-123"},
		},
	}
	reader := newFakeSeriesReader()
	reader.tags[inst1ID] = []arr.Tag{{ID: 42, Label: "snatcharr-no-gate"}}
	reader.series[inst1ID] = []watchaware.GateSeries{
		{
			ID:     1,
			TvdbID: 101,
			Title:  "Existing Series One",
			Seasons: []watchaware.GateSeason{
				{Number: 1, Monitored: true, FileCount: 10},
				{Number: 2, Monitored: true, FileCount: 10},
			},
		},
		{
			ID:     2,
			TvdbID: 102,
			Title:  "Existing Series Two",
			Seasons: []watchaware.GateSeason{
				{Number: 1, Monitored: true, FileCount: 5},
			},
		},
	}

	env.svc.SetInstanceManager(instMgr)
	env.svc.SetSeriesReader(reader)

	// 1. Enable module with gate enabled in config
	gateCfg := map[string]any{
		"sync_interval_minutes": 15,
		"gate": map[string]any{
			"enabled":        true,
			"mode":           "observe",
			"seasons_on_add": 1,
			"exempt_tag":     "snatcharr-no-gate",
		},
	}
	var err error
	rawCfg, err := json.Marshal(gateCfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	rawMsg := json.RawMessage(rawCfg)
	if _, err = env.modulesSvc.Update(ctx, domain.ModuleWatchAware, true, &rawMsg); err != nil {
		t.Fatalf("modulesSvc.Update: %v", err)
	}

	// 2. First pass: baseline -> zero decisions and before_gate rows
	if err = env.svc.ObserveGatePass(ctx); err != nil {
		t.Fatalf("ObserveGatePass baseline: %v", err)
	}

	seriesList, err := env.svc.ListGateSeries(ctx, inst1ID, 100)
	if err != nil {
		t.Fatalf("ListGateSeries: %v", err)
	}
	if len(seriesList) != 2 {
		t.Fatalf("len(seriesList) = %d, want 2", len(seriesList))
	}
	for _, s := range seriesList {
		if s.State != watchaware.GateStateBeforeGate {
			t.Errorf("series %d state = %q, want %q", s.SeriesID, s.State, watchaware.GateStateBeforeGate)
		}
		if !s.AddedBeforeGate {
			t.Errorf("series %d added_before_gate = false, want true", s.SeriesID)
		}
		if s.GateSeason != 0 {
			t.Errorf("series %d gate_season = %d, want 0", s.SeriesID, s.GateSeason)
		}
		if len(s.HeldSeasons) != 0 {
			t.Errorf("series %d held_seasons = %v, want empty", s.SeriesID, s.HeldSeasons)
		}
	}

	decisions, err := env.svc.ListGateDecisions(ctx, &inst1ID, 100)
	if err != nil {
		t.Fatalf("ListGateDecisions: %v", err)
	}
	if len(decisions) != 0 {
		t.Fatalf("len(decisions) = %d, want 0 on baseline", len(decisions))
	}

	// 3. Second pass: add one new 3-season series -> its decisions
	reader.series[inst1ID] = append(reader.series[inst1ID], watchaware.GateSeries{
		ID:     3,
		TvdbID: 103,
		Title:  "New Incoming Series",
		Seasons: []watchaware.GateSeason{
			{Number: 1, Monitored: true, FileCount: 0},
			{Number: 2, Monitored: true, FileCount: 0},
			{Number: 3, Monitored: true, FileCount: 0},
		},
	})

	if err = env.svc.ObserveGatePass(ctx); err != nil {
		t.Fatalf("ObserveGatePass incremental: %v", err)
	}

	seriesList, err = env.svc.ListGateSeries(ctx, inst1ID, 100)
	if err != nil {
		t.Fatalf("ListGateSeries second pass: %v", err)
	}
	if len(seriesList) != 3 {
		t.Fatalf("len(seriesList) = %d, want 3 after adding new series", len(seriesList))
	}

	// Find the new series
	var newSeries *watchaware.GateSeriesRecord
	for i := range seriesList {
		if seriesList[i].SeriesID == 3 {
			newSeries = &seriesList[i]
			break
		}
	}
	if newSeries == nil {
		t.Fatalf("new series with ID 3 not found in gate series list")
	}
	if newSeries.State != watchaware.GateStateGated {
		t.Errorf("newSeries.State = %q, want %q", newSeries.State, watchaware.GateStateGated)
	}
	if newSeries.AddedBeforeGate {
		t.Errorf("newSeries.AddedBeforeGate = true, want false")
	}
	if newSeries.GateSeason != 1 {
		t.Errorf("newSeries.GateSeason = %d, want 1", newSeries.GateSeason)
	}
	if len(newSeries.HeldSeasons) != 2 {
		t.Errorf("newSeries.HeldSeasons = %v, want [2, 3]", newSeries.HeldSeasons)
	}

	decisions, err = env.svc.ListGateDecisions(ctx, &inst1ID, 100)
	if err != nil {
		t.Fatalf("ListGateDecisions: %v", err)
	}
	if len(decisions) != 2 {
		t.Fatalf("len(decisions) = %d, want 2", len(decisions))
	}
	for _, d := range decisions {
		if d.SeriesID != 3 {
			t.Errorf("decision series_id = %d, want 3", d.SeriesID)
		}
		if d.Action != watchaware.GateActionUnmonitor {
			t.Errorf("decision action = %q, want %q", d.Action, watchaware.GateActionUnmonitor)
		}
		if d.WouldAct {
			t.Errorf("decision would_act = true, want false in observe mode")
		}
		if d.Reason != "season after gate" {
			t.Errorf("decision reason = %q, want %q", d.Reason, "season after gate")
		}
	}
}

func TestGatePass_InstanceErrorContinues(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := setupTestEnv(ctx, t)

	instErrID := uuid.New()
	instOkID := uuid.New()
	createTestInstance(ctx, t, env.st, instErrID, "Sonarr Failing")
	createTestInstance(ctx, t, env.st, instOkID, "Sonarr Working")

	instMgr := &fakeInstanceManager{
		instances: []domain.Instance{
			{ID: instErrID, Kind: domain.KindSonarr, Name: "Sonarr Failing", Enabled: true},
			{ID: instOkID, Kind: domain.KindSonarr, Name: "Sonarr Working", Enabled: true},
		},
		creds: map[uuid.UUID]instances.Credentials{
			instErrID: {BaseURL: "http://failing.local", APIKey: "key-err"},
			instOkID:  {BaseURL: "http://working.local", APIKey: "key-ok"},
		},
	}
	reader := newFakeSeriesReader()
	reader.tagsErr[instErrID] = errors.New("connection refused")
	reader.series[instOkID] = []watchaware.GateSeries{
		{
			ID:     50,
			TvdbID: 500,
			Title:  "Working Series",
			Seasons: []watchaware.GateSeason{
				{Number: 1, Monitored: true, FileCount: 1},
			},
		},
	}

	env.svc.SetInstanceManager(instMgr)
	env.svc.SetSeriesReader(reader)

	gateCfg := map[string]any{
		"gate": map[string]any{
			"enabled": true,
			"mode":    "observe",
		},
	}
	rawCfg, _ := json.Marshal(gateCfg)
	rawMsg := json.RawMessage(rawCfg)
	if _, err := env.modulesSvc.Update(ctx, domain.ModuleWatchAware, true, &rawMsg); err != nil {
		t.Fatalf("modulesSvc.Update: %v", err)
	}

	// Run pass: failing instance logs error, working instance succeeds
	if err := env.svc.ObserveGatePass(ctx); err != nil {
		t.Fatalf("ObserveGatePass should not return error on single instance failure: %v", err)
	}

	// Working instance series must be recorded
	okSeries, err := env.svc.ListGateSeries(ctx, instOkID, 100)
	if err != nil {
		t.Fatalf("ListGateSeries(instOkID): %v", err)
	}
	if len(okSeries) != 1 {
		t.Fatalf("len(okSeries) = %d, want 1", len(okSeries))
	}
	if okSeries[0].SeriesID != 50 {
		t.Errorf("okSeries[0].SeriesID = %d, want 50", okSeries[0].SeriesID)
	}

	// Failing instance has zero recorded series
	errSeries, err := env.svc.ListGateSeries(ctx, instErrID, 100)
	if err != nil {
		t.Fatalf("ListGateSeries(instErrID): %v", err)
	}
	if len(errSeries) != 0 {
		t.Fatalf("len(errSeries) = %d, want 0", len(errSeries))
	}
}

func TestGatePass_GateDisabledSkipsReader(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := setupTestEnv(ctx, t)

	instID := uuid.New()
	createTestInstance(ctx, t, env.st, instID, "Sonarr Disabled Gate")
	instMgr := &fakeInstanceManager{
		instances: []domain.Instance{
			{ID: instID, Kind: domain.KindSonarr, Name: "Sonarr Disabled Gate", Enabled: true},
		},
		creds: map[uuid.UUID]instances.Credentials{
			instID: {BaseURL: "http://sonarr.local", APIKey: "key-123"},
		},
	}
	reader := newFakeSeriesReader()
	env.svc.SetInstanceManager(instMgr)
	env.svc.SetSeriesReader(reader)

	// Gate is explicitly disabled
	gateCfg := map[string]any{
		"gate": map[string]any{
			"enabled": false,
		},
	}
	rawCfg, _ := json.Marshal(gateCfg)
	rawMsg2 := json.RawMessage(rawCfg)
	if _, err := env.modulesSvc.Update(ctx, domain.ModuleWatchAware, true, &rawMsg2); err != nil {
		t.Fatalf("modulesSvc.Update: %v", err)
	}

	if err := env.svc.ObserveGatePass(ctx); err != nil {
		t.Fatalf("ObserveGatePass: %v", err)
	}

	if count := reader.getCallCount(); count != 0 {
		t.Fatalf("reader.callCount = %d, want 0 when gate is disabled", count)
	}
}

func TestGatePass_LifecycleCases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		run  func(t *testing.T, ctx context.Context, env *testEnv, instID uuid.UUID, reader *fakeSeriesReader)
	}{
		{
			name: "positive: baseline with empty Sonarr writes baseline row, next pass gates new series",
			run: func(t *testing.T, ctx context.Context, env *testEnv, instID uuid.UUID, reader *fakeSeriesReader) {
				t.Helper()
				reader.series[instID] = []watchaware.GateSeries{}

				if bErr := env.svc.ObserveGatePass(ctx); bErr != nil {
					t.Fatalf("ObserveGatePass baseline: %v", bErr)
				}

				baseline, err := env.st.Q().GetGateBaseline(ctx, instID)
				if err != nil {
					t.Fatalf("GetGateBaseline: %v", err)
				}
				if baseline.SeriesCount != 0 {
					t.Errorf("baseline.SeriesCount = %d, want 0", baseline.SeriesCount)
				}
				if baseline.BaselinedAt.IsZero() {
					t.Errorf("baseline.BaselinedAt is zero")
				}

				series, err := env.svc.ListGateSeries(ctx, instID, 100)
				if err != nil {
					t.Fatalf("ListGateSeries: %v", err)
				}
				if len(series) != 0 {
					t.Errorf("len(series) = %d, want 0", len(series))
				}
				decisions, err := env.svc.ListGateDecisions(ctx, &instID, 100)
				if err != nil {
					t.Fatalf("ListGateDecisions: %v", err)
				}
				if len(decisions) != 0 {
					t.Errorf("len(decisions) = %d, want 0", len(decisions))
				}

				reader.series[instID] = []watchaware.GateSeries{
					{
						ID:     10,
						TvdbID: 1001,
						Title:  "Post-Baseline Show",
						Seasons: []watchaware.GateSeason{
							{Number: 1, Monitored: true, FileCount: 0},
							{Number: 2, Monitored: true, FileCount: 0},
							{Number: 3, Monitored: true, FileCount: 0},
						},
					},
				}

				if incErr := env.svc.ObserveGatePass(ctx); incErr != nil {
					t.Fatalf("ObserveGatePass incremental: %v", incErr)
				}

				series, err = env.svc.ListGateSeries(ctx, instID, 100)
				if err != nil {
					t.Fatalf("ListGateSeries after add: %v", err)
				}
				if len(series) != 1 {
					t.Fatalf("len(series) = %d, want 1", len(series))
				}
				s := series[0]
				if s.State != watchaware.GateStateGated {
					t.Errorf("series.State = %q, want %q", s.State, watchaware.GateStateGated)
				}
				if s.AddedBeforeGate {
					t.Errorf("series.AddedBeforeGate = true, want false")
				}
				if s.GateSeason != 1 {
					t.Errorf("series.GateSeason = %d, want 1", s.GateSeason)
				}
				if len(s.HeldSeasons) != 2 || s.HeldSeasons[0] != 2 || s.HeldSeasons[1] != 3 {
					t.Errorf("series.HeldSeasons = %v, want [2, 3]", s.HeldSeasons)
				}

				decisions, err = env.svc.ListGateDecisions(ctx, &instID, 100)
				if err != nil {
					t.Fatalf("ListGateDecisions: %v", err)
				}
				if len(decisions) != 2 {
					t.Fatalf("len(decisions) = %d, want 2", len(decisions))
				}
			},
		},
		{
			name: "negative: baseline failure rolls back transaction, next pass retries",
			run: func(t *testing.T, ctx context.Context, env *testEnv, instID uuid.UUID, reader *fakeSeriesReader) {
				t.Helper()
				reader.series[instID] = []watchaware.GateSeries{
					{
						ID:     20,
						TvdbID: 2001,
						Title:  "Fail Show One",
						Seasons: []watchaware.GateSeason{
							{Number: 1, Monitored: true, FileCount: 1},
						},
					},
					{
						ID:     21,
						TvdbID: 2002,
						Title:  "Fail Show Two",
						Seasons: []watchaware.GateSeason{
							{Number: 1, Monitored: true, FileCount: 1},
						},
					},
				}

				if _, cErr := env.st.Pool().Exec(ctx, "ALTER TABLE watchaware_gate_baselines ADD CONSTRAINT test_baseline_fail CHECK (series_count < 0)"); cErr != nil {
					t.Fatalf("add constraint: %v", cErr)
				}

				if pErr := env.svc.ObserveGatePass(ctx); pErr != nil {
					t.Fatalf("ObserveGatePass should not return error on per-instance failure: %v", pErr)
				}

				_, bErr := env.st.Q().GetGateBaseline(ctx, instID)
				if !errors.Is(store.MapError(bErr), domain.ErrNotFound) {
					t.Fatalf("GetGateBaseline err = %v, want ErrNotFound", bErr)
				}

				series, err := env.svc.ListGateSeries(ctx, instID, 100)
				if err != nil {
					t.Fatalf("ListGateSeries: %v", err)
				}
				if len(series) != 0 {
					t.Fatalf("len(series) = %d, want 0 after rollback", len(series))
				}

				if _, dErr := env.st.Pool().Exec(ctx, "ALTER TABLE watchaware_gate_baselines DROP CONSTRAINT test_baseline_fail"); dErr != nil {
					t.Fatalf("drop constraint: %v", dErr)
				}

				if rErr := env.svc.ObserveGatePass(ctx); rErr != nil {
					t.Fatalf("ObserveGatePass retry: %v", rErr)
				}

				baseline, err := env.st.Q().GetGateBaseline(ctx, instID)
				if err != nil {
					t.Fatalf("GetGateBaseline after retry: %v", err)
				}
				if baseline.SeriesCount != 2 {
					t.Errorf("baseline.SeriesCount = %d, want 2", baseline.SeriesCount)
				}

				series, err = env.svc.ListGateSeries(ctx, instID, 100)
				if err != nil {
					t.Fatalf("ListGateSeries after retry: %v", err)
				}
				if len(series) != 2 {
					t.Fatalf("len(series) = %d, want 2 after retry", len(series))
				}
				for _, s := range series {
					if s.State != watchaware.GateStateBeforeGate {
						t.Errorf("series %d state = %q, want %q", s.SeriesID, s.State, watchaware.GateStateBeforeGate)
					}
					if !s.AddedBeforeGate {
						t.Errorf("series %d added_before_gate = false, want true", s.SeriesID)
					}
				}
			},
		},
		{
			name: "boundary: held seasons survive decision pruning",
			run: func(t *testing.T, ctx context.Context, env *testEnv, instID uuid.UUID, reader *fakeSeriesReader) {
				t.Helper()
				reader.series[instID] = []watchaware.GateSeries{}
				if bErr := env.svc.ObserveGatePass(ctx); bErr != nil {
					t.Fatalf("ObserveGatePass baseline: %v", bErr)
				}

				reader.series[instID] = []watchaware.GateSeries{
					{
						ID:     30,
						TvdbID: 3001,
						Title:  "Held Seasons Show",
						Seasons: []watchaware.GateSeason{
							{Number: 1, Monitored: true, FileCount: 0},
							{Number: 2, Monitored: true, FileCount: 0},
							{Number: 3, Monitored: true, FileCount: 0},
						},
					},
				}
				if incErr := env.svc.ObserveGatePass(ctx); incErr != nil {
					t.Fatalf("ObserveGatePass incremental: %v", incErr)
				}

				decisions, err := env.svc.ListGateDecisions(ctx, &instID, 100)
				if err != nil {
					t.Fatalf("ListGateDecisions: %v", err)
				}
				if len(decisions) != 2 {
					t.Fatalf("len(decisions) = %d, want 2 before prune", len(decisions))
				}

				oldTime := env.clk.Now().Add(-60 * 24 * time.Hour)
				if _, uErr := env.st.Pool().Exec(ctx, "UPDATE watchaware_gate_decisions SET decided_at = $1 WHERE instance_id = $2", oldTime, instID); uErr != nil {
					t.Fatalf("update decided_at: %v", uErr)
				}

				if pErr := env.svc.ObserveGatePass(ctx); pErr != nil {
					t.Fatalf("ObserveGatePass with prune: %v", pErr)
				}

				decisions, err = env.svc.ListGateDecisions(ctx, &instID, 100)
				if err != nil {
					t.Fatalf("ListGateDecisions: %v", err)
				}
				if len(decisions) != 0 {
					t.Fatalf("len(decisions) = %d, want 0 after prune", len(decisions))
				}

				series, err := env.svc.ListGateSeries(ctx, instID, 100)
				if err != nil {
					t.Fatalf("ListGateSeries: %v", err)
				}
				if len(series) != 1 {
					t.Fatalf("len(series) = %d, want 1", len(series))
				}
				if len(series[0].HeldSeasons) != 2 || series[0].HeldSeasons[0] != 2 || series[0].HeldSeasons[1] != 3 {
					t.Errorf("series.HeldSeasons = %v, want [2, 3] after prune", series[0].HeldSeasons)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			env := setupTestEnv(ctx, t)

			instID := uuid.New()
			createTestInstance(ctx, t, env.st, instID, "Sonarr Test")
			instMgr := &fakeInstanceManager{
				instances: []domain.Instance{
					{ID: instID, Kind: domain.KindSonarr, Name: "Sonarr Test", Enabled: true},
				},
				creds: map[uuid.UUID]instances.Credentials{
					instID: {BaseURL: "http://sonarr.local", APIKey: "key-123"},
				},
			}
			reader := newFakeSeriesReader()
			reader.tags[instID] = []arr.Tag{{ID: 42, Label: "snatcharr-no-gate"}}
			env.svc.SetInstanceManager(instMgr)
			env.svc.SetSeriesReader(reader)

			gateCfg := map[string]any{
				"gate": map[string]any{
					"enabled":        true,
					"mode":           "observe",
					"seasons_on_add": 1,
					"exempt_tag":     "snatcharr-no-gate",
				},
			}
			rawCfg, _ := json.Marshal(gateCfg)
			rawMsg := json.RawMessage(rawCfg)
			if _, err := env.modulesSvc.Update(ctx, domain.ModuleWatchAware, true, &rawMsg); err != nil {
				t.Fatalf("modulesSvc.Update: %v", err)
			}

			tc.run(t, ctx, env, instID, reader)
		})
	}
}

func TestGatePass_GateDecidedEvents(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		run  func(t *testing.T, ctx context.Context, env *testEnv, instID uuid.UUID, reader *fakeSeriesReader, rec *fakeEventRecorder)
	}{
		{
			name: "positive: new gated series with 3 seasons -> exactly 1 event, type watchaware_gate_decided, EntityID series id, detail names seasons 2, 3",
			run: func(t *testing.T, ctx context.Context, env *testEnv, instID uuid.UUID, reader *fakeSeriesReader, rec *fakeEventRecorder) {
				t.Helper()
				if err := env.svc.ObserveGatePass(ctx); err != nil {
					t.Fatalf("baseline pass: %v", err)
				}
				reader.series[instID] = append(reader.series[instID], watchaware.GateSeries{
					ID:     10,
					TvdbID: 110,
					Title:  "Three Seasons Show",
					Seasons: []watchaware.GateSeason{
						{Number: 1, Monitored: true, FileCount: 0},
						{Number: 2, Monitored: true, FileCount: 0},
						{Number: 3, Monitored: true, FileCount: 0},
					},
				})
				if err := env.svc.ObserveGatePass(ctx); err != nil {
					t.Fatalf("incremental pass: %v", err)
				}
				evs := rec.getEvents()
				if len(evs) != 1 {
					t.Fatalf("got %d events, want 1", len(evs))
				}
				e := evs[0]
				if e.Type != "watchaware_gate_decided" {
					t.Errorf("event Type = %q, want %q", e.Type, "watchaware_gate_decided")
				}
				if e.Level != "info" {
					t.Errorf("event Level = %q, want %q", e.Level, "info")
				}
				if e.InstanceID != instID {
					t.Errorf("event InstanceID = %s, want %s", e.InstanceID, instID)
				}
				if e.EntityType != "series" {
					t.Errorf("event EntityType = %q, want %q", e.EntityType, "series")
				}
				if e.EntityID != 10 {
					t.Errorf("event EntityID = %d, want 10", e.EntityID)
				}
				if e.Title != "Three Seasons Show" {
					t.Errorf("event Title = %q, want %q", e.Title, "Three Seasons Show")
				}
				if e.Detail != "would unmonitor seasons 2, 3" {
					t.Errorf("event Detail = %q, want %q", e.Detail, "would unmonitor seasons 2, 3")
				}
			},
		},
		{
			name: "negative: exempt-tag series -> 0 events",
			run: func(t *testing.T, ctx context.Context, env *testEnv, instID uuid.UUID, reader *fakeSeriesReader, rec *fakeEventRecorder) {
				t.Helper()
				if err := env.svc.ObserveGatePass(ctx); err != nil {
					t.Fatalf("baseline pass: %v", err)
				}
				reader.series[instID] = append(reader.series[instID], watchaware.GateSeries{
					ID:     20,
					TvdbID: 120,
					Title:  "Exempt Show",
					TagIDs: []int{42},
					Seasons: []watchaware.GateSeason{
						{Number: 1, Monitored: true, FileCount: 0},
						{Number: 2, Monitored: true, FileCount: 0},
					},
				})
				if err := env.svc.ObserveGatePass(ctx); err != nil {
					t.Fatalf("incremental pass: %v", err)
				}
				if evs := rec.getEvents(); len(evs) != 0 {
					t.Errorf("got %d events for exempt series, want 0", len(evs))
				}
			},
		},
		{
			name: "negative: before_gate baseline pass -> 0 events",
			run: func(t *testing.T, ctx context.Context, env *testEnv, instID uuid.UUID, reader *fakeSeriesReader, rec *fakeEventRecorder) {
				t.Helper()
				reader.series[instID] = []watchaware.GateSeries{
					{
						ID:     30,
						TvdbID: 130,
						Title:  "Initial Show",
						Seasons: []watchaware.GateSeason{
							{Number: 1, Monitored: true, FileCount: 0},
							{Number: 2, Monitored: true, FileCount: 0},
						},
					},
				}
				if err := env.svc.ObserveGatePass(ctx); err != nil {
					t.Fatalf("baseline pass: %v", err)
				}
				if evs := rec.getEvents(); len(evs) != 0 {
					t.Errorf("got %d events on baseline pass, want 0", len(evs))
				}
			},
		},
		{
			name: "negative: files-beyond-gate -> 0 events",
			run: func(t *testing.T, ctx context.Context, env *testEnv, instID uuid.UUID, reader *fakeSeriesReader, rec *fakeEventRecorder) {
				t.Helper()
				if err := env.svc.ObserveGatePass(ctx); err != nil {
					t.Fatalf("baseline pass: %v", err)
				}
				reader.series[instID] = append(reader.series[instID], watchaware.GateSeries{
					ID:     40,
					TvdbID: 140,
					Title:  "Files Beyond Gate Show",
					Seasons: []watchaware.GateSeason{
						{Number: 1, Monitored: true, FileCount: 5},
						{Number: 2, Monitored: true, FileCount: 3},
					},
				})
				if err := env.svc.ObserveGatePass(ctx); err != nil {
					t.Fatalf("incremental pass: %v", err)
				}
				if evs := rec.getEvents(); len(evs) != 0 {
					t.Errorf("got %d events for files-beyond-gate series, want 0", len(evs))
				}
			},
		},
		{
			name: "negative: recorder returning error -> pass still succeeds, rows stored",
			run: func(t *testing.T, ctx context.Context, env *testEnv, instID uuid.UUID, reader *fakeSeriesReader, rec *fakeEventRecorder) {
				t.Helper()
				if err := env.svc.ObserveGatePass(ctx); err != nil {
					t.Fatalf("baseline pass: %v", err)
				}
				rec.err = errors.New("recorder failure")
				reader.series[instID] = append(reader.series[instID], watchaware.GateSeries{
					ID:     50,
					TvdbID: 150,
					Title:  "Recorder Error Show",
					Seasons: []watchaware.GateSeason{
						{Number: 1, Monitored: true, FileCount: 0},
						{Number: 2, Monitored: true, FileCount: 0},
						{Number: 3, Monitored: true, FileCount: 0},
					},
				})
				if err := env.svc.ObserveGatePass(ctx); err != nil {
					t.Fatalf("incremental pass failed despite event recording error: %v", err)
				}
				seriesList, err := env.svc.ListGateSeries(ctx, instID, 100)
				if err != nil {
					t.Fatalf("ListGateSeries: %v", err)
				}
				found := false
				for _, s := range seriesList {
					if s.SeriesID == 50 {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("series 50 was not stored when event recording failed")
				}
				decisions, err := env.svc.ListGateDecisions(ctx, &instID, 100)
				if err != nil {
					t.Fatalf("ListGateDecisions: %v", err)
				}
				if len(decisions) != 2 {
					t.Errorf("got %d decisions, want 2", len(decisions))
				}
			},
		},
		{
			name: "boundary: single-season series (no decisions) -> 0 events",
			run: func(t *testing.T, ctx context.Context, env *testEnv, instID uuid.UUID, reader *fakeSeriesReader, rec *fakeEventRecorder) {
				t.Helper()
				if err := env.svc.ObserveGatePass(ctx); err != nil {
					t.Fatalf("baseline pass: %v", err)
				}
				reader.series[instID] = append(reader.series[instID], watchaware.GateSeries{
					ID:     60,
					TvdbID: 160,
					Title:  "Single Season Mini-Series",
					Seasons: []watchaware.GateSeason{
						{Number: 1, Monitored: true, FileCount: 0},
					},
				})
				if err := env.svc.ObserveGatePass(ctx); err != nil {
					t.Fatalf("incremental pass: %v", err)
				}
				if evs := rec.getEvents(); len(evs) != 0 {
					t.Errorf("got %d events for single-season series, want 0", len(evs))
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			env := setupTestEnv(ctx, t)

			instID := uuid.New()
			createTestInstance(ctx, t, env.st, instID, "Sonarr Event Test")
			instMgr := &fakeInstanceManager{
				instances: []domain.Instance{
					{ID: instID, Kind: domain.KindSonarr, Name: "Sonarr Event Test", Enabled: true},
				},
				creds: map[uuid.UUID]instances.Credentials{
					instID: {BaseURL: "http://sonarr.local", APIKey: "key-123"},
				},
			}
			reader := newFakeSeriesReader()
			reader.tags[instID] = []arr.Tag{{ID: 42, Label: "snatcharr-no-gate"}}
			rec := &fakeEventRecorder{}
			env.svc.SetInstanceManager(instMgr)
			env.svc.SetSeriesReader(reader)
			env.svc.SetEventRecorder(rec)

			gateCfg := map[string]any{
				"gate": map[string]any{
					"enabled":        true,
					"mode":           "observe",
					"seasons_on_add": 1,
					"exempt_tag":     "snatcharr-no-gate",
				},
			}
			rawCfg, _ := json.Marshal(gateCfg)
			rawMsg := json.RawMessage(rawCfg)
			if _, err := env.modulesSvc.Update(ctx, domain.ModuleWatchAware, true, &rawMsg); err != nil {
				t.Fatalf("modulesSvc.Update: %v", err)
			}

			tc.run(t, ctx, env, instID, reader, rec)
		})
	}
}
