// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/instances"
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

func lpCreateInstance(t *testing.T, f *routerFixture, name string) string {
	t.Helper()
	inst, err := f.h.instances.Create(t.Context(), instances.Input{
		Kind:    domain.KindSonarr,
		Name:    name,
		BaseURL: "http://" + name + ".invalid:8989",
		APIKey:  "test-secret-key",
		Enabled: true,
	})
	if err != nil {
		t.Fatalf("create instance %s: %v", name, err)
	}
	return inst.ID.String()
}

func lpGetRule(t *testing.T, client *http.Client, base, id string) map[string]any {
	t.Helper()
	res := doHTTP(t, client, http.MethodGet, base+"/library-processing/rules/"+id, "", nil)
	if res.statusCode != http.StatusOK {
		t.Fatalf("get rule %s status = %d, want 200; body: %s", id, res.statusCode, string(res.body))
	}
	return lpDecode(t, res.body)
}

func lpListRules(t *testing.T, client *http.Client, base string) []map[string]any {
	t.Helper()
	res := doHTTP(t, client, http.MethodGet, base+"/library-processing/rules", "", nil)
	if res.statusCode != http.StatusOK {
		t.Fatalf("list rules status = %d, want 200; body: %s", res.statusCode, string(res.body))
	}
	var list []map[string]any
	if err := json.Unmarshal(res.body, &list); err != nil {
		t.Fatalf("decode list: %v; body: %s", err, string(res.body))
	}
	return list
}

func lpAssertProblem(t *testing.T, res httpResult, wantStatus int) {
	t.Helper()
	if res.statusCode != wantStatus {
		t.Fatalf("status = %d, want %d; body: %s", res.statusCode, wantStatus, string(res.body))
	}
	prob := lpDecode(t, res.body)
	if prob["type"] != "about:blank" {
		t.Errorf("problem type = %v, want about:blank", prob["type"])
	}
	if prob["title"] != http.StatusText(wantStatus) {
		t.Errorf("problem title = %v, want %q", prob["title"], http.StatusText(wantStatus))
	}
	if status, ok := prob["status"].(float64); !ok || int(status) != wantStatus {
		t.Errorf("problem status = %v, want %d", prob["status"], wantStatus)
	}
	if detail, ok := prob["detail"].(string); !ok || detail == "" {
		t.Errorf("problem detail = %v, want non-empty string", prob["detail"])
	}
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

	// Disable: read and admin mutation endpoints are rejected and do not touch stored rows.
	lpSetModule(t, f.client, base, csrf, false)
	f.clk.Advance(time.Minute)
	lpAssertProblem(t, doHTTP(t, f.client, http.MethodGet, base+"/library-processing/rules/"+id, "", nil), http.StatusNotFound)
	lpAssertProblem(t, doHTTP(t, f.client, http.MethodGet, base+"/library-processing/resolve", "", nil), http.StatusNotFound)
	lpAssertProblem(t, doHTTP(t, f.client, http.MethodPut, base+"/library-processing/rules/"+id, csrf, map[string]any{
		"scope": "global_default",
		"rule":  lpRuleBody("fr"),
	}), http.StatusNotFound)
	lpAssertProblem(t, doHTTP(t, f.client, http.MethodDelete, base+"/library-processing/rules/"+id, csrf, nil), http.StatusNotFound)

	// Re-enable: stored rule survived unchanged across rejected admin update and delete.
	lpSetModule(t, f.client, base, csrf, true)
	got := lpGetRule(t, f.client, base, id)
	if !reflect.DeepEqual(got, created) {
		t.Errorf("persisted rule after disable/enable = %v, want %v", got, created)
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

func TestLibraryProcessingResolveMalformedQuery(t *testing.T) {
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
	instanceID := lpCreateInstance(t, f, "resolve-query")
	libRule := lpCreateRule(t, f.client, base, csrf, map[string]any{
		"scope":       "library",
		"instance_id": instanceID,
		"root_path":   "/media/TV Shows",
		"rule":        lpRuleBody("ja", "en"),
	})

	// Malformed percent encoding in the library selector is a bad request.
	res := doHTTP(t, f.client, http.MethodGet, base+"/library-processing/resolve?instance_id="+instanceID+"&root_path=%zz", "", nil)
	lpAssertProblem(t, res, http.StatusBadRequest)

	// A valid percent-encoded root resolves to the library-specific rule.
	res = doHTTP(t, f.client, http.MethodGet, base+"/library-processing/resolve?instance_id="+instanceID+"&root_path=%2Fmedia%2FTV%20Shows", "", nil)
	if res.statusCode != http.StatusOK {
		t.Fatalf("resolve encoded root status = %d, want 200; body: %s", res.statusCode, string(res.body))
	}
	resolved := lpDecode(t, res.body)
	if resolved["rule_id"] != libRule["id"] {
		t.Errorf("resolve encoded root rule_id = %v, want %v", resolved["rule_id"], libRule["id"])
	}

	// Omitting the selector falls back to the global default.
	res = doHTTP(t, f.client, http.MethodGet, base+"/library-processing/resolve", "", nil)
	if res.statusCode != http.StatusOK {
		t.Fatalf("resolve omitted status = %d, want 200; body: %s", res.statusCode, string(res.body))
	}
	resolved = lpDecode(t, res.body)
	if resolved["rule_id"] != global["id"] {
		t.Errorf("resolve omitted rule_id = %v, want %v", resolved["rule_id"], global["id"])
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
	instID := lpCreateInstance(t, f, "lp-invalid")

	cases := []struct {
		name    string
		payload map[string]any
	}{
		{
			name:    "unknown scope",
			payload: map[string]any{"scope": "nonsense", "rule": lpRuleBody("en")},
		},
		{
			name:    "global default with forbidden user_language",
			payload: map[string]any{"scope": "global_default", "user_language": "de", "rule": lpRuleBody("en")},
		},
		{
			name:    "item_override missing instance_id",
			payload: map[string]any{"scope": "item_override", "item_id": "ep-1", "rule": lpRuleBody("en")},
		},
		{
			name:    "item_override missing item_id",
			payload: map[string]any{"scope": "item_override", "instance_id": instID, "rule": lpRuleBody("en")},
		},
		{
			name:    "item_override with whitespace-padded item_id",
			payload: map[string]any{"scope": "item_override", "instance_id": instID, "item_id": " ep-1 ", "rule": lpRuleBody("en")},
		},
		{
			name:    "item_override with forbidden root_path",
			payload: map[string]any{"scope": "item_override", "instance_id": instID, "item_id": "ep-1", "root_path": "/media/tv", "rule": lpRuleBody("en")},
		},
		{
			name:    "user_language scope missing user_language",
			payload: map[string]any{"scope": "user_language", "rule": lpRuleBody("en")},
		},
		{
			name:    "user_language undetermined code",
			payload: map[string]any{"scope": "user_language", "user_language": "und", "rule": lpRuleBody("en")},
		},
		{
			name:    "user_language uppercase code",
			payload: map[string]any{"scope": "user_language", "user_language": "DE", "rule": lpRuleBody("en")},
		},
		{
			name:    "user_language with forbidden instance_id",
			payload: map[string]any{"scope": "user_language", "user_language": "de", "instance_id": instID, "rule": lpRuleBody("en")},
		},
		{
			name:    "library missing instance_id",
			payload: map[string]any{"scope": "library", "root_path": "/media/tv", "rule": lpRuleBody("en")},
		},
		{
			name:    "library missing root_path",
			payload: map[string]any{"scope": "library", "instance_id": instID, "rule": lpRuleBody("en")},
		},
		{
			name:    "library relative root_path",
			payload: map[string]any{"scope": "library", "instance_id": instID, "root_path": "media/tv", "rule": lpRuleBody("en")},
		},
		{
			name:    "library non-clean root_path",
			payload: map[string]any{"scope": "library", "instance_id": instID, "root_path": "/media/../tv", "rule": lpRuleBody("en")},
		},
		{
			name:    "library with forbidden item_id",
			payload: map[string]any{"scope": "library", "instance_id": instID, "root_path": "/media/tv", "item_id": "ep-1", "rule": lpRuleBody("en")},
		},
		{
			name:    "instance missing instance_id",
			payload: map[string]any{"scope": "instance", "rule": lpRuleBody("en")},
		},
		{
			name:    "instance nil uuid",
			payload: map[string]any{"scope": "instance", "instance_id": uuid.Nil.String(), "rule": lpRuleBody("en")},
		},
		{
			name:    "instance with forbidden root_path",
			payload: map[string]any{"scope": "instance", "instance_id": instID, "root_path": "/media/tv", "rule": lpRuleBody("en")},
		},
		{
			name:    "media_type missing media_type",
			payload: map[string]any{"scope": "media_type", "rule": lpRuleBody("en")},
		},
		{
			name:    "media_type unknown value",
			payload: map[string]any{"scope": "media_type", "media_type": "podcast", "rule": lpRuleBody("en")},
		},
		{
			name:    "media_type with forbidden instance_id",
			payload: map[string]any{"scope": "media_type", "media_type": "movie", "instance_id": instID, "rule": lpRuleBody("en")},
		},
		{
			name:    "invalid language code",
			payload: map[string]any{"scope": "global_default", "rule": lpRuleBody("!!not-a-language!!")},
		},
	}
	for _, tc := range cases {
		res := doHTTP(t, f.client, http.MethodPost, base+"/library-processing/rules", csrf, tc.payload)
		if res.statusCode != http.StatusUnprocessableEntity {
			t.Errorf("%s: status = %d, want 422; body: %s", tc.name, res.statusCode, string(res.body))
			continue
		}
		lpAssertProblem(t, res, http.StatusUnprocessableEntity)
	}

	// Instance-scoped resolve targets without instance_id or with unclean root_path are rejected.
	for _, rawQuery := range []string{
		"item_id=ep-1",
		"root_path=/media/tv",
		"instance_id=" + url.QueryEscape(instID) + "&root_path=media/tv",
	} {
		lpAssertProblem(t, doHTTP(t, f.client, http.MethodGet, base+"/library-processing/resolve?"+rawQuery, "", nil), http.StatusUnprocessableEntity)
	}

	// No invalid write may have been persisted.
	if list := lpListRules(t, f.client, base); len(list) != 0 {
		t.Errorf("list after invalid writes = %v, want empty", list)
	}

	// Invalid replacement of an existing rule is rejected and leaves it intact.
	created := lpCreateRule(t, f.client, base, csrf, map[string]any{
		"scope": "global_default",
		"rule":  lpRuleBody("en"),
	})
	id := created["id"].(string)
	f.clk.Advance(time.Minute)
	lpAssertProblem(t, doHTTP(t, f.client, http.MethodPut, base+"/library-processing/rules/"+id, csrf, map[string]any{
		"scope":         "global_default",
		"user_language": "de",
		"rule":          lpRuleBody("fr"),
	}), http.StatusUnprocessableEntity)
	if got := lpGetRule(t, f.client, base, id); !reflect.DeepEqual(got, created) {
		t.Errorf("rule after rejected update = %v, want %v", got, created)
	}
}

func TestLibraryProcessingScopeConflictPreservesData(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	csrf := getAdminCsrfToken(t, f)
	base := f.srv.URL + PathPrefix
	lpSetModule(t, f.client, base, csrf, true)
	instID := lpCreateInstance(t, f, "lp-conflict")

	seedIdentities := []map[string]any{
		{"scope": "item_override", "instance_id": instID, "item_id": "ep-10"},
		{"scope": "user_language", "user_language": "de"},
		{"scope": "library", "instance_id": instID, "root_path": "/media/tv"},
		{"scope": "instance", "instance_id": instID},
		{"scope": "media_type", "media_type": "movie"},
		{"scope": "global_default"},
	}
	seedLangs := []string{"en", "de", "fr", "es", "it", "ja"}

	originals := make([]map[string]any, 0, len(seedIdentities)+1)
	for i, target := range seedIdentities {
		payload := map[string]any{"rule": lpRuleBody(seedLangs[i])}
		maps.Copy(payload, target)
		originals = append(originals, lpCreateRule(t, f.client, base, csrf, payload))
	}
	secondLib := lpCreateRule(t, f.client, base, csrf, map[string]any{
		"scope":       "library",
		"instance_id": instID,
		"root_path":   "/media/movies",
		"rule":        lpRuleBody("ko"),
	})
	originals = append(originals, secondLib)

	f.clk.Advance(time.Minute)

	// Duplicate scoped creates across all six uniqueness keys (plus ISO 639-3 alias "deu" for "de").
	dupTargets := append(
		append([]map[string]any{}, seedIdentities...),
		map[string]any{"scope": "user_language", "user_language": "deu"},
	)
	for _, target := range dupTargets {
		payload := map[string]any{"rule": lpRuleBody("pt")}
		maps.Copy(payload, target)
		res := doHTTP(t, f.client, http.MethodPost, base+"/library-processing/rules", csrf, payload)
		lpAssertProblem(t, res, http.StatusConflict)
	}

	// Identity-colliding updates (same-scope target, cross-scope singleton, canonical ISO 639 alias).
	secondLibID := secondLib["id"].(string)
	for _, target := range []map[string]any{
		{"scope": "library", "instance_id": instID, "root_path": "/media/tv"},
		{"scope": "global_default"},
		{"scope": "user_language", "user_language": "deu"},
	} {
		payload := map[string]any{"rule": lpRuleBody("pt")}
		maps.Copy(payload, target)
		res := doHTTP(t, f.client, http.MethodPut, base+"/library-processing/rules/"+secondLibID, csrf, payload)
		lpAssertProblem(t, res, http.StatusConflict)
	}

	// Row count and every rule's stored identity, languages, and timestamps remain untouched.
	if list := lpListRules(t, f.client, base); len(list) != len(originals) {
		t.Fatalf("rule count after conflicts = %d, want %d", len(list), len(originals))
	}
	for _, orig := range originals {
		id := orig["id"].(string)
		if got := lpGetRule(t, f.client, base, id); !reflect.DeepEqual(got, orig) {
			t.Errorf("rule %s mutated on conflict:\n got: %v\nwant: %v", id, got, orig)
		}
	}
}

func TestLibraryProcessingIdentityChangePersistsAndResolves(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	csrf := getAdminCsrfToken(t, f)
	base := f.srv.URL + PathPrefix
	lpSetModule(t, f.client, base, csrf, true)
	instID := lpCreateInstance(t, f, "lp-rekey")

	created := lpCreateRule(t, f.client, base, csrf, map[string]any{
		"scope":      "media_type",
		"media_type": "movie",
		"rule":       lpRuleBody("en"),
	})
	id := created["id"].(string)
	createdAt := created["created_at"].(string)

	// Cross-scope identity move: media_type -> library on the same rule ID.
	f.clk.Advance(time.Minute)
	res := doHTTP(t, f.client, http.MethodPut, base+"/library-processing/rules/"+id, csrf, map[string]any{
		"scope":       "library",
		"instance_id": instID,
		"root_path":   "/media/anime",
		"rule":        lpRuleBody("ja", "en"),
	})
	if res.statusCode != http.StatusOK {
		t.Fatalf("update to library status = %d, want 200; body: %s", res.statusCode, string(res.body))
	}
	updatedLib := lpDecode(t, res.body)
	if updatedLib["id"] != id || updatedLib["created_at"] != createdAt || updatedLib["updated_at"] == created["updated_at"] {
		t.Fatalf("updated library rule metadata = %v, want preserved id/created_at and advanced updated_at", updatedLib)
	}
	if updatedLib["scope"] != "library" || updatedLib["instance_id"] != instID || updatedLib["root_path"] != "/media/anime" {
		t.Errorf("updated library target = %v, want library (%s, /media/anime)", updatedLib, instID)
	}
	if _, hasOldMediaType := updatedLib["media_type"]; hasOldMediaType {
		t.Errorf("stale media_type retained after scope change: %v", updatedLib)
	}
	if got := lpGetRule(t, f.client, base, id); !reflect.DeepEqual(got, updatedLib) {
		t.Errorf("persisted library rule = %v, want %v", got, updatedLib)
	}
	if list := lpListRules(t, f.client, base); len(list) != 1 || list[0]["id"] != id {
		t.Fatalf("list after identity change = %v, want single rule %s", list, id)
	}

	// Old identity no longer matches; new identity resolves with preserved rule_id and updated scope provenance.
	lpAssertProblem(t, doHTTP(t, f.client, http.MethodGet, base+"/library-processing/resolve?media_type=movie", "", nil), http.StatusNotFound)

	q := url.Values{}
	q.Set("instance_id", instID)
	q.Set("root_path", "/media/anime")
	q.Set("media_type", "movie")
	res = doHTTP(t, f.client, http.MethodGet, base+"/library-processing/resolve?"+q.Encode(), "", nil)
	if res.statusCode != http.StatusOK {
		t.Fatalf("resolve library status = %d, want 200; body: %s", res.statusCode, string(res.body))
	}
	resolved := lpDecode(t, res.body)
	if resolved["rule_id"] != id || resolved["scope"] != "library" || !reflect.DeepEqual(lpLanguages(t, resolved), []any{"ja", "en"}) {
		t.Errorf("resolved library rule = %v, want rule_id=%s scope=library languages=[ja en]", resolved, id)
	}

	// Second move: library -> item_override clears root_path and re-keys resolution to item_id.
	f.clk.Advance(time.Minute)
	res = doHTTP(t, f.client, http.MethodPut, base+"/library-processing/rules/"+id, csrf, map[string]any{
		"scope":       "item_override",
		"instance_id": instID,
		"item_id":     "ep-42",
		"rule":        lpRuleBody("de"),
	})
	if res.statusCode != http.StatusOK {
		t.Fatalf("update to item_override status = %d, want 200; body: %s", res.statusCode, string(res.body))
	}
	updatedItem := lpDecode(t, res.body)
	if _, hasOldRoot := updatedItem["root_path"]; hasOldRoot {
		t.Errorf("stale root_path retained after scope change: %v", updatedItem)
	}
	if updatedItem["id"] != id || updatedItem["created_at"] != createdAt || updatedItem["updated_at"] == updatedLib["updated_at"] {
		t.Errorf("updated item rule metadata = %v, want preserved id/created_at and advanced updated_at", updatedItem)
	}
	lpAssertProblem(t, doHTTP(t, f.client, http.MethodGet, base+"/library-processing/resolve?"+q.Encode(), "", nil), http.StatusNotFound)

	q.Set("item_id", "ep-42")
	res = doHTTP(t, f.client, http.MethodGet, base+"/library-processing/resolve?"+q.Encode(), "", nil)
	if res.statusCode != http.StatusOK {
		t.Fatalf("resolve item_override status = %d, want 200; body: %s", res.statusCode, string(res.body))
	}
	resolved = lpDecode(t, res.body)
	if resolved["rule_id"] != id || resolved["scope"] != "item_override" || !reflect.DeepEqual(lpLanguages(t, resolved), []any{"de"}) {
		t.Errorf("resolved item_override rule = %v, want rule_id=%s scope=item_override languages=[de]", resolved, id)
	}
}

func TestLibraryProcessingDeleteAndUnknownIDs(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)
	csrf := getAdminCsrfToken(t, f)
	base := f.srv.URL + PathPrefix
	lpSetModule(t, f.client, base, csrf, true)

	unknownRuleID := uuid.NewString()
	lpAssertProblem(t, doHTTP(t, f.client, http.MethodGet, base+"/library-processing/rules/"+unknownRuleID, "", nil), http.StatusNotFound)
	lpAssertProblem(t, doHTTP(t, f.client, http.MethodPut, base+"/library-processing/rules/"+unknownRuleID, csrf, map[string]any{
		"scope": "global_default",
		"rule":  lpRuleBody("en"),
	}), http.StatusNotFound)
	lpAssertProblem(t, doHTTP(t, f.client, http.MethodDelete, base+"/library-processing/rules/"+unknownRuleID, csrf, nil), http.StatusNotFound)

	unknownInstID := uuid.NewString()
	lpAssertProblem(t, doHTTP(t, f.client, http.MethodPost, base+"/library-processing/rules", csrf, map[string]any{
		"scope":       "instance",
		"instance_id": unknownInstID,
		"rule":        lpRuleBody("en"),
	}), http.StatusNotFound)

	created := lpCreateRule(t, f.client, base, csrf, map[string]any{
		"scope":      "media_type",
		"media_type": "anime",
		"rule":       lpRuleBody("ja"),
	})
	id := created["id"].(string)

	f.clk.Advance(time.Minute)
	lpAssertProblem(t, doHTTP(t, f.client, http.MethodPut, base+"/library-processing/rules/"+id, csrf, map[string]any{
		"scope":       "instance",
		"instance_id": unknownInstID,
		"rule":        lpRuleBody("en"),
	}), http.StatusNotFound)
	if got := lpGetRule(t, f.client, base, id); !reflect.DeepEqual(got, created) {
		t.Fatalf("rule after update with unknown instance_id = %v, want %v", got, created)
	}

	delRes := doHTTP(t, f.client, http.MethodDelete, base+"/library-processing/rules/"+id, csrf, nil)
	if delRes.statusCode != http.StatusNoContent || len(delRes.body) != 0 {
		t.Fatalf("delete status = %d body = %q, want 204 empty", delRes.statusCode, string(delRes.body))
	}
	lpAssertProblem(t, doHTTP(t, f.client, http.MethodGet, base+"/library-processing/rules/"+id, "", nil), http.StatusNotFound)
	if list := lpListRules(t, f.client, base); len(list) != 0 {
		t.Errorf("list after delete = %v, want empty", list)
	}
	lpAssertProblem(t, doHTTP(t, f.client, http.MethodGet, base+"/library-processing/resolve?media_type=anime", "", nil), http.StatusNotFound)
	lpAssertProblem(t, doHTTP(t, f.client, http.MethodDelete, base+"/library-processing/rules/"+id, csrf, nil), http.StatusNotFound)
}
