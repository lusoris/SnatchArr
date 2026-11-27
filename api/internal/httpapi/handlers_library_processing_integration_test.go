// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/domain"
)

func lpSetModule(t *testing.T, client *http.Client, base, csrf string, enabled bool) {
	t.Helper()
	res := doHTTP(t, client, http.MethodPut, base+"/system/modules/"+domain.ModuleLibraryProcessing, csrf, map[string]any{"enabled": enabled})
	if res.statusCode != http.StatusOK {
		t.Fatalf("set module enabled=%v status = %d, want 200; body: %s", enabled, res.statusCode, string(res.body))
	}
	m := lpDecode(t, res.body)
	if m["enabled"] != enabled {
		t.Fatalf("module enabled = %v, want %v", m["enabled"], enabled)
	}
}

func lpDecode(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("decode json object: %v; body: %s", err, string(body))
	}
	return m
}

func lpRuleBody(langs ...string) map[string]any {
	return map[string]any{"track": map[string]any{"languages": langs}}
}

func lpLanguages(t *testing.T, m map[string]any) []any {
	t.Helper()
	rule, ok := m["rule"].(map[string]any)
	if !ok {
		t.Fatalf("rule = %v, want object", m["rule"])
	}
	track, ok := rule["track"].(map[string]any)
	if !ok {
		t.Fatalf("rule.track = %v, want object", rule["track"])
	}
	langs, ok := track["languages"].([]any)
	if !ok {
		t.Fatalf("rule.track.languages = %v, want array", track["languages"])
	}
	return langs
}

func lpCreateRule(t *testing.T, client *http.Client, base, csrf string, payload map[string]any) map[string]any {
	t.Helper()
	res := doHTTP(t, client, http.MethodPost, base+"/library-processing/rules", csrf, payload)
	if res.statusCode != http.StatusCreated {
		t.Fatalf("create rule status = %d, want 201; body: %s", res.statusCode, string(res.body))
	}
	m := lpDecode(t, res.body)
	if id, ok := m["id"].(string); !ok || id == "" {
		t.Fatalf("created rule id = %v, want non-empty string", m["id"])
	}
	for _, k := range []string{"created_at", "updated_at"} {
		if s, ok := m[k].(string); !ok || s == "" {
			t.Errorf("created rule %s = %v, want non-empty timestamp", k, m[k])
		}
	}
	return m
}

func TestLibraryProcessingAdminCreateGetListResolve(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	csrf := getAdminCsrfToken(t, f)
	base := f.srv.URL + PathPrefix
	lpSetModule(t, f.client, base, csrf, true)

	created := lpCreateRule(t, f.client, base, csrf, map[string]any{
		"scope": "global_default",
		"rule":  lpRuleBody("en", "de"),
	})
	id := created["id"].(string)
	if created["scope"] != "global_default" {
		t.Errorf("created scope = %v, want global_default", created["scope"])
	}
	if got := lpLanguages(t, created); !reflect.DeepEqual(got, []any{"en", "de"}) {
		t.Errorf("created languages = %v, want [en de]", got)
	}

	// GET by id
	res := doHTTP(t, f.client, http.MethodGet, base+"/library-processing/rules/"+id, "", nil)
	if res.statusCode != http.StatusOK {
		t.Fatalf("get rule status = %d, want 200; body: %s", res.statusCode, string(res.body))
	}
	got := lpDecode(t, res.body)
	if got["id"] != id || got["scope"] != "global_default" {
		t.Errorf("get rule = %v, want id %s scope global_default", got, id)
	}
	if langs := lpLanguages(t, got); !reflect.DeepEqual(langs, []any{"en", "de"}) {
		t.Errorf("get languages = %v, want [en de]", langs)
	}

	// List contains the rule
	res = doHTTP(t, f.client, http.MethodGet, base+"/library-processing/rules", "", nil)
	if res.statusCode != http.StatusOK {
		t.Fatalf("list rules status = %d, want 200; body: %s", res.statusCode, string(res.body))
	}
	var list []map[string]any
	if err := json.Unmarshal(res.body, &list); err != nil {
		t.Fatalf("decode list: %v; body: %s", err, string(res.body))
	}
	if len(list) != 1 || list[0]["id"] != id {
		t.Fatalf("list = %v, want exactly the created rule %s", list, id)
	}

	// Resolve with no context falls back to the global default
	res = doHTTP(t, f.client, http.MethodGet, base+"/library-processing/resolve", "", nil)
	if res.statusCode != http.StatusOK {
		t.Fatalf("resolve status = %d, want 200; body: %s", res.statusCode, string(res.body))
	}
	resolved := lpDecode(t, res.body)
	if resolved["rule_id"] != id {
		t.Errorf("resolve rule_id = %v, want %s", resolved["rule_id"], id)
	}
	if resolved["scope"] != "global_default" {
		t.Errorf("resolve scope = %v, want global_default", resolved["scope"])
	}
	if langs := lpLanguages(t, resolved); len(langs) == 0 || !reflect.DeepEqual(langs, []any{"en", "de"}) {
		t.Errorf("resolve languages = %v, want non-empty [en de]", langs)
	}
}

func TestLibraryProcessingViewerMutationDeniedWithValidCSRF(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	adminCsrf := getAdminCsrfToken(t, f)
	base := f.srv.URL + PathPrefix
	lpSetModule(t, f.client, base, adminCsrf, true)
	viewerClient, viewerCsrf := loginViewerSession(t, f)

	res := doHTTP(t, viewerClient, http.MethodPost, base+"/library-processing/rules", viewerCsrf, map[string]any{
		"scope": "global_default",
		"rule":  lpRuleBody("en"),
	})
	if res.statusCode != http.StatusForbidden {
		t.Fatalf("viewer create status = %d, want 403; body: %s", res.statusCode, string(res.body))
	}

	// Admin creates a rule; viewer must not be able to update or delete it.
	created := lpCreateRule(t, f.client, base, adminCsrf, map[string]any{
		"scope": "global_default",
		"rule":  lpRuleBody("en"),
	})
	id := created["id"].(string)

	res = doHTTP(t, viewerClient, http.MethodPut, base+"/library-processing/rules/"+id, viewerCsrf, map[string]any{
		"scope": "global_default",
		"rule":  lpRuleBody("fr"),
	})
	if res.statusCode != http.StatusForbidden {
		t.Fatalf("viewer update status = %d, want 403; body: %s", res.statusCode, string(res.body))
	}
	res = doHTTP(t, viewerClient, http.MethodDelete, base+"/library-processing/rules/"+id, viewerCsrf, nil)
	if res.statusCode != http.StatusForbidden {
		t.Fatalf("viewer delete status = %d, want 403; body: %s", res.statusCode, string(res.body))
	}

	// Rule is unchanged and viewer can still read and resolve.
	res = doHTTP(t, viewerClient, http.MethodGet, base+"/library-processing/rules/"+id, "", nil)
	if res.statusCode != http.StatusOK {
		t.Fatalf("viewer get status = %d, want 200; body: %s", res.statusCode, string(res.body))
	}
	if langs := lpLanguages(t, lpDecode(t, res.body)); !reflect.DeepEqual(langs, []any{"en"}) {
		t.Errorf("rule languages after denied update = %v, want [en]", langs)
	}
	res = doHTTP(t, viewerClient, http.MethodGet, base+"/library-processing/resolve", "", nil)
	if res.statusCode != http.StatusOK {
		t.Fatalf("viewer resolve status = %d, want 200; body: %s", res.statusCode, string(res.body))
	}
}

func TestLibraryProcessingModuleOffOnPersistence(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	csrf := getAdminCsrfToken(t, f)
	base := f.srv.URL + PathPrefix

	// Module is off by default: endpoints are not available.
	res := doHTTP(t, f.client, http.MethodGet, base+"/library-processing/rules", "", nil)
	if res.statusCode != http.StatusNotFound {
		t.Fatalf("list while module off status = %d, want 404; body: %s", res.statusCode, string(res.body))
	}
	res = doHTTP(t, f.client, http.MethodPost, base+"/library-processing/rules", csrf, map[string]any{
		"scope": "global_default",
		"rule":  lpRuleBody("en"),
	})
	if res.statusCode != http.StatusNotFound {
		t.Fatalf("create while module off status = %d, want 404; body: %s", res.statusCode, string(res.body))
	}

	lpSetModule(t, f.client, base, csrf, true)
	created := lpCreateRule(t, f.client, base, csrf, map[string]any{
		"scope":         "user_language",
		"user_language": "de",
		"rule":          lpRuleBody("de", "en"),
	})
	id := created["id"].(string)

	// Disable: endpoints go away again.
	lpSetModule(t, f.client, base, csrf, false)
	res = doHTTP(t, f.client, http.MethodGet, base+"/library-processing/rules/"+id, "", nil)
	if res.statusCode != http.StatusNotFound {
		t.Fatalf("get while module disabled status = %d, want 404; body: %s", res.statusCode, string(res.body))
	}
	res = doHTTP(t, f.client, http.MethodGet, base+"/library-processing/resolve", "", nil)
	if res.statusCode != http.StatusNotFound {
		t.Fatalf("resolve while module disabled status = %d, want 404; body: %s", res.statusCode, string(res.body))
	}

	// Re-enable: stored rule survived.
	lpSetModule(t, f.client, base, csrf, true)
	res = doHTTP(t, f.client, http.MethodGet, base+"/library-processing/rules/"+id, "", nil)
	if res.statusCode != http.StatusOK {
		t.Fatalf("get after re-enable status = %d, want 200; body: %s", res.statusCode, string(res.body))
	}
	got := lpDecode(t, res.body)
	if got["id"] != id || got["scope"] != "user_language" || got["user_language"] != "de" {
		t.Errorf("persisted rule = %v, want id %s scope=user_language user_language=de", got, id)
	}
	if langs := lpLanguages(t, got); !reflect.DeepEqual(langs, []any{"de", "en"}) {
		t.Errorf("persisted languages = %v, want [de en]", langs)
	}
	if got["created_at"] != created["created_at"] {
		t.Errorf("created_at changed across disable/enable: %v -> %v", created["created_at"], got["created_at"])
	}
}

func TestLibraryProcessingResolveSelectedProvenance(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	csrf := getAdminCsrfToken(t, f)
	base := f.srv.URL + PathPrefix
	lpSetModule(t, f.client, base, csrf, true)

	global := lpCreateRule(t, f.client, base, csrf, map[string]any{
		"scope": "global_default",
		"rule":  lpRuleBody("en"),
	})
	userRule := lpCreateRule(t, f.client, base, csrf, map[string]any{
		"scope":         "user_language",
		"user_language": "de",
		"rule":          lpRuleBody("de", "en"),
	})

	// Matching requester language selects the more specific rule.
	res := doHTTP(t, f.client, http.MethodGet, base+"/library-processing/resolve?requester_language=de", "", nil)
	if res.statusCode != http.StatusOK {
		t.Fatalf("resolve de status = %d, want 200; body: %s", res.statusCode, string(res.body))
	}
	resolved := lpDecode(t, res.body)
	if resolved["rule_id"] != userRule["id"] {
		t.Errorf("resolve de rule_id = %v, want %v", resolved["rule_id"], userRule["id"])
	}
	if resolved["scope"] != "user_language" {
		t.Errorf("resolve de scope = %v, want user_language", resolved["scope"])
	}
	if langs := lpLanguages(t, resolved); !reflect.DeepEqual(langs, []any{"de", "en"}) {
		t.Errorf("resolve de languages = %v, want [de en]", langs)
	}

	// Non-matching requester language falls back to the global default.
	res = doHTTP(t, f.client, http.MethodGet, base+"/library-processing/resolve?requester_language=fr", "", nil)
	if res.statusCode != http.StatusOK {
		t.Fatalf("resolve fr status = %d, want 200; body: %s", res.statusCode, string(res.body))
	}
	resolved = lpDecode(t, res.body)
	if resolved["rule_id"] != global["id"] || resolved["scope"] != "global_default" {
		t.Errorf("resolve fr = %v, want global rule %v with scope global_default", resolved, global["id"])
	}
	if langs := lpLanguages(t, resolved); !reflect.DeepEqual(langs, []any{"en"}) {
		t.Errorf("resolve fr languages = %v, want [en]", langs)
	}
}

func TestLibraryProcessingInvalidRuleWrite(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	csrf := getAdminCsrfToken(t, f)
	base := f.srv.URL + PathPrefix
	lpSetModule(t, f.client, base, csrf, true)

	cases := []struct {
		name    string
		payload map[string]any
	}{
		{
			name: "unknown scope",
			payload: map[string]any{
				"scope": "nonsense",
				"rule":  lpRuleBody("en"),
			},
		},
		{
			name: "global default with forbidden user_language",
			payload: map[string]any{
				"scope":         "global_default",
				"user_language": "de",
				"rule":          lpRuleBody("en"),
			},
		},
		{
			name: "user_language scope missing user_language",
			payload: map[string]any{
				"scope": "user_language",
				"rule":  lpRuleBody("en"),
			},
		},
		{
			name: "invalid language code",
			payload: map[string]any{
				"scope": "global_default",
				"rule":  lpRuleBody("!!not-a-language!!"),
			},
		},
	}
	for _, tc := range cases {
		res := doHTTP(t, f.client, http.MethodPost, base+"/library-processing/rules", csrf, tc.payload)
		if res.statusCode != http.StatusBadRequest && res.statusCode != http.StatusUnprocessableEntity {
			t.Errorf("%s: status = %d, want 400 or 422; body: %s", tc.name, res.statusCode, string(res.body))
		}
	}

	// No invalid write may have been persisted.
	res := doHTTP(t, f.client, http.MethodGet, base+"/library-processing/rules", "", nil)
	if res.statusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200; body: %s", res.statusCode, string(res.body))
	}
	var list []map[string]any
	if err := json.Unmarshal(res.body, &list); err != nil {
		t.Fatalf("decode list: %v; body: %s", err, string(res.body))
	}
	if len(list) != 0 {
		t.Errorf("list after invalid writes = %v, want empty", list)
	}

	// Invalid replacement of an existing rule is rejected and leaves it intact.
	created := lpCreateRule(t, f.client, base, csrf, map[string]any{
		"scope": "global_default",
		"rule":  lpRuleBody("en"),
	})
	id := created["id"].(string)
	res = doHTTP(t, f.client, http.MethodPut, base+"/library-processing/rules/"+id, csrf, map[string]any{
		"scope":         "global_default",
		"user_language": "de",
		"rule":          lpRuleBody("fr"),
	})
	if res.statusCode != http.StatusBadRequest && res.statusCode != http.StatusUnprocessableEntity {
		t.Fatalf("invalid update status = %d, want 400 or 422; body: %s", res.statusCode, string(res.body))
	}
	res = doHTTP(t, f.client, http.MethodGet, base+"/library-processing/rules/"+id, "", nil)
	if res.statusCode != http.StatusOK {
		t.Fatalf("get after invalid update status = %d, want 200; body: %s", res.statusCode, string(res.body))
	}
	if langs := lpLanguages(t, lpDecode(t, res.body)); !reflect.DeepEqual(langs, []any{"en"}) {
		t.Errorf("languages after rejected update = %v, want [en]", langs)
	}
}
