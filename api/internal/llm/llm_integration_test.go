// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package llm_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/golusoris/golusoris/auth/apikey"
	"github.com/golusoris/golusoris/auth/session"
	"github.com/golusoris/golusoris/core/clock"
	"github.com/golusoris/golusoris/core/crypto"
	"github.com/golusoris/golusoris/core/id"

	"github.com/lusoris/SnatchArr/api/internal/auth"
	"github.com/lusoris/SnatchArr/api/internal/buildinfo"
	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/httpapi"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/llm"
	"github.com/lusoris/SnatchArr/api/internal/policies"
	"github.com/lusoris/SnatchArr/api/internal/settings"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

type mcpFixture struct {
	st          *store.Store
	settingsSvc *settings.Service
	instSvc     *instances.Service
	apiKeys     *auth.APIKeys
	admin       domain.User
	srv         *httptest.Server
	client      *http.Client
}

func newMCPFixture(t *testing.T) *mcpFixture {
	t.Helper()
	st := storetest.New(t)
	clk := clock.NewFake()
	ids := id.New()
	logger := slog.New(slog.DiscardHandler)
	enc, err := crypto.NewEncryptor(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("encryptor: %v", err)
	}
	authSvc, err := auth.NewService(st, crypto.NewPasswordHasher(1), clk, ids, logger)
	if err != nil {
		t.Fatalf("auth.NewService: %v", err)
	}
	admin, err := authSvc.Setup(t.Context(), "mcp-admin", "correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("authSvc.Setup: %v", err)
	}
	keys, err := apikey.New(auth.NewPgAPIKeyStore(st, clk), apikey.Options{
		Prefix: "sk", HMACSecret: []byte("test-secret-of-at-least-32-bytes"), Clock: clk,
	})
	if err != nil {
		t.Fatalf("apikey.New: %v", err)
	}
	apiKeys := auth.NewAPIKeys(st, keys, clk, logger)
	sessMgr, err := session.NewManager(auth.NewPgSessionStore(st, clk), session.Options{
		CookieName: "snatcharr_session", TTL: time.Hour, AllowInsecureCookie: true,
	})
	if err != nil {
		t.Fatalf("session.NewManager: %v", err)
	}
	instSvc := instances.New(st, enc, clk, ids, nil, logger)
	polSvc := policies.New(st, clk)
	settingsSvc := settings.New(st, clk)
	workers := snatch.NewWorkers(st, clk)
	cfg := config.Options{Profile: config.ProfileDev}
	build := buildinfo.Info{Version: "0.1.0-test", Commit: "abc1234", Date: "2026-10-21"}
	handlers := httpapi.NewHandlers(build, cfg, clk, authSvc, apiKeys, sessMgr, instSvc, polSvc, logger, httpapi.Deps{
		Workers: workers, Settings: settingsSvc,
	})
	sec := httpapi.NewSecurity(sessMgr, cfg, keys, authSvc, apiKeys, clk, logger)
	llmSvc := llm.New(build, settingsSvc, sec, handlers, logger)
	r := chi.NewRouter()
	llm.Mount(r, llmSvc)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return &mcpFixture{
		st: st, settingsSvc: settingsSvc, instSvc: instSvc, apiKeys: apiKeys, admin: admin,
		srv: srv, client: &http.Client{Timeout: 10 * time.Second},
	}
}

func enableLLM(t *testing.T, f *mcpFixture) {
	t.Helper()
	cfg, err := f.settingsSvc.Get(t.Context())
	if err != nil {
		t.Fatalf("settings.Get: %v", err)
	}
	cfg.LLMEnabled = true
	if _, err := f.settingsSvc.Update(t.Context(), cfg); err != nil {
		t.Fatalf("settings.Update: %v", err)
	}
}

func issueKey(t *testing.T, f *mcpFixture, name string, scopes ...string) string {
	t.Helper()
	created, err := f.apiKeys.Create(t.Context(), f.admin.ID, name, nil, scopes...)
	if err != nil {
		t.Fatalf("apiKeys.Create(%s): %v", name, err)
	}
	return created.Secret
}

type mcpHTTPResult struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

func postMCP(t *testing.T, f *mcpFixture, token, origin string, payload any) mcpHTTPResult {
	t.Helper()
	rawReq, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.srv.URL+llm.Path, bytes.NewReader(rawReq))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return mcpHTTPResult{StatusCode: resp.StatusCode, Header: resp.Header, Body: body}
}

func TestMCPMiddlewareGates(t *testing.T) {
	t.Parallel()
	initRPC := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test-client", "version": "1.0.0"},
		},
	}
	callListInstancesRPC := map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call",
		"params": map[string]any{"name": "snatcharr_list_instances", "arguments": map[string]any{}},
	}

	cases := []struct {
		name        string
		enable      bool
		scopes      []string
		omitBearer  bool
		origin      string
		payload     any
		wantStatus  int
		wantWWWAuth []string
	}{
		{
			name:       "negative module disabled returns 404 problem (ADR planted case 3)",
			enable:     false,
			scopes:     []string{domain.ScopeCoreRead},
			payload:    initRPC,
			wantStatus: http.StatusNotFound,
		},
		{
			name:        "negative missing bearer returns 401 with WWW-Authenticate Bearer",
			enable:      true,
			omitBearer:  true,
			payload:     initRPC,
			wantStatus:  http.StatusUnauthorized,
			wantWWWAuth: []string{"Bearer"},
		},
		{
			name:       "negative cross-origin request returns 403 problem",
			enable:     true,
			scopes:     []string{domain.ScopeCoreRead},
			origin:     "https://evil.example:8443",
			payload:    initRPC,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "negative unscoped key returns 403 problem",
			enable:     true,
			scopes:     nil,
			payload:    initRPC,
			wantStatus: http.StatusForbidden,
		},
		{
			name:        "negative insufficient scope on tools/call returns 403 with scope challenge (ADR planted case 2)",
			enable:      true,
			scopes:      []string{domain.ScopeRequestsRead},
			payload:     callListInstancesRPC,
			wantStatus:  http.StatusForbidden,
			wantWWWAuth: []string{`error="insufficient_scope"`, `scope="core:read"`},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newMCPFixture(t)
			if tc.enable {
				enableLLM(t, f)
			}
			var token string
			if !tc.omitBearer {
				token = issueKey(t, f, "test-key", tc.scopes...)
			}
			res := postMCP(t, f, token, tc.origin, tc.payload)
			assertGateProblem(t, res, tc.wantStatus, tc.wantWWWAuth)
		})
	}
}

func assertGateProblem(t *testing.T, res mcpHTTPResult, wantStatus int, wantWWWAuth []string) {
	t.Helper()
	if res.StatusCode != wantStatus {
		t.Fatalf("status = %d, want %d; body = %s", res.StatusCode, wantStatus, string(res.Body))
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("Content-Type = %q, want application/problem+json", ct)
	}
	www := res.Header.Get("WWW-Authenticate")
	for _, sub := range wantWWWAuth {
		if !strings.Contains(www, sub) {
			t.Fatalf("WWW-Authenticate = %q, want substring %q", www, sub)
		}
	}
}

func TestMCPToolsListAndCallScoped(t *testing.T) {
	t.Parallel()
	f := newMCPFixture(t)
	enableLLM(t, f)

	_, err := f.instSvc.Create(t.Context(), instances.Input{
		Kind: domain.KindSonarr, Name: "Sonarr Main", BaseURL: "http://sonarr.internal:8989", APIKey: "secret-api-key", Enabled: true,
	})
	if err != nil {
		t.Fatalf("instSvc.Create: %v", err)
	}

	coreToken := issueKey(t, f, "core-reader", domain.ScopeCoreRead)
	assertInitializeOK(t, f, coreToken, f.srv.URL)
	tools := callToolsList(t, f, coreToken)
	if !slices.Contains(tools, "snatcharr_list_instances") {
		t.Fatalf("tools/list for core:read missing snatcharr_list_instances: %v", tools)
	}

	structured := callToolStructured(t, f, coreToken, "snatcharr_list_instances")
	var instancesOut []map[string]any
	if err := json.Unmarshal(structured, &instancesOut); err != nil {
		t.Fatalf("unmarshal structuredContent (%s): %v", string(structured), err)
	}
	if len(instancesOut) != 1 || instancesOut[0]["name"] != "Sonarr Main" {
		t.Fatalf("instances structuredContent = %v, want Sonarr Main", instancesOut)
	}
	if strings.Contains(string(structured), "secret-api-key") {
		t.Fatalf("structuredContent leaked api key: %s", string(structured))
	}

	adminToken := issueKey(t, f, "admin-reader", domain.ScopeAdminRead)
	statusStructured := callToolStructured(t, f, adminToken, "snatcharr_get_system_status")
	var statusOut map[string]any
	if err := json.Unmarshal(statusStructured, &statusOut); err != nil {
		t.Fatalf("unmarshal system status (%s): %v", string(statusStructured), err)
	}
	if statusOut["version"] != "0.1.0-test" || statusOut["setup_complete"] != true {
		t.Fatalf("system status structuredContent = %v", statusOut)
	}

	// Boundary: unknown tool passes through middleware and is answered by SDK JSON-RPC error.
	unknownRes := postMCP(t, f, coreToken, "", map[string]any{
		"jsonrpc": "2.0", "id": 4, "method": "tools/call",
		"params": map[string]any{"name": "snatcharr_unknown_tool", "arguments": map[string]any{}},
	})
	if unknownRes.StatusCode != http.StatusOK || !strings.Contains(string(unknownRes.Body), `"error"`) {
		t.Fatalf("unknown tool = %d %s, want 200 JSON-RPC error", unknownRes.StatusCode, string(unknownRes.Body))
	}
}

func assertInitializeOK(t *testing.T, f *mcpFixture, token, sameOrigin string) {
	t.Helper()
	res := postMCP(t, f, token, sameOrigin, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test-client", "version": "1.0.0"},
		},
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("initialize status = %d, body = %s", res.StatusCode, string(res.Body))
	}
}

func callToolsList(t *testing.T, f *mcpFixture, token string) []string {
	t.Helper()
	res := postMCP(t, f, token, "", map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": map[string]any{},
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("tools/list status = %d, body = %s", res.StatusCode, string(res.Body))
	}
	var rpc struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(res.Body, &rpc); err != nil {
		t.Fatalf("decode tools/list: %v (%s)", err, string(res.Body))
	}
	names := make([]string, 0, len(rpc.Result.Tools))
	for _, tl := range rpc.Result.Tools {
		names = append(names, tl.Name)
	}
	return names
}

func callToolStructured(t *testing.T, f *mcpFixture, token, toolName string) json.RawMessage {
	t.Helper()
	res := postMCP(t, f, token, "", map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "tools/call",
		"params": map[string]any{"name": toolName, "arguments": map[string]any{}},
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("tools/call(%s) status = %d, body = %s", toolName, res.StatusCode, string(res.Body))
	}
	var rpc struct {
		Result struct {
			IsError           bool            `json:"isError"`           //nolint:tagliatelle // MCP JSON-RPC wire field
			StructuredContent json.RawMessage `json:"structuredContent"` //nolint:tagliatelle // MCP JSON-RPC wire field
		} `json:"result"`
	}
	if err := json.Unmarshal(res.Body, &rpc); err != nil {
		t.Fatalf("decode tools/call(%s): %v (%s)", toolName, err, string(res.Body))
	}
	if rpc.Result.IsError || len(rpc.Result.StructuredContent) == 0 {
		t.Fatalf("tools/call(%s) failed or missing structuredContent: %s", toolName, string(res.Body))
	}
	return rpc.Result.StructuredContent
}

func TestMCPOriginAndBodyBounds(t *testing.T) {
	t.Parallel()
	initRPC := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "test-client", "version": "1.0.0"},
		},
	}
	oversized := map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "ping",
		"params": map[string]any{"pad": strings.Repeat("a", 1<<20)},
	}

	cases := []struct {
		name       string
		sameOrigin bool
		payload    any
		wantStatus int
		wantProb   bool
	}{
		{name: "positive same-origin request passes the origin gate", sameOrigin: true, payload: initRPC, wantStatus: http.StatusOK},
		{name: "boundary body above 1 MiB returns 400 problem", payload: oversized, wantStatus: http.StatusBadRequest, wantProb: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newMCPFixture(t)
			enableLLM(t, f)
			token := issueKey(t, f, "test-key", domain.ScopeCoreRead)
			origin := ""
			if tc.sameOrigin {
				origin = f.srv.URL
			}
			res := postMCP(t, f, token, origin, tc.payload)
			if tc.wantProb {
				assertGateProblem(t, res, tc.wantStatus, nil)
				return
			}
			if res.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", res.StatusCode, tc.wantStatus, string(res.Body))
			}
		})
	}
}

func toSnakeCase(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestMCPRegistryNaming(t *testing.T) {
	t.Parallel()
	namePattern := regexp.MustCompile(`^snatcharr_[a-z0-9_]+$`)
	tools := llm.RegisteredToolsForTest()
	if len(tools) == 0 {
		t.Fatal("no tools registered")
	}

	for _, spec := range tools {
		t.Run(spec.Name, func(t *testing.T) {
			t.Parallel()
			if !namePattern.MatchString(spec.Name) {
				t.Fatalf("tool name %q does not match pattern ^snatcharr_[a-z0-9_]+$", spec.Name)
			}
			expectedName := "snatcharr_" + toSnakeCase(spec.OperationID)
			if spec.Name != expectedName {
				t.Fatalf("tool name %q does not match snake_case(operationId) %q", spec.Name, expectedName)
			}
			if !spec.ReadOnly {
				t.Fatalf("read tool %q must have ReadOnly=true", spec.Name)
			}
			if spec.Destructive {
				t.Fatalf("read tool %q must have Destructive=false", spec.Name)
			}
			if spec.Scope == "" {
				t.Fatalf("read tool %q has empty Scope", spec.Name)
			}
			if spec.Description == "" {
				t.Fatalf("read tool %q has empty Description", spec.Name)
			}
		})
	}
}

func TestMCPToolsListScopeFiltering(t *testing.T) {
	t.Parallel()
	f := newMCPFixture(t)
	enableLLM(t, f)

	coreReadTools := []string{
		"snatcharr_list_instances",
		"snatcharr_get_instance",
		"snatcharr_get_policy",
		"snatcharr_get_policy_filter_options",
		"snatcharr_get_queue_policy",
		"snatcharr_list_queue_strikes",
		"snatcharr_list_runs",
		"snatcharr_get_run",
		"snatcharr_list_events",
		"snatcharr_get_hourly_caps",
		"snatcharr_list_schedules",
		"snatcharr_get_configarr_status",
	}

	adminReadTools := []string{
		"snatcharr_get_system_status",
		"snatcharr_get_setup_status",
		"snatcharr_get_settings",
		"snatcharr_list_logs",
	}

	allReadScopes := []string{
		domain.ScopeAdminRead,
		domain.ScopeCoreRead,
		domain.ScopeDownloadClientsRead,
		domain.ScopeRequestsRead,
		domain.ScopeProwlarrRead,
		domain.ScopeNotificationsRead,
	}

	cases := []struct {
		name         string
		scopes       []string
		wantExact    []string
		wantAbsent   []string
		wantCountAll bool
	}{
		{
			name:       "key core:read returns exactly core:read tools and no admin:read tools",
			scopes:     []string{domain.ScopeCoreRead},
			wantExact:  coreReadTools,
			wantAbsent: adminReadTools,
		},
		{
			name:         "key with all *:read scopes returns count equal to registered Read rows",
			scopes:       allReadScopes,
			wantCountAll: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			token := issueKey(t, f, "filter-"+tc.name, tc.scopes...)
			assertInitializeOK(t, f, token, f.srv.URL)
			got := callToolsList(t, f, token)

			if tc.wantCountAll {
				registered := llm.RegisteredToolsForTest()
				if len(got) != len(registered) {
					t.Fatalf("tools/list count = %d, want registered count %d; tools = %v", len(got), len(registered), got)
				}
			}
			if len(tc.wantExact) > 0 {
				if len(got) != len(tc.wantExact) {
					t.Fatalf("tools/list count = %d, want %d; got %v, want %v", len(got), len(tc.wantExact), got, tc.wantExact)
				}
				for _, exp := range tc.wantExact {
					if !slices.Contains(got, exp) {
						t.Fatalf("tools/list missing expected tool %q; got %v", exp, got)
					}
				}
			}
			for _, abs := range tc.wantAbsent {
				if slices.Contains(got, abs) {
					t.Fatalf("tools/list unexpectedly contains absent tool %q; got %v", abs, got)
				}
			}
		})
	}
}

type toolCallResultDetails struct {
	IsError           bool
	StructuredContent json.RawMessage
	ContentText       string
}

func callToolWithParams(t *testing.T, f *mcpFixture, token, toolName string, args map[string]any) toolCallResultDetails {
	t.Helper()
	res := postMCP(t, f, token, "", map[string]any{
		"jsonrpc": "2.0", "id": 5, "method": "tools/call",
		"params": map[string]any{"name": toolName, "arguments": args},
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("tools/call(%s) status = %d, body = %s", toolName, res.StatusCode, string(res.Body))
	}
	var rpc struct {
		Result struct {
			IsError           bool            `json:"isError"`           //nolint:tagliatelle // MCP JSON-RPC wire field
			StructuredContent json.RawMessage `json:"structuredContent"` //nolint:tagliatelle // MCP JSON-RPC wire field
			Content           []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(res.Body, &rpc); err != nil {
		t.Fatalf("decode tools/call(%s): %v (%s)", toolName, err, string(res.Body))
	}
	var textBuilder strings.Builder
	for _, c := range rpc.Result.Content {
		if c.Type == "text" {
			textBuilder.WriteString(c.Text)
		}
	}
	return toolCallResultDetails{
		IsError:           rpc.Result.IsError,
		StructuredContent: rpc.Result.StructuredContent,
		ContentText:       textBuilder.String(),
	}
}

func TestMCPParameterizedToolCall(t *testing.T) {
	t.Parallel()
	f := newMCPFixture(t)
	enableLLM(t, f)
	coreToken := issueKey(t, f, "param-tester", domain.ScopeCoreRead)
	assertInitializeOK(t, f, coreToken, f.srv.URL)

	seeded, err := f.instSvc.Create(t.Context(), instances.Input{
		Kind: domain.KindRadarr, Name: "Radarr Seeded", BaseURL: "http://radarr.internal:7878", APIKey: "radarr-secret-key", Enabled: true,
	})
	if err != nil {
		t.Fatalf("instSvc.Create: %v", err)
	}

	cases := []struct {
		name       string
		args       map[string]any
		wantError  bool
		wantID     string
		wantDetail string
	}{
		{
			name:      "positive seeded instance id returns matching structuredContent id",
			args:      map[string]any{"id": seeded.ID.String()},
			wantError: false,
			wantID:    seeded.ID.String(),
		},
		{
			name:      "negative bad uuid returns isError true",
			args:      map[string]any{"id": "not-a-valid-uuid"},
			wantError: true,
		},
		{
			name:       "boundary unknown valid uuid returns isError true with not-found detail",
			args:       map[string]any{"id": uuid.NewString()},
			wantError:  true,
			wantDetail: "not found",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := callToolWithParams(t, f, coreToken, "snatcharr_get_instance", tc.args)
			if got.IsError != tc.wantError {
				t.Fatalf("isError = %v, want %v; text = %q", got.IsError, tc.wantError, got.ContentText)
			}
			if !tc.wantError {
				var instanceOut map[string]any
				if err := json.Unmarshal(got.StructuredContent, &instanceOut); err != nil {
					t.Fatalf("unmarshal structuredContent: %v (%s)", err, string(got.StructuredContent))
				}
				if instanceOut["id"] != tc.wantID {
					t.Fatalf("structuredContent id = %v, want %s", instanceOut["id"], tc.wantID)
				}
			}
			if tc.wantDetail != "" && !strings.Contains(strings.ToLower(got.ContentText), tc.wantDetail) {
				t.Fatalf("contentText = %q, want substring %q", got.ContentText, tc.wantDetail)
			}
		})
	}
}
