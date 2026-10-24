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

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/auth"
	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/queue"
	"github.com/lusoris/SnatchArr/api/internal/seerr"
	"github.com/lusoris/SnatchArr/api/internal/seerrclient"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
	"github.com/lusoris/SnatchArr/api/internal/testdiag"
)

type routerFixture struct {
	*integrationFixture
	srv          *httptest.Server
	client       *http.Client
	jar          http.CookieJar
	keys         *apikey.Service
	notifySrvURL string
}

func newRouterFixture(t *testing.T) *routerFixture {
	t.Helper()
	f := newIntegrationFixture(t)
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
	mountParams := MountParams{
		Logger:   slog.New(slog.DiscardHandler),
		Clock:    f.clk,
		Handlers: f.h,
		Security: NewSecurity(f.sessMgr, f.h.cfg, keys, f.h.auth, apiKeys, f.clk, slog.New(slog.DiscardHandler)),
		CSRF:     CrossOrigin(slog.New(slog.DiscardHandler)),
	}
	err = Mount(r, mountParams)
	if err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if err = MountLogStream(r, mountParams, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})); err != nil {
		t.Fatalf("MountLogStream: %v", err)
	}
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	notifySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(notifySrv.Close)

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
		notifySrvURL:       notifySrv.URL,
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
	csrfToken      string
	apiKeyID       string
	instanceID     string
	runID          string
	scheduleID     string
	clientID       string
	seerrLinkID    string
	seerrReqID     int
	prowlarrLinkID string
	configarrPath  string
	policy         map[string]any
	queuePolicy    map[string]any
	settings       map[string]any
	channelID      string
	ruleID         string
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
	f.dlDisc.labels = arrclient.Labels{Tags: []arrclient.Label{{ID: 3, Name: "anime"}}, Profiles: []arrclient.Label{}}
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
				p["include_tags"] = []int{3}
				p["release_delay_h"] = 12
				delete(p, "updated_at")
				return p
			},
			want: http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if capVal, ok := decoded["hourly_cap"].(float64); !ok || int(capVal) != 25 {
					t.Fatalf("updatePolicy hourly_cap = %v, want 25", decoded["hourly_cap"])
				}
				if tags, ok := decoded["include_tags"].([]any); !ok || len(tags) != 1 || decoded["release_delay_h"] != float64(12) {
					t.Fatalf("updatePolicy filters = %v, %v; want [3], 12", decoded["include_tags"], decoded["release_delay_h"])
				}
			},
		},
		{
			op:     "getPolicyFilterOptions",
			method: http.MethodGet,
			path:   func(ids *idsState) string { return "/instances/" + ids.instanceID + "/policy/filter-options" },
			want:   http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if tags, ok := decoded["tags"].([]any); !ok || len(tags) != 1 || decoded["quality_profiles"] == nil {
					t.Fatalf("getPolicyFilterOptions = %v; want one tag and a profile list", decoded)
				}
			},
		},
		{
			op:     "getQueuePolicy",
			method: http.MethodGet,
			path:   func(ids *idsState) string { return "/instances/" + ids.instanceID + "/queue-policy" },
			want:   http.StatusOK,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["rules"] == nil {
					t.Fatalf("getQueuePolicy missing rules: %v", decoded)
				}
				ids.queuePolicy = decoded
			},
		},
		{
			op:     "updateQueuePolicy",
			method: http.MethodPut,
			path:   func(ids *idsState) string { return "/instances/" + ids.instanceID + "/queue-policy" },
			body: func(ids *idsState) any {
				qp := copyMap(ids.queuePolicy)
				qp["enabled"] = true
				qp["grace_seconds"] = 1800
				return qp
			},
			want: http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if enabled, ok := decoded["enabled"].(bool); !ok || !enabled {
					t.Fatalf("updateQueuePolicy enabled = %v, want true", decoded["enabled"])
				}
			},
		},
		{
			op:     "listQueueStrikes",
			method: http.MethodGet,
			path:   func(ids *idsState) string { return "/instances/" + ids.instanceID + "/queue-strikes" },
			want:   http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if _, ok := decoded["items"].([]any); !ok {
					t.Fatalf("listQueueStrikes = %v; want items array", decoded)
				}
			},
		},
		{
			op:     "listQueueItems",
			method: http.MethodGet,
			path:   func(ids *idsState) string { return "/instances/" + ids.instanceID + "/queue" },
			want:   http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if _, ok := decoded["items"].([]any); !ok {
					t.Fatalf("listQueueItems = %v; want items array", decoded)
				}
				if _, ok := decoded["truncated"].(bool); !ok {
					t.Fatalf("listQueueItems = %v; want truncated bool", decoded)
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
			op:     "getRun",
			method: http.MethodGet,
			path:   func(ids *idsState) string { return "/runs/" + ids.runID },
			want:   http.StatusOK,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["id"] != ids.runID {
					t.Fatalf("getRun id = %v, want %s", decoded["id"], ids.runID)
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
			op:     "previewRun",
			method: http.MethodPost,
			path:   func(ids *idsState) string { return "/instances/" + ids.instanceID + "/preview?kind=missing" },
			want:   http.StatusAccepted,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				runID, ok := decoded["run_id"].(string)
				if !ok || runID == "" {
					t.Fatalf("previewRun missing run_id: %v", decoded)
				}
				seed, ok := decoded["seed"].(float64)
				if !ok || int64(seed) == 0 {
					t.Fatalf("previewRun seed = %v, want non-zero", decoded["seed"])
				}
				ids.runID = runID
			},
		},
		{
			op:     "cancelRun",
			method: http.MethodPost,
			path:   func(ids *idsState) string { return "/runs/" + ids.runID + "/cancel" },
			want:   http.StatusNoContent,
		},
		{
			op: "listEvents",

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
			op:     "listLogs",
			method: http.MethodGet,
			path:   func(_ *idsState) string { return "/logs?level=warn&limit=10" },
			want:   http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["records"] == nil || decoded["capacity"] == nil {
					t.Fatalf("listLogs missing records or capacity: %v", decoded)
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

func stepsModules() []routerStep {
	return []routerStep{
		{
			op:     "listModules",
			method: http.MethodGet,
			path:   func(_ *idsState) string { return "/system/modules" },
			want:   http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				items, ok := decoded["items"].([]any)
				if !ok || len(items) == 0 {
					t.Fatalf("listModules items = %v, want non-empty", decoded)
				}
			},
		},
		{
			op:     "getModule",
			method: http.MethodGet,
			path:   func(_ *idsState) string { return "/system/modules/llm" },
			want:   http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if id, ok := decoded["id"].(string); !ok || id != "llm" {
					t.Fatalf("getModule id = %v, want llm", decoded["id"])
				}
			},
		},
		{
			op:     "updateModule",
			method: http.MethodPut,
			path:   func(_ *idsState) string { return "/system/modules/llm" },
			body: func(_ *idsState) any {
				return map[string]any{
					"enabled": false,
					"config":  map[string]any{},
				}
			},
			want: http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if enabled, ok := decoded["enabled"].(bool); !ok || enabled {
					t.Fatalf("updateModule enabled = %v, want false", decoded["enabled"])
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

func stepsConfigarr() []routerStep {
	return []routerStep{
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

func stepsProwlarr(f *routerFixture) []routerStep {
	return []routerStep{
		{
			op:     "createProwlarrLink",
			method: http.MethodPost,
			path:   func(_ *idsState) string { return "/prowlarr" },
			body: func(_ *idsState) any {
				return map[string]any{
					"name":     "prowlarr-test",
					"base_url": f.prowlarrServer.URL,
					"api_key":  "secret-prowlarr-key",
					"enabled":  true,
				}
			},
			want: http.StatusCreated,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				id, ok := decoded["id"].(string)
				if !ok || id == "" {
					t.Fatalf("createProwlarrLink id = %v, want valid uuid", decoded["id"])
				}
				ids.prowlarrLinkID = id
				if decoded["name"] != "prowlarr-test" {
					t.Fatalf("createProwlarrLink name = %v, want prowlarr-test", decoded["name"])
				}
			},
		},
		{
			op:     "listProwlarrLinks",
			method: http.MethodGet,
			path:   func(_ *idsState) string { return "/prowlarr" },
			want:   http.StatusOK,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				if !containsID(decoded["items"], ids.prowlarrLinkID) {
					t.Fatalf("listProwlarrLinks missing link %s", ids.prowlarrLinkID)
				}
			},
		},
		{
			op:     "getProwlarrLink",
			method: http.MethodGet,
			path:   func(ids *idsState) string { return "/prowlarr/" + ids.prowlarrLinkID },
			want:   http.StatusOK,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["id"] != ids.prowlarrLinkID {
					t.Fatalf("getProwlarrLink id = %v, want %s", decoded["id"], ids.prowlarrLinkID)
				}
			},
		},
		{
			op:     "updateProwlarrLink",
			method: http.MethodPut,
			path:   func(ids *idsState) string { return "/prowlarr/" + ids.prowlarrLinkID },
			body: func(_ *idsState) any {
				return map[string]any{
					"name":     "prowlarr-updated",
					"base_url": f.prowlarrServer.URL,
					"api_key":  "secret-prowlarr-key",
					"enabled":  true,
				}
			},
			want: http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["name"] != "prowlarr-updated" {
					t.Fatalf("updateProwlarrLink name = %v, want prowlarr-updated", decoded["name"])
				}
			},
		},
		{
			op:     "testProwlarrLinkInput",
			method: http.MethodPost,
			path:   func(_ *idsState) string { return "/prowlarr/test" },
			body: func(_ *idsState) any {
				return map[string]any{
					"name":     "prowlarr-test",
					"base_url": f.prowlarrServer.URL,
					"api_key":  "secret-prowlarr-key",
					"enabled":  true,
				}
			},
			want: http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["version"] != "1.15.0" {
					t.Fatalf("testProwlarrLinkInput version = %v, want 1.15.0", decoded["version"])
				}
			},
		},
		{
			op:     "testProwlarrLink",
			method: http.MethodPost,
			path:   func(ids *idsState) string { return "/prowlarr/" + ids.prowlarrLinkID + "/test" },
			want:   http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["version"] != "1.15.0" {
					t.Fatalf("testProwlarrLink version = %v, want 1.15.0", decoded["version"])
				}
			},
		},
		{
			op:     "getProwlarrStatus",
			method: http.MethodGet,
			path:   func(_ *idsState) string { return "/prowlarr/status" },
			want:   http.StatusOK,
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
			op:     "deleteProwlarrLink",
			method: http.MethodDelete,
			path:   func(ids *idsState) string { return "/prowlarr/" + ids.prowlarrLinkID },
			want:   http.StatusNoContent,
		},
		{
			op:     "deleteNotificationRule",
			method: http.MethodDelete,
			path:   func(ids *idsState) string { return "/notifications/rules/" + ids.ruleID },
			want:   http.StatusNoContent,
		},
		{
			op:     "deleteNotificationChannel",
			method: http.MethodDelete,
			path:   func(ids *idsState) string { return "/notifications/channels/" + ids.channelID },
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

func stepsNotificationChannels(f *routerFixture) []routerStep {
	return []routerStep{
		{
			op:     "listNotificationChannels",
			method: http.MethodGet,
			path:   func(_ *idsState) string { return "/notifications/channels" },
			want:   http.StatusOK,
		},
		{
			op:     "createNotificationChannel",
			method: http.MethodPost,
			path:   func(_ *idsState) string { return "/notifications/channels" },
			body: func(_ *idsState) any {
				return map[string]any{
					"name":    "router-webhook",
					"kind":    "webhook",
					"enabled": true,
					"config": map[string]any{
						"url":                   f.notifySrvURL,
						"secret":                "32-bytes-test-secret-for-webhook",
						"allow_insecure_http":   true,
						"allow_private_network": true,
					},
				}
			},
			want: http.StatusCreated,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				id, ok := decoded["id"].(string)
				if !ok || id == "" {
					t.Fatalf("createNotificationChannel id = %v, want non-empty", decoded["id"])
				}
				ids.channelID = id
			},
		},
		{
			op:     "getNotificationChannel",
			method: http.MethodGet,
			path:   func(ids *idsState) string { return "/notifications/channels/" + ids.channelID },
			want:   http.StatusOK,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["id"] != ids.channelID {
					t.Fatalf("getNotificationChannel id = %v, want %s", decoded["id"], ids.channelID)
				}
			},
		},
		{
			op:     "updateNotificationChannel",
			method: http.MethodPut,
			path:   func(ids *idsState) string { return "/notifications/channels/" + ids.channelID },
			body: func(_ *idsState) any {
				return map[string]any{
					"name":    "router-webhook-updated",
					"kind":    "webhook",
					"enabled": true,
					"config": map[string]any{
						"url":                   f.notifySrvURL,
						"allow_insecure_http":   true,
						"allow_private_network": true,
					},
				}
			},
			want: http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["name"] != "router-webhook-updated" {
					t.Fatalf("updateNotificationChannel name = %v, want router-webhook-updated", decoded["name"])
				}
			},
		},
		{
			op:     "testNotificationChannel",
			method: http.MethodPost,
			path:   func(ids *idsState) string { return "/notifications/channels/" + ids.channelID + "/test" },
			want:   http.StatusNoContent,
		},
	}
}

func stepsNotificationRules() []routerStep {
	return []routerStep{
		{
			op:     "listNotificationRules",
			method: http.MethodGet,
			path:   func(_ *idsState) string { return "/notifications/rules" },
			want:   http.StatusOK,
		},
		{
			op:     "createNotificationRule",
			method: http.MethodPost,
			path:   func(_ *idsState) string { return "/notifications/rules" },
			body: func(ids *idsState) any {
				return map[string]any{
					"name":        "router-streak-rule",
					"event_kind":  "run_failure_streak",
					"threshold":   3,
					"enabled":     true,
					"channel_ids": []string{ids.channelID},
				}
			},
			want: http.StatusCreated,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				id, ok := decoded["id"].(string)
				if !ok || id == "" {
					t.Fatalf("createNotificationRule id = %v, want non-empty", decoded["id"])
				}
				ids.ruleID = id
			},
		},
		{
			op:     "getNotificationRule",
			method: http.MethodGet,
			path:   func(ids *idsState) string { return "/notifications/rules/" + ids.ruleID },
			want:   http.StatusOK,
			capture: func(t *testing.T, ids *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["id"] != ids.ruleID {
					t.Fatalf("getNotificationRule id = %v, want %s", decoded["id"], ids.ruleID)
				}
			},
		},
		{
			op:     "updateNotificationRule",
			method: http.MethodPut,
			path:   func(ids *idsState) string { return "/notifications/rules/" + ids.ruleID },
			body: func(ids *idsState) any {
				return map[string]any{
					"name":        "router-streak-rule-updated",
					"event_kind":  "run_failure_streak",
					"threshold":   5,
					"enabled":     true,
					"channel_ids": []string{ids.channelID},
				}
			},
			want: http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["name"] != "router-streak-rule-updated" {
					t.Fatalf("updateNotificationRule name = %v, want router-streak-rule-updated", decoded["name"])
				}
			},
		},
	}
}

func stepsPriority() []routerStep {
	return []routerStep{
		{
			op:     "getPriorityPresets",
			method: http.MethodGet,
			path:   func(_ *idsState) string { return "/priority-presets" },
			want:   http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				items, ok := decoded["items"].([]any)
				if !ok || len(items) == 0 {
					t.Fatalf("getPriorityPresets items = %v, want non-empty", decoded["items"])
				}
			},
		},
		{
			op:     "getInstancePriority",
			method: http.MethodGet,
			path:   func(ids *idsState) string { return "/instances/" + ids.instanceID + "/priority" },
			want:   http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["search_source"] != "preset" {
					t.Fatalf("getInstancePriority search_source = %v, want preset", decoded["search_source"])
				}
			},
		},
		{
			op:     "updateInstancePriority",
			method: http.MethodPut,
			path:   func(ids *idsState) string { return "/instances/" + ids.instanceID + "/priority" },
			body: func(_ *idsState) any {
				return map[string]any{"search_priority": 15, "download_priority": 25}
			},
			want: http.StatusOK,
			capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
				t.Helper()
				if decoded["search_source"] != "instance" || decoded["effective_search_priority"] != float64(15) {
					t.Fatalf("updateInstancePriority = %v, want search_source=instance, effective=15", decoded)
				}
			},
		},
	}
}

func buildRouterSteps(f *routerFixture) []routerStep {
	steps := make([]routerStep, 0, 78)
	steps = append(steps, stepsAuth()...)
	steps = append(steps, stepsAPIKeys()...)
	steps = append(steps, stepsInstances()...)
	steps = append(steps, stepsPoliciesAndRuns()...)
	steps = append(steps, stepsPriority()...)
	steps = append(steps, stepsSettingsAndSchedules()...)
	steps = append(steps, stepsModules()...)
	steps = append(steps, stepsDownloadClients()...)
	steps = append(steps, stepsSeerr(f)...)
	steps = append(steps, stepsConfigarr()...)
	steps = append(steps, stepsProwlarr(f)...)
	steps = append(steps, stepsNotificationChannels(f)...)
	steps = append(steps, stepsNotificationRules()...)
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
	w := testdiag.Watch(10*time.Second, f.st.Pool())
	resp, err := f.client.Do(req)
	w.Stop()
	w.Report(t, err)
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

func createAdminUser(t *testing.T, f *routerFixture) (string, string) {
	t.Helper()
	username := "admin-" + uuid.NewString()[:8]
	password := "admin-pass-1234"
	hasher := crypto.NewPasswordHasher(1)
	hash, err := hasher.Hash(t.Context(), password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	adminID := uuid.New()
	_, err = f.st.Q().CreateUser(t.Context(), sqlcgen.CreateUserParams{
		ID:           adminID,
		Username:     username,
		PasswordHash: hash,
		Role:         string(domain.RoleAdmin),
		CreatedAt:    f.clk.Now(),
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return username, password
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

func loginViewerSession(t *testing.T, f *routerFixture) (*http.Client, string) {
	t.Helper()
	viewerUser, viewerPass := createViewerUser(t, f)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Timeout: 10 * time.Second, Jar: jar}

	loginBody := bytes.NewReader([]byte(fmt.Sprintf(`{"username":%q,"password":%q}`, viewerUser, viewerPass)))
	loginReq, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.srv.URL+PathPrefix+"/auth/login", loginBody)
	if err != nil {
		t.Fatalf("login request: %v", err)
	}
	loginReq.Header.Set("Content-Type", "application/json")
	loginResp, err := client.Do(loginReq)
	if err != nil {
		t.Fatalf("viewer login: %v", err)
	}
	defer func() { _ = loginResp.Body.Close() }()
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("viewer login status = %d, want 200", loginResp.StatusCode)
	}

	sessReq, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.srv.URL+PathPrefix+"/auth/session", nil)
	if err != nil {
		t.Fatalf("session request: %v", err)
	}
	sessResp, err := client.Do(sessReq)
	if err != nil {
		t.Fatalf("session Do: %v", err)
	}
	defer func() { _ = sessResp.Body.Close() }()
	if sessResp.StatusCode != http.StatusOK {
		t.Fatalf("session status = %d, want 200", sessResp.StatusCode)
	}
	var sess oas.Session
	if err := json.NewDecoder(sessResp.Body).Decode(&sess); err != nil {
		t.Fatalf("decode session: %v", err)
	}
	return client, sess.CsrfToken
}

func initViewerIDsState(t *testing.T, csrfToken string) *idsState {
	t.Helper()
	dummyUUID := "00000000-0000-0000-0000-000000000001"
	ids := newIDsState(t)
	ids.csrfToken = csrfToken
	ids.apiKeyID = dummyUUID
	ids.instanceID = dummyUUID
	ids.runID = dummyUUID
	ids.scheduleID = dummyUUID
	ids.clientID = dummyUUID
	ids.seerrLinkID = dummyUUID
	ids.seerrReqID = 101
	ids.prowlarrLinkID = dummyUUID
	ids.channelID = dummyUUID
	ids.ruleID = dummyUUID

	pRaw, err := json.Marshal(policyToOAS(domain.DefaultPolicy(uuid.New())))
	if err != nil {
		t.Fatalf("marshal default policy: %v", err)
	}
	var policyMap map[string]any
	if err = json.Unmarshal(pRaw, &policyMap); err != nil {
		t.Fatalf("unmarshal default policy: %v", err)
	}
	ids.policy = policyMap

	qpRaw, err := json.Marshal(queuePolicyToOAS(queue.DefaultSettings()))
	if err != nil {
		t.Fatalf("marshal default queue policy: %v", err)
	}
	var qpMap map[string]any
	if err = json.Unmarshal(qpRaw, &qpMap); err != nil {
		t.Fatalf("unmarshal default queue policy: %v", err)
	}
	ids.queuePolicy = qpMap

	ids.settings = map[string]any{
		"history_retention_days": 30,
		"user_agent":             "SnatchArr/1.0",
		"global_hourly_cap":      0,
		// Required since ADR-0022: without them the body fails decoding (400) before the role check.
		"search_priority_preset":   viewerPriorityPreset(),
		"download_priority_preset": viewerPriorityPreset(),
	}
	return ids
}

func viewerPriorityPreset() map[string]any {
	return map[string]any{
		"sonarr": 10, "radarr": 10, "lidarr": 20, "readarr": 30, "whisparr_v2": 40, "whisparr_v3": 40,
	}
}

func indexRouterSteps(steps []routerStep) map[string]routerStep {
	m := make(map[string]routerStep, len(steps))
	for _, s := range steps {
		m[s.op] = s
	}
	return m
}

func isViewerExemptSessionEndpoint(path string) bool {
	switch path {
	case "/auth/setup", "/auth/login", "/auth/logout", "/auth/password", "/auth/proxy-session":
		return true
	default:
		return false
	}
}

func executeViewerForbiddenStep(t *testing.T, f *routerFixture, client *http.Client, ids *idsState, step routerStep) {
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

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: client.Do: %v", step.method, reqPath, err)
	}
	defer func() { _ = resp.Body.Close() }()

	assertProblem(t, resp, http.StatusForbidden)
}

func TestRouterRejectsViewerMutationsAndProbes(t *testing.T) {
	t.Parallel()
	f := newRouterFixture(t)
	setupFixtureStubs(f)
	_ = completeInitialSetup(t, f)

	viewerClient, csrfToken := loginViewerSession(t, f)
	ids := initViewerIDsState(t, csrfToken)

	stepsByOp := indexRouterSteps(buildRouterSteps(f))
	ops := specOperations(t)
	testedCount := 0

	for _, op := range ops {
		if isViewerExemptSessionEndpoint(op.path) {
			continue
		}
		if op.method == http.MethodGet && !strings.HasPrefix(strings.ToLower(op.operationID), "test") {
			continue
		}
		step, ok := stepsByOp[op.operationID]
		if !ok {
			t.Fatalf("missing routerStep for operation %q", op.operationID)
		}
		testedCount++
		t.Run(op.operationID, func(t *testing.T) {
			t.Parallel()
			executeViewerForbiddenStep(t, f, viewerClient, ids, step)
		})
	}
	if testedCount == 0 {
		t.Fatal("no operations tested")
	}

	adminOnlyReads := []string{"listLogs", "getConfigarrStatus", "getPolicyFilterOptions", "getModule"}
	for _, opID := range adminOnlyReads {
		step, ok := stepsByOp[opID]
		if !ok {
			t.Fatalf("missing routerStep for admin read %q", opID)
		}
		t.Run(opID, func(t *testing.T) {
			t.Parallel()
			executeViewerForbiddenStep(t, f, viewerClient, ids, step)
		})
	}

	t.Run("streamLogs", func(t *testing.T) {
		t.Parallel()
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.srv.URL+LogStreamPath, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		resp, err := viewerClient.Do(req)
		if err != nil {
			t.Fatalf("client.Do: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		assertProblem(t, resp, http.StatusForbidden)
	})
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

func TestRouterProbeAuthProxyRedirect(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/seerr/test", "/prowlarr/test"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/outpost.goauthentik.io/start" {
					w.Header().Set("Content-Type", "text/html")
					_, _ = w.Write([]byte("<html>login</html>"))
					return
				}
				http.Redirect(w, r, "/outpost.goauthentik.io/start?rd=private-return", http.StatusFound)
			}))
			t.Cleanup(upstream.Close)
			f := newRouterFixture(t)
			f.h.seerr = seerr.New(nil, nil, nil, nil, seerrclient.New(time.Second, "test"), nil, nil, nil, nil, nil)
			completeInitialSetup(t, f)
			step := routerStep{
				method: http.MethodPost,
				path:   func(_ *idsState) string { return path },
				body: func(_ *idsState) any {
					return map[string]any{"name": "proxy-test", "base_url": upstream.URL, "api_key": "test-key", "enabled": true}
				},
				want: http.StatusBadGateway,
				capture: func(t *testing.T, _ *idsState, decoded map[string]any) {
					t.Helper()
					detail, _ := decoded["detail"].(string)
					if !strings.Contains(detail, "redirects to "+upstream.URL+"/outpost.goauthentik.io/start") {
						t.Fatalf("problem detail = %q, want redirect destination", detail)
					}
					if strings.Contains(detail, "private-return") {
						t.Fatalf("redirect query leaked: %s", detail)
					}
				},
			}
			executeRouterStep(t, f, newIDsState(t), step)
		})
	}
}
