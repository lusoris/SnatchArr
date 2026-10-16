// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golusoris/goenvoy/arr/v2"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/golusoris/golusoris/auth/apikey"
	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/httpx/csrf"

	"github.com/lusoris/SnatchArr/api/internal/auth"
	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/seerrclient"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

type routerFixture struct {
	*integrationFixture
	srv    *httptest.Server
	client *http.Client
	jar    http.CookieJar
	keys   *apikey.Service
}

func newRouterFixture(t *testing.T) *routerFixture {
	t.Helper()
	f := newIntegrationFixture(t)
	csrfMW, err := csrf.New(csrf.Options{})
	if err != nil {
		t.Fatalf("csrf.New: %v", err)
	}
	keyStore := auth.NewPgAPIKeyStore(f.st, f.clk)
	keys, err := apikey.New(keyStore, apikey.Options{
		Prefix:     "sk",
		HMACSecret: []byte("test-secret-of-at-least-32-bytes"),
		Clock:      f.clk,
	})
	if err != nil {
		t.Fatalf("apikey.New: %v", err)
	}
	apiKeys := auth.NewAPIKeys(f.st, keys, f.clk, slog.New(slog.DiscardHandler))
	f.h.apiKeys = apiKeys
	r := chi.NewRouter()
	err = Mount(r, MountParams{
		Logger:   slog.New(slog.DiscardHandler),
		Handlers: f.h,
		Security: NewSecurity(f.sessMgr, keys, f.h.auth, apiKeys, f.clk, slog.New(slog.DiscardHandler)),
		CSRF:     csrfMW,
	})
	if err != nil {
		t.Fatalf("Mount: %v", err)
	}
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
		Jar:     jar,
	}
	return &routerFixture{
		integrationFixture: f,
		srv:                srv,
		client:             client,
		jar:                jar,
		keys:               keys,
	}
}

type specOp struct {
	method      string
	path        string
	operationID string
	public      bool
	hasBody     bool
}

type openAPISpec struct {
	Paths map[string]map[string]yaml.Node `yaml:"paths"`
}

type openAPIOperation struct {
	OperationID string    `yaml:"operationId"` //nolint:tagliatelle // OpenAPI 3.1 YAML uses camelCase field names
	Security    *[]rawSec `yaml:"security"`
	RequestBody *rawBody  `yaml:"requestBody"` //nolint:tagliatelle // OpenAPI 3.1 YAML uses camelCase field names
}

type rawSec map[string][]string

type rawBody struct{}

func specOperations(t *testing.T) []specOp {
	t.Helper()
	specPath := filepath.Clean("../../openapi/openapi.yaml")
	data, err := os.ReadFile(specPath) // #nosec G304 -- static repo path
	if err != nil {
		t.Fatalf("read openapi.yaml: %v", err)
	}
	var spec openAPISpec
	if err = yaml.Unmarshal(data, &spec); err != nil {
		t.Fatalf("parse openapi.yaml: %v", err)
	}

	methods := map[string]bool{
		"get": true, "post": true, "put": true, "delete": true, "patch": true,
	}

	ops := make([]specOp, 0, len(spec.Paths))
	for p, item := range spec.Paths {
		for m, node := range item {
			lowerM := strings.ToLower(m)
			if !methods[lowerM] {
				continue
			}
			var op openAPIOperation
			if err = node.Decode(&op); err != nil {
				t.Fatalf("decode op %s %s: %v", m, p, err)
			}
			isPublic := op.Security != nil && len(*op.Security) == 0
			hasBody := op.RequestBody != nil
			ops = append(ops, specOp{
				method:      strings.ToUpper(m),
				path:        p,
				operationID: op.OperationID,
				public:      isPublic,
				hasBody:     hasBody,
			})
		}
	}
	if len(ops) == 0 {
		t.Fatal("specOperations: no operations parsed")
	}
	sort.Slice(ops, func(i, j int) bool {
		return ops[i].operationID < ops[j].operationID
	})
	return ops
}

type idsState struct {
	csrfToken     string
	apiKeyID      string
	instanceID    string
	runID         string
	scheduleID    string
	clientID      string
	seerrLinkID   string
	seerrReqID    int
	cleanupLinkID string
	configarrPath string
	policy        map[string]any
	settings      map[string]any
}

func newIDsState(t *testing.T) *idsState {
	t.Helper()
	dir := t.TempDir()
	configPath := writeConfigarrYAML(t, dir, "config.yml", `
sonarr:
  series:
    base_url: http://sonarr:8989
    api_key: sonarr-key-1
`)
	return &idsState{
		configarrPath: configPath,
	}
}

type routerStep struct {
	op      string
	method  string
	path    func(ids *idsState) string
	body    func(ids *idsState) any
	want    int
	capture func(t *testing.T, ids *idsState, decoded map[string]any)
}

func copyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	maps.Copy(out, m)
	return out
}

func containsID(items any, wantID string) bool {
	arr, ok := items.([]any)
	if !ok {
		return false
	}
	for _, item := range arr {
		if m, ok := item.(map[string]any); ok {
			if id, ok := m["id"].(string); ok && id == wantID {
				return true
			}
		}
	}
	return false
}

func setupFixtureStubs(f *routerFixture) {
	enabled := true
	f.dlDisc.res = []arr.ProviderResource{
		{
			ID: 101, Name: "qbit-disc", Implementation: "QBittorrent", ImplementationName: "qBittorrent", Enable: &enabled,
			Fields: []arr.ProviderField{
				{Name: "host", Value: "127.0.0.1"},
				{Name: "port", Value: float64(8080)},
				{Name: "username", Value: "admin"},
				{Name: "password", Value: "secret"},
			},
		},
	}
	f.seerrClient.status = seerrclient.Status{Version: "2.1.0"}
	f.seerrClient.reqs = []seerrclient.Request{
		{ID: 101, Type: "movie", Status: 2},
	}
	f.seerrClient.servers = []seerrclient.Server{
		{Kind: domain.KindSonarr, Name: "ImportedTV", BaseURL: "http://imported-tv.invalid:8989", APIKey: "imported-key-0001"},
	}
}

func stepsAuth() []routerStep {
	return []routerStep{
		{
			op:     "getSetupStatus",
			method: http.MethodGet,
			path:   func(_ *idsState) string { return "/auth/setup/status" },
			want:   http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if complete, ok := decoded["setup_complete"].(bool); !ok || complete {
					t.Fatalf("getSetupStatus setup_complete = %v, want false", decoded["setup_complete"])
				}
			},
		},
		{
			op:     "getSSO",
			method: http.MethodGet,
			path:   func(_ *idsState) string { return "/auth/sso" },
			want:   http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if _, ok := decoded["oidc"].(bool); !ok {
					t.Fatalf("getSSO oidc field = %v, want bool", decoded["oidc"])
				}
				if _, ok := decoded["proxy"].(bool); !ok {
					t.Fatalf("getSSO proxy field = %v, want bool", decoded["proxy"])
				}
			},
		},
		{
			op:     "createProxySession",
			method: http.MethodPost,
			path:   func(_ *idsState) string { return "/auth/proxy-session" },
			want:   http.StatusNotFound,
		},
		{
			op:     "completeSetup",
			method: http.MethodPost,
			path:   func(_ *idsState) string { return "/auth/setup" },
			body: func(_ *idsState) any {
				return map[string]any{
					"username": "admin",
					"password": "correct-horse-battery-staple",
				}
			},
			want: http.StatusCreated,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				user, ok := decoded["user"].(map[string]any)
				if !ok || user["username"] != "admin" {
					t.Fatalf("completeSetup user = %v, want admin", decoded["user"])
				}
				if csrfToken, ok := decoded["csrf_token"].(string); ok {
					ids.csrfToken = csrfToken
				}
			},
		},
		{
			op:     "getSystemStatus",
			method: http.MethodGet,
			path:   func(_ *idsState) string { return "/system/status" },
			want:   http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if v, ok := decoded["version"].(string); !ok || v == "" {
					t.Fatalf("getSystemStatus version = %v, want non-empty", decoded["version"])
				}
			},
		},
		{
			op:     "getSession",
			method: http.MethodGet,
			path:   func(_ *idsState) string { return "/auth/session" },
			want:   http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				user, ok := decoded["user"].(map[string]any)
				if !ok || user["username"] != "admin" {
					t.Fatalf("getSession user = %v, want admin", decoded["user"])
				}
			},
		},
	}
}

func stepsAPIKeys() []routerStep {
	return []routerStep{
		{
			op:     "createAPIKey",
			method: http.MethodPost,
			path:   func(_ *idsState) string { return "/auth/api-keys" },
			body: func(_ *idsState) any {
				return map[string]any{
					"name": "router-test-key",
				}
			},
			want: http.StatusCreated,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				id, ok := decoded["id"].(string)
				if !ok || id == "" {
					t.Fatalf("createAPIKey id = %v, want non-empty", decoded["id"])
				}
				ids.apiKeyID = id
				secret, ok := decoded["secret"].(string)
				if !ok || secret == "" {
					t.Fatalf("createAPIKey secret = %v, want non-empty", decoded["secret"])
				}
				if decoded["name"] != "router-test-key" {
					t.Fatalf("createAPIKey name = %v, want router-test-key", decoded["name"])
				}
			},
		},
		{
			op:     "listAPIKeys",
			method: http.MethodGet,
			path:   func(_ *idsState) string { return "/auth/api-keys" },
			want:   http.StatusOK,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				if !containsID(decoded["items"], ids.apiKeyID) {
					t.Fatalf("listAPIKeys missing key %s", ids.apiKeyID)
				}
			},
		},
		{
			op:     "revokeAPIKey",
			method: http.MethodDelete,
			path:   func(ids *idsState) string { return "/auth/api-keys/" + ids.apiKeyID },
			want:   http.StatusNoContent,
		},
	}
}

func stepsInstances() []routerStep {
	return []routerStep{
		{
			op:     "createInstance",
			method: http.MethodPost,
			path:   func(_ *idsState) string { return "/instances" },
			body: func(_ *idsState) any {
				return map[string]any{
					"kind":     "sonarr",
					"name":     "tv-shows",
					"base_url": "http://sonarr.invalid:8989",
					"api_key":  "secret-test-key",
					"enabled":  true,
				}
			},
			want: http.StatusCreated,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				id, ok := decoded["id"].(string)
				if !ok || id == "" {
					t.Fatalf("createInstance id = %v, want valid uuid", decoded["id"])
				}
				ids.instanceID = id
				if decoded["name"] != "tv-shows" {
					t.Fatalf("createInstance name = %v, want tv-shows", decoded["name"])
				}
			},
		},
		{
			op:     "listInstances",
			method: http.MethodGet,
			path:   func(_ *idsState) string { return "/instances" },
			want:   http.StatusOK,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				if !containsID(decoded["items"], ids.instanceID) {
					t.Fatalf("listInstances missing created instance %s", ids.instanceID)
				}
			},
		},
		{
			op:     "getInstance",
			method: http.MethodGet,
			path:   func(ids *idsState) string { return "/instances/" + ids.instanceID },
			want:   http.StatusOK,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["id"] != ids.instanceID {
					t.Fatalf("getInstance id = %v, want %s", decoded["id"], ids.instanceID)
				}
			},
		},
		{
			op:     "updateInstance",
			method: http.MethodPut,
			path:   func(ids *idsState) string { return "/instances/" + ids.instanceID },
			body: func(_ *idsState) any {
				return map[string]any{
					"kind":     "sonarr",
					"name":     "tv-shows-updated",
					"base_url": "http://sonarr.invalid:8989",
					"api_key":  "secret-test-key",
					"enabled":  true,
				}
			},
			want: http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["name"] != "tv-shows-updated" {
					t.Fatalf("updateInstance name = %v, want tv-shows-updated", decoded["name"])
				}
			},
		},
		{
			op:     "testInstanceInput",
			method: http.MethodPost,
			path:   func(_ *idsState) string { return "/instances/test" },
			body: func(_ *idsState) any {
				return map[string]any{
					"kind":     "sonarr",
					"name":     "tv-test",
					"base_url": "http://sonarr.invalid:8989",
					"api_key":  "secret-test-key",
					"enabled":  true,
				}
			},
			want: http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if v, ok := decoded["app_name"].(string); !ok || v == "" {
					t.Fatalf("testInstanceInput app_name = %v, want non-empty", decoded["app_name"])
				}
			},
		},
		{
			op:     "testInstance",
			method: http.MethodPost,
			path:   func(ids *idsState) string { return "/instances/" + ids.instanceID + "/test" },
			want:   http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if v, ok := decoded["app_name"].(string); !ok || v == "" {
					t.Fatalf("testInstance app_name = %v, want non-empty", decoded["app_name"])
				}
			},
		},
	}
}

func stepsPoliciesAndRuns() []routerStep {
	return []routerStep{
		{
			op:     "getPolicy",
			method: http.MethodGet,
			path:   func(ids *idsState) string { return "/instances/" + ids.instanceID + "/policy" },
			want:   http.StatusOK,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["hourly_cap"] == nil {
					t.Fatalf("getPolicy missing hourly_cap: %v", decoded)
				}
				ids.policy = decoded
			},
		},
		{
			op:     "updatePolicy",
			method: http.MethodPut,
			path:   func(ids *idsState) string { return "/instances/" + ids.instanceID + "/policy" },
			body: func(ids *idsState) any {
				p := copyMap(ids.policy)
				p["hourly_cap"] = 25
				delete(p, "updated_at")
				return p
			},
			want: http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if capVal, ok := decoded["hourly_cap"].(float64); !ok || int(capVal) != 25 {
					t.Fatalf("updatePolicy hourly_cap = %v, want 25", decoded["hourly_cap"])
				}
			},
		},
		{
			op:     "triggerRun",
			method: http.MethodPost,
			path:   func(ids *idsState) string { return "/instances/" + ids.instanceID + "/runs" },
			body: func(_ *idsState) any {
				return map[string]any{"kind": "missing"}
			},
			want: http.StatusCreated,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				id, ok := decoded["id"].(string)
				if !ok || id == "" {
					t.Fatalf("triggerRun id = %v, want valid uuid", decoded["id"])
				}
				ids.runID = id
				if decoded["status"] != "queued" {
					t.Fatalf("triggerRun status = %v, want queued", decoded["status"])
				}
			},
		},
		{
			op:     "listRuns",
			method: http.MethodGet,
			path:   func(_ *idsState) string { return "/runs" },
			want:   http.StatusOK,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				if !containsID(decoded["items"], ids.runID) {
					t.Fatalf("listRuns missing triggered run %s", ids.runID)
				}
			},
		},
		{
			op:     "cancelRun",
			method: http.MethodPost,
			path:   func(ids *idsState) string { return "/runs/" + ids.runID + "/cancel" },
			want:   http.StatusNoContent,
		},
		{
			op:     "listEvents",
			method: http.MethodGet,
			path:   func(_ *idsState) string { return "/events" },
			want:   http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["items"] == nil {
					t.Fatalf("listEvents missing items: %v", decoded)
				}
			},
		},
		{
			op:     "deleteInstanceEvents",
			method: http.MethodDelete,
			path:   func(ids *idsState) string { return "/instances/" + ids.instanceID + "/events" },
			want:   http.StatusNoContent,
		},
	}
}

func stepsSettingsAndSchedules() []routerStep {
	return []routerStep{
		{
			op:     "getSettings",
			method: http.MethodGet,
			path:   func(_ *idsState) string { return "/settings" },
			want:   http.StatusOK,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["history_retention_days"] == nil {
					t.Fatalf("getSettings missing retention: %v", decoded)
				}
				ids.settings = decoded
			},
		},
		{
			op:     "updateSettings",
			method: http.MethodPut,
			path:   func(_ *idsState) string { return "/settings" },
			body: func(ids *idsState) any {
				s := copyMap(ids.settings)
				s["global_hourly_cap"] = 50
				delete(s, "updated_at")
				return s
			},
			want: http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if capVal, ok := decoded["global_hourly_cap"].(float64); !ok || int(capVal) != 50 {
					t.Fatalf("updateSettings global_hourly_cap = %v, want 50", decoded["global_hourly_cap"])
				}
			},
		},
		{
			op:     "getHourlyCaps",
			method: http.MethodGet,
			path:   func(_ *idsState) string { return "/hourly-caps" },
			want:   http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["items"] == nil {
					t.Fatalf("getHourlyCaps missing items: %v", decoded)
				}
			},
		},
		{
			op:     "createSchedule",
			method: http.MethodPost,
			path:   func(_ *idsState) string { return "/schedules" },
			body: func(_ *idsState) any {
				return map[string]any{
					"name":    "night-pause",
					"days":    []int{0, 1, 2, 3, 4, 5, 6},
					"start":   "22:00",
					"end":     "06:00",
					"tz":      "Europe/Berlin",
					"action":  "pause",
					"enabled": true,
				}
			},
			want: http.StatusCreated,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				id, ok := decoded["id"].(string)
				if !ok || id == "" {
					t.Fatalf("createSchedule id = %v, want valid uuid", decoded["id"])
				}
				ids.scheduleID = id
				if decoded["name"] != "night-pause" {
					t.Fatalf("createSchedule name = %v, want night-pause", decoded["name"])
				}
			},
		},
		{
			op:     "listSchedules",
			method: http.MethodGet,
			path:   func(_ *idsState) string { return "/schedules" },
			want:   http.StatusOK,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				if !containsID(decoded["items"], ids.scheduleID) {
					t.Fatalf("listSchedules missing schedule %s", ids.scheduleID)
				}
			},
		},
		{
			op:     "updateSchedule",
			method: http.MethodPut,
			path:   func(ids *idsState) string { return "/schedules/" + ids.scheduleID },
			body: func(_ *idsState) any {
				return map[string]any{
					"name":    "night-pause-updated",
					"days":    []int{0, 1, 2, 3, 4, 5, 6},
					"start":   "22:00",
					"end":     "06:00",
					"tz":      "Europe/Berlin",
					"action":  "pause",
					"enabled": true,
				}
			},
			want: http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["name"] != "night-pause-updated" {
					t.Fatalf("updateSchedule name = %v, want night-pause-updated", decoded["name"])
				}
			},
		},
	}
}

func stepsDownloadClients() []routerStep {
	return []routerStep{
		{
			op:     "createDownloadClient",
			method: http.MethodPost,
			path:   func(_ *idsState) string { return "/download-clients" },
			body: func(_ *idsState) any {
				return map[string]any{
					"kind":       "qbittorrent",
					"name":       "qbit-test",
					"base_url":   "http://qbit.invalid:8080",
					"username":   "admin",
					"secret":     "secret-pass",
					"enabled":    true,
					"max_active": 10,
				}
			},
			want: http.StatusCreated,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				id, ok := decoded["id"].(string)
				if !ok || id == "" {
					t.Fatalf("createDownloadClient id = %v, want valid uuid", decoded["id"])
				}
				ids.clientID = id
				if decoded["name"] != "qbit-test" {
					t.Fatalf("createDownloadClient name = %v, want qbit-test", decoded["name"])
				}
			},
		},
		{
			op:     "listDownloadClients",
			method: http.MethodGet,
			path:   func(_ *idsState) string { return "/download-clients" },
			want:   http.StatusOK,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				if !containsID(decoded["items"], ids.clientID) {
					t.Fatalf("listDownloadClients missing client %s", ids.clientID)
				}
			},
		},
		{
			op:     "getDownloadClient",
			method: http.MethodGet,
			path:   func(ids *idsState) string { return "/download-clients/" + ids.clientID },
			want:   http.StatusOK,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["id"] != ids.clientID {
					t.Fatalf("getDownloadClient id = %v, want %s", decoded["id"], ids.clientID)
				}
			},
		},
		{
			op:     "updateDownloadClient",
			method: http.MethodPut,
			path:   func(ids *idsState) string { return "/download-clients/" + ids.clientID },
			body: func(_ *idsState) any {
				return map[string]any{
					"kind":       "qbittorrent",
					"name":       "qbit-updated",
					"base_url":   "http://qbit.invalid:8080",
					"username":   "admin",
					"secret":     "secret-pass",
					"enabled":    true,
					"max_active": 15,
				}
			},
			want: http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["name"] != "qbit-updated" {
					t.Fatalf("updateDownloadClient name = %v, want qbit-updated", decoded["name"])
				}
			},
		},
		{
			op:     "testDownloadClientInput",
			method: http.MethodPost,
			path:   func(_ *idsState) string { return "/download-clients/test" },
			body: func(_ *idsState) any {
				return map[string]any{
					"kind":       "qbittorrent",
					"name":       "qbit-test",
					"base_url":   "http://qbit.invalid:8080",
					"username":   "admin",
					"secret":     "secret-pass",
					"enabled":    true,
					"max_active": 10,
				}
			},
			want: http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if r, ok := decoded["reachable"].(bool); !ok || !r {
					t.Fatalf("testDownloadClientInput reachable = %v, want true", decoded["reachable"])
				}
			},
		},
		{
			op:     "testDownloadClient",
			method: http.MethodPost,
			path:   func(ids *idsState) string { return "/download-clients/" + ids.clientID + "/test" },
			want:   http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if r, ok := decoded["reachable"].(bool); !ok || !r {
					t.Fatalf("testDownloadClient reachable = %v, want true", decoded["reachable"])
				}
			},
		},
		{
			op:     "getDownloadClientStatus",
			method: http.MethodGet,
			path:   func(_ *idsState) string { return "/download-clients/status" },
			want:   http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["items"] == nil {
					t.Fatalf("getDownloadClientStatus missing items: %v", decoded)
				}
			},
		},
		{
			op:     "discoverDownloadClients",
			method: http.MethodPost,
			path:   func(ids *idsState) string { return "/instances/" + ids.instanceID + "/download-clients/discover" },
			want:   http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["imported"] == nil {
					t.Fatalf("discoverDownloadClients missing imported: %v", decoded)
				}
			},
		},
	}
}

func stepsSeerr(f *routerFixture) []routerStep {
	return []routerStep{
		{
			op:     "createSeerrLink",
			method: http.MethodPost,
			path:   func(_ *idsState) string { return "/seerr" },
			body: func(_ *idsState) any {
				return map[string]any{
					"name":     "seerr-test",
					"base_url": "http://seerr.invalid:5055",
					"api_key":  "secret-seerr-key",
					"enabled":  true,
				}
			},
			want: http.StatusCreated,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				id, ok := decoded["id"].(string)
				if !ok || id == "" {
					t.Fatalf("createSeerrLink id = %v, want valid uuid", decoded["id"])
				}
				ids.seerrLinkID = id
				if decoded["name"] != "seerr-test" {
					t.Fatalf("createSeerrLink name = %v, want seerr-test", decoded["name"])
				}
				seerrUUID := uuid.MustParse(ids.seerrLinkID)
				instUUID := uuid.MustParse(ids.instanceID)
				entID := int64(100)
				ids.seerrReqID = seedTestSeerrRequest(t, f.integrationFixture, seerrUUID, &instUUID, &entID)
			},
		},
		{
			op:     "listSeerrLinks",
			method: http.MethodGet,
			path:   func(_ *idsState) string { return "/seerr" },
			want:   http.StatusOK,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				if !containsID(decoded["items"], ids.seerrLinkID) {
					t.Fatalf("listSeerrLinks missing link %s", ids.seerrLinkID)
				}
			},
		},
		{
			op:     "getSeerrLink",
			method: http.MethodGet,
			path:   func(ids *idsState) string { return "/seerr/" + ids.seerrLinkID },
			want:   http.StatusOK,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["id"] != ids.seerrLinkID {
					t.Fatalf("getSeerrLink id = %v, want %s", decoded["id"], ids.seerrLinkID)
				}
			},
		},
		{
			op:     "updateSeerrLink",
			method: http.MethodPut,
			path:   func(ids *idsState) string { return "/seerr/" + ids.seerrLinkID },
			body: func(_ *idsState) any {
				return map[string]any{
					"name":     "seerr-updated",
					"base_url": "http://seerr.invalid:5055",
					"api_key":  "secret-seerr-key",
					"enabled":  true,
				}
			},
			want: http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["name"] != "seerr-updated" {
					t.Fatalf("updateSeerrLink name = %v, want seerr-updated", decoded["name"])
				}
			},
		},
		{
			op:     "testSeerrLinkInput",
			method: http.MethodPost,
			path:   func(_ *idsState) string { return "/seerr/test" },
			body: func(_ *idsState) any {
				return map[string]any{
					"name":     "seerr-test",
					"base_url": "http://seerr.invalid:5055",
					"api_key":  "secret-seerr-key",
					"enabled":  true,
				}
			},
			want: http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if v, ok := decoded["app_name"].(string); !ok || v == "" {
					t.Fatalf("testSeerrLinkInput app_name = %v, want non-empty", decoded["app_name"])
				}
			},
		},
		{
			op:     "testSeerrLink",
			method: http.MethodPost,
			path:   func(ids *idsState) string { return "/seerr/" + ids.seerrLinkID + "/test" },
			want:   http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if v, ok := decoded["app_name"].(string); !ok || v == "" {
					t.Fatalf("testSeerrLink app_name = %v, want non-empty", decoded["app_name"])
				}
			},
		},
		{
			op:     "syncSeerrLink",
			method: http.MethodPost,
			path:   func(ids *idsState) string { return "/seerr/" + ids.seerrLinkID + "/sync" },
			want:   http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["seen"] == nil {
					t.Fatalf("syncSeerrLink missing seen: %v", decoded)
				}
			},
		},
		{
			op:     "importSeerrInstances",
			method: http.MethodPost,
			path:   func(ids *idsState) string { return "/seerr/" + ids.seerrLinkID + "/import-instances" },
			want:   http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["created"] == nil {
					t.Fatalf("importSeerrInstances missing created: %v", decoded)
				}
			},
		},
		{
			op:     "listSeerrRequests",
			method: http.MethodGet,
			path:   func(_ *idsState) string { return "/seerr/requests" },
			want:   http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				items, ok := decoded["items"].([]any)
				if !ok || len(items) == 0 {
					t.Fatalf("listSeerrRequests items = %v, want non-empty", decoded["items"])
				}
			},
		},
		{
			op:     "snatchSeerrRequest",
			method: http.MethodPost,
			path: func(ids *idsState) string {
				return "/seerr/requests/" + ids.seerrLinkID + "/" + strconv.Itoa(ids.seerrReqID) + "/snatch"
			},
			want: http.StatusCreated,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["kind"] != "missing" {
					t.Fatalf("snatchSeerrRequest kind = %v, want missing", decoded["kind"])
				}
			},
		},
	}
}

func stepsCleanuparrAndConfigarr(f *routerFixture) []routerStep {
	return []routerStep{
		{
			op:     "createCleanuparrLink",
			method: http.MethodPost,
			path:   func(_ *idsState) string { return "/cleanuparr" },
			body: func(_ *idsState) any {
				return map[string]any{
					"name":     "cleanup-test",
					"base_url": f.cleanupServer.URL,
					"api_key":  "secret-cleanup-key",
					"enabled":  true,
				}
			},
			want: http.StatusCreated,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				id, ok := decoded["id"].(string)
				if !ok || id == "" {
					t.Fatalf("createCleanuparrLink id = %v, want valid uuid", decoded["id"])
				}
				ids.cleanupLinkID = id
				if decoded["name"] != "cleanup-test" {
					t.Fatalf("createCleanuparrLink name = %v, want cleanup-test", decoded["name"])
				}
			},
		},
		{
			op:     "listCleanuparrLinks",
			method: http.MethodGet,
			path:   func(_ *idsState) string { return "/cleanuparr" },
			want:   http.StatusOK,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				if !containsID(decoded["items"], ids.cleanupLinkID) {
					t.Fatalf("listCleanuparrLinks missing link %s", ids.cleanupLinkID)
				}
			},
		},
		{
			op:     "getCleanuparrLink",
			method: http.MethodGet,
			path:   func(ids *idsState) string { return "/cleanuparr/" + ids.cleanupLinkID },
			want:   http.StatusOK,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["id"] != ids.cleanupLinkID {
					t.Fatalf("getCleanuparrLink id = %v, want %s", decoded["id"], ids.cleanupLinkID)
				}
			},
		},
		{
			op:     "updateCleanuparrLink",
			method: http.MethodPut,
			path:   func(ids *idsState) string { return "/cleanuparr/" + ids.cleanupLinkID },
			body: func(_ *idsState) any {
				return map[string]any{
					"name":     "cleanup-updated",
					"base_url": f.cleanupServer.URL,
					"api_key":  "secret-cleanup-key",
					"enabled":  true,
				}
			},
			want: http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["name"] != "cleanup-updated" {
					t.Fatalf("updateCleanuparrLink name = %v, want cleanup-updated", decoded["name"])
				}
			},
		},
		{
			op:     "testCleanuparrLinkInput",
			method: http.MethodPost,
			path:   func(_ *idsState) string { return "/cleanuparr/test" },
			body: func(_ *idsState) any {
				return map[string]any{
					"name":     "cleanup-test",
					"base_url": f.cleanupServer.URL,
					"api_key":  "secret-cleanup-key",
					"enabled":  true,
				}
			},
			want: http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if v, ok := decoded["app_name"].(string); !ok || v == "" {
					t.Fatalf("testCleanuparrLinkInput app_name = %v, want non-empty", decoded["app_name"])
				}
			},
		},
		{
			op:     "testCleanuparrLink",
			method: http.MethodPost,
			path:   func(ids *idsState) string { return "/cleanuparr/" + ids.cleanupLinkID + "/test" },
			want:   http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if v, ok := decoded["app_name"].(string); !ok || v == "" {
					t.Fatalf("testCleanuparrLink app_name = %v, want non-empty", decoded["app_name"])
				}
			},
		},
		{
			op:     "getCleanuparrStatus",
			method: http.MethodGet,
			path:   func(_ *idsState) string { return "/cleanuparr/status" },
			want:   http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["items"] == nil {
					t.Fatalf("getCleanuparrStatus missing items: %v", decoded)
				}
			},
		},
		{
			op:     "importConfigarr",
			method: http.MethodPost,
			path:   func(_ *idsState) string { return "/configarr/import" },
			body: func(ids *idsState) any {
				return map[string]any{"config_path": ids.configarrPath}
			},
			want: http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if imp, ok := decoded["imported"].(float64); !ok || int(imp) != 1 {
					t.Fatalf("importConfigarr imported = %v, want 1", decoded["imported"])
				}
			},
		},
		{
			op:     "getConfigarrStatus",
			method: http.MethodGet,
			path:   func(_ *idsState) string { return "/configarr/status" },
			want:   http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["last_result"] == nil {
					t.Fatalf("getConfigarrStatus missing last_result: %v", decoded)
				}
			},
		},
		{
			op:     "resetState",
			method: http.MethodPost,
			path:   func(_ *idsState) string { return "/state/reset" },
			body: func(_ *idsState) any {
				return map[string]any{}
			},
			want: http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["forgotten"] == nil {
					t.Fatalf("resetState missing forgotten: %v", decoded)
				}
			},
		},
	}
}

func stepsDeletesAndAuth() []routerStep {
	return []routerStep{
		{
			op:     "deleteSchedule",
			method: http.MethodDelete,
			path:   func(ids *idsState) string { return "/schedules/" + ids.scheduleID },
			want:   http.StatusNoContent,
		},
		{
			op:     "deleteDownloadClient",
			method: http.MethodDelete,
			path:   func(ids *idsState) string { return "/download-clients/" + ids.clientID },
			want:   http.StatusNoContent,
		},
		{
			op:     "deleteSeerrLink",
			method: http.MethodDelete,
			path:   func(ids *idsState) string { return "/seerr/" + ids.seerrLinkID },
			want:   http.StatusNoContent,
		},
		{
			op:     "deleteCleanuparrLink",
			method: http.MethodDelete,
			path:   func(ids *idsState) string { return "/cleanuparr/" + ids.cleanupLinkID },
			want:   http.StatusNoContent,
		},
		{
			op:     "deleteInstance",
			method: http.MethodDelete,
			path:   func(ids *idsState) string { return "/instances/" + ids.instanceID },
			want:   http.StatusNoContent,
		},
		{
			op:     "logout",
			method: http.MethodPost,
			path:   func(_ *idsState) string { return "/auth/logout" },
			want:   http.StatusNoContent,
		},
		{
			op:     "login",
			method: http.MethodPost,
			path:   func(_ *idsState) string { return "/auth/login" },
			body: func(_ *idsState) any {
				return map[string]any{
					"username": "admin",
					"password": "correct-horse-battery-staple",
				}
			},
			want: http.StatusOK,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				user, ok := decoded["user"].(map[string]any)
				if !ok || user["username"] != "admin" {
					t.Fatalf("login user = %v, want admin", decoded["user"])
				}
				if csrfToken, ok := decoded["csrf_token"].(string); ok {
					ids.csrfToken = csrfToken
				}
			},
		},
		{
			op:     "changePassword",
			method: http.MethodPost,
			path:   func(_ *idsState) string { return "/auth/password" },
			body: func(_ *idsState) any {
				return map[string]any{
					"current_password": "correct-horse-battery-staple",
					"new_password":     "changed-horse-battery-staple",
				}
			},
			want: http.StatusNoContent,
		},
		{
			op:     "logout",
			method: http.MethodPost,
			path:   func(_ *idsState) string { return "/auth/logout" },
			want:   http.StatusNoContent,
		},
	}
}

func buildRouterSteps(f *routerFixture) []routerStep {
	steps := make([]routerStep, 0, 60)
	steps = append(steps, stepsAuth()...)
	steps = append(steps, stepsAPIKeys()...)
	steps = append(steps, stepsInstances()...)
	steps = append(steps, stepsPoliciesAndRuns()...)
	steps = append(steps, stepsSettingsAndSchedules()...)
	steps = append(steps, stepsDownloadClients()...)
	steps = append(steps, stepsSeerr(f)...)
	steps = append(steps, stepsCleanuparrAndConfigarr(f)...)
	steps = append(steps, stepsDeletesAndAuth()...)
	return steps
}

func assertCoverageGuard(t *testing.T, steps []routerStep) {
	t.Helper()
	ops := specOperations(t)
	specMethods := make(map[string]string, len(ops))
	for _, op := range ops {
		specMethods[op.operationID] = op.method
	}

	stepOpIDs := make(map[string]bool, len(steps))
	for _, step := range steps {
		stepOpIDs[step.op] = true
		method, ok := specMethods[step.op]
		switch {
		case !ok:
			t.Errorf("step operation %q not found in OpenAPI spec", step.op)
		case method != step.method:
			t.Errorf("step operation %q uses %s, spec declares %s", step.op, step.method, method)
		}
	}
	for op := range specMethods {
		if !stepOpIDs[op] {
			t.Errorf("OpenAPI spec operation %q not covered in router steps", op)
		}
	}
}

func executeRouterStep(t *testing.T, f *routerFixture, ids *idsState, step routerStep) {
	t.Helper()
	reqPath := step.path(ids)
	reqURL := f.srv.URL + PathPrefix + reqPath

	var bodyReader io.Reader
	if step.body != nil {
		if bodyVal := step.body(ids); bodyVal != nil {
			bodyBytes, err := json.Marshal(bodyVal)
			if err != nil {
				t.Fatalf("%s %s: marshal body: %v", step.method, reqPath, err)
			}
			bodyReader = bytes.NewReader(bodyBytes)
		}
	}

	req, err := http.NewRequestWithContext(t.Context(), step.method, reqURL, bodyReader)
	if err != nil {
		t.Fatalf("%s %s: new request: %v", step.method, reqPath, err)
	}
	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if ids.csrfToken != "" && step.method != http.MethodGet && step.method != http.MethodHead {
		req.Header.Set("X-Csrf-Token", ids.csrfToken)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: client.Do: %v", step.method, reqPath, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != step.want {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s %s: got status %d, want %d; body: %s", step.method, reqPath, resp.StatusCode, step.want, string(body))
	}

	assertResponseContentType(t, step, reqPath, resp.Header.Get("Content-Type"))

	var decoded map[string]any
	if step.want != http.StatusNoContent {
		decoded = decodeResponseBody(t, step, reqPath, resp.Body)
	}
	if step.capture != nil {
		step.capture(t, ids, decoded)
	}
}

func assertResponseContentType(t *testing.T, step routerStep, reqPath, ct string) {
	t.Helper()
	if step.want == http.StatusNoContent {
		if ct != "" {
			t.Errorf("%s %s: 204 response has Content-Type %q, want empty", step.method, reqPath, ct)
		}
		return
	}
	if step.want >= 400 {
		if !strings.Contains(ct, "application/problem+json") {
			t.Errorf("%s %s: Content-Type = %q, want application/problem+json", step.method, reqPath, ct)
		}
		return
	}
	if !strings.Contains(ct, "application/json") {
		t.Errorf("%s %s: Content-Type = %q, want application/json", step.method, reqPath, ct)
	}
}

func decodeResponseBody(t *testing.T, step routerStep, reqPath string, r io.Reader) map[string]any {
	t.Helper()
	var raw any
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		t.Fatalf("%s %s: decode JSON body: %v", step.method, reqPath, err)
	}
	if m, ok := raw.(map[string]any); ok {
		return m
	}
	if arr, ok := raw.([]any); ok {
		return map[string]any{"items": arr}
	}
	return map[string]any{"value": raw}
}

//nolint:tparallel,paralleltest // sequential scenario: each step reads ids the previous step created
func TestRouterOperations(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	steps := buildRouterSteps(f)
	assertCoverageGuard(t, steps)

	ids := newIDsState(t)

	//nolint:paralleltest // sequential scenario: each step reads ids the previous step created
	for _, step := range steps {
		t.Run(step.op+"_"+step.method, func(t *testing.T) {
			executeRouterStep(t, f, ids, step)
		})
	}
}

func TestRouterRejectsAnonymousRequests(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	ops := specOperations(t)
	fixedUUID := "00000000-0000-0000-0000-000000000001"
	pathParamPattern := regexp.MustCompile(`\{[^}]+\}`)
	client := &http.Client{Timeout: 10 * time.Second}

	for _, op := range ops {
		if op.public {
			continue
		}
		t.Run(op.operationID, func(t *testing.T) {
			t.Parallel()
			checkAnonymousRejection(t, f, client, op, pathParamPattern, fixedUUID)
		})
	}
}

func checkAnonymousRejection(t *testing.T, f *routerFixture, client *http.Client, op specOp, pathParamPattern *regexp.Regexp, fixedUUID string) {
	t.Helper()
	targetPath := pathParamPattern.ReplaceAllString(op.path, fixedUUID)
	reqURL := f.srv.URL + PathPrefix + targetPath

	var bodyReader io.Reader
	if op.hasBody {
		bodyReader = strings.NewReader("{}")
	}
	req, err := http.NewRequestWithContext(t.Context(), op.method, reqURL, bodyReader)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if op.hasBody {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertProblem(t, resp, http.StatusUnauthorized)
}

func assertProblem(t *testing.T, resp *http.Response, wantCode int) {
	t.Helper()
	if resp.StatusCode != wantCode {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, wantCode, string(body))
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("Content-Type = %q, want application/problem+json", ct)
	}
	var prob map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&prob); err != nil {
		t.Fatalf("decode problem body: %v", err)
	}
	status, ok := prob["status"].(float64)
	if !ok || int(status) != wantCode {
		t.Fatalf("problem status = %v, want %d", prob["status"], wantCode)
	}
	title, ok := prob["title"].(string)
	if !ok || strings.TrimSpace(title) == "" {
		t.Fatalf("problem title = %v, want non-empty", prob["title"])
	}
}

func completeInitialSetup(t *testing.T, f *routerFixture) domain.User {
	t.Helper()
	body := strings.NewReader(`{"username":"admin","password":"password-admin-1234"}`)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.srv.URL+PathPrefix+"/auth/setup", body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatalf("completeSetup: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("completeSetup status = %d, want 201", resp.StatusCode)
	}
	var sess oas.Session
	if err = json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	return domain.User{
		ID:       sess.User.ID,
		Username: sess.User.Username,
		Role:     domain.Role(sess.User.Role),
	}
}

func createViewerUser(t *testing.T, f *routerFixture) (string, string) {
	t.Helper()
	username := "viewer-" + uuid.NewString()[:8]
	password := "viewer-pass-1234"
	hasher := crypto.NewPasswordHasher(1)
	hash, err := hasher.Hash(t.Context(), password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	viewerID := uuid.New()
	_, err = f.st.Q().CreateUser(t.Context(), sqlcgen.CreateUserParams{
		ID:           viewerID,
		Username:     username,
		PasswordHash: hash,
		Role:         string(domain.RoleViewer),
		CreatedAt:    f.clk.Now(),
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return username, password
}

func testProblemMalformedJSON(t *testing.T, f *routerFixture) {
	t.Helper()
	cases := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/instances"},
		{http.MethodPut, "/settings"},
		{http.MethodPost, "/schedules"},
	}
	for _, tc := range cases {
		checkMalformedJSON(t, f, tc.method, tc.path)
	}
}

func checkMalformedJSON(t *testing.T, f *routerFixture, method, path string) {
	t.Helper()
	body := strings.NewReader("{")
	req, err := http.NewRequestWithContext(t.Context(), method, f.srv.URL+PathPrefix+path, body)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertProblem(t, resp, http.StatusBadRequest)
}

func testProblemNotFound(t *testing.T, f *routerFixture) {
	t.Helper()
	unknownUUID := "00000000-0000-0000-0000-000000000099"
	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/instances/" + unknownUUID},
		{http.MethodGet, "/download-clients/" + unknownUUID},
		{http.MethodGet, "/seerr/" + unknownUUID},
		{http.MethodGet, "/cleanuparr/" + unknownUUID},
		{http.MethodPost, "/runs/" + unknownUUID + "/cancel"},
	}
	for _, tc := range cases {
		checkNotFoundPath(t, f, tc.method, tc.path)
	}
}

func checkNotFoundPath(t *testing.T, f *routerFixture, method, path string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, f.srv.URL+PathPrefix+path, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertProblem(t, resp, http.StatusNotFound)
}

func testProblemInvalidParamsAndAuth(t *testing.T, f *routerFixture) {
	t.Helper()
	checkInvalidParam(t, f)
	checkWrongPassword(t, f)
	checkSecondSetup(t, f)
}

func checkInvalidParam(t *testing.T, f *routerFixture) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.srv.URL+PathPrefix+"/instances/not-a-uuid", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatalf("getInstance invalid param: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertProblem(t, resp, http.StatusBadRequest)
}

func checkWrongPassword(t *testing.T, f *routerFixture) {
	t.Helper()
	loginBody := strings.NewReader(`{"username":"admin","password":"wrong-password"}`)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.srv.URL+PathPrefix+"/auth/login", loginBody)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatalf("login wrong pass: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertProblem(t, resp, http.StatusUnauthorized)
}

func checkSecondSetup(t *testing.T, f *routerFixture) {
	t.Helper()
	setupBody := strings.NewReader(`{"username":"admin2","password":"password-admin-5678"}`)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.srv.URL+PathPrefix+"/auth/setup", setupBody)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatalf("second completeSetup: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertProblem(t, resp, http.StatusConflict)
}

func testProblemViewerAndAPIKeys(t *testing.T, f *routerFixture, adminUser domain.User) {
	t.Helper()
	viewerUser, viewerPass := createViewerUser(t, f)
	viewerJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	viewerClient := &http.Client{Timeout: 10 * time.Second, Jar: viewerJar}

	loginBody := bytes.NewReader([]byte(fmt.Sprintf(`{"username":%q,"password":%q}`, viewerUser, viewerPass)))
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.srv.URL+PathPrefix+"/auth/login", loginBody)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := viewerClient.Do(req)
	if err != nil {
		t.Fatalf("viewer login: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("viewer login status = %d, want 200", resp.StatusCode)
	}

	instBody := strings.NewReader(`{"kind":"sonarr","name":"viewer-inst","base_url":"http://sonarr.invalid:8989","api_key":"secret-test-key","enabled":true}`)
	req, err = http.NewRequestWithContext(t.Context(), http.MethodPost, f.srv.URL+PathPrefix+"/instances", instBody)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err = viewerClient.Do(req)
	if err != nil {
		t.Fatalf("viewer createInstance: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertProblem(t, resp, http.StatusForbidden)

	testAPIKeyAuthentication(t, f, adminUser)
}

func testAPIKeyAuthentication(t *testing.T, f *routerFixture, adminUser domain.User) {
	t.Helper()
	rawKey, _, err := f.keys.Issue(t.Context(), adminUser.ID.String(), []string{"admin"})
	if err != nil {
		t.Fatalf("keys.Issue: %v", err)
	}
	noCookieClient := &http.Client{Timeout: 10 * time.Second}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.srv.URL+PathPrefix+"/instances", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+rawKey)
	resp, err := noCookieClient.Do(req)
	if err != nil {
		t.Fatalf("listInstances with apiKey: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("listInstances with apiKey status = %d, want 200", resp.StatusCode)
	}

	req, err = http.NewRequestWithContext(t.Context(), http.MethodGet, f.srv.URL+PathPrefix+"/instances", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer sk_bogus")
	resp, err = noCookieClient.Do(req)
	if err != nil {
		t.Fatalf("listInstances with bogus key: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	assertProblem(t, resp, http.StatusUnauthorized)
}

func TestRouterProblemResponses(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	adminUser := completeInitialSetup(t, f)

	testProblemMalformedJSON(t, f)
	testProblemNotFound(t, f)
	testProblemInvalidParamsAndAuth(t, f)
	testProblemViewerAndAPIKeys(t, f, adminUser)
}

func TestChangePasswordSessionTermination(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	_ = completeInitialSetup(t, f)
	ctx := t.Context()

	// 1. Session A: f.client (jar A) is already logged in as "admin" ("password-admin-1234") from completeInitialSetup.
	// Get CSRF token for Session A.
	sessReq, err := http.NewRequestWithContext(ctx, http.MethodGet, f.srv.URL+PathPrefix+"/auth/session", nil)
	if err != nil {
		t.Fatalf("session A request: %v", err)
	}
	sessResp, err := f.client.Do(sessReq)
	if err != nil {
		t.Fatalf("session A Do: %v", err)
	}
	defer func() { _ = sessResp.Body.Close() }()
	if sessResp.StatusCode != http.StatusOK {
		t.Fatalf("session A status = %d, want 200", sessResp.StatusCode)
	}
	var sessA oas.Session
	if err = json.NewDecoder(sessResp.Body).Decode(&sessA); err != nil {
		t.Fatalf("decode session A: %v", err)
	}
	csrfTokenA := sessA.CsrfToken

	// 2. Session B: second login as admin with a separate cookie jar
	jarB, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar B: %v", err)
	}
	clientB := &http.Client{Timeout: 10 * time.Second, Jar: jarB}
	loginBodyB := strings.NewReader(`{"username":"admin","password":"password-admin-1234"}`)
	loginReqB, err := http.NewRequestWithContext(ctx, http.MethodPost, f.srv.URL+PathPrefix+"/auth/login", loginBodyB)
	if err != nil {
		t.Fatalf("loginReqB: %v", err)
	}
	loginReqB.Header.Set("Content-Type", "application/json")
	loginRespB, err := clientB.Do(loginReqB)
	if err != nil {
		t.Fatalf("clientB login: %v", err)
	}
	_ = loginRespB.Body.Close()
	if loginRespB.StatusCode != http.StatusOK {
		t.Fatalf("clientB login status = %d, want 200", loginRespB.StatusCode)
	}

	// 3. Other user session: viewer user with its own cookie jar
	viewerUser, viewerPass := createViewerUser(t, f)
	jarOther, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar Other: %v", err)
	}
	clientOther := &http.Client{Timeout: 10 * time.Second, Jar: jarOther}
	loginBodyOther := bytes.NewReader([]byte(fmt.Sprintf(`{"username":%q,"password":%q}`, viewerUser, viewerPass)))
	loginReqOther, err := http.NewRequestWithContext(ctx, http.MethodPost, f.srv.URL+PathPrefix+"/auth/login", loginBodyOther)
	if err != nil {
		t.Fatalf("loginReqOther: %v", err)
	}
	loginReqOther.Header.Set("Content-Type", "application/json")
	loginRespOther, err := clientOther.Do(loginReqOther)
	if err != nil {
		t.Fatalf("clientOther login: %v", err)
	}
	_ = loginRespOther.Body.Close()
	if loginRespOther.StatusCode != http.StatusOK {
		t.Fatalf("clientOther login status = %d, want 200", loginRespOther.StatusCode)
	}

	// 4. POST /auth/password from session A with CSRF
	newAdminPass := "new-password-admin-5678"
	changeBody := bytes.NewReader([]byte(fmt.Sprintf(`{"current_password":"password-admin-1234","new_password":%q}`, newAdminPass)))
	changeReq, err := http.NewRequestWithContext(ctx, http.MethodPost, f.srv.URL+PathPrefix+"/auth/password", changeBody)
	if err != nil {
		t.Fatalf("changeReq: %v", err)
	}
	changeReq.Header.Set("Content-Type", "application/json")
	changeReq.Header.Set("X-Csrf-Token", csrfTokenA)
	changeResp, err := f.client.Do(changeReq)
	if err != nil {
		t.Fatalf("changePassword Do: %v", err)
	}
	_ = changeResp.Body.Close()
	if changeResp.StatusCode != http.StatusNoContent {
		t.Fatalf("changePassword status = %d, want 204", changeResp.StatusCode)
	}

	// 5. Verification:
	// A still reaches /auth/session (200)
	checkReqA, err := http.NewRequestWithContext(ctx, http.MethodGet, f.srv.URL+PathPrefix+"/auth/session", nil)
	if err != nil {
		t.Fatalf("checkReqA: %v", err)
	}
	checkRespA, err := f.client.Do(checkReqA)
	if err != nil {
		t.Fatalf("checkRespA: %v", err)
	}
	_ = checkRespA.Body.Close()
	if checkRespA.StatusCode != http.StatusOK {
		t.Fatalf("session A /auth/session status = %d, want 200", checkRespA.StatusCode)
	}

	// B gets 401
	checkReqB, err := http.NewRequestWithContext(ctx, http.MethodGet, f.srv.URL+PathPrefix+"/auth/session", nil)
	if err != nil {
		t.Fatalf("checkReqB: %v", err)
	}
	checkRespB, err := clientB.Do(checkReqB)
	if err != nil {
		t.Fatalf("checkRespB: %v", err)
	}
	_ = checkRespB.Body.Close()
	if checkRespB.StatusCode != http.StatusUnauthorized {
		t.Fatalf("session B /auth/session status = %d, want 401", checkRespB.StatusCode)
	}

	// other user's session still 200
	checkReqOther, err := http.NewRequestWithContext(ctx, http.MethodGet, f.srv.URL+PathPrefix+"/auth/session", nil)
	if err != nil {
		t.Fatalf("checkReqOther: %v", err)
	}
	checkRespOther, err := clientOther.Do(checkReqOther)
	if err != nil {
		t.Fatalf("checkRespOther: %v", err)
	}
	_ = checkRespOther.Body.Close()
	if checkRespOther.StatusCode != http.StatusOK {
		t.Fatalf("other user /auth/session status = %d, want 200", checkRespOther.StatusCode)
	}

	// old password login fails
	oldLoginBody := strings.NewReader(`{"username":"admin","password":"password-admin-1234"}`)
	oldLoginReq, err := http.NewRequestWithContext(ctx, http.MethodPost, f.srv.URL+PathPrefix+"/auth/login", oldLoginBody)
	if err != nil {
		t.Fatalf("oldLoginReq: %v", err)
	}
	oldLoginReq.Header.Set("Content-Type", "application/json")
	oldLoginResp, err := (&http.Client{Timeout: 10 * time.Second}).Do(oldLoginReq)
	if err != nil {
		t.Fatalf("oldLogin: %v", err)
	}
	_ = oldLoginResp.Body.Close()
	if oldLoginResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("login with old password status = %d, want 401", oldLoginResp.StatusCode)
	}

	// new password login works
	newLoginBody := bytes.NewReader([]byte(fmt.Sprintf(`{"username":"admin","password":%q}`, newAdminPass)))
	newLoginReq, err := http.NewRequestWithContext(ctx, http.MethodPost, f.srv.URL+PathPrefix+"/auth/login", newLoginBody)
	if err != nil {
		t.Fatalf("newLoginReq: %v", err)
	}
	newLoginReq.Header.Set("Content-Type", "application/json")
	newLoginResp, err := (&http.Client{Timeout: 10 * time.Second}).Do(newLoginReq)
	if err != nil {
		t.Fatalf("newLogin: %v", err)
	}
	_ = newLoginResp.Body.Close()
	if newLoginResp.StatusCode != http.StatusOK {
		t.Fatalf("login with new password status = %d, want 200", newLoginResp.StatusCode)
	}
}
