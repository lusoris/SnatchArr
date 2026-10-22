// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/instances"
)

func doPriorityRequest(t *testing.T, f *routerFixture, method, path, csrf, body string) (*http.Response, []byte) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, f.srv.URL+PathPrefix+path, reader)
	if err != nil {
		t.Fatalf("NewRequest(%s %s): %v", method, path, err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if csrf != "" {
		req.Header.Set("X-Csrf-Token", csrf)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatalf("Do(%s %s): %v", method, path, err)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll(%s %s): %v", method, path, err)
	}
	resp.Body = io.NopCloser(bytes.NewReader(raw))
	return resp, raw
}

func TestInstancePriorityHTTP(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	_ = completeInitialSetup(t, f)
	csrf := getAdminCsrfToken(t, f)
	inst, err := f.h.instances.Create(t.Context(), instances.Input{
		Kind:    domain.KindSonarr,
		Name:    "sonarr-prio",
		BaseURL: "http://sonarr-prio.invalid:8989",
		APIKey:  "secret-test-key",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("instances.Create: %v", err)
	}
	instID := inst.ID.String()

	// Positive: GET /priority-presets returns media_first.
	pResp, pRaw := doPriorityRequest(t, f, http.MethodGet, "/priority-presets", "", "")
	defer func() { _ = pResp.Body.Close() }()
	if pResp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", pResp.StatusCode, string(pRaw))
	}
	var presets []oas.PriorityPreset
	if err := json.Unmarshal(pRaw, &presets); err != nil {
		t.Fatalf("decode presets: %v", err)
	}
	if len(presets) != 1 || presets[0].Name != "media_first" || presets[0].Search["sonarr"] != 10 || presets[0].Download["whisparr_v3"] != 40 {
		t.Fatalf("unexpected presets: %+v", presets)
	}

	// Boundary: 1 and 100 accepted, null clears back to preset.
	resp, raw := doPriorityRequest(t, f, http.MethodPut, "/instances/"+instID+"/priority", csrf, `{"search_priority":1,"download_priority":100}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, string(raw))
	}
	var got oas.InstancePriority
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode priority: %v", err)
	}
	if got.EffectiveSearchPriority != 1 || got.SearchSource != oas.PrioritySourceInstance || got.EffectiveDownloadPriority != 100 || got.DownloadSource != oas.PrioritySourceInstance {
		t.Fatalf("got %+v, want effective 1/100 from instance", got)
	}

	// Clear overrides back to preset.
	cResp, cRaw := doPriorityRequest(t, f, http.MethodPut, "/instances/"+instID+"/priority", csrf, `{"search_priority":null,"download_priority":null}`)
	defer func() { _ = cResp.Body.Close() }()
	if cResp.StatusCode != http.StatusOK {
		t.Fatalf("clear status = %d, want 200: %s", cResp.StatusCode, string(cRaw))
	}
	if err := json.Unmarshal(cRaw, &got); err != nil {
		t.Fatalf("decode cleared priority: %v", err)
	}
	if !got.SearchPriority.IsNull() || got.SearchSource != oas.PrioritySourcePreset || got.EffectiveSearchPriority != 10 {
		t.Fatalf("cleared priority = %+v, want null/preset/10", got)
	}

	// Negative: PUT priority 0 or 101 -> 422 problem+json (schema bounds); string -> 400 (malformed body).
	badBodies := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{name: "negative search_priority 0", body: `{"search_priority":0}`, wantStatus: http.StatusUnprocessableEntity},
		{name: "negative search_priority 101", body: `{"search_priority":101}`, wantStatus: http.StatusUnprocessableEntity},
		{name: "negative download_priority 0", body: `{"download_priority":0}`, wantStatus: http.StatusUnprocessableEntity},
		{name: "negative download_priority 101", body: `{"download_priority":101}`, wantStatus: http.StatusUnprocessableEntity},
		{name: "negative search_priority string", body: `{"search_priority":"high"}`, wantStatus: http.StatusBadRequest},
	}
	for _, tc := range badBodies {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			bResp, _ := doPriorityRequest(t, f, http.MethodPut, "/instances/"+instID+"/priority", csrf, tc.body)
			defer func() { _ = bResp.Body.Close() }()
			assertProblem(t, bResp, tc.wantStatus)
		})
	}
}

func TestInstancePriorityConfigarrAndSettingsValidation(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	_ = completeInitialSetup(t, f)
	csrf := getAdminCsrfToken(t, f)

	// Positive: PUT priority on a source=configarr instance succeeds and survives Configarr re-import.
	cfgID := seedConfigarrInstance(t, f, configarrDeleteCase{enabled: true}).String()
	resp, raw := doPriorityRequest(t, f, http.MethodPut, "/instances/"+cfgID+"/priority", csrf, `{"search_priority":5,"download_priority":15}`)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT priority on configarr status = %d, want 200: %s", resp.StatusCode, string(raw))
	}
	var got oas.InstancePriority
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode priority: %v", err)
	}
	if got.EffectiveSearchPriority != 5 || got.EffectiveDownloadPriority != 15 || got.SearchSource != oas.PrioritySourceInstance {
		t.Fatalf("configarr priority = %+v, want 5/15 from instance", got)
	}

	// Re-import from Configarr and assert the priority override survives.
	_, err := f.h.instances.UpsertConfigarr(t.Context(), "sonarr.series", instances.Input{
		Kind:    domain.KindSonarr,
		Name:    "series",
		BaseURL: "http://sonarr-configarr.invalid:8989",
		APIKey:  "secret-test-key",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("UpsertConfigarr re-import: %v", err)
	}
	reResp, reRaw := doPriorityRequest(t, f, http.MethodGet, "/instances/"+cfgID+"/priority", "", "")
	defer func() { _ = reResp.Body.Close() }()
	if reResp.StatusCode != http.StatusOK {
		t.Fatalf("GET priority after re-import status = %d: %s", reResp.StatusCode, string(reRaw))
	}
	var reGot oas.InstancePriority
	if err := json.Unmarshal(reRaw, &reGot); err != nil {
		t.Fatalf("decode priority after re-import: %v", err)
	}
	if reGot.EffectiveSearchPriority != 5 || reGot.EffectiveDownloadPriority != 15 || reGot.SearchSource != oas.PrioritySourceInstance {
		t.Fatalf("configarr priority after re-import = %+v, want 5/15 from instance", reGot)
	}

	// Negative: PUT /settings with missing kind, unknown kind, rank 0 or 101 -> 422 problem+json.
	badSettings := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "negative settings search_priority_preset missing kind rejected with 422 by domain validation",
			body:       `{"history_retention_days":90,"user_agent":"SnatchArr/1.0","global_hourly_cap":0,"search_priority_preset":{"sonarr":10,"radarr":10,"lidarr":20,"readarr":30,"whisparr_v2":40},"download_priority_preset":{"sonarr":10,"radarr":10,"lidarr":20,"readarr":30,"whisparr_v2":40,"whisparr_v3":40}}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "negative settings download_priority_preset missing kind rejected with 422 by domain validation",
			body:       `{"history_retention_days":90,"user_agent":"SnatchArr/1.0","global_hourly_cap":0,"search_priority_preset":{"sonarr":10,"radarr":10,"lidarr":20,"readarr":30,"whisparr_v2":40,"whisparr_v3":40},"download_priority_preset":{"sonarr":10,"radarr":10,"lidarr":20,"readarr":30,"whisparr_v2":40}}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "negative settings unknown kind rejected with 422 by domain validation",
			body:       `{"history_retention_days":90,"user_agent":"SnatchArr/1.0","global_hourly_cap":0,"search_priority_preset":{"sonarr":10,"radarr":10,"lidarr":20,"readarr":30,"whisparr_v2":40,"whisparr_v3":40,"bazarr":50},"download_priority_preset":{"sonarr":10,"radarr":10,"lidarr":20,"readarr":30,"whisparr_v2":40,"whisparr_v3":40}}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "negative settings rank 0 rejected with 422 by ogen schema validation",
			body:       `{"history_retention_days":90,"user_agent":"SnatchArr/1.0","global_hourly_cap":0,"search_priority_preset":{"sonarr":0,"radarr":10,"lidarr":20,"readarr":30,"whisparr_v2":40,"whisparr_v3":40},"download_priority_preset":{"sonarr":10,"radarr":10,"lidarr":20,"readarr":30,"whisparr_v2":40,"whisparr_v3":40}}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "negative settings rank 101 rejected with 422 by ogen schema validation",
			body:       `{"history_retention_days":90,"user_agent":"SnatchArr/1.0","global_hourly_cap":0,"search_priority_preset":{"sonarr":101,"radarr":10,"lidarr":20,"readarr":30,"whisparr_v2":40,"whisparr_v3":40},"download_priority_preset":{"sonarr":10,"radarr":10,"lidarr":20,"readarr":30,"whisparr_v2":40,"whisparr_v3":40}}`,
			wantStatus: http.StatusUnprocessableEntity,
		},
	}
	for _, tc := range badSettings {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sResp, _ := doPriorityRequest(t, f, http.MethodPut, "/settings", csrf, tc.body)
			defer func() { _ = sResp.Body.Close() }()
			assertProblem(t, sResp, tc.wantStatus)
		})
	}
}

func TestInstancePriorityAuthAndNotFound(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	_ = completeInitialSetup(t, f)
	csrf := getAdminCsrfToken(t, f)
	inst, err := f.h.instances.Create(t.Context(), instances.Input{
		Kind:    domain.KindSonarr,
		Name:    "sonarr-prio-auth",
		BaseURL: "http://sonarr-prio-auth.invalid:8989",
		APIKey:  "secret-test-key",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("instances.Create: %v", err)
	}

	// Negative: PUT /instances/{unknown uuid}/priority -> 404 problem.
	unknownID := uuid.NewString()
	uResp, _ := doPriorityRequest(t, f, http.MethodPut, "/instances/"+unknownID+"/priority", csrf, `{"search_priority":10}`)
	defer func() { _ = uResp.Body.Close() }()
	assertProblem(t, uResp, http.StatusNotFound)

	// Negative: PUT /instances/{id}/priority as a non-admin session -> 403 problem.
	viewerClient, viewerCsrf := setupViewerClient(t, f)
	vReq, err := http.NewRequestWithContext(t.Context(), http.MethodPut,
		f.srv.URL+PathPrefix+"/instances/"+inst.ID.String()+"/priority",
		strings.NewReader(`{"search_priority":10}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	vReq.Header.Set("Content-Type", "application/json")
	vReq.Header.Set("X-Csrf-Token", viewerCsrf)
	vResp, err := viewerClient.Do(vReq)
	if err != nil {
		t.Fatalf("viewer PUT priority: %v", err)
	}
	defer func() { _ = vResp.Body.Close() }()
	assertProblem(t, vResp, http.StatusForbidden)
}

func setupViewerClient(t *testing.T, f *routerFixture) (*http.Client, string) {
	t.Helper()
	username, pass := createViewerUser(t, f)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Timeout: 10 * time.Second, Jar: jar}
	loginBody := bytes.NewReader([]byte(fmt.Sprintf(`{"username":%q,"password":%q}`, username, pass)))
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.srv.URL+PathPrefix+"/auth/login", loginBody)
	if err != nil {
		t.Fatalf("NewRequest login: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("viewer login: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("viewer login status = %d, want 200", resp.StatusCode)
	}
	var sess oas.Session
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	return client, sess.CsrfToken
}

func TestSettingsPriorityPresetBoundary(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	_ = completeInitialSetup(t, f)
	csrf := getAdminCsrfToken(t, f)

	// Positive boundary: PUT /settings with search preset all 1 and download preset all 100 -> 200, GET returns same.
	boundaryBody := `{"history_retention_days":90,"user_agent":"SnatchArr/1.0","global_hourly_cap":0,` +
		`"search_priority_preset":{"sonarr":1,"radarr":1,"lidarr":1,"readarr":1,"whisparr_v2":1,"whisparr_v3":1},` +
		`"download_priority_preset":{"sonarr":100,"radarr":100,"lidarr":100,"readarr":100,"whisparr_v2":100,"whisparr_v3":100}}`
	bPutResp, bPutRaw := doPriorityRequest(t, f, http.MethodPut, "/settings", csrf, boundaryBody)
	defer func() { _ = bPutResp.Body.Close() }()
	if bPutResp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /settings boundary status = %d, want 200: %s", bPutResp.StatusCode, string(bPutRaw))
	}

	bGetResp, bGetRaw := doPriorityRequest(t, f, http.MethodGet, "/settings", "", "")
	defer func() { _ = bGetResp.Body.Close() }()
	if bGetResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /settings boundary status = %d, want 200: %s", bGetResp.StatusCode, string(bGetRaw))
	}
	var bSettings oas.Settings
	if err := json.Unmarshal(bGetRaw, &bSettings); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	for _, k := range domain.AllKinds {
		ks := string(k)
		if bSettings.SearchPriorityPreset[ks] != 1 {
			t.Errorf("search preset %s = %d, want 1", ks, bSettings.SearchPriorityPreset[ks])
		}
		if bSettings.DownloadPriorityPreset[ks] != 100 {
			t.Errorf("download preset %s = %d, want 100", ks, bSettings.DownloadPriorityPreset[ks])
		}
	}
}
