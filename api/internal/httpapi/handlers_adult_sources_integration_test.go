// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func newAdultGraphQLStub(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(req.Query, "queryPerformers") {
			writeTestJSON(t, w, map[string]any{
				"data": map[string]any{
					"query_performers": map[string]any{
						"count": 1,
						"performers": []map[string]any{
							{
								"id":             "perf-ext-1",
								"name":           "Performer One",
								"disambiguation": "Cohort A",
								"aliases":        []string{},
								"urls": []map[string]any{
									{"url": "https://profile.example/perf-1", "site": map[string]any{"id": "s1", "name": "Profile", "url": "https://profile.example"}},
								},
							},
						},
					},
				},
			})
			return
		}
		writeTestJSON(t, w, map[string]any{
			"data": map[string]any{
				"query_studios": map[string]any{
					"count": 1,
					"studios": []map[string]any{
						{
							"id":      "studio-ext-1",
							"name":    "Studio One",
							"aliases": []string{},
							"urls": []map[string]any{
								{"url": "https://studio.example/studio-1", "site": map[string]any{"id": "s2", "name": "Studio", "url": "https://studio.example"}},
							},
						},
					},
				},
			},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func enableAdultModuleForTest(t *testing.T, f *routerFixture, adminCsrf string) {
	t.Helper()
	payload := map[string]any{
		"enabled": true,
		"config": map[string]any{
			"image_set_dir":         t.TempDir(),
			"sync_interval_minutes": 360,
		},
	}
	res := doHTTP(t, f.client, http.MethodPut, f.srv.URL+PathPrefix+"/system/modules/"+domain.ModuleAdult, adminCsrf, payload)
	if res.statusCode != http.StatusOK {
		t.Fatalf("enable adult module status = %d, want 200; body: %s", res.statusCode, string(res.body))
	}
}

func assertProblemStatusAndType(t *testing.T, res httpResult, wantStatus int) {
	t.Helper()
	if res.statusCode != wantStatus {
		t.Fatalf("status = %d, want %d; body: %s", res.statusCode, wantStatus, string(res.body))
	}
	var prob map[string]any
	if err := json.Unmarshal(res.body, &prob); err != nil {
		t.Fatalf("decode problem json: %v; body: %s", err, string(res.body))
	}
	if int(prob["status"].(float64)) != wantStatus {
		t.Errorf("problem status = %v, want %d", prob["status"], wantStatus)
	}
}

func assertAdultSyncAndStatus(t *testing.T, f *routerFixture, adminCsrf string) {
	t.Helper()
	syncRes := doHTTP(t, f.client, http.MethodPost, f.srv.URL+PathPrefix+"/adult/sync", adminCsrf, nil)
	if syncRes.statusCode != http.StatusOK {
		t.Fatalf("sync status = %d, want 200; body: %s", syncRes.statusCode, string(syncRes.body))
	}
	var summary map[string]any
	if err := json.Unmarshal(syncRes.body, &summary); err != nil {
		t.Fatalf("unmarshal sync summary: %v", err)
	}
	if summary["sources_synced"] != float64(1) || summary["performers_synced"] != float64(1) || summary["studios_synced"] != float64(1) {
		t.Errorf("unexpected sync summary: %+v", summary)
	}

	statusRes := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/sync/status", "", nil)
	if statusRes.statusCode != http.StatusOK {
		t.Fatalf("sync status code = %d, want 200; body: %s", statusRes.statusCode, string(statusRes.body))
	}
	var status map[string]any
	if err := json.Unmarshal(statusRes.body, &status); err != nil {
		t.Fatalf("unmarshal sync status: %v", err)
	}
	if status["total_sources"] != float64(1) || status["enabled_sources"] != float64(1) || status["performers_synced"] != float64(1) || status["studios_synced"] != float64(1) {
		t.Errorf("unexpected sync status: %+v", status)
	}
	if status["last_sync_at"] == nil || status["last_sync_at"] == "" {
		t.Errorf("expected last_sync_at to be set: %+v", status)
	}
}

func TestAdultSources_Positive_CRUDAndSync(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	adminCsrf := getAdminCsrfToken(t, f)
	enableAdultModuleForTest(t, f, adminCsrf)
	gqlStub := newAdultGraphQLStub(t)

	createPayload := map[string]any{
		"name":           "Primary Catalogue",
		"endpoint":       gqlStub.URL + "/graphql",
		"api_key":        "secret-catalogue-token-999",
		"enabled":        true,
		"sync_favorites": true,
		"priority":       10,
	}
	createRes := doHTTP(t, f.client, http.MethodPost, f.srv.URL+PathPrefix+"/adult/sources", adminCsrf, createPayload)
	if createRes.statusCode != http.StatusCreated {
		t.Fatalf("create source status = %d, want 201; body: %s", createRes.statusCode, string(createRes.body))
	}
	if strings.Contains(string(createRes.body), "secret-catalogue-token-999") {
		t.Fatalf("create response leaked api_key: %s", string(createRes.body))
	}
	var created map[string]any
	if err := json.Unmarshal(createRes.body, &created); err != nil {
		t.Fatalf("unmarshal created: %v", err)
	}
	srcID, _ := created["id"].(string)
	if created["has_api_key"] != true || created["name"] != "Primary Catalogue" {
		t.Errorf("unexpected created source: %+v", created)
	}

	getRes := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/sources/"+srcID, "", nil)
	if getRes.statusCode != http.StatusOK || strings.Contains(string(getRes.body), "secret-catalogue-token-999") {
		t.Fatalf("get source failed or leaked key: status=%d body=%s", getRes.statusCode, string(getRes.body))
	}

	updatePayload := map[string]any{
		"name":           "Primary Catalogue Renamed",
		"endpoint":       gqlStub.URL + "/graphql",
		"enabled":        true,
		"sync_favorites": true,
		"priority":       5,
	}
	updateRes := doHTTP(t, f.client, http.MethodPut, f.srv.URL+PathPrefix+"/adult/sources/"+srcID, adminCsrf, updatePayload)
	if updateRes.statusCode != http.StatusOK {
		t.Fatalf("update source status = %d, want 200; body: %s", updateRes.statusCode, string(updateRes.body))
	}
	assertAdultSyncAndStatus(t, f, adminCsrf)

	delRes := doHTTP(t, f.client, http.MethodDelete, f.srv.URL+PathPrefix+"/adult/sources/"+srcID, adminCsrf, nil)
	if delRes.statusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", delRes.statusCode)
	}
	afterGet := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/sources/"+srcID, "", nil)
	assertProblemStatusAndType(t, afterGet, http.StatusNotFound)
}

func TestAdultSources_Negative_ValidationAndNotFound(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	adminCsrf := getAdminCsrfToken(t, f)
	enableAdultModuleForTest(t, f, adminCsrf)

	badPayloads := []struct {
		name string
		body map[string]any
	}{
		{"bad scheme ftp", map[string]any{"name": "Bad Scheme", "endpoint": "ftp://catalogue.local/graphql", "api_key": "k"}},
		{"missing host", map[string]any{"name": "No Host", "endpoint": "not-a-url", "api_key": "k"}},
		{"missing api key on create", map[string]any{"name": "No Key", "endpoint": "https://catalogue.local/graphql"}},
		{"whitespace api key", map[string]any{"name": "Blank Key", "endpoint": "https://catalogue.local/graphql", "api_key": "   "}},
	}
	for _, tc := range badPayloads {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := doHTTP(t, f.client, http.MethodPost, f.srv.URL+PathPrefix+"/adult/sources", adminCsrf, tc.body)
			assertProblemStatusAndType(t, res, http.StatusBadRequest)
		})
	}

	unknownID := uuid.New().String()
	validBody := map[string]any{"name": "Valid", "endpoint": "https://catalogue.local/graphql", "api_key": "key"}
	assertProblemStatusAndType(t, doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/adult/sources/"+unknownID, "", nil), http.StatusNotFound)
	assertProblemStatusAndType(t, doHTTP(t, f.client, http.MethodPut, f.srv.URL+PathPrefix+"/adult/sources/"+unknownID, adminCsrf, validBody), http.StatusNotFound)
	assertProblemStatusAndType(t, doHTTP(t, f.client, http.MethodDelete, f.srv.URL+PathPrefix+"/adult/sources/"+unknownID, adminCsrf, nil), http.StatusNotFound)
}

func TestAdultSources_Negative_ModuleOffReturns404(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	adminCsrf := getAdminCsrfToken(t, f)

	disableRes := doHTTP(t, f.client, http.MethodPut, f.srv.URL+PathPrefix+"/system/modules/"+domain.ModuleAdult, adminCsrf, map[string]any{"enabled": false})
	if disableRes.statusCode != http.StatusOK {
		t.Fatalf("disable module status = %d, want 200", disableRes.statusCode)
	}

	dummyID := uuid.New().String()
	payload := map[string]any{"name": "Blocked", "endpoint": "https://catalogue.local/graphql", "api_key": "token"}
	endpoints := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"list sources", http.MethodGet, "/adult/sources", nil},
		{"get source", http.MethodGet, "/adult/sources/" + dummyID, nil},
		{"create source", http.MethodPost, "/adult/sources", payload},
		{"update source", http.MethodPut, "/adult/sources/" + dummyID, payload},
		{"delete source", http.MethodDelete, "/adult/sources/" + dummyID, nil},
		{"trigger sync", http.MethodPost, "/adult/sync", nil},
		{"sync status", http.MethodGet, "/adult/sync/status", nil},
	}
	for _, ep := range endpoints {
		t.Run(ep.name, func(t *testing.T) {
			t.Parallel()
			res := doHTTP(t, f.client, ep.method, f.srv.URL+PathPrefix+ep.path, adminCsrf, ep.body)
			assertProblemStatusAndType(t, res, http.StatusNotFound)
		})
	}
}

func TestAdultSources_Boundary_NameLengthAndDisabledSync(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	adminCsrf := getAdminCsrfToken(t, f)
	enableAdultModuleForTest(t, f, adminCsrf)
	gqlStub := newAdultGraphQLStub(t)

	// Boundary: empty name (0 chars) and 129 chars -> 400
	emptyRes := doHTTP(t, f.client, http.MethodPost, f.srv.URL+PathPrefix+"/adult/sources", adminCsrf, map[string]any{
		"name": "", "endpoint": gqlStub.URL + "/graphql", "api_key": "k",
	})
	assertProblemStatusAndType(t, emptyRes, http.StatusBadRequest)

	tooLongRes := doHTTP(t, f.client, http.MethodPost, f.srv.URL+PathPrefix+"/adult/sources", adminCsrf, map[string]any{
		"name": strings.Repeat("x", 129), "endpoint": gqlStub.URL + "/graphql", "api_key": "k",
	})
	assertProblemStatusAndType(t, tooLongRes, http.StatusBadRequest)

	// Boundary: 1 char and 128 chars -> 201, with enabled=false so sync returns 0
	minRes := doHTTP(t, f.client, http.MethodPost, f.srv.URL+PathPrefix+"/adult/sources", adminCsrf, map[string]any{
		"name": "a", "endpoint": gqlStub.URL + "/graphql", "api_key": "k", "enabled": false,
	})
	if minRes.statusCode != http.StatusCreated {
		t.Fatalf("1-char name status = %d, want 201; body: %s", minRes.statusCode, string(minRes.body))
	}
	maxRes := doHTTP(t, f.client, http.MethodPost, f.srv.URL+PathPrefix+"/adult/sources", adminCsrf, map[string]any{
		"name": strings.Repeat("b", 128), "endpoint": gqlStub.URL + "/graphql", "api_key": "k", "enabled": false,
	})
	if maxRes.statusCode != http.StatusCreated {
		t.Fatalf("128-char name status = %d, want 201; body: %s", maxRes.statusCode, string(maxRes.body))
	}

	// Boundary: disabled sources -> sync summary 0
	syncRes := doHTTP(t, f.client, http.MethodPost, f.srv.URL+PathPrefix+"/adult/sync", adminCsrf, nil)
	if syncRes.statusCode != http.StatusOK {
		t.Fatalf("sync status = %d, want 200", syncRes.statusCode)
	}
	var summary map[string]any
	_ = json.Unmarshal(syncRes.body, &summary)
	if summary["sources_synced"] != float64(0) || summary["performers_synced"] != float64(0) || summary["studios_synced"] != float64(0) {
		t.Errorf("disabled sources should yield 0 sync summary, got: %+v", summary)
	}
}
