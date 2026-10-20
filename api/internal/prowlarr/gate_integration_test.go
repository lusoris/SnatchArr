// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package prowlarr_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golusoris/goenvoy/arr/v2"
	"github.com/google/uuid"

	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/prowlarr"
	"github.com/lusoris/SnatchArr/api/internal/prowlarrclient"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

type fakeProwlarrServer struct {
	mu           sync.Mutex
	statusVer    string
	indexers     []map[string]any
	stats        []map[string]any
	statsHour    []map[string]any
	statuses     []map[string]any
	applications []map[string]any
	failRequests bool
}

func newFakeProwlarr(t *testing.T) (*fakeProwlarrServer, *httptest.Server) {
	t.Helper()
	fps := &fakeProwlarrServer{statusVer: "1.15.0"}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/system/status", func(w http.ResponseWriter, _ *http.Request) {
		fps.mu.Lock()
		defer fps.mu.Unlock()
		if fps.failRequests {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"version": fps.statusVer})
	})
	mux.HandleFunc("GET /api/v1/indexer", func(w http.ResponseWriter, _ *http.Request) {
		fps.mu.Lock()
		defer fps.mu.Unlock()
		if fps.failRequests {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(fps.indexers)
	})
	mux.HandleFunc("GET /api/v1/indexerstats", func(w http.ResponseWriter, r *http.Request) {
		fps.mu.Lock()
		defer fps.mu.Unlock()
		if fps.failRequests {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		stats := fps.stats
		if fps.statsHour != nil && r.URL.Query().Get("startDate") != "" {
			start, _ := time.Parse(time.RFC3339, r.URL.Query().Get("startDate"))
			end, _ := time.Parse(time.RFC3339, r.URL.Query().Get("endDate"))
			if !start.IsZero() && !end.IsZero() && end.Sub(start) <= 2*time.Hour {
				stats = fps.statsHour
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"indexers": stats})
	})
	mux.HandleFunc("GET /api/v1/indexerstatus", func(w http.ResponseWriter, _ *http.Request) {
		fps.mu.Lock()
		defer fps.mu.Unlock()
		if fps.failRequests {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(fps.statuses)
	})
	mux.HandleFunc("GET /api/v1/applications", func(w http.ResponseWriter, _ *http.Request) {
		fps.mu.Lock()
		defer fps.mu.Unlock()
		if fps.failRequests {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		apps := fps.applications
		if apps == nil {
			apps = []map[string]any{}
		}
		_ = json.NewEncoder(w).Encode(apps)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return fps, srv
}

type fakeArrServer struct {
	mu       sync.Mutex
	indexers []map[string]any
}

func newFakeArr(t *testing.T) (*fakeArrServer, *httptest.Server) {
	t.Helper()
	fas := &fakeArrServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v3/system/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"appName": "Sonarr", "version": "3.0.0"})
	})
	mux.HandleFunc("GET /api/v3/indexer", func(w http.ResponseWriter, _ *http.Request) {
		fas.mu.Lock()
		defer fas.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(fas.indexers)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return fas, srv
}

type gateTestCase struct {
	name         string
	setupProwl   func(p *fakeProwlarrServer, clk clock.Clock)
	setupArr     func(a *fakeArrServer, prowlURL string)
	disableLink  bool
	killProwlarr bool
	wantAllowed  bool
	wantRefusal  string
	wantMessage  string
}

func runGateTestCase(t *testing.T, tc gateTestCase) {
	t.Helper()
	fakeProwl, prowlSrv := newFakeProwlarr(t)
	fakeArr, arrSrv := newFakeArr(t)

	st := storetest.New(t)
	clk := clock.NewFake()
	enc, err := crypto.NewEncryptor(bytes.Repeat([]byte{0x55}, 32))
	if err != nil {
		t.Fatalf("encryptor: %v", err)
	}
	prober := arrclient.NewGoenvoyProber(arrclient.DefaultOptions())
	instSvc := instances.New(st, enc, clk, id.New(), prober, slog.New(slog.DiscardHandler))
	prowlClient := prowlarrclient.New(5*time.Second, "gate-test")
	cfg := config.Options{
		Prowlarr: config.ProwlarrOptions{LimitThreshold: 0.9},
	}
	svc := prowlarr.New(st, enc, clk, id.New(), prowlClient, prober, instSvc, slog.New(slog.DiscardHandler), cfg)

	inst, err := instSvc.Create(t.Context(), instances.Input{
		Name:    "sonarr-integration",
		Kind:    domain.KindSonarr,
		BaseURL: arrSrv.URL,
		APIKey:  "sonarr-secret",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}

	prowlLink, err := svc.Create(t.Context(), prowlarr.Input{
		Name:    "prowlarr-integration",
		BaseURL: prowlSrv.URL,
		APIKey:  "prowl-secret",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("create prowlarr link: %v", err)
	}

	fakeProwl.mu.Lock()
	fakeProwl.failRequests = tc.killProwlarr
	fakeProwl.mu.Unlock()

	if tc.setupProwl != nil {
		fakeProwl.mu.Lock()
		tc.setupProwl(fakeProwl, clk)
		fakeProwl.mu.Unlock()
	}
	if tc.setupArr != nil {
		fakeArr.mu.Lock()
		tc.setupArr(fakeArr, prowlSrv.URL)
		fakeArr.mu.Unlock()
	}

	if tc.disableLink {
		if _, updateErr := svc.Update(t.Context(), prowlLink.ID, prowlarr.Input{
			Name:    prowlLink.Name,
			BaseURL: prowlLink.BaseURL,
			Enabled: false,
		}); updateErr != nil {
			t.Fatalf("update link: %v", updateErr)
		}
	}

	allowed, refusal, err := svc.Allow(t.Context(), inst, domain.SnatchMissing)
	if err != nil {
		t.Fatalf("Allow() unexpected err: %v", err)
	}
	if allowed != tc.wantAllowed {
		t.Errorf("Allow() = %v, want %v (refusal: %+v)", allowed, tc.wantAllowed, refusal)
	}
	if !tc.wantAllowed && tc.wantRefusal != "" && refusal.Code != tc.wantRefusal {
		t.Errorf("refusal.Code = %q, want %q", refusal.Code, tc.wantRefusal)
	}
	if !tc.wantAllowed && tc.wantMessage != "" && refusal.Message != tc.wantMessage {
		t.Errorf("refusal.Message = %q, want %q", refusal.Message, tc.wantMessage)
	}
}

func TestGate_Integration(t *testing.T) {
	t.Parallel()

	tests := []gateTestCase{
		{
			name: "all limited exhausted -> defer indexer_limit",
			setupArr: func(fakeArr *fakeArrServer, prowlURL string) {
				fakeArr.indexers = []map[string]any{
					{"id": 10, "name": "Sonarr Idx 1", "fields": []map[string]any{{"name": "baseUrl", "value": prowlURL + "/1/"}}},
				}
			},
			setupProwl: func(fakeProwl *fakeProwlarrServer, _ clock.Clock) {
				fakeProwl.indexers = []map[string]any{
					{
						"id": 1, "name": "Prowl Idx 1", "enable": true,
						"fields": []map[string]any{
							{"name": "baseSettings.queryLimit", "value": 100},
							{"name": "baseSettings.limitsUnit", "value": 0},
						},
					},
				}
				fakeProwl.stats = []map[string]any{
					{"indexerId": 1, "numberOfQueries": 95, "numberOfRssQueries": 0, "numberOfGrabs": 0},
				}
				fakeProwl.statuses = []map[string]any{}
			},
			wantAllowed: false,
			wantRefusal: snatch.GateIndexerLimit,
		},
		{
			name: "one with headroom -> allow",
			setupArr: func(fakeArr *fakeArrServer, prowlURL string) {
				fakeArr.indexers = []map[string]any{
					{"id": 10, "name": "Sonarr Idx 1", "fields": []map[string]any{{"name": "baseUrl", "value": prowlURL + "/1/"}}},
					{"id": 20, "name": "Sonarr Idx 2", "fields": []map[string]any{{"name": "baseUrl", "value": prowlURL + "/2/"}}},
				}
			},
			setupProwl: func(fakeProwl *fakeProwlarrServer, _ clock.Clock) {
				fakeProwl.indexers = []map[string]any{
					{
						"id": 1, "name": "Prowl Idx 1", "enable": true,
						"fields": []map[string]any{{"name": "baseSettings.queryLimit", "value": 100}, {"name": "baseSettings.limitsUnit", "value": 0}},
					},
					{
						"id": 2, "name": "Prowl Idx 2", "enable": true,
						"fields": []map[string]any{{"name": "baseSettings.queryLimit", "value": 100}, {"name": "baseSettings.limitsUnit", "value": 0}},
					},
				}
				fakeProwl.stats = []map[string]any{
					{"indexerId": 1, "numberOfQueries": 95, "numberOfRssQueries": 0},
					{"indexerId": 2, "numberOfQueries": 50, "numberOfRssQueries": 0},
				}
				fakeProwl.statuses = []map[string]any{}
			},
			wantAllowed: true,
		},
		{
			name: "one unlimited indexer -> allow",
			setupArr: func(fakeArr *fakeArrServer, prowlURL string) {
				fakeArr.indexers = []map[string]any{
					{"id": 10, "name": "Sonarr Idx 1", "fields": []map[string]any{{"name": "baseUrl", "value": prowlURL + "/1/"}}},
					{"id": 30, "name": "Sonarr Idx Unlimited", "fields": []map[string]any{{"name": "baseUrl", "value": prowlURL + "/3/"}}},
				}
			},
			setupProwl: func(fakeProwl *fakeProwlarrServer, _ clock.Clock) {
				fakeProwl.indexers = []map[string]any{
					{
						"id": 1, "name": "Prowl Idx 1", "enable": true,
						"fields": []map[string]any{{"name": "baseSettings.queryLimit", "value": 100}, {"name": "baseSettings.limitsUnit", "value": 0}},
					},
					{
						"id": 3, "name": "Prowl Idx Unlimited", "enable": true,
						"fields": []map[string]any{},
					},
				}
				fakeProwl.stats = []map[string]any{
					{"indexerId": 1, "numberOfQueries": 95, "numberOfRssQueries": 0},
				}
				fakeProwl.statuses = []map[string]any{}
			},
			wantAllowed: true,
		},
		{
			name: "used exactly 90 % of limit with threshold 0.9 -> defer (>=)",
			setupArr: func(fakeArr *fakeArrServer, prowlURL string) {
				fakeArr.indexers = []map[string]any{
					{"id": 10, "name": "Sonarr Idx 1", "fields": []map[string]any{{"name": "baseUrl", "value": prowlURL + "/1/"}}},
				}
			},
			setupProwl: func(fakeProwl *fakeProwlarrServer, _ clock.Clock) {
				fakeProwl.indexers = []map[string]any{
					{
						"id": 1, "name": "Prowl Idx 1", "enable": true,
						"fields": []map[string]any{{"name": "baseSettings.queryLimit", "value": 100}, {"name": "baseSettings.limitsUnit", "value": 0}},
					},
				}
				fakeProwl.stats = []map[string]any{
					{"indexerId": 1, "numberOfQueries": 90, "numberOfRssQueries": 0},
				}
				fakeProwl.statuses = []map[string]any{}
			},
			wantAllowed: false,
			wantRefusal: snatch.GateIndexerLimit,
		},
		{
			name: "used 89 % of limit with threshold 0.9 -> allow",
			setupArr: func(fakeArr *fakeArrServer, prowlURL string) {
				fakeArr.indexers = []map[string]any{
					{"id": 10, "name": "Sonarr Idx 1", "fields": []map[string]any{{"name": "baseUrl", "value": prowlURL + "/1/"}}},
				}
			},
			setupProwl: func(fakeProwl *fakeProwlarrServer, _ clock.Clock) {
				fakeProwl.indexers = []map[string]any{
					{
						"id": 1, "name": "Prowl Idx 1", "enable": true,
						"fields": []map[string]any{{"name": "baseSettings.queryLimit", "value": 100}, {"name": "baseSettings.limitsUnit", "value": 0}},
					},
				}
				fakeProwl.stats = []map[string]any{
					{"indexerId": 1, "numberOfQueries": 89, "numberOfRssQueries": 0},
				}
				fakeProwl.statuses = []map[string]any{}
			},
			wantAllowed: true,
		},
		{
			name: "disabledTill future -> exhausted",
			setupArr: func(fakeArr *fakeArrServer, prowlURL string) {
				fakeArr.indexers = []map[string]any{
					{"id": 10, "name": "Sonarr Idx 1", "fields": []map[string]any{{"name": "baseUrl", "value": prowlURL + "/1/"}}},
				}
			},
			setupProwl: func(fakeProwl *fakeProwlarrServer, clk clock.Clock) {
				fakeProwl.indexers = []map[string]any{
					{
						"id": 1, "name": "Prowl Idx 1", "enable": true,
						"fields": []map[string]any{{"name": "baseSettings.queryLimit", "value": 100}, {"name": "baseSettings.limitsUnit", "value": 0}},
					},
				}
				fakeProwl.stats = []map[string]any{
					{"indexerId": 1, "numberOfQueries": 10, "numberOfRssQueries": 0},
				}
				fakeProwl.statuses = []map[string]any{
					{"indexerId": 1, "disabledTill": clk.Now().Add(2 * time.Hour).Format(time.RFC3339)},
				}
			},
			wantAllowed: false,
			wantRefusal: snatch.GateIndexerLimit,
		},
		{
			name: "disabledTill past -> not",
			setupArr: func(fakeArr *fakeArrServer, prowlURL string) {
				fakeArr.indexers = []map[string]any{
					{"id": 10, "name": "Sonarr Idx 1", "fields": []map[string]any{{"name": "baseUrl", "value": prowlURL + "/1/"}}},
				}
			},
			setupProwl: func(fakeProwl *fakeProwlarrServer, clk clock.Clock) {
				fakeProwl.indexers = []map[string]any{
					{
						"id": 1, "name": "Prowl Idx 1", "enable": true,
						"fields": []map[string]any{{"name": "baseSettings.queryLimit", "value": 100}, {"name": "baseSettings.limitsUnit", "value": 0}},
					},
				}
				fakeProwl.stats = []map[string]any{
					{"indexerId": 1, "numberOfQueries": 10, "numberOfRssQueries": 0},
				}
				fakeProwl.statuses = []map[string]any{
					{"indexerId": 1, "disabledTill": clk.Now().Add(-2 * time.Hour).Format(time.RFC3339)},
				}
			},
			wantAllowed: true,
		},
		{
			name:        "no link -> allow",
			disableLink: true,
			setupArr: func(fakeArr *fakeArrServer, prowlURL string) {
				fakeArr.indexers = []map[string]any{
					{"id": 10, "name": "Sonarr Idx 1", "fields": []map[string]any{{"name": "baseUrl", "value": prowlURL + "/1/"}}},
				}
			},
			setupProwl: func(fakeProwl *fakeProwlarrServer, _ clock.Clock) {
				fakeProwl.indexers = []map[string]any{}
			},
			wantAllowed: true,
		},
		{
			name:         "Prowlarr down -> allow",
			killProwlarr: true,
			setupArr: func(fakeArr *fakeArrServer, prowlURL string) {
				fakeArr.indexers = []map[string]any{
					{"id": 10, "name": "Sonarr Idx 1", "fields": []map[string]any{{"name": "baseUrl", "value": prowlURL + "/1/"}}},
				}
			},
			setupProwl: func(fakeProwl *fakeProwlarrServer, _ clock.Clock) {
				fakeProwl.failRequests = true
			},
			wantAllowed: true,
		},
		{
			name: "app prowlarrUrl matching -> matched id 3",
			setupArr: func(fakeArr *fakeArrServer, _ string) {
				fakeArr.indexers = []map[string]any{
					{"id": 10, "name": "Sonarr Idx 3", "fields": []map[string]any{{"name": "baseUrl", "value": "http://prowlarr-app:9696/3/"}}},
				}
			},
			setupProwl: func(fakeProwl *fakeProwlarrServer, _ clock.Clock) {
				fakeProwl.applications = []map[string]any{
					{
						"id": 1, "name": "Sonarr",
						"fields": []map[string]any{{"name": "prowlarrUrl", "value": "http://prowlarr-app:9696"}},
					},
				}
				fakeProwl.indexers = []map[string]any{
					{
						"id": 3, "name": "Prowl Idx 3", "enable": true,
						"fields": []map[string]any{{"name": "baseSettings.queryLimit", "value": 100}, {"name": "baseSettings.limitsUnit", "value": 0}},
					},
				}
				fakeProwl.stats = []map[string]any{
					{"indexerId": 3, "numberOfQueries": 95, "numberOfRssQueries": 0},
				}
				fakeProwl.statuses = []map[string]any{}
			},
			wantAllowed: false,
			wantRefusal: snatch.GateIndexerLimit,
		},
		{
			name: "all Prowlarr exhausted + direct automatic-search indexer -> allow",
			setupArr: func(fakeArr *fakeArrServer, prowlURL string) {
				tVal := true
				fakeArr.indexers = []map[string]any{
					{"id": 10, "name": "Prowlarr Idx 1", "fields": []map[string]any{{"name": "baseUrl", "value": prowlURL + "/1/"}}},
					{"id": 20, "name": "Direct Idx", "enableAutomaticSearch": &tVal, "fields": []map[string]any{{"name": "baseUrl", "value": "http://direct-tracker/api"}}},
				}
			},
			setupProwl: func(fakeProwl *fakeProwlarrServer, _ clock.Clock) {
				fakeProwl.indexers = []map[string]any{
					{
						"id": 1, "name": "Prowl Idx 1", "enable": true,
						"fields": []map[string]any{{"name": "baseSettings.queryLimit", "value": 100}, {"name": "baseSettings.limitsUnit", "value": 0}},
					},
				}
				fakeProwl.stats = []map[string]any{
					{"indexerId": 1, "numberOfQueries": 95, "numberOfRssQueries": 0},
				}
				fakeProwl.statuses = []map[string]any{}
			},
			wantAllowed: true,
		},
		{
			name: "all Prowlarr exhausted + direct indexer with enableAutomaticSearch=false -> defer",
			setupArr: func(fakeArr *fakeArrServer, prowlURL string) {
				fVal := false
				fakeArr.indexers = []map[string]any{
					{"id": 10, "name": "Prowlarr Idx 1", "fields": []map[string]any{{"name": "baseUrl", "value": prowlURL + "/1/"}}},
					{"id": 20, "name": "Direct Idx", "enableAutomaticSearch": &fVal, "fields": []map[string]any{{"name": "baseUrl", "value": "http://direct-tracker/api"}}},
				}
			},
			setupProwl: func(fakeProwl *fakeProwlarrServer, _ clock.Clock) {
				fakeProwl.indexers = []map[string]any{
					{
						"id": 1, "name": "Prowl Idx 1", "enable": true,
						"fields": []map[string]any{{"name": "baseSettings.queryLimit", "value": 100}, {"name": "baseSettings.limitsUnit", "value": 0}},
					},
				}
				fakeProwl.stats = []map[string]any{
					{"indexerId": 1, "numberOfQueries": 95, "numberOfRssQueries": 0},
				}
				fakeProwl.statuses = []map[string]any{}
			},
			wantAllowed: false,
			wantRefusal: snatch.GateIndexerLimit,
		},
		{
			name: "unlimited but disabledTill future + all limited exhausted -> defer",
			setupArr: func(fakeArr *fakeArrServer, prowlURL string) {
				fakeArr.indexers = []map[string]any{
					{"id": 10, "name": "Sonarr Idx 1", "fields": []map[string]any{{"name": "baseUrl", "value": prowlURL + "/1/"}}},
					{"id": 20, "name": "Sonarr Idx Unlimited", "fields": []map[string]any{{"name": "baseUrl", "value": prowlURL + "/2/"}}},
				}
			},
			setupProwl: func(fakeProwl *fakeProwlarrServer, clk clock.Clock) {
				fakeProwl.indexers = []map[string]any{
					{
						"id": 1, "name": "Prowl Idx 1", "enable": true,
						"fields": []map[string]any{{"name": "baseSettings.queryLimit", "value": 100}, {"name": "baseSettings.limitsUnit", "value": 0}},
					},
					{
						"id": 2, "name": "Prowl Idx Unlimited", "enable": true,
						"fields": []map[string]any{},
					},
				}
				fakeProwl.stats = []map[string]any{
					{"indexerId": 1, "numberOfQueries": 95, "numberOfRssQueries": 0},
				}
				fakeProwl.statuses = []map[string]any{
					{"indexerId": 2, "disabledTill": clk.Now().Add(2 * time.Hour).Format(time.RFC3339)},
				}
			},
			wantAllowed: false,
			wantRefusal: snatch.GateIndexerLimit,
		},
		{
			name: "unlimited alive -> allow",
			setupArr: func(fakeArr *fakeArrServer, prowlURL string) {
				fakeArr.indexers = []map[string]any{
					{"id": 10, "name": "Sonarr Idx 1", "fields": []map[string]any{{"name": "baseUrl", "value": prowlURL + "/1/"}}},
					{"id": 20, "name": "Sonarr Idx Unlimited", "fields": []map[string]any{{"name": "baseUrl", "value": prowlURL + "/2/"}}},
				}
			},
			setupProwl: func(fakeProwl *fakeProwlarrServer, _ clock.Clock) {
				fakeProwl.indexers = []map[string]any{
					{
						"id": 1, "name": "Prowl Idx 1", "enable": true,
						"fields": []map[string]any{{"name": "baseSettings.queryLimit", "value": 100}, {"name": "baseSettings.limitsUnit", "value": 0}},
					},
					{
						"id": 2, "name": "Prowl Idx Unlimited", "enable": true,
						"fields": []map[string]any{},
					},
				}
				fakeProwl.stats = []map[string]any{
					{"indexerId": 1, "numberOfQueries": 95, "numberOfRssQueries": 0},
				}
				fakeProwl.statuses = []map[string]any{}
			},
			wantAllowed: true,
		},
		{
			name: "Prowlarr enable=false limited one + others exhausted -> defer",
			setupArr: func(fakeArr *fakeArrServer, prowlURL string) {
				fakeArr.indexers = []map[string]any{
					{"id": 10, "name": "Sonarr Idx 1", "fields": []map[string]any{{"name": "baseUrl", "value": prowlURL + "/1/"}}},
					{"id": 20, "name": "Sonarr Idx 2 Disabled", "fields": []map[string]any{{"name": "baseUrl", "value": prowlURL + "/2/"}}},
				}
			},
			setupProwl: func(fakeProwl *fakeProwlarrServer, _ clock.Clock) {
				fakeProwl.indexers = []map[string]any{
					{
						"id": 1, "name": "Prowl Idx 1", "enable": true,
						"fields": []map[string]any{{"name": "baseSettings.queryLimit", "value": 100}, {"name": "baseSettings.limitsUnit", "value": 0}},
					},
					{
						"id": 2, "name": "Prowl Idx 2 Disabled", "enable": false,
						"fields": []map[string]any{{"name": "baseSettings.queryLimit", "value": 100}, {"name": "baseSettings.limitsUnit", "value": 0}},
					},
				}
				fakeProwl.stats = []map[string]any{
					{"indexerId": 1, "numberOfQueries": 95, "numberOfRssQueries": 0},
					{"indexerId": 2, "numberOfQueries": 0, "numberOfRssQueries": 0},
				}
				fakeProwl.statuses = []map[string]any{}
			},
			wantAllowed: false,
			wantRefusal: snatch.GateIndexerLimit,
		},
		{
			name: "matched id missing from Prowlarr list -> treat as unknown headroom -> allow",
			setupArr: func(fakeArr *fakeArrServer, prowlURL string) {
				fakeArr.indexers = []map[string]any{
					{"id": 10, "name": "Sonarr Idx 999", "fields": []map[string]any{{"name": "baseUrl", "value": prowlURL + "/999/"}}},
				}
			},
			setupProwl: func(fakeProwl *fakeProwlarrServer, _ clock.Clock) {
				fakeProwl.indexers = []map[string]any{
					{
						"id": 1, "name": "Prowl Idx 1", "enable": true,
						"fields": []map[string]any{{"name": "baseSettings.queryLimit", "value": 100}, {"name": "baseSettings.limitsUnit", "value": 0}},
					},
				}
				fakeProwl.stats = []map[string]any{
					{"indexerId": 1, "numberOfQueries": 95, "numberOfRssQueries": 0},
				}
				fakeProwl.statuses = []map[string]any{}
			},
			wantAllowed: true,
		},
		{
			name: "positive grab at limit -> refuses with grab limit reason",
			setupArr: func(fakeArr *fakeArrServer, prowlURL string) {
				fakeArr.indexers = []map[string]any{
					{"id": 10, "name": "Sonarr Idx 1", "fields": []map[string]any{{"name": "baseUrl", "value": prowlURL + "/1/"}}},
				}
			},
			setupProwl: func(fakeProwl *fakeProwlarrServer, _ clock.Clock) {
				fakeProwl.indexers = []map[string]any{
					{
						"id": 1, "name": "Prowl Idx 1", "enable": true,
						"fields": []map[string]any{
							{"name": "baseSettings.grabLimit", "value": 100},
							{"name": "baseSettings.limitsUnit", "value": 0},
						},
					},
				}
				fakeProwl.stats = []map[string]any{
					{"indexerId": 1, "numberOfQueries": 0, "numberOfRssQueries": 0, "numberOfGrabs": 90},
				}
				fakeProwl.statuses = []map[string]any{}
			},
			wantAllowed: false,
			wantRefusal: snatch.GateIndexerLimit,
			wantMessage: "all Prowlarr indexers exhausted (grab limit)",
		},
		{
			name: "boundary grab one below threshold -> allow",
			setupArr: func(fakeArr *fakeArrServer, prowlURL string) {
				fakeArr.indexers = []map[string]any{
					{"id": 10, "name": "Sonarr Idx 1", "fields": []map[string]any{{"name": "baseUrl", "value": prowlURL + "/1/"}}},
				}
			},
			setupProwl: func(fakeProwl *fakeProwlarrServer, _ clock.Clock) {
				fakeProwl.indexers = []map[string]any{
					{
						"id": 1, "name": "Prowl Idx 1", "enable": true,
						"fields": []map[string]any{
							{"name": "baseSettings.grabLimit", "value": 100},
							{"name": "baseSettings.limitsUnit", "value": 0},
						},
					},
				}
				fakeProwl.stats = []map[string]any{
					{"indexerId": 1, "numberOfQueries": 0, "numberOfRssQueries": 0, "numberOfGrabs": 89},
				}
				fakeProwl.statuses = []map[string]any{}
			},
			wantAllowed: true,
		},
		{
			name: "positive query-only limit -> refuses with unchanged message",
			setupArr: func(fakeArr *fakeArrServer, prowlURL string) {
				fakeArr.indexers = []map[string]any{
					{"id": 10, "name": "Sonarr Idx 1", "fields": []map[string]any{{"name": "baseUrl", "value": prowlURL + "/1/"}}},
				}
			},
			setupProwl: func(fakeProwl *fakeProwlarrServer, _ clock.Clock) {
				fakeProwl.indexers = []map[string]any{
					{
						"id": 1, "name": "Prowl Idx 1", "enable": true,
						"fields": []map[string]any{
							{"name": "baseSettings.queryLimit", "value": 100},
							{"name": "baseSettings.limitsUnit", "value": 0},
						},
					},
				}
				fakeProwl.stats = []map[string]any{
					{"indexerId": 1, "numberOfQueries": 90, "numberOfRssQueries": 0, "numberOfGrabs": 0},
				}
				fakeProwl.statuses = []map[string]any{}
			},
			wantAllowed: false,
			wantRefusal: snatch.GateIndexerLimit,
			wantMessage: "all Prowlarr indexers exhausted",
		},
		{
			name: "negative grab limit absent is unlimited -> allows even if limited indexer exhausted",
			setupArr: func(fakeArr *fakeArrServer, prowlURL string) {
				fakeArr.indexers = []map[string]any{
					{"id": 10, "name": "Sonarr Idx 1", "fields": []map[string]any{{"name": "baseUrl", "value": prowlURL + "/1/"}}},
					{"id": 20, "name": "Sonarr Idx Unlimited", "fields": []map[string]any{{"name": "baseUrl", "value": prowlURL + "/2/"}}},
				}
			},
			setupProwl: func(fakeProwl *fakeProwlarrServer, _ clock.Clock) {
				fakeProwl.indexers = []map[string]any{
					{
						"id": 1, "name": "Prowl Idx 1", "enable": true,
						"fields": []map[string]any{
							{"name": "baseSettings.grabLimit", "value": 100},
							{"name": "baseSettings.limitsUnit", "value": 0},
						},
					},
					{
						"id": 2, "name": "Prowl Idx Unlimited", "enable": true,
						"fields": []map[string]any{},
					},
				}
				fakeProwl.stats = []map[string]any{
					{"indexerId": 1, "numberOfQueries": 0, "numberOfRssQueries": 0, "numberOfGrabs": 95},
				}
				fakeProwl.statuses = []map[string]any{}
			},
			wantAllowed: true,
		},
		{
			name: "boundary grabLimit set but queryLimit nil -> limited, refuses when exhausted",
			setupArr: func(fakeArr *fakeArrServer, prowlURL string) {
				fakeArr.indexers = []map[string]any{
					{"id": 10, "name": "Sonarr Idx 1", "fields": []map[string]any{{"name": "baseUrl", "value": prowlURL + "/1/"}}},
				}
			},
			setupProwl: func(fakeProwl *fakeProwlarrServer, _ clock.Clock) {
				fakeProwl.indexers = []map[string]any{
					{
						"id": 1, "name": "Prowl Idx 1", "enable": true,
						"fields": []map[string]any{
							{"name": "baseSettings.grabLimit", "value": 50},
							{"name": "baseSettings.limitsUnit", "value": 0},
						},
					},
				}
				fakeProwl.stats = []map[string]any{
					{"indexerId": 1, "numberOfQueries": 0, "numberOfRssQueries": 0, "numberOfGrabs": 45},
				}
				fakeProwl.statuses = []map[string]any{}
			},
			wantAllowed: false,
			wantRefusal: snatch.GateIndexerLimit,
			wantMessage: "all Prowlarr indexers exhausted (grab limit)",
		},
		{
			name: "positive limitsUnit 1 uses hour usage for grabs -> refuses when hour exhausted",
			setupArr: func(fakeArr *fakeArrServer, prowlURL string) {
				fakeArr.indexers = []map[string]any{
					{"id": 10, "name": "Sonarr Idx 1", "fields": []map[string]any{{"name": "baseUrl", "value": prowlURL + "/1/"}}},
				}
			},
			setupProwl: func(fakeProwl *fakeProwlarrServer, _ clock.Clock) {
				fakeProwl.indexers = []map[string]any{
					{
						"id": 1, "name": "Prowl Idx 1", "enable": true,
						"fields": []map[string]any{
							{"name": "baseSettings.grabLimit", "value": 10},
							{"name": "baseSettings.limitsUnit", "value": 1},
						},
					},
				}
				fakeProwl.stats = []map[string]any{
					{"indexerId": 1, "numberOfQueries": 0, "numberOfRssQueries": 0, "numberOfGrabs": 2},
				}
				fakeProwl.statsHour = []map[string]any{
					{"indexerId": 1, "numberOfQueries": 0, "numberOfRssQueries": 0, "numberOfGrabs": 9},
				}
				fakeProwl.statuses = []map[string]any{}
			},
			wantAllowed: false,
			wantRefusal: snatch.GateIndexerLimit,
			wantMessage: "all Prowlarr indexers exhausted (grab limit)",
		},
		{
			name: "boundary limitsUnit 1 uses hour usage for grabs -> allows when hour below limit despite day above",
			setupArr: func(fakeArr *fakeArrServer, prowlURL string) {
				fakeArr.indexers = []map[string]any{
					{"id": 10, "name": "Sonarr Idx 1", "fields": []map[string]any{{"name": "baseUrl", "value": prowlURL + "/1/"}}},
				}
			},
			setupProwl: func(fakeProwl *fakeProwlarrServer, _ clock.Clock) {
				fakeProwl.indexers = []map[string]any{
					{
						"id": 1, "name": "Prowl Idx 1", "enable": true,
						"fields": []map[string]any{
							{"name": "baseSettings.grabLimit", "value": 10},
							{"name": "baseSettings.limitsUnit", "value": 1},
						},
					},
				}
				fakeProwl.stats = []map[string]any{
					{"indexerId": 1, "numberOfQueries": 0, "numberOfRssQueries": 0, "numberOfGrabs": 10},
				}
				fakeProwl.statsHour = []map[string]any{
					{"indexerId": 1, "numberOfQueries": 0, "numberOfRssQueries": 0, "numberOfGrabs": 5},
				}
				fakeProwl.statuses = []map[string]any{}
			},
			wantAllowed: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runGateTestCase(t, tc)
		})
	}
}

func TestGate_F2_MatchPrefixes(t *testing.T) {
	t.Parallel()
	// Test: link base http://prowlarr.media.svc:9696, app prowlarrUrl http://prowlarr:9696, *arr indexer baseUrl http://prowlarr:9696/3/ -> matched id 3
	link := sqlcgen.ProwlarrLink{
		ID:      uuid.New(),
		Name:    "Prowlarr",
		BaseUrl: "http://prowlarr.media.svc:9696",
	}
	views := map[uuid.UUID]prowlarr.LinkStatus{
		link.ID: {
			MatchPrefixes: []string{"http://prowlarr.media.svc:9696", "http://prowlarr:9696"},
		},
	}
	arrIdx := arr.ProviderResource{
		ID:   10,
		Name: "Sonarr Idx 3",
		Fields: []arr.ProviderField{
			{Name: "baseUrl", Value: "http://prowlarr:9696/3/"},
		},
	}
	matched, hasUnmatched := prowlarr.MatchIndexersForTest([]arr.ProviderResource{arrIdx}, []sqlcgen.ProwlarrLink{link}, views)
	if len(matched) != 1 {
		t.Fatalf("matched len = %d, want 1", len(matched))
	}
	if matched[0].ID != 3 {
		t.Errorf("matched id = %d, want 3", matched[0].ID)
	}
	if hasUnmatched {
		t.Errorf("hasUnmatched = true, want false")
	}
}

type recordLogHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordLogHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordLogHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}
func (h *recordLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordLogHandler) WithGroup(string) slog.Handler      { return h }

func TestGate_F6_WarnWhenMatchingMisses(t *testing.T) {
	t.Parallel()
	_, prowlSrv := newFakeProwlarr(t)
	fakeArr, arrSrv := newFakeArr(t)
	fakeArr.indexers = []map[string]any{
		{"id": 10, "name": "Direct Tracker", "fields": []map[string]any{{"name": "baseUrl", "value": "http://unrelated-tracker:8080/api"}}},
	}

	st := storetest.New(t)
	clk := clock.NewFake()
	enc, err := crypto.NewEncryptor(bytes.Repeat([]byte{0x55}, 32))
	if err != nil {
		t.Fatalf("encryptor: %v", err)
	}
	prober := arrclient.NewGoenvoyProber(arrclient.DefaultOptions())
	instSvc := instances.New(st, enc, clk, id.New(), prober, slog.New(slog.DiscardHandler))
	prowlClient := prowlarrclient.New(5*time.Second, "gate-test")
	cfg := config.Options{
		Prowlarr: config.ProwlarrOptions{LimitThreshold: 0.9},
	}
	handler := &recordLogHandler{}
	logger := slog.New(handler)
	svc := prowlarr.New(st, enc, clk, id.New(), prowlClient, prober, instSvc, logger, cfg)

	inst, err := instSvc.Create(t.Context(), instances.Input{
		Name:    "sonarr-f6",
		Kind:    domain.KindSonarr,
		BaseURL: arrSrv.URL,
		APIKey:  "sonarr-secret",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	_, err = svc.Create(t.Context(), prowlarr.Input{
		Name:    "prowlarr-f6",
		BaseURL: prowlSrv.URL,
		APIKey:  "prowl-secret",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("create prowlarr link: %v", err)
	}

	allowed, refusal, err := svc.Allow(t.Context(), inst, domain.SnatchMissing)
	if err != nil {
		t.Fatalf("Allow unexpected error: %v", err)
	}
	if !allowed {
		t.Fatalf("Allow = false, want true (refusal: %+v)", refusal)
	}

	handler.mu.Lock()
	defer handler.mu.Unlock()
	found := false
	for _, r := range handler.records {
		if strings.Contains(r.Message, "no *arr indexer points at a linked Prowlarr; check the application's Prowlarr Server URL") {
			found = true
			var instAttr, linksAttr string
			r.Attrs(func(a slog.Attr) bool {
				switch a.Key {
				case "instance":
					instAttr = a.Value.String()
				case "links":
					linksAttr = a.Value.String()
				}
				return true
			})
			if instAttr != "sonarr-f6" {
				t.Errorf("instance attr = %q, want sonarr-f6", instAttr)
			}
			if !strings.Contains(linksAttr, "prowlarr-f6") {
				t.Errorf("links attr = %q, want contain prowlarr-f6", linksAttr)
			}
		}
	}
	if !found {
		t.Error("expected matching miss warning log record, found none")
	}
}

func TestGate_F7_ArrIndexerCaching(t *testing.T) {
	t.Parallel()
	_, prowlSrv := newFakeProwlarr(t)

	var arrHits atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v3/system/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"appName": "Sonarr", "version": "3.0.0"})
	})
	mux.HandleFunc("GET /api/v3/indexer", func(w http.ResponseWriter, _ *http.Request) {
		arrHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": 10, "name": "Sonarr Idx 1", "fields": []map[string]any{{"name": "baseUrl", "value": prowlSrv.URL + "/1/"}}},
		})
	})
	arrSrv := httptest.NewServer(mux)
	t.Cleanup(arrSrv.Close)

	st := storetest.New(t)
	clk := clock.NewFake()
	enc, err := crypto.NewEncryptor(bytes.Repeat([]byte{0x55}, 32))
	if err != nil {
		t.Fatalf("encryptor: %v", err)
	}
	prober := arrclient.NewGoenvoyProber(arrclient.DefaultOptions())
	instSvc := instances.New(st, enc, clk, id.New(), prober, slog.New(slog.DiscardHandler))
	prowlClient := prowlarrclient.New(5*time.Second, "gate-test")
	cfg := config.Options{
		Prowlarr: config.ProwlarrOptions{LimitThreshold: 0.9},
	}
	svc := prowlarr.New(st, enc, clk, id.New(), prowlClient, prober, instSvc, slog.New(slog.DiscardHandler), cfg)

	inst, err := instSvc.Create(t.Context(), instances.Input{
		Name:    "sonarr-f7",
		Kind:    domain.KindSonarr,
		BaseURL: arrSrv.URL,
		APIKey:  "sonarr-secret",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	_, err = svc.Create(t.Context(), prowlarr.Input{
		Name:    "prowlarr-f7",
		BaseURL: prowlSrv.URL,
		APIKey:  "prowl-secret",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("create prowlarr link: %v", err)
	}

	// 1st call: hits arr
	if _, _, err := svc.Allow(t.Context(), inst, domain.SnatchMissing); err != nil {
		t.Fatalf("1st Allow() err: %v", err)
	}
	if hits := arrHits.Load(); hits != 1 {
		t.Fatalf("hits after 1st call = %d, want 1", hits)
	}

	// 2nd call immediately: cached, should NOT hit arr
	if _, _, err := svc.Allow(t.Context(), inst, domain.SnatchMissing); err != nil {
		t.Fatalf("2nd Allow() err: %v", err)
	}
	if hits := arrHits.Load(); hits != 1 {
		t.Fatalf("hits after 2nd call = %d, want 1 (cache should prevent request)", hits)
	}

	// Advance clock past StatusTTL (30s)
	clk.Advance(prowlarr.StatusTTL + time.Second)

	// 3rd call: cache expired, hits arr again
	if _, _, err := svc.Allow(t.Context(), inst, domain.SnatchMissing); err != nil {
		t.Fatalf("3rd Allow() err: %v", err)
	}
	if hits := arrHits.Load(); hits != 2 {
		t.Fatalf("hits after 3rd call = %d, want 2 (cache expired)", hits)
	}
}
