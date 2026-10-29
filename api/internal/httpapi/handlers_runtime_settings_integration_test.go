// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
)

const plantedSettingValue = "fake-api-key-0123456789"

func TestRuntimeSettingsDirectHandler(t *testing.T) {
	t.Setenv("APP_SNATCHARR_ITEM_SEARCH_COOLDOWN_SECONDS", "500")
	f := newIntegrationFixture(t)

	admin := domain.User{ID: uuid.New(), Username: "admin-settings", Role: domain.RoleAdmin}
	viewer := domain.User{ID: uuid.New(), Username: "viewer-settings", Role: domain.RoleViewer}
	seedTestUser(t, f, admin)
	seedTestUser(t, f, viewer)

	adminCtx := withUser(t.Context(), admin)
	viewerCtx := withUser(t.Context(), viewer)

	testDirectAuthChecks(t, f, adminCtx, viewerCtx)
	testDirectValidationErrors(t, f, adminCtx)
	testDirectRoundTrip(t, f, adminCtx)
}

func testDirectAuthChecks(t *testing.T, f *integrationFixture, adminCtx, viewerCtx context.Context) {
	t.Helper()
	_, err := f.h.GetRuntimeSettings(viewerCtx)
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("viewer GetRuntimeSettings() err = %v, want ErrForbidden", err)
	}

	_, err = f.h.UpdateRuntimeSettings(viewerCtx, &oas.RuntimeSettingsUpdateRequest{
		Values: map[string]string{"snatcharr.session.ttl": "24h"},
	})
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("viewer UpdateRuntimeSettings() err = %v, want ErrForbidden", err)
	}

	res, err := f.h.GetRuntimeSettings(adminCtx)
	if err != nil {
		t.Fatalf("admin GetRuntimeSettings() err = %v", err)
	}
	okRes, ok := res.(*oas.GetRuntimeSettingsOKApplicationJSON)
	if !ok {
		t.Fatalf("expected *oas.GetRuntimeSettingsOKApplicationJSON, got %T", res)
	}
	if len(*okRes) == 0 {
		t.Fatalf("expected non-empty runtime settings list")
	}
}

func testDirectValidationErrors(t *testing.T, f *integrationFixture, adminCtx context.Context) {
	t.Helper()
	// Unknown key -> 422
	_, err := f.h.UpdateRuntimeSettings(adminCtx, &oas.RuntimeSettingsUpdateRequest{
		Values: map[string]string{"snatcharr.unknown.key": "foo"},
	})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("unknown key err = %v, want ErrInvalid", err)
	}

	// Bootstrap key -> 422 read-only
	_, err = f.h.UpdateRuntimeSettings(adminCtx, &oas.RuntimeSettingsUpdateRequest{
		Values: map[string]string{"snatcharr.profile": "prod"},
	})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("bootstrap key err = %v, want ErrInvalid", err)
	}

	// Env-locked -> 409 Conflict
	_, err = f.h.UpdateRuntimeSettings(adminCtx, &oas.RuntimeSettingsUpdateRequest{
		Values: map[string]string{"snatcharr.item_search.cooldown_seconds": "1000"},
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("env-locked err = %v, want ErrConflict", err)
	}

	// Invalid value failing Options.Validate -> 422
	_, err = f.h.UpdateRuntimeSettings(adminCtx, &oas.RuntimeSettingsUpdateRequest{
		Values: map[string]string{"snatcharr.session.ttl": "10s"},
	})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("invalid session ttl err = %v, want ErrInvalid", err)
	}
}

func testDirectRoundTrip(t *testing.T, f *integrationFixture, adminCtx context.Context) {
	t.Helper()
	res, err := f.h.UpdateRuntimeSettings(adminCtx, &oas.RuntimeSettingsUpdateRequest{
		Values: map[string]string{"snatcharr.session.ttl": "48h"},
	})
	if err != nil {
		t.Fatalf("UpdateRuntimeSettings() err = %v", err)
	}
	upRes, ok := res.(*oas.RuntimeSettingsUpdateResult)
	if !ok {
		t.Fatalf("expected *oas.RuntimeSettingsUpdateResult, got %T", res)
	}
	if !upRes.PendingRestart {
		t.Errorf("expected PendingRestart = true")
	}

	listRes, err := f.h.GetRuntimeSettings(adminCtx)
	if err != nil {
		t.Fatalf("GetRuntimeSettings() err = %v", err)
	}
	list, ok := listRes.(*oas.GetRuntimeSettingsOKApplicationJSON)
	if !ok {
		t.Fatalf("expected *oas.GetRuntimeSettingsOKApplicationJSON, got %T", listRes)
	}
	found := false
	for _, item := range *list {
		if item.Key == "snatcharr.session.ttl" {
			found = true
			if !item.Value.IsSet() || item.Value.Value != "48h" {
				t.Errorf("expected session.ttl value=48h, got %+v", item.Value)
			}
			if item.Source != oas.RuntimeSettingSourceDb {
				t.Errorf("expected source=db, got %s", item.Source)
			}
		}
	}
	if !found {
		t.Errorf("snatcharr.session.ttl not found in settings list")
	}
}

func TestRuntimeSettingsHTTPEndpoints(t *testing.T) {
	t.Setenv("APP_SNATCHARR_ITEM_SEARCH_COOLDOWN_SECONDS", "500")
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	adminCsrf := getAdminCsrfToken(t, f)

	testHTTPAuthChecks(t, f)
	testHTTPValidationErrors(t, f, adminCsrf)
	testHTTPSecretNeverLeaked(t, f, adminCsrf)
}

func testHTTPAuthChecks(t *testing.T, f *routerFixture) {
	t.Helper()
	anonGet := doHTTP(t, &http.Client{}, http.MethodGet, f.srv.URL+PathPrefix+"/settings/runtime", "", nil)
	assertProblemStatusAndType(t, anonGet, http.StatusUnauthorized)

	anonPut := doHTTP(t, &http.Client{}, http.MethodPut, f.srv.URL+PathPrefix+"/settings/runtime", "", map[string]any{
		"values": map[string]string{"snatcharr.session.ttl": "24h"},
	})
	assertProblemStatusAndType(t, anonPut, http.StatusUnauthorized)

	viewerClient, viewerCsrf := loginViewerSession(t, f)
	viewerGet := doHTTP(t, viewerClient, http.MethodGet, f.srv.URL+PathPrefix+"/settings/runtime", "", nil)
	assertProblemStatusAndType(t, viewerGet, http.StatusForbidden)

	viewerPut := doHTTP(t, viewerClient, http.MethodPut, f.srv.URL+PathPrefix+"/settings/runtime", viewerCsrf, map[string]any{
		"values": map[string]string{"snatcharr.session.ttl": "24h"},
	})
	assertProblemStatusAndType(t, viewerPut, http.StatusForbidden)

	adminGet := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/settings/runtime", "", nil)
	if adminGet.statusCode != http.StatusOK {
		t.Fatalf("GET /settings/runtime status = %d, want 200", adminGet.statusCode)
	}
}

func testHTTPValidationErrors(t *testing.T, f *routerFixture, adminCsrf string) {
	t.Helper()
	// Unknown key -> 422
	badKey := doHTTP(t, f.client, http.MethodPut, f.srv.URL+PathPrefix+"/settings/runtime", adminCsrf, map[string]any{
		"values": map[string]string{"snatcharr.unknown.key": "val"},
	})
	assertProblemStatusAndType(t, badKey, http.StatusUnprocessableEntity)

	// Bootstrap key -> 422
	bootstrapKey := doHTTP(t, f.client, http.MethodPut, f.srv.URL+PathPrefix+"/settings/runtime", adminCsrf, map[string]any{
		"values": map[string]string{"snatcharr.profile": "prod"},
	})
	assertProblemStatusAndType(t, bootstrapKey, http.StatusUnprocessableEntity)

	// Env locked -> 409
	lockedKey := doHTTP(t, f.client, http.MethodPut, f.srv.URL+PathPrefix+"/settings/runtime", adminCsrf, map[string]any{
		"values": map[string]string{"snatcharr.item_search.cooldown_seconds": "1000"},
	})
	assertProblemStatusAndType(t, lockedKey, http.StatusConflict)

	// Invalid value failing Options.Validate -> 422
	invalidVal := doHTTP(t, f.client, http.MethodPut, f.srv.URL+PathPrefix+"/settings/runtime", adminCsrf, map[string]any{
		"values": map[string]string{"snatcharr.session.ttl": "10s"},
	})
	assertProblemStatusAndType(t, invalidVal, http.StatusUnprocessableEntity)
}

func testHTTPSecretNeverLeaked(t *testing.T, f *routerFixture, adminCsrf string) {
	t.Helper()
	putRes := doHTTP(t, f.client, http.MethodPut, f.srv.URL+PathPrefix+"/settings/runtime", adminCsrf, map[string]any{
		"values": map[string]string{
			"snatcharr.setup.secret": plantedSettingValue,
		},
	})
	if putRes.statusCode != http.StatusOK {
		t.Fatalf("PUT /settings/runtime status = %d, want 200; body: %s", putRes.statusCode, string(putRes.body))
	}

	// 1. MUST assert planted secret string is NEVER in the PUT response body
	if strings.Contains(string(putRes.body), plantedSettingValue) {
		t.Fatalf("CRITICAL SECURITY INVARIANT: planted secret string leaked in PUT response body: %s", string(putRes.body))
	}

	// 2. MUST assert planted secret string is NEVER in subsequent GET response body
	getRes := doHTTP(t, f.client, http.MethodGet, f.srv.URL+PathPrefix+"/settings/runtime", "", nil)
	if getRes.statusCode != http.StatusOK {
		t.Fatalf("GET /settings/runtime status = %d, want 200", getRes.statusCode)
	}
	if strings.Contains(string(getRes.body), plantedSettingValue) {
		t.Fatalf("CRITICAL SECURITY INVARIANT: planted secret string leaked in GET response body: %s", string(getRes.body))
	}

	// 3. Verify is_set is true for the secret setting
	var items []map[string]any
	if err := json.Unmarshal(getRes.body, &items); err != nil {
		t.Fatalf("unmarshal GET response: %v", err)
	}
	foundSecret := false
	for _, it := range items {
		if it["key"] == "snatcharr.setup.secret" {
			foundSecret = true
			if isSet, ok := it["is_set"].(bool); !ok || !isSet {
				t.Errorf("expected is_set=true for snatcharr.setup.secret, got %v", it["is_set"])
			}
			if _, hasVal := it["value"]; hasVal {
				t.Errorf("value field MUST be absent for secret setting, got %v", it["value"])
			}
		}
	}
	if !foundSecret {
		t.Errorf("snatcharr.setup.secret not found in response items")
	}
}
