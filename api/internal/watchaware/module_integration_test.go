// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package watchaware_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golusoris/goenvoy/mediaserver/jellyfin/v3"
	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"

	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/modules"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
	"github.com/lusoris/SnatchArr/api/internal/watchaware"
)

func TestMain(m *testing.M) {
	os.Exit(storetest.Main(m))
}

type testEnv struct {
	st         *store.Store
	clk        *clockwork.FakeClock
	ids        id.Generator
	enc        *crypto.Encryptor
	modulesSvc *modules.Service
	svc        *watchaware.Service
	logger     *slog.Logger
}

func setupTestEnv(ctx context.Context, t *testing.T) *testEnv {
	t.Helper()
	st := storetest.New(t)
	clk := clockwork.NewFakeClockAt(time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC))
	ids := id.New()
	enc, err := crypto.NewEncryptor([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatalf("crypto.NewEncryptor: %v", err)
	}

	reg, err := modules.DefaultRegistry()
	if err != nil {
		t.Fatalf("modules.DefaultRegistry: %v", err)
	}

	modulesSvc := modules.New(st, clk, reg)
	if err := modulesSvc.Ensure(ctx); err != nil {
		t.Fatalf("modulesSvc.Ensure: %v", err)
	}

	logger := slog.New(slog.DiscardHandler)
	svc := watchaware.NewService(st, enc, clk, ids, logger, modulesSvc, nil, nil, nil, nil)

	return &testEnv{
		st:         st,
		clk:        clk,
		ids:        ids,
		enc:        enc,
		modulesSvc: modulesSvc,
		svc:        svc,
		logger:     logger,
	}
}

func newFakeJellyfinServer(t *testing.T, reqCount *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/Users":
			_ = json.NewEncoder(w).Encode([]jellyfin.UserDto{
				{ID: "user-1", Name: "Alice"},
			})
		case "/Items":
			pct := 92.5
			_ = json.NewEncoder(w).Encode(jellyfin.ItemsResult{
				TotalRecordCount: 2,
				StartIndex:       0,
				Items: []jellyfin.BaseItemDto{
					{
						ID:                "ep-101",
						Type:              "Episode",
						SeriesID:          "series-1",
						ParentIndexNumber: 1,
						IndexNumber:       1,
						UserData: &jellyfin.UserItemDataDto{
							Played:                true,
							PlaybackPositionTicks: 1200,
							LastPlayedDate:        "2026-10-24T04:00:00Z",
						},
					},
					{
						ID:                "movie-202",
						Type:              "Movie",
						SeriesID:          "",
						ParentIndexNumber: 0,
						IndexNumber:       0,
						UserData: &jellyfin.UserItemDataDto{
							Played:                false,
							PlayedPercentage:      &pct,
							PlaybackPositionTicks: 5500,
							LastPlayedDate:        "2026-10-24T05:00:00Z",
						},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestService_Tick_Positive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := setupTestEnv(ctx, t)

	// Enable module
	if err := env.modulesSvc.SetEnabled(ctx, domain.ModuleWatchAware, true); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}

	var reqCount atomic.Int32
	srv := newFakeJellyfinServer(t, &reqCount)

	// Create source
	src, err := env.svc.Create(ctx, watchaware.SourceInput{
		Kind:    watchaware.KindJellyfin,
		Name:    "Jellyfin Main",
		URL:     srv.URL,
		APIKey:  "secret-token-123",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("svc.Create: %v", err)
	}
	if !src.HasAPIKey {
		t.Errorf("src.HasAPIKey = false, want true")
	}

	// Execute Tick
	if tickErr := env.svc.Tick(ctx); tickErr != nil {
		t.Fatalf("svc.Tick: %v", tickErr)
	}

	// Assert HTTP calls reached fake server
	if got := reqCount.Load(); got < 2 {
		t.Errorf("reqCount = %d, want >= 2", got)
	}

	// Assert rows landed in watchaware_item_watches
	watches, err := env.st.Q().ListItemWatchesForSource(ctx, src.ID)
	if err != nil {
		t.Fatalf("ListItemWatchesForSource: %v", err)
	}
	if len(watches) != 2 {
		t.Fatalf("len(watches) = %d, want 2", len(watches))
	}

	if watches[0].ItemID != "ep-101" {
		t.Errorf("watches[0].ItemID = %q, want ep-101", watches[0].ItemID)
	}
	if watches[0].ItemType != "Episode" {
		t.Errorf("watches[0].ItemType = %q, want Episode", watches[0].ItemType)
	}
	if !watches[0].Played {
		t.Errorf("watches[0].Played = false, want true")
	}
	if watches[0].ResumePosition != 1200 {
		t.Errorf("watches[0].ResumePosition = %d, want 1200", watches[0].ResumePosition)
	}

	if watches[1].ItemID != "movie-202" {
		t.Errorf("watches[1].ItemID = %q, want movie-202", watches[1].ItemID)
	}
	if watches[1].ItemType != "Movie" {
		t.Errorf("watches[1].ItemType = %q, want Movie", watches[1].ItemType)
	}
	if watches[1].Played {
		t.Errorf("watches[1].Played = true, want false")
	}
	if watches[1].PlayedPercentage == nil {
		t.Errorf("watches[1].PlayedPercentage is nil")
	} else if math.Abs(*watches[1].PlayedPercentage-92.5) > 0.01 {
		t.Errorf("watches[1].PlayedPercentage = %v, want 92.5", *watches[1].PlayedPercentage)
	}

	// Check source sync status
	updatedSrc, err := env.svc.Get(ctx, src.ID)
	if err != nil {
		t.Fatalf("svc.Get: %v", err)
	}
	if updatedSrc.LastSyncAt == nil {
		t.Errorf("updatedSrc.LastSyncAt is nil")
	}
	if updatedSrc.LastError != nil {
		t.Errorf("updatedSrc.LastError = %v, want nil", *updatedSrc.LastError)
	}

	// Check overall SyncStatus
	status, err := env.svc.GetSyncStatus(ctx)
	if err != nil {
		t.Fatalf("svc.GetSyncStatus: %v", err)
	}
	if status.TotalSources != 1 {
		t.Errorf("status.TotalSources = %d, want 1", status.TotalSources)
	}
	if status.EnabledSources != 1 {
		t.Errorf("status.EnabledSources = %d, want 1", status.EnabledSources)
	}
	if status.TotalWatches != 2 {
		t.Errorf("status.TotalWatches = %d, want 2", status.TotalWatches)
	}
	if status.LastSyncAt == nil {
		t.Errorf("status.LastSyncAt is nil")
	}
	if status.LastError != nil {
		t.Errorf("status.LastError = %v, want nil", *status.LastError)
	}
}

func TestService_Tick_Negative_ModuleOff(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := setupTestEnv(ctx, t)

	// Ensure module is OFF
	if err := env.modulesSvc.SetEnabled(ctx, domain.ModuleWatchAware, false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}

	var reqCount atomic.Int32
	srv := newFakeJellyfinServer(t, &reqCount)

	src, err := env.svc.Create(ctx, watchaware.SourceInput{
		Kind:    watchaware.KindJellyfin,
		Name:    "Jellyfin Disabled Mod",
		URL:     srv.URL,
		APIKey:  "secret-token-123",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("svc.Create: %v", err)
	}

	// Execute Tick
	if tickErr := env.svc.Tick(ctx); tickErr != nil {
		t.Fatalf("svc.Tick: %v", tickErr)
	}

	// Zero network calls
	if got := reqCount.Load(); got != 0 {
		t.Errorf("reqCount = %d, want 0", got)
	}

	// Zero sink writes
	count, err := env.st.Q().CountWatchAwareItemWatchesForSource(ctx, src.ID)
	if err != nil {
		t.Fatalf("CountWatchAwareItemWatchesForSource: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}

	// Source last_sync_at remains nil
	s, err := env.svc.Get(ctx, src.ID)
	if err != nil {
		t.Fatalf("svc.Get: %v", err)
	}
	if s.LastSyncAt != nil {
		t.Errorf("s.LastSyncAt = %v, want nil", s.LastSyncAt)
	}
	if s.LastError != nil {
		t.Errorf("s.LastError = %v, want nil", s.LastError)
	}
}

func TestService_Tick_Negative_UnreachableSource(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := setupTestEnv(ctx, t)

	if err := env.modulesSvc.SetEnabled(ctx, domain.ModuleWatchAware, true); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}

	// Source 1: closed server (unreachable)
	closedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedSrv.Close()

	src1, err := env.svc.Create(ctx, watchaware.SourceInput{
		Kind:    watchaware.KindJellyfin,
		Name:    "Unreachable Server",
		URL:     closedSrv.URL,
		APIKey:  "token-1",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("svc.Create src1: %v", err)
	}

	// Source 2: working server
	var reqCount atomic.Int32
	workingSrv := newFakeJellyfinServer(t, &reqCount)

	src2, err := env.svc.Create(ctx, watchaware.SourceInput{
		Kind:    watchaware.KindJellyfin,
		Name:    "Working Server",
		URL:     workingSrv.URL,
		APIKey:  "token-2",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("svc.Create src2: %v", err)
	}

	// Tick
	if tickErr := env.svc.Tick(ctx); tickErr != nil {
		t.Fatalf("svc.Tick: %v", tickErr)
	}

	// Source 1 records last_error
	s1, err := env.svc.Get(ctx, src1.ID)
	if err != nil {
		t.Fatalf("svc.Get src1: %v", err)
	}
	if s1.LastError == nil {
		t.Errorf("s1.LastError is nil, want error recorded")
	}
	if s1.LastSyncAt != nil {
		t.Errorf("s1.LastSyncAt = %v, want nil", s1.LastSyncAt)
	}

	// Source 2 syncs successfully
	s2, err := env.svc.Get(ctx, src2.ID)
	if err != nil {
		t.Fatalf("svc.Get src2: %v", err)
	}
	if s2.LastSyncAt == nil {
		t.Errorf("s2.LastSyncAt is nil, want time")
	}
	if s2.LastError != nil {
		t.Errorf("s2.LastError = %v, want nil", *s2.LastError)
	}

	count2, err := env.st.Q().CountWatchAwareItemWatchesForSource(ctx, src2.ID)
	if err != nil {
		t.Fatalf("CountWatchAwareItemWatchesForSource src2: %v", err)
	}
	if count2 != 2 {
		t.Errorf("count2 = %d, want 2", count2)
	}
}

func TestService_Tick_Boundary_IntervalAndToggle(t *testing.T) {
	t.Parallel()

	// Sub-test: Toggle off -> on -> off across consecutive ticks
	t.Run("toggle off-on-off", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		env := setupTestEnv(ctx, t)

		var reqCount atomic.Int32
		srv := newFakeJellyfinServer(t, &reqCount)

		_, err := env.svc.Create(ctx, watchaware.SourceInput{
			Kind:    watchaware.KindJellyfin,
			Name:    "Boundary Server",
			URL:     srv.URL,
			APIKey:  "token",
			Enabled: true,
		})
		if err != nil {
			t.Fatalf("svc.Create: %v", err)
		}

		reqCount.Store(0)

		// Tick 1: module OFF
		if err := env.modulesSvc.SetEnabled(ctx, domain.ModuleWatchAware, false); err != nil {
			t.Fatalf("SetEnabled false: %v", err)
		}
		if tickErr := env.svc.Tick(ctx); tickErr != nil {
			t.Fatalf("Tick 1: %v", tickErr)
		}
		if got := reqCount.Load(); got != 0 {
			t.Errorf("tick 1 (off) should make 0 calls, got %d", got)
		}

		// Tick 2: module ON
		if err := env.modulesSvc.SetEnabled(ctx, domain.ModuleWatchAware, true); err != nil {
			t.Fatalf("SetEnabled true: %v", err)
		}
		if tickErr := env.svc.Tick(ctx); tickErr != nil {
			t.Fatalf("Tick 2: %v", tickErr)
		}
		callsAfterTick2 := reqCount.Load()
		if callsAfterTick2 < 2 {
			t.Errorf("tick 2 (on) callsAfterTick2 = %d, want >= 2", callsAfterTick2)
		}

		// Tick 3: module OFF
		if err := env.modulesSvc.SetEnabled(ctx, domain.ModuleWatchAware, false); err != nil {
			t.Fatalf("SetEnabled false: %v", err)
		}
		if tickErr := env.svc.Tick(ctx); tickErr != nil {
			t.Fatalf("Tick 3: %v", tickErr)
		}
		if got := reqCount.Load(); got != callsAfterTick2 {
			t.Errorf("tick 3 (off) should make no new calls: got %d, want %d", got, callsAfterTick2)
		}
	})

	// Sub-test: Interval exactly reached vs one second short
	t.Run("interval exact vs short", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		env2 := setupTestEnv(ctx, t)
		if err := env2.modulesSvc.SetEnabled(ctx, domain.ModuleWatchAware, true); err != nil {
			t.Fatalf("SetEnabled: %v", err)
		}

		var reqCount2 atomic.Int32
		srv2 := newFakeJellyfinServer(t, &reqCount2)

		_, err := env2.svc.Create(ctx, watchaware.SourceInput{
			Kind:    watchaware.KindJellyfin,
			Name:    "Boundary Server 2",
			URL:     srv2.URL,
			APIKey:  "token",
			Enabled: true,
		})
		if err != nil {
			t.Fatalf("svc.Create: %v", err)
		}

		// Set custom config of 10 minutes
		cfgRaw := json.RawMessage(`{"sync_interval_minutes": 10}`)
		if _, err := env2.modulesSvc.Update(ctx, domain.ModuleWatchAware, true, &cfgRaw); err != nil {
			t.Fatalf("modulesSvc.Update: %v", err)
		}

		// First tick sets lastPass
		if tickErr := env2.svc.Tick(ctx); tickErr != nil {
			t.Fatalf("Tick initial: %v", tickErr)
		}
		initialCalls := reqCount2.Load()
		if initialCalls < 2 {
			t.Fatalf("initialCalls = %d, want >= 2", initialCalls)
		}

		// Advance clock by 9 minutes 59 seconds (1 second short)
		env2.clk.Advance(9*time.Minute + 59*time.Second)
		if tickErr := env2.svc.Tick(ctx); tickErr != nil {
			t.Fatalf("Tick 9m59s: %v", tickErr)
		}
		if got := reqCount2.Load(); got != initialCalls {
			t.Errorf("1 second short of interval must skip: got %d, want %d", got, initialCalls)
		}

		// Advance clock by 1 second (interval exactly reached: 10 minutes total)
		env2.clk.Advance(1 * time.Second)
		if tickErr := env2.svc.Tick(ctx); tickErr != nil {
			t.Fatalf("Tick 10m: %v", tickErr)
		}
		if got := reqCount2.Load(); got <= initialCalls {
			t.Errorf("interval exactly reached must sync: got %d, want > %d", got, initialCalls)
		}
	})
}

func TestService_Source_CRUD(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := setupTestEnv(ctx, t)

	// Table-driven validation tests (HISS-15)
	tests := []struct {
		name    string
		input   watchaware.SourceInput
		wantErr error
	}{
		{
			name: "valid jellyfin",
			input: watchaware.SourceInput{
				Kind:    watchaware.KindJellyfin,
				Name:    "Jellyfin Valid",
				URL:     "http://jellyfin.local:8096",
				APIKey:  "secret-123",
				Enabled: true,
			},
			wantErr: nil,
		},
		{
			name: "valid emby",
			input: watchaware.SourceInput{
				Kind:    watchaware.KindEmby,
				Name:    "Emby Valid",
				URL:     "http://emby.local:8096",
				APIKey:  "secret-456",
				Enabled: false,
			},
			wantErr: nil,
		},
		{
			name: "invalid kind",
			input: watchaware.SourceInput{
				Kind:    "plex",
				Name:    "Plex Invalid",
				URL:     "http://plex.local:32400",
				APIKey:  "secret",
				Enabled: true,
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "empty name",
			input: watchaware.SourceInput{
				Kind:    watchaware.KindJellyfin,
				Name:    "   ",
				URL:     "http://jellyfin.local:8096",
				APIKey:  "secret",
				Enabled: true,
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "invalid url",
			input: watchaware.SourceInput{
				Kind:    watchaware.KindJellyfin,
				Name:    "Bad URL",
				URL:     "://invalid-url",
				APIKey:  "secret",
				Enabled: true,
			},
			wantErr: domain.ErrInvalid,
		},
		{
			name: "missing api key on create",
			input: watchaware.SourceInput{
				Kind:    watchaware.KindJellyfin,
				Name:    "No Key",
				URL:     "http://jellyfin.local:8096",
				APIKey:  "",
				Enabled: true,
			},
			wantErr: domain.ErrInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src, err := env.svc.Create(ctx, tt.input)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v, got nil", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if src.ID == uuid.Nil {
				t.Errorf("src.ID is nil UUID")
			}
			if !src.HasAPIKey {
				t.Errorf("src.HasAPIKey = false, want true")
			}

			// Get
			fetched, err := env.svc.Get(ctx, src.ID)
			if err != nil {
				t.Fatalf("svc.Get: %v", err)
			}
			if fetched.Name != src.Name {
				t.Errorf("fetched.Name = %q, want %q", fetched.Name, src.Name)
			}
			if !fetched.HasAPIKey {
				t.Errorf("fetched.HasAPIKey = false, want true")
			}

			// Update keeping key
			updated, err := env.svc.Update(ctx, src.ID, watchaware.SourceInput{
				Kind:    src.Kind,
				Name:    src.Name + " Updated",
				URL:     src.URL,
				APIKey:  "", // keeps existing key
				Enabled: false,
			})
			if err != nil {
				t.Fatalf("svc.Update: %v", err)
			}
			if updated.Name != src.Name+" Updated" {
				t.Errorf("updated.Name = %q, want %q", updated.Name, src.Name+" Updated")
			}
			if updated.Enabled {
				t.Errorf("updated.Enabled = true, want false")
			}
			if !updated.HasAPIKey {
				t.Errorf("updated.HasAPIKey = false, want true")
			}

			// Delete
			if err := env.svc.Delete(ctx, src.ID); err != nil {
				t.Fatalf("svc.Delete: %v", err)
			}

			// Get deleted -> not found
			if _, err := env.svc.Get(ctx, src.ID); !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("err after delete = %v, want ErrNotFound", err)
			}
		})
	}
}
