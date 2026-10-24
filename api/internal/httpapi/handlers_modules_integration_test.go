// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

type httpResult struct {
	statusCode int
	body       []byte
}

func doHTTP(t *testing.T, client *http.Client, method, url, csrfToken string, body any) httpResult {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, url, bodyReader)
	if err != nil {
		t.Fatalf("new request %s %s: %v", method, url, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if csrfToken != "" && method != http.MethodGet && method != http.MethodHead {
		req.Header.Set("X-Csrf-Token", csrfToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request %s %s: %v", method, url, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if resp.StatusCode >= 400 {
		ct := resp.Header.Get("Content-Type")
		if !strings.Contains(ct, "application/problem+json") {
			t.Errorf("%s %s got status %d but Content-Type = %q, want application/problem+json", method, url, resp.StatusCode, ct)
		}
	}
	return httpResult{
		statusCode: resp.StatusCode,
		body:       respBody,
	}
}

func TestListModulesAsViewer(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	viewerClient, _ := loginViewerSession(t, f)

	res := doHTTP(t, viewerClient, http.MethodGet, f.srv.URL+PathPrefix+"/system/modules", "", nil)
	if res.statusCode != http.StatusOK {
		t.Fatalf("listModules status = %d, want 200", res.statusCode)
	}

	var items []map[string]any
	if err := json.Unmarshal(res.body, &items); err != nil {
		t.Fatalf("unmarshal listModules: %v; body: %s", err, string(res.body))
	}

	foundLLM := false
	foundWatchAware := false
	for _, item := range items {
		id, _ := item["id"].(string)
		enabled, _ := item["enabled"].(bool)
		if enabled {
			t.Errorf("module %s enabled = true, want false by default", id)
		}
		if _, hasConfig := item["config"]; hasConfig {
			t.Errorf("module %s unexpectedly returned config field in listModules", id)
		}
		switch id {
		case domain.ModuleLLM:
			foundLLM = true
		case domain.ModuleWatchAware:
			foundWatchAware = true
		}
	}
	if !foundLLM || !foundWatchAware {
		t.Fatalf("listModules missing expected modules (llm=%v, watchaware=%v): %v", foundLLM, foundWatchAware, items)
	}
}

func TestGetModuleRBAC(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	viewerClient, _ := loginViewerSession(t, f)

	// Viewer -> 403 problem
	res := doHTTP(t, viewerClient, http.MethodGet, f.srv.URL+PathPrefix+"/system/modules/llm", "", nil)
	if res.statusCode != http.StatusForbidden {
		t.Fatalf("getModule viewer status = %d, want 403; body: %s", res.statusCode, string(res.body))
	}

	// Admin -> 200 with config {}
	res = doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/system/modules/llm", "", nil)
	if res.statusCode != http.StatusOK {
		t.Fatalf("getModule admin status = %d, want 200; body: %s", res.statusCode, string(res.body))
	}
	var mod map[string]any
	if err := json.Unmarshal(res.body, &mod); err != nil {
		t.Fatalf("unmarshal getModule admin: %v", err)
	}
	if mod["id"] != domain.ModuleLLM {
		t.Errorf("getModule id = %v, want %s", mod["id"], domain.ModuleLLM)
	}
	if mod["enabled"] != false {
		t.Errorf("getModule enabled = %v, want false", mod["enabled"])
	}
	cfg, ok := mod["config"].(map[string]any)
	if !ok || !reflect.DeepEqual(cfg, map[string]any{}) {
		t.Errorf("getModule config = %v, want empty object {}", mod["config"])
	}
	if _, hasUpdated := mod["updated_at"]; !hasUpdated {
		t.Errorf("getModule missing updated_at")
	}
}

func TestUpdateModuleViewerForbidden(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	viewerClient, viewerCsrf := loginViewerSession(t, f)

	payload := map[string]any{
		"enabled": true,
	}
	res := doHTTP(t, viewerClient, http.MethodPut, f.srv.URL+PathPrefix+"/system/modules/llm", viewerCsrf, payload)
	if res.statusCode != http.StatusForbidden {
		t.Fatalf("updateModule viewer status = %d, want 403; body: %s", res.statusCode, string(res.body))
	}
}

func TestUpdateModuleAdminMirror(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	adminCsrf := getAdminCsrfToken(t, f)

	enableLLM := map[string]any{
		"enabled": true,
	}
	res := doHTTP(t, f.client, http.MethodPut, f.srv.URL+PathPrefix+"/system/modules/llm", adminCsrf, enableLLM)
	if res.statusCode != http.StatusOK {
		t.Fatalf("enable llm status = %d, want 200; body: %s", res.statusCode, string(res.body))
	}
	var updatedLLM map[string]any
	if err := json.Unmarshal(res.body, &updatedLLM); err != nil {
		t.Fatalf("decode updated llm: %v", err)
	}
	if updatedLLM["enabled"] != true {
		t.Fatalf("updated llm enabled = %v, want true", updatedLLM["enabled"])
	}

	res = doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/settings", "", nil)
	if res.statusCode != http.StatusOK {
		t.Fatalf("get settings status = %d, want 200; body: %s", res.statusCode, string(res.body))
	}
	var settings map[string]any
	if err := json.Unmarshal(res.body, &settings); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if settings["llm_enabled"] != true {
		t.Fatalf("settings llm_enabled = %v, want true (mirror)", settings["llm_enabled"])
	}
}

func TestUpdateModuleAdminValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		method     string
		urlPath    string
		payload    any
		wantStatus int
	}{
		{
			name:    "watchaware config sync_interval_minutes 5 ok",
			method:  http.MethodPut,
			urlPath: "/system/modules/watchaware",
			payload: map[string]any{
				"enabled": true,
				"config":  map[string]any{"sync_interval_minutes": 5},
			},
			wantStatus: http.StatusOK,
		},
		{
			name:    "watchaware config sync_interval_minutes 1441 error problem",
			method:  http.MethodPut,
			urlPath: "/system/modules/watchaware",
			payload: map[string]any{
				"enabled": true,
				"config":  map[string]any{"sync_interval_minutes": 1441},
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:    "watchaware config unknown key error problem",
			method:  http.MethodPut,
			urlPath: "/system/modules/watchaware",
			payload: map[string]any{
				"enabled": true,
				"config":  map[string]any{"unknown_key": "val"},
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:    "llm config with unknown field error problem",
			method:  http.MethodPut,
			urlPath: "/system/modules/llm",
			payload: map[string]any{
				"enabled": true,
				"config":  map[string]any{"unexpected": true},
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name:       "unknown id nope GET 404 problem",
			method:     http.MethodGet,
			urlPath:    "/system/modules/nope",
			payload:    nil,
			wantStatus: http.StatusNotFound,
		},
		{
			name:    "unknown id nope PUT 404 problem",
			method:  http.MethodPut,
			urlPath: "/system/modules/nope",
			payload: map[string]any{
				"enabled": true,
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "malformed id Bad-Id GET 4xx problem",
			method:     http.MethodGet,
			urlPath:    "/system/modules/Bad-Id",
			payload:    nil,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:    "malformed id Bad-Id PUT 4xx problem",
			method:  http.MethodPut,
			urlPath: "/system/modules/Bad-Id",
			payload: map[string]any{
				"enabled": true,
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newRouterFixture(t)
			setupFixtureStubs(f)
			_ = completeInitialSetup(t, f)
			adminCsrf := getAdminCsrfToken(t, f)
			res := doHTTP(t, f.client, tc.method, f.srv.URL+PathPrefix+tc.urlPath, adminCsrf, tc.payload)
			if res.statusCode != tc.wantStatus {
				t.Fatalf("%s status = %d, want %d; body: %s", tc.name, res.statusCode, tc.wantStatus, string(res.body))
			}
		})
	}
}

func TestBoundaryDataOnDisable(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	adminCsrf := getAdminCsrfToken(t, f)

	// Step 1: Set config {"sync_interval_minutes": 15} and enable
	setReq := map[string]any{
		"enabled": true,
		"config":  map[string]any{"sync_interval_minutes": 15},
	}
	res := doHTTP(t, f.client, http.MethodPut, f.srv.URL+PathPrefix+"/system/modules/watchaware", adminCsrf, setReq)
	if res.statusCode != http.StatusOK {
		t.Fatalf("set config status = %d, want 200; body: %s", res.statusCode, string(res.body))
	}

	// Step 2: Disable module without providing config (config omitted)
	disableReq := map[string]any{
		"enabled": false,
	}
	res = doHTTP(t, f.client, http.MethodPut, f.srv.URL+PathPrefix+"/system/modules/watchaware", adminCsrf, disableReq)
	if res.statusCode != http.StatusOK {
		t.Fatalf("disable status = %d, want 200; body: %s", res.statusCode, string(res.body))
	}
	var disabledMod map[string]any
	if err := json.Unmarshal(res.body, &disabledMod); err != nil {
		t.Fatalf("decode disabled: %v", err)
	}
	if disabledMod["enabled"] != false {
		t.Errorf("disabled enabled = %v, want false", disabledMod["enabled"])
	}
	cfg, _ := disabledMod["config"].(map[string]any)
	if int(cfg["sync_interval_minutes"].(float64)) != 15 {
		t.Errorf("disabled config = %v, want sync_interval_minutes=15 preserved", cfg)
	}

	// Step 3: Enable module without providing config
	enableReq := map[string]any{
		"enabled": true,
	}
	res = doHTTP(t, f.client, http.MethodPut, f.srv.URL+PathPrefix+"/system/modules/watchaware", adminCsrf, enableReq)
	if res.statusCode != http.StatusOK {
		t.Fatalf("enable status = %d, want 200; body: %s", res.statusCode, string(res.body))
	}

	// Step 4: GET module returns same preserved config
	res = doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/system/modules/watchaware", "", nil)
	if res.statusCode != http.StatusOK {
		t.Fatalf("get status = %d, want 200; body: %s", res.statusCode, string(res.body))
	}
	var gotMod map[string]any
	if err := json.Unmarshal(res.body, &gotMod); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if gotMod["enabled"] != true {
		t.Errorf("get enabled = %v, want true", gotMod["enabled"])
	}
	cfg, _ = gotMod["config"].(map[string]any)
	if int(cfg["sync_interval_minutes"].(float64)) != 15 {
		t.Errorf("get config = %v, want sync_interval_minutes=15 preserved", cfg)
	}
}
