// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

//go:build integration

package adult

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonboulle/clockwork"

	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/modules"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

type testEnv struct {
	st         *store.Store
	clk        *clockwork.FakeClock
	ids        id.Generator
	enc        *crypto.Encryptor
	modulesSvc *modules.Service
	svc        *Service
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
	svc := NewService(st, enc, clk, ids, modulesSvc, logger)

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

func insertTestInstance(ctx context.Context, t *testing.T, env *testEnv, instID uuid.UUID, name, endpoint string) {
	t.Helper()
	sealed, err := env.enc.Seal([]byte("test-api-key"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	now := env.clk.Now()
	_, err = env.st.Q().UpsertAdultStashboxInstance(ctx, sqlcgen.UpsertAdultStashboxInstanceParams{
		ID:            instID,
		Name:          name,
		Endpoint:      endpoint,
		ApiKeyEnc:     sealed,
		Enabled:       true,
		SyncFavorites: true,
		Priority:      0,
		CreatedAt:     now,
		UpdatedAt:     now,
	})
	if err != nil {
		t.Fatalf("UpsertAdultStashboxInstance: %v", err)
	}
}

func TestSync_Positive_MultiPageAndCrossInstanceMerge(t *testing.T) {
	ctx := context.Background()
	env := setupTestEnv(ctx, t)

	inst1ID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	inst2ID := uuid.MustParse("00000000-0000-0000-0000-000000000002")

	// Instance 1: 60 performers (50 page 1, 10 page 2), 2 studios
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string `json:"query"`
			Variables struct {
				Input struct {
					Page int `json:"page"`
				} `json:"input"`
			} `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")

		if req.Variables.Input.Page == 1 {
			var performers []map[string]any
			// Performer 1 has twitter URL
			performers = append(performers, map[string]any{
				"id":   "inst1-performer-0",
				"name": "Jane Doe",
				"urls": []any{
					map[string]any{"url": "https://twitter.com/janedoe"},
				},
			})
			for i := 1; i < 50; i++ {
				performers = append(performers, map[string]any{
					"id":   fmt.Sprintf("inst1-performer-%d", i),
					"name": fmt.Sprintf("Performer 1-%d", i),
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"query_performers": map[string]any{
						"count":      60,
						"performers": performers,
					},
					"query_studios": map[string]any{
						"count": 2,
						"studios": []any{
							map[string]any{"id": "inst1-studio-1", "name": "Studio 1-1"},
							map[string]any{"id": "inst1-studio-2", "name": "Studio 1-2"},
						},
					},
				},
			})
			return
		}

		// Page 2
		var performers []map[string]any
		for i := 50; i < 60; i++ {
			performers = append(performers, map[string]any{
				"id":   fmt.Sprintf("inst1-performer-%d", i),
				"name": fmt.Sprintf("Performer 1-%d", i),
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"query_performers": map[string]any{
					"count":      60,
					"performers": performers,
				},
			},
		})
	}))
	t.Cleanup(srv1.Close)

	// Instance 2: 5 performers, 1 studio
	// Performer 1 on Instance 2 has normalized twitter URL matching Instance 1
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"query_performers": map[string]any{
					"count": 5,
					"performers": []any{
						map[string]any{
							"id":   "inst2-performer-0",
							"name": "Jane D.",
							"urls": []any{
								map[string]any{"url": "http://www.twitter.com/janedoe/"},
							},
						},
						map[string]any{"id": "inst2-performer-1", "name": "Unique 2-1"},
					},
				},
				"query_studios": map[string]any{
					"count": 1,
					"studios": []any{
						map[string]any{"id": "inst2-studio-1", "name": "Studio 2-1"},
					},
				},
			},
		})
	}))
	t.Cleanup(srv2.Close)

	insertTestInstance(ctx, t, env, inst1ID, "Instance 1", srv1.URL)
	insertTestInstance(ctx, t, env, inst2ID, "Instance 2", srv2.URL)

	if err := env.svc.SyncAll(ctx); err != nil {
		t.Fatalf("SyncAll: %v", err)
	}

	performers, err := env.st.Q().ListAdultFavoritePerformers(ctx)
	if err != nil {
		t.Fatalf("ListAdultFavoritePerformers: %v", err)
	}
	// 60 from instance 1 + 1 unique from instance 2 = 61 total (Jane Doe merged!)
	if len(performers) != 61 {
		t.Fatalf("expected 61 performers after merge, got %d", len(performers))
	}

	studios, err := env.st.Q().ListAdultFavoriteStudios(ctx)
	if err != nil {
		t.Fatalf("ListAdultFavoriteStudios: %v", err)
	}
	if len(studios) != 3 {
		t.Fatalf("expected 3 studios, got %d", len(studios))
	}

	// Verify links for merged Jane Doe:
	link1, err := env.st.Q().GetAdultPerformerLink(ctx, sqlcgen.GetAdultPerformerLinkParams{
		InstanceID: inst1ID,
		ForeignID:  "inst1-performer-0",
	})
	if err != nil {
		t.Fatalf("link1: %v", err)
	}
	link2, err := env.st.Q().GetAdultPerformerLink(ctx, sqlcgen.GetAdultPerformerLinkParams{
		InstanceID: inst2ID,
		ForeignID:  "inst2-performer-0",
	})
	if err != nil {
		t.Fatalf("link2: %v", err)
	}

	if link1.FavoriteID != link2.FavoriteID {
		t.Fatalf("expected cross-instance merge: link1.FavoriteID (%v) != link2.FavoriteID (%v)",
			link1.FavoriteID, link2.FavoriteID)
	}
}

func TestSync_Negative_401AndGraphQL_ContinueSweep(t *testing.T) {
	ctx := context.Background()
	env := setupTestEnv(ctx, t)

	inst1ID := uuid.MustParse("00000000-0000-0000-0000-000000000011")
	inst2ID := uuid.MustParse("00000000-0000-0000-0000-000000000012")
	inst3ID := uuid.MustParse("00000000-0000-0000-0000-000000000013")

	srv401 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}))
	t.Cleanup(srv401.Close)

	srvGQL := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errors": []any{
				map[string]any{"message": "token expired"},
			},
		})
	}))
	t.Cleanup(srvGQL.Close)

	srvOK := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"query_performers": map[string]any{
					"count": 1,
					"performers": []any{
						map[string]any{"id": "ok-p1", "name": "Successful Performer"},
					},
				},
				"query_studios": map[string]any{
					"count":   0,
					"studios": []any{},
				},
			},
		})
	}))
	t.Cleanup(srvOK.Close)

	insertTestInstance(ctx, t, env, inst1ID, "Instance 401", srv401.URL)
	insertTestInstance(ctx, t, env, inst2ID, "Instance GQL", srvGQL.URL)
	insertTestInstance(ctx, t, env, inst3ID, "Instance OK", srvOK.URL)

	// Direct check that SyncInstance wraps with domain.ErrInvalid
	row1, _ := env.st.Q().GetAdultStashboxInstance(ctx, inst1ID)
	client1, _ := env.svc.ClientForTest(row1)
	err1 := env.svc.SyncInstance(ctx, row1, client1, nil)
	if !errors.Is(err1, domain.ErrInvalid) {
		t.Fatalf("expected 401 error to wrap domain.ErrInvalid, got: %v", err1)
	}

	row2, _ := env.st.Q().GetAdultStashboxInstance(ctx, inst2ID)
	client2, _ := env.svc.ClientForTest(row2)
	err2 := env.svc.SyncInstance(ctx, row2, client2, nil)
	if !errors.Is(err2, domain.ErrInvalid) {
		t.Fatalf("expected GraphQL error to wrap domain.ErrInvalid, got: %v", err2)
	}

	// SyncAll must NOT abort on errors from inst1 and inst2; inst3 must still sync!
	if err := env.svc.SyncAll(ctx); err != nil {
		t.Fatalf("SyncAll failed: %v", err)
	}

	performers, err := env.st.Q().ListAdultFavoritePerformers(ctx)
	if err != nil {
		t.Fatalf("ListAdultFavoritePerformers: %v", err)
	}
	if len(performers) != 1 || performers[0].CanonicalName != "Successful Performer" {
		t.Fatalf("expected Instance OK to have synced 1 performer, got: %+v", performers)
	}
}

func TestSync_Boundary_EmptyAnd500CapAndDisabledTick(t *testing.T) {
	ctx := context.Background()
	env := setupTestEnv(ctx, t)

	var perfReqCount atomic.Int32
	var studioReqCount atomic.Int32
	srvCap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")

		if strings.Contains(req.Query, "queryPerformers") {
			reqNum := perfReqCount.Add(1)
			if reqNum > 10 {
				t.Errorf("boundary violated: performer request %d made beyond 500 cap", reqNum)
			}
			var pageItems []map[string]any
			for i := 0; i < 50; i++ {
				pageItems = append(pageItems, map[string]any{
					"id":   fmt.Sprintf("cap-p-%d-%d", reqNum, i),
					"name": fmt.Sprintf("Cap Performer %d-%d", reqNum, i),
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"query_performers": map[string]any{
						"count":      1000,
						"performers": pageItems,
					},
				},
			})
			return
		}

		reqNum := studioReqCount.Add(1)
		if reqNum > 10 {
			t.Errorf("boundary violated: studio request %d made beyond 500 cap", reqNum)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"query_studios": map[string]any{
					"count":   0,
					"studios": []any{},
				},
			},
		})
	}))
	t.Cleanup(srvCap.Close)

	instCapID := uuid.MustParse("00000000-0000-0000-0000-000000000021")
	insertTestInstance(ctx, t, env, instCapID, "Instance Cap", srvCap.URL)

	// Sync 500 cap
	if err := env.svc.SyncAll(ctx); err != nil {
		t.Fatalf("SyncAll: %v", err)
	}
	if got := perfReqCount.Load(); got != 10 {
		t.Errorf("expected 10 performer requests to reach 500 cap, got %d", got)
	}
	if got := studioReqCount.Load(); got != 1 {
		t.Errorf("expected 1 studio request for empty response, got %d", got)
	}

	// Module disabled: tick does zero requests
	if err := env.modulesSvc.SetEnabled(ctx, "adult", false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	beforeHits := perfReqCount.Load() + studioReqCount.Load()
	if err := env.svc.Tick(ctx); err != nil {
		t.Fatalf("Tick when disabled: %v", err)
	}
	afterHits := perfReqCount.Load() + studioReqCount.Load()
	if afterHits != beforeHits {
		t.Fatalf("expected zero HTTP requests when module disabled; hits grew from %d to %d", beforeHits, afterHits)
	}
}

func TestSearch_FederatedAndDeduplicated(t *testing.T) {
	ctx := context.Background()
	env := setupTestEnv(ctx, t)

	inst1ID := uuid.MustParse("00000000-0000-0000-0000-000000000031")
	inst2ID := uuid.MustParse("00000000-0000-0000-0000-000000000032")

	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"searchPerformer": []any{
					map[string]any{
						"id":   "search-p1",
						"name": "Kira Noir",
						"urls": []any{
							map[string]any{"url": "https://twitter.com/kiranoir"},
						},
					},
				},
				"searchStudio": []any{
					map[string]any{"id": "search-s1", "name": "Brazzers"},
				},
			},
		})
	}))
	t.Cleanup(srv1.Close)

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"searchPerformer": []any{
					map[string]any{
						"id":   "search-p2",
						"name": "Kira Noir",
						"urls": []any{
							map[string]any{"url": "http://www.twitter.com/kiranoir/"},
						},
					},
				},
				"searchStudio": []any{
					map[string]any{"id": "search-s2", "name": "Evil Angel"},
				},
			},
		})
	}))
	t.Cleanup(srv2.Close)

	insertTestInstance(ctx, t, env, inst1ID, "Search 1", srv1.URL)
	insertTestInstance(ctx, t, env, inst2ID, "Search 2", srv2.URL)

	results, err := env.svc.Search(ctx, "kira")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	// Kira Noir was deduplicated across the two instances!
	if len(results.Performers) != 1 {
		t.Fatalf("expected 1 deduplicated performer, got %d", len(results.Performers))
	}
	if len(results.Studios) != 2 {
		t.Fatalf("expected 2 studios, got %d", len(results.Studios))
	}
}

func TestSync_F1_ExactLinkReuse_DisambiguationNoURLs_NoOrphanRows(t *testing.T) {
	ctx := context.Background()
	env := setupTestEnv(ctx, t)
	instID := uuid.MustParse("00000000-0000-0000-0000-000000000041")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"query_performers": map[string]any{
					"count": 1,
					"performers": []any{
						map[string]any{
							"id":             "p-disambig-1",
							"name":           "Jane Doe",
							"disambiguation": "actress",
							"urls":           []any{},
						},
					},
				},
				"query_studios": map[string]any{
					"count":   0,
					"studios": []any{},
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	insertTestInstance(ctx, t, env, instID, "Instance F1", srv.URL)

	if err := env.svc.SyncAll(ctx); err != nil {
		t.Fatalf("SyncAll 1: %v", err)
	}
	perfs1, err := env.st.Q().ListAdultFavoritePerformers(ctx)
	if err != nil || len(perfs1) != 1 {
		t.Fatalf("sweep 1: expected 1 performer, got %d (err: %v)", len(perfs1), err)
	}

	env.clk.Advance(2 * time.Hour)
	if err := env.svc.SyncAll(ctx); err != nil {
		t.Fatalf("SyncAll 2: %v", err)
	}

	perfs2, err := env.st.Q().ListAdultFavoritePerformers(ctx)
	if err != nil {
		t.Fatalf("ListAdultFavoritePerformers: %v", err)
	}
	if len(perfs2) != 1 {
		t.Fatalf("expected exactly 1 favourite row after 2nd sweep (no orphans), got %d", len(perfs2))
	}
	links, err := env.st.Q().ListAdultPerformerLinks(ctx)
	if err != nil {
		t.Fatalf("ListAdultPerformerLinks: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected exactly 1 link row after 2nd sweep, got %d", len(links))
	}
	if perfs2[0].ID != perfs1[0].ID {
		t.Fatalf("favourite ID changed across sweeps: %v != %v", perfs2[0].ID, perfs1[0].ID)
	}
}

func TestSync_F2_FreshService_MergesBySharedExternalURL(t *testing.T) {
	ctx := context.Background()
	env := setupTestEnv(ctx, t)
	inst1ID := uuid.MustParse("00000000-0000-0000-0000-000000000061")
	inst2ID := uuid.MustParse("00000000-0000-0000-0000-000000000062")

	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"query_performers": map[string]any{
					"count": 1,
					"performers": []any{
						map[string]any{
							"id":   "inst1-p1",
							"name": "Alice Singer",
							"urls": []any{map[string]any{"url": "https://twitter.com/alicesinger"}},
						},
					},
				},
				"query_studios": map[string]any{"count": 0, "studios": []any{}},
			},
		})
	}))
	t.Cleanup(srv1.Close)

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"query_performers": map[string]any{
					"count": 1,
					"performers": []any{
						map[string]any{
							"id":   "inst2-p1",
							"name": "A. Singer",
							"urls": []any{map[string]any{"url": "http://www.twitter.com/alicesinger/"}},
						},
					},
				},
				"query_studios": map[string]any{"count": 0, "studios": []any{}},
			},
		})
	}))
	t.Cleanup(srv2.Close)

	insertTestInstance(ctx, t, env, inst1ID, "Instance F2-1", srv1.URL)
	if err := env.svc.SyncAll(ctx); err != nil {
		t.Fatalf("SyncAll inst1: %v", err)
	}

	freshSvc := NewService(env.st, env.enc, env.clk, env.ids, env.modulesSvc, env.logger)
	insertTestInstance(ctx, t, env, inst2ID, "Instance F2-2", srv2.URL)
	if err := freshSvc.SyncAll(ctx); err != nil {
		t.Fatalf("freshSvc.SyncAll: %v", err)
	}

	perfs, err := env.st.Q().ListAdultFavoritePerformers(ctx)
	if err != nil {
		t.Fatalf("ListAdultFavoritePerformers: %v", err)
	}
	if len(perfs) != 1 {
		t.Fatalf("expected 1 merged performer after fresh service restart, got %d", len(perfs))
	}
	links, err := env.st.Q().ListAdultPerformerLinks(ctx)
	if err != nil || len(links) != 2 {
		t.Fatalf("expected 2 links for merged performer, got %d (err: %v)", len(links), err)
	}
	if links[0].FavoriteID != links[1].FavoriteID {
		t.Fatalf("links point to different favorites: %v != %v", links[0].FavoriteID, links[1].FavoriteID)
	}
	if len(perfs[0].Urls) == 0 {
		t.Fatalf("expected persisted URLs on performer, got empty")
	}
}

func TestSync_F3_Boundary500Cap_LeavesItem501Favorite(t *testing.T) {
	ctx := context.Background()
	env := setupTestEnv(ctx, t)
	instID := uuid.MustParse("00000000-0000-0000-0000-000000000071")

	var reqCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reqNum := reqCount.Add(1)
		items := make([]map[string]any, 0, 50)
		for i := range 50 {
			items = append(items, map[string]any{
				"id":   fmt.Sprintf("foreign-%d-%d", reqNum, i),
				"name": fmt.Sprintf("Foreign Performer %d-%d", reqNum, i),
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"query_performers": map[string]any{
					"count":      1000,
					"performers": items,
				},
				"query_studios": map[string]any{"count": 0, "studios": []any{}},
			},
		})
	}))
	t.Cleanup(srv.Close)

	insertTestInstance(ctx, t, env, instID, "Instance F3", srv.URL)

	p501ID := uuid.MustParse("00000000-0000-0000-0000-000000000501")
	oldTime := env.clk.Now().Add(-2 * time.Hour)
	_, err := env.st.Q().UpsertAdultFavoritePerformer(ctx, sqlcgen.UpsertAdultFavoritePerformerParams{
		ID:            p501ID,
		CanonicalName: "Performer 501",
		Urls:          []string{},
		CreatedAt:     oldTime,
		UpdatedAt:     oldTime,
	})
	if err != nil {
		t.Fatalf("UpsertAdultFavoritePerformer: %v", err)
	}
	_, err = env.st.Q().UpsertAdultPerformerLink(ctx, sqlcgen.UpsertAdultPerformerLinkParams{
		FavoriteID:   p501ID,
		InstanceID:   instID,
		ForeignID:    "foreign-501",
		IsFavorite:   true,
		LastSyncedAt: oldTime,
	})
	if err != nil {
		t.Fatalf("UpsertAdultPerformerLink: %v", err)
	}

	env.clk.Advance(1 * time.Hour)
	if err := env.svc.SyncAll(ctx); err != nil {
		t.Fatalf("SyncAll: %v", err)
	}

	link501, err := env.st.Q().GetAdultPerformerLink(ctx, sqlcgen.GetAdultPerformerLinkParams{
		InstanceID: instID,
		ForeignID:  "foreign-501",
	})
	if err != nil {
		t.Fatalf("GetAdultPerformerLink for 501: %v", err)
	}
	if !link501.IsFavorite {
		t.Fatalf("expected item 501 is_favorite to remain true when sweep is capped at 500, but got false")
	}
}
