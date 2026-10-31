// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
	"github.com/lusoris/SnatchArr/api/internal/watchaware"
)

func TestWatchawareRoutes_Positive_CRUD(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	adminCsrf := getAdminCsrfToken(t, f)

	// Enable watchaware module
	enablePayload := map[string]any{"enabled": true}
	enableRes := doHTTP(t, f.client, http.MethodPut, f.srv.URL+PathPrefix+"/system/modules/"+domain.ModuleWatchAware, adminCsrf, enablePayload)
	if enableRes.statusCode != http.StatusOK {
		t.Fatalf("enable module status = %d, want 200", enableRes.statusCode)
	}

	// 1. Create source (POST /watchaware/sources) -> 201
	createPayload := map[string]any{
		"kind":    "jellyfin",
		"name":    "Jellyfin Main",
		"url":     "http://jellyfin.local:8096",
		"api_key": "super-secret-key-12345",
		"enabled": true,
	}
	createRes := doHTTP(t, f.client, http.MethodPost, f.srv.URL+PathPrefix+"/watchaware/sources", adminCsrf, createPayload)
	if createRes.statusCode != http.StatusCreated {
		t.Fatalf("create response status = %d, want 201; body: %s", createRes.statusCode, string(createRes.body))
	}

	// Verify API key never in response body
	if strings.Contains(string(createRes.body), "super-secret-key-12345") {
		t.Errorf("create response leaked api key: %s", string(createRes.body))
	}
	if strings.Contains(string(createRes.body), `"api_key"`) {
		t.Errorf("create response contained api_key: %s", string(createRes.body))
	}

	var created map[string]any
	if err := json.Unmarshal(createRes.body, &created); err != nil {
		t.Fatalf("json.Unmarshal created: %v", err)
	}
	srcIDStr, ok := created["id"].(string)
	if !ok {
		t.Fatalf("created id is not string: %v", created["id"])
	}
	srcID, err := uuid.Parse(srcIDStr)
	if err != nil {
		t.Fatalf("uuid.Parse: %v", err)
	}
	if created["kind"] != "jellyfin" || created["name"] != "Jellyfin Main" || created["has_api_key"] != true || created["enabled"] != true {
		t.Errorf("unexpected created source: %+v", created)
	}

	// 2. List sources (GET /watchaware/sources) -> 200
	listRes := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/watchaware/sources", "", nil)
	if listRes.statusCode != http.StatusOK {
		t.Fatalf("list sources status = %d, want 200", listRes.statusCode)
	}
	if strings.Contains(string(listRes.body), "super-secret-key-12345") {
		t.Errorf("list response leaked api key: %s", string(listRes.body))
	}

	var list []map[string]any
	if err := json.Unmarshal(listRes.body, &list); err != nil {
		t.Fatalf("json.Unmarshal list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}
	if list[0]["id"] != srcIDStr || list[0]["has_api_key"] != true {
		t.Errorf("unexpected list[0]: %+v", list[0])
	}

	// 3. Update source (PUT /watchaware/sources/{id}) -> 200
	updatePayload := map[string]any{
		"kind":    "jellyfin",
		"name":    "Jellyfin Renamed",
		"url":     "http://jellyfin-new.local:8096",
		"enabled": false,
		// omit api_key to keep existing key
	}
	updateRes := doHTTP(t, f.client, http.MethodPut, f.srv.URL+PathPrefix+"/watchaware/sources/"+srcID.String(), adminCsrf, updatePayload)
	if updateRes.statusCode != http.StatusOK {
		t.Fatalf("update source status = %d, want 200", updateRes.statusCode)
	}
	if strings.Contains(string(updateRes.body), "super-secret-key-12345") {
		t.Errorf("update response leaked api key: %s", string(updateRes.body))
	}

	var updated map[string]any
	if err := json.Unmarshal(updateRes.body, &updated); err != nil {
		t.Fatalf("json.Unmarshal updated: %v", err)
	}
	if updated["name"] != "Jellyfin Renamed" || updated["enabled"] != false || updated["has_api_key"] != true {
		t.Errorf("unexpected updated source: %+v", updated)
	}

	// 4. Sync status (GET /watchaware/sync/status) -> 200
	statusRes := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/watchaware/sync/status", "", nil)
	if statusRes.statusCode != http.StatusOK {
		t.Fatalf("sync status code = %d, want 200; body: %s", statusRes.statusCode, string(statusRes.body))
	}

	var status map[string]any
	if err := json.Unmarshal(statusRes.body, &status); err != nil {
		t.Fatalf("json.Unmarshal status: %v", err)
	}
	if status["total_sources"] != float64(1) || status["enabled_sources"] != float64(0) || status["total_watches"] != float64(0) {
		t.Errorf("unexpected status: %+v", status)
	}

	// 5. Delete source (DELETE /watchaware/sources/{id}) -> 204
	deleteRes := doHTTP(t, f.client, http.MethodDelete, f.srv.URL+PathPrefix+"/watchaware/sources/"+srcID.String(), adminCsrf, nil)
	if deleteRes.statusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", deleteRes.statusCode)
	}

	// List again -> empty list
	listAfterRes := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/watchaware/sources", "", nil)
	if listAfterRes.statusCode != http.StatusOK {
		t.Fatalf("list after delete status = %d, want 200", listAfterRes.statusCode)
	}
	var listAfter []map[string]any
	if err := json.Unmarshal(listAfterRes.body, &listAfter); err != nil {
		t.Fatalf("json.Unmarshal listAfter: %v", err)
	}
	if len(listAfter) != 0 {
		t.Errorf("len(listAfter) = %d, want 0", len(listAfter))
	}
}

func TestWatchawareRoutes_Negative_ModuleOffReturns404(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	adminCsrf := getAdminCsrfToken(t, f)

	// Ensure module is OFF
	disablePayload := map[string]any{"enabled": false}
	disableRes := doHTTP(t, f.client, http.MethodPut, f.srv.URL+PathPrefix+"/system/modules/"+domain.ModuleWatchAware, adminCsrf, disablePayload)
	if disableRes.statusCode != http.StatusOK {
		t.Fatalf("disable module status = %d, want 200", disableRes.statusCode)
	}

	dummyID := uuid.New().String()
	payload := map[string]any{
		"kind":    "jellyfin",
		"name":    "Blocked",
		"url":     "http://jellyfin:8096",
		"api_key": "token",
	}

	endpoints := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"list sources", http.MethodGet, "/watchaware/sources", nil},
		{"create source", http.MethodPost, "/watchaware/sources", payload},
		{"update source", http.MethodPut, "/watchaware/sources/" + dummyID, payload},
		{"delete source", http.MethodDelete, "/watchaware/sources/" + dummyID, nil},
		{"sync status", http.MethodGet, "/watchaware/sync/status", nil},
	}

	for _, ep := range endpoints {
		t.Run(ep.name, func(t *testing.T) {
			t.Parallel()
			res := doHTTP(t, f.client, ep.method, f.srv.URL+PathPrefix+ep.path, adminCsrf, ep.body)
			if res.statusCode != http.StatusNotFound {
				t.Errorf("status = %d, want 404; body: %s", res.statusCode, string(res.body))
			}
			bodyStr := string(res.body)
			if !strings.Contains(bodyStr, "module watchaware is disabled") && !strings.Contains(bodyStr, "Not Found") {
				t.Errorf("body %q does not mention module disabled or Not Found", bodyStr)
			}
		})
	}
}

func TestWatchawareRoutes_Negative_ViewerForbidden(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	adminCsrf := getAdminCsrfToken(t, f)
	viewerClient, viewerCsrf := loginViewerSession(t, f)

	// Enable module as admin
	enableRes := doHTTP(t, f.client, http.MethodPut, f.srv.URL+PathPrefix+"/system/modules/"+domain.ModuleWatchAware, adminCsrf, map[string]any{"enabled": true})
	if enableRes.statusCode != http.StatusOK {
		t.Fatalf("enable module status = %d, want 200", enableRes.statusCode)
	}

	dummyID := uuid.New().String()
	payload := map[string]any{
		"kind":    "jellyfin",
		"name":    "Forbidden",
		"url":     "http://jellyfin:8096",
		"api_key": "token",
	}

	// Viewer can read
	listRes := doHTTP(t, viewerClient, http.MethodGet, f.srv.URL+PathPrefix+"/watchaware/sources", "", nil)
	if listRes.statusCode != http.StatusOK {
		t.Errorf("viewer list sources status = %d, want 200", listRes.statusCode)
	}

	statusRes := doHTTP(t, viewerClient, http.MethodGet, f.srv.URL+PathPrefix+"/watchaware/sync/status", "", nil)
	if statusRes.statusCode != http.StatusOK {
		t.Errorf("viewer sync status code = %d, want 200", statusRes.statusCode)
	}

	// Viewer cannot mutate (403 Forbidden)
	mutations := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"create", http.MethodPost, "/watchaware/sources", payload},
		{"update", http.MethodPut, "/watchaware/sources/" + dummyID, payload},
		{"delete", http.MethodDelete, "/watchaware/sources/" + dummyID, nil},
	}

	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			res := doHTTP(t, viewerClient, m.method, f.srv.URL+PathPrefix+m.path, viewerCsrf, m.body)
			if res.statusCode != http.StatusForbidden {
				t.Errorf("%s status = %d, want 403", m.name, res.statusCode)
			}
		})
	}
}

func TestWatchawareRoutes_Boundary_ToggleOffOnLive(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	adminCsrf := getAdminCsrfToken(t, f)

	// 1. Off -> 404
	res1 := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/watchaware/sources", "", nil)
	if res1.statusCode != http.StatusNotFound {
		t.Errorf("res1 status = %d, want 404", res1.statusCode)
	}

	// 2. Toggle On -> 200
	enableRes := doHTTP(t, f.client, http.MethodPut, f.srv.URL+PathPrefix+"/system/modules/"+domain.ModuleWatchAware, adminCsrf, map[string]any{"enabled": true})
	if enableRes.statusCode != http.StatusOK {
		t.Fatalf("enable module status = %d, want 200", enableRes.statusCode)
	}

	res2 := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/watchaware/sources", "", nil)
	if res2.statusCode != http.StatusOK {
		t.Errorf("res2 status = %d, want 200", res2.statusCode)
	}

	// 3. Toggle Off -> 404
	disableRes := doHTTP(t, f.client, http.MethodPut, f.srv.URL+PathPrefix+"/system/modules/"+domain.ModuleWatchAware, adminCsrf, map[string]any{"enabled": false})
	if disableRes.statusCode != http.StatusOK {
		t.Fatalf("disable module status = %d, want 200", disableRes.statusCode)
	}

	res3 := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/watchaware/sources", "", nil)
	if res3.statusCode != http.StatusNotFound {
		t.Errorf("res3 status = %d, want 404", res3.statusCode)
	}
}

func TestWatchawareRoutes_GateEndpoints(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	adminCsrf := getAdminCsrfToken(t, f)

	// 1. Off -> 404
	instID := uuid.New().String()
	resOff1 := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/watchaware/gate/series?instance_id="+instID, "", nil)
	if resOff1.statusCode != http.StatusNotFound {
		t.Errorf("resOff1 status = %d, want 404", resOff1.statusCode)
	}
	resOff2 := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/watchaware/gate/decisions", "", nil)
	if resOff2.statusCode != http.StatusNotFound {
		t.Errorf("resOff2 status = %d, want 404", resOff2.statusCode)
	}

	// 2. Enable watchaware
	enableRes := doHTTP(t, f.client, http.MethodPut, f.srv.URL+PathPrefix+"/system/modules/"+domain.ModuleWatchAware, adminCsrf, map[string]any{"enabled": true})
	if enableRes.statusCode != http.StatusOK {
		t.Fatalf("enable module status = %d, want 200", enableRes.statusCode)
	}

	// 3. GET gate series missing instance_id -> 400
	resBad := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/watchaware/gate/series", "", nil)
	if resBad.statusCode != http.StatusBadRequest {
		t.Errorf("resBad status = %d, want 400", resBad.statusCode)
	}

	// 4. GET gate series valid instance_id -> 200
	resSeries := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/watchaware/gate/series?instance_id="+instID, "", nil)
	if resSeries.statusCode != http.StatusOK {
		t.Errorf("resSeries status = %d, want 200", resSeries.statusCode)
	}

	// 5. GET gate decisions with and without instance_id -> 200
	resDec1 := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/watchaware/gate/decisions", "", nil)
	if resDec1.statusCode != http.StatusOK {
		t.Errorf("resDec1 status = %d, want 200", resDec1.statusCode)
	}
	resDec2 := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/watchaware/gate/decisions?instance_id="+instID+"&limit=50", "", nil)
	if resDec2.statusCode != http.StatusOK {
		t.Errorf("resDec2 status = %d, want 200", resDec2.statusCode)
	}
}

func TestReleaseWatchawareGateSeries_Integration(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	adminCsrf := getAdminCsrfToken(t, f)
	instUUID := uuid.New()
	instID := instUUID.String()

	now := f.clk.Now()
	if _, err := f.st.Q().CreateInstance(t.Context(), sqlcgen.CreateInstanceParams{
		ID:        instUUID,
		Kind:      "sonarr",
		Name:      "Sonarr Test",
		BaseUrl:   "http://sonarr.local",
		ApiKeyEnc: []byte("encrypted"),
		Enabled:   true,
		Source:    "manual",
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("CreateInstance: %v", err)
	}

	// Enable watchaware
	enableRes := doHTTP(t, f.client, http.MethodPut, f.srv.URL+PathPrefix+"/system/modules/"+domain.ModuleWatchAware, adminCsrf, map[string]any{"enabled": true})
	if enableRes.statusCode != http.StatusOK {
		t.Fatalf("enable module status = %d, want 200", enableRes.statusCode)
	}

	// Seed gated series with held seasons [2, 3]
	if err := f.st.Q().UpsertGateSeries(t.Context(), sqlcgen.UpsertGateSeriesParams{
		InstanceID:      instUUID,
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

	releaseURL := fmt.Sprintf("%s%s/watchaware/gate/series/%s/42/release", f.srv.URL, PathPrefix, instID)

	// 1. Viewer role gets 403
	viewerClient, viewerCsrf := loginViewerSession(t, f)
	viewerRes := doHTTP(t, viewerClient, http.MethodPost, releaseURL, viewerCsrf, nil)
	if viewerRes.statusCode != http.StatusForbidden {
		t.Errorf("viewer release status = %d, want 403", viewerRes.statusCode)
	}

	// 2. Admin role gets 200 OK
	adminRes := doHTTP(t, f.client, http.MethodPost, releaseURL, adminCsrf, nil)
	if adminRes.statusCode != http.StatusOK {
		t.Fatalf("admin release status = %d, want 200; body: %s", adminRes.statusCode, string(adminRes.body))
	}
	var resBody map[string]any
	if err := json.Unmarshal(adminRes.body, &resBody); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resBody["state"] != "released" {
		t.Errorf("state = %v, want released", resBody["state"])
	}
}

func TestUpdateWatchawareSource_TableDriven(t *testing.T) {
	t.Parallel()
	f := newIntegrationFixture(t)
	ctx := withUser(t.Context(), f.admin)
	u, err := url.Parse("http://jellyfin:8096")
	if err != nil {
		t.Fatalf("url.Parse: %v", err)
	}

	created, err := f.h.CreateWatchawareSource(ctx, &oas.WatchawareSourceInput{
		Kind:   oas.WatchawareSourceKindJellyfin,
		Name:   "watch-943",
		URL:    *u,
		APIKey: oas.NewOptString("my-api-key"),
	})
	if err != nil || !created.Enabled {
		t.Fatalf("CreateWatchawareSource() = %+v, %v, want enabled:true", created, err)
	}

	disabled, err := f.h.UpdateWatchawareSource(ctx, &oas.WatchawareSourceInput{
		Kind:    oas.WatchawareSourceKindJellyfin,
		Name:    "watch-943",
		URL:     *u,
		Enabled: oas.NewOptBool(false),
	}, oas.UpdateWatchawareSourceParams{SourceId: created.ID})
	if err != nil || disabled.Enabled {
		t.Fatalf("UpdateWatchawareSource() initial disable = %+v, %v", disabled, err)
	}

	tests := []struct {
		name        string
		createInit  bool
		srcID       uuid.UUID
		req         oas.WatchawareSourceInput
		wantEnabled bool
		wantErr     error
	}{
		{
			name:       "positive: omitted enabled keeps stored disabled value",
			createInit: true,
			req: oas.WatchawareSourceInput{
				Kind: oas.WatchawareSourceKindJellyfin,
				Name: "watch-943-renamed",
				URL:  *u,
			},
			wantEnabled: false,
		},
		{
			name:       "negative: explicit true replaces stored disabled value",
			createInit: true,
			req: oas.WatchawareSourceInput{
				Kind:    oas.WatchawareSourceKindJellyfin,
				Name:    "watch-943-renamed",
				URL:     *u,
				Enabled: oas.NewOptBool(true),
			},
			wantEnabled: true,
		},
		{
			name:       "boundary: explicit false replaces stored true value",
			createInit: true,
			req: oas.WatchawareSourceInput{
				Kind:    oas.WatchawareSourceKindJellyfin,
				Name:    "watch-943-renamed",
				URL:     *u,
				Enabled: oas.NewOptBool(false),
			},
			wantEnabled: false,
		},
		{
			name:  "boundary: unknown source id returns not found",
			srcID: uuid.New(),
			req: oas.WatchawareSourceInput{
				Kind: oas.WatchawareSourceKindJellyfin,
				Name: "unknown",
				URL:  *u,
			},
			wantErr: domain.ErrNotFound,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srcID := tc.srcID
			if tc.createInit {
				c, err := f.h.CreateWatchawareSource(ctx, &oas.WatchawareSourceInput{
					Kind:   oas.WatchawareSourceKindJellyfin,
					Name:   "watch-" + uuid.NewString()[:8],
					URL:    *u,
					APIKey: oas.NewOptString("my-api-key"),
				})
				if err != nil {
					t.Fatalf("CreateWatchawareSource: %v", err)
				}
				dis, err := f.h.UpdateWatchawareSource(ctx, &oas.WatchawareSourceInput{
					Kind:    c.Kind,
					Name:    c.Name,
					URL:     *u,
					Enabled: oas.NewOptBool(false),
				}, oas.UpdateWatchawareSourceParams{SourceId: c.ID})
				if err != nil || dis.Enabled {
					t.Fatalf("initial disable: %v", err)
				}
				srcID = c.ID
			}
			req := tc.req
			if tc.createInit {
				req.Name = "watch-" + uuid.NewString()[:8]
			}
			got, err := f.h.UpdateWatchawareSource(ctx, &req, oas.UpdateWatchawareSourceParams{SourceId: srcID})
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("UpdateWatchawareSource() err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("UpdateWatchawareSource() err = %v", err)
			}
			if got.Enabled != tc.wantEnabled {
				t.Fatalf("UpdateWatchawareSource() Enabled = %v, want %v", got.Enabled, tc.wantEnabled)
			}
		})
	}
}
