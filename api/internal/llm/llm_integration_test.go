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
	"sync"
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

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/auth"
	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/buildinfo"
	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/configarr"
	"github.com/lusoris/SnatchArr/api/internal/dlclients"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/events"
	"github.com/lusoris/SnatchArr/api/internal/httpapi"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/llm"
	"github.com/lusoris/SnatchArr/api/internal/modules"
	"github.com/lusoris/SnatchArr/api/internal/notify"
	"github.com/lusoris/SnatchArr/api/internal/policies"
	"github.com/lusoris/SnatchArr/api/internal/prowlarr"
	"github.com/lusoris/SnatchArr/api/internal/prowlarrclient"
	"github.com/lusoris/SnatchArr/api/internal/queue"
	"github.com/lusoris/SnatchArr/api/internal/seerr"
	"github.com/lusoris/SnatchArr/api/internal/seerrclient"
	"github.com/lusoris/SnatchArr/api/internal/settings"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
	"github.com/lusoris/SnatchArr/api/internal/store"
	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type mcpFixture struct {
	st          *store.Store
	clk         clock.Clock
	settingsSvc *settings.Service
	modulesSvc  *modules.Service
	handlers    *httpapi.Handlers
	security    *httpapi.Security
	build       buildinfo.Info
	logger      *slog.Logger
	instSvc     *instances.Service
	polSvc      *policies.Service
	apiKeys     *auth.APIKeys
	runs        *snatch.Runs
	schedules   *snatch.Schedules
	dlSvc       *dlclients.Service
	seerrSvc    *seerr.Service
	prowlarrSvc *prowlarr.Service
	notifySvc   *notify.Service
	admin       domain.User
	srv         *httptest.Server
	client      *http.Client
	logBuf      *syncBuffer
}

func buildFixtureAuth(t *testing.T, st *store.Store, clk clock.Clock, ids id.Generator, logger *slog.Logger) (*auth.Service, *auth.APIKeys, *session.Manager, *apikey.Service, domain.User) {
	t.Helper()
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
	return authSvc, apiKeys, sessMgr, keys, admin
}

func newMCPFixture(t *testing.T) *mcpFixture {
	t.Helper()
	st := storetest.New(t)
	clk := clock.NewFake()
	ids := id.New()
	logBuf := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	enc, err := crypto.NewEncryptor(bytes.Repeat([]byte{0x42}, 32))
	if err != nil {
		t.Fatalf("encryptor: %v", err)
	}
	authSvc, apiKeys, sessMgr, keys, admin := buildFixtureAuth(t, st, clk, ids, logger)
	probe := arrclient.NewGoenvoyProber(arrclient.Options{Timeout: 5 * time.Second})
	instSvc := instances.New(st, enc, clk, ids, probe, logger)
	polSvc := policies.New(st, clk)
	settingsSvc := settings.New(st, clk)
	runs := snatch.NewRuns(st, clk, ids)
	rec := snatch.NewRecorder(st, clk, events.New(logger))
	schedules := snatch.NewSchedules(st, clk, ids)
	queueStore := queue.NewStore(st, clk)
	queueSvc := queue.NewService(queueStore, queue.NewDBInstanceSource(st, enc), probe, clk, nil, nil, logger)
	dlSnap := dlclients.NewGoenvoy(clk)
	dlSvc := dlclients.New(st, enc, clk, ids, dlSnap, probe, instSvc, logger)
	seerrClient := seerrclient.New(5*time.Second, "SnatchArr-test")
	seerrSvc := seerr.New(st, enc, clk, ids, seerrClient, probe, instSvc, runs, rec, logger)
	prowlarrClient := prowlarrclient.New(5*time.Second, "SnatchArr-test")
	prowlarrSvc := prowlarr.New(st, enc, clk, ids, prowlarrClient, probe, instSvc, logger, config.Options{Prowlarr: config.ProwlarrOptions{LimitThreshold: 0.9}})
	notifySvc := notify.NewService(st, notify.NewWebhookStore(st, enc), enc, clk, ids, logger)
	cfg := config.Options{Profile: config.ProfileDev}
	configarrSvc := configarr.New(cfg, instSvc, dlSvc, polSvc, queueStore, probe, clk, logger)
	build := buildinfo.Info{Version: "0.1.0-test", Commit: "abc1234", Date: "2026-10-21"}
	handlers := httpapi.NewHandlers(build, cfg, clk, authSvc, apiKeys, sessMgr, instSvc, polSvc, logger, httpapi.Deps{
		Runs: runs, Rec: rec, Workers: snatch.NewWorkers(st, clk), Settings: settingsSvc,
		Schedules: schedules, Memory: snatch.NewMemory(st, clk), Queue: queueStore, QueueSvc: queueSvc, Configarr: configarrSvc,
		Clients: dlSvc, Seerr: seerrSvc, Prowlarr: prowlarrSvc, Notify: notifySvc,
	})
	var modReg *modules.Registry
	modReg, err = modules.DefaultRegistry()
	if err != nil {
		t.Fatalf("modules.DefaultRegistry: %v", err)
	}
	modulesSvc := modules.New(st, clk, modReg)
	if err = modulesSvc.Ensure(t.Context()); err != nil {
		t.Fatalf("modulesSvc.Ensure: %v", err)
	}
	security := httpapi.NewSecurity(sessMgr, cfg, keys, authSvc, apiKeys, clk, logger, nil)
	llmSvc, err := llm.New(build, settingsSvc, security, handlers, modulesSvc, logger)
	if err != nil {
		t.Fatalf("llm.New: %v", err)
	}
	r := chi.NewRouter()
	llm.Mount(r, llmSvc)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return &mcpFixture{
		st: st, clk: clk, settingsSvc: settingsSvc, modulesSvc: modulesSvc, handlers: handlers,
		security: security, build: build, logger: logger, instSvc: instSvc, polSvc: polSvc, apiKeys: apiKeys,
		runs: runs, schedules: schedules, dlSvc: dlSvc, seerrSvc: seerrSvc,
		prowlarrSvc: prowlarrSvc, notifySvc: notifySvc, admin: admin,
		srv: srv, client: &http.Client{Timeout: 10 * time.Second}, logBuf: logBuf,
	}
}

func enableLLM(t *testing.T, f *mcpFixture, writeTools ...string) {
	t.Helper()
	cfg, err := f.settingsSvc.Get(t.Context())
	if err != nil {
		t.Fatalf("settings.Get: %v", err)
	}
	cfg.LLMEnabled = true
	cfg.LLMWriteTools = writeTools
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
	callTriggerRunRPC := map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "tools/call",
		"params": map[string]any{"name": "snatcharr_trigger_run", "arguments": map[string]any{}},
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
		{
			name:        "negative missing write scope on write tool returns 403 before opt-in check",
			enable:      true,
			scopes:      []string{domain.ScopeCoreRead},
			payload:     callTriggerRunRPC,
			wantStatus:  http.StatusForbidden,
			wantWWWAuth: []string{`error="insufficient_scope"`, `scope="core:write"`},
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

	var writeNames []string
	for _, spec := range tools {
		if !spec.ReadOnly {
			writeNames = append(writeNames, spec.Name)
		}
		t.Run(spec.Name, func(t *testing.T) {
			t.Parallel()
			if !namePattern.MatchString(spec.Name) {
				t.Fatalf("tool name %q does not match pattern ^snatcharr_[a-z0-9_]+$", spec.Name)
			}
			expectedName := "snatcharr_" + toSnakeCase(spec.OperationID)
			if spec.Name != expectedName {
				t.Fatalf("tool name %q does not match snake_case(operationId) %q", spec.Name, expectedName)
			}
			if spec.ReadOnly && spec.Destructive {
				t.Fatalf("read tool %q must have Destructive=false", spec.Name)
			}
			if spec.Scope == "" {
				t.Fatalf("tool %q has empty Scope", spec.Name)
			}
			if spec.Description == "" {
				t.Fatalf("tool %q has empty Description", spec.Name)
			}
		})
	}

	enumVals := oas.LlmWriteTool("").AllValues()
	wantEnum := make([]string, len(enumVals))
	for i, v := range enumVals {
		wantEnum[i] = string(v)
	}
	if !slices.Equal(writeNames, wantEnum) {
		t.Fatalf("registry write tools = %v, want LlmWriteTool enum %v", writeNames, wantEnum)
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
				wantReadCount := 0
				for _, spec := range llm.RegisteredToolsForTest() {
					if spec.ReadOnly {
						wantReadCount++
					}
				}
				if len(got) != wantReadCount {
					t.Fatalf("tools/list count = %d, want registered read count %d; tools = %v", len(got), wantReadCount, got)
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
			args:      map[string]any{"instanceId": seeded.ID.String()},
			wantError: false,
			wantID:    seeded.ID.String(),
		},
		{
			name:      "negative bad uuid returns isError true",
			args:      map[string]any{"instanceId": "not-a-valid-uuid"},
			wantError: true,
		},
		{
			name:      "negative unknown alias key id is rejected",
			args:      map[string]any{"id": seeded.ID.String()},
			wantError: true,
		},
		{
			name:       "boundary unknown valid uuid returns isError true with not-found detail",
			args:       map[string]any{"instanceId": uuid.NewString()},
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

func TestMCPWriteToolOptInFence(t *testing.T) {
	t.Parallel()
	f := newMCPFixture(t)
	enableLLM(t, f)
	inst, err := f.instSvc.Create(t.Context(), instances.Input{
		Kind: domain.KindRadarr, Name: "Radarr Write", BaseURL: "http://radarr.internal:7878", APIKey: "radarr-secret-key", Enabled: true,
	})
	if err != nil {
		t.Fatalf("instSvc.Create: %v", err)
	}
	createdKey, err := f.apiKeys.Create(t.Context(), f.admin.ID, "writer", nil, domain.ScopeCoreRead, domain.ScopeCoreWrite)
	if err != nil {
		t.Fatalf("apiKeys.Create: %v", err)
	}
	token := createdKey.Secret

	// Not opted in: hidden from tools/list and refused with -32021 (ADR planted case 1).
	if slices.Contains(callToolsList(t, f, token), domain.LLMWriteToolTriggerRun) {
		t.Fatal("tools/list included snatcharr_trigger_run before opt-in")
	}
	assertRefusedWriteCall(t, f, token, createdKey.ID, inst.ID)

	// Opted in: visible in tools/list and succeeds on call.
	enableLLM(t, f, domain.LLMWriteToolTriggerRun)
	listed := callToolsList(t, f, token)
	if !slices.Contains(listed, domain.LLMWriteToolTriggerRun) || slices.Contains(listed, domain.LLMWriteToolDeleteInstance) {
		t.Fatalf("tools/list after opt-in = %v, want trigger_run only", listed)
	}
	got := callToolWithParams(t, f, token, domain.LLMWriteToolTriggerRun, map[string]any{
		"instanceId": inst.ID.String(), "body": map[string]any{"kind": "missing"},
	})
	if got.IsError {
		t.Fatalf("trigger_run after opt-in failed: %s", got.ContentText)
	}
	assertLogContains(t, f.logBuf.String(), "llm: write tool called", "tool=snatcharr_trigger_run", "owner="+f.admin.ID.String(), "key_id="+createdKey.ID)
	if runs := listRunsCount(t, f, token); runs != 1 {
		t.Fatalf("runs count after opted-in trigger_run = %d, want 1", runs)
	}
}

func assertRefusedWriteCall(t *testing.T, f *mcpFixture, token, keyID string, instID uuid.UUID) {
	t.Helper()
	res := postMCP(t, f, token, "", map[string]any{
		"jsonrpc": "2.0", "id": 42, "method": "tools/call",
		"params": map[string]any{
			"name":      domain.LLMWriteToolTriggerRun,
			"arguments": map[string]any{"instanceId": instID.String(), "body": map[string]any{"kind": "missing"}},
		},
	})
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("refused write status=%d ct=%q body=%s", res.StatusCode, res.Header.Get("Content-Type"), string(res.Body))
	}
	var rpcErr struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Error   struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(res.Body, &rpcErr); err != nil {
		t.Fatalf("decode JSON-RPC error: %v (%s)", err, string(res.Body))
	}
	if rpcErr.ID != 42 || rpcErr.Error.Code != llm.ErrCodeToolNotEnabled || rpcErr.Error.Message != "tool not enabled by administrator" {
		t.Fatalf("rpcErr = %+v, want id=42 code=-32021 msg='tool not enabled by administrator'", rpcErr)
	}
	assertLogContains(t, f.logBuf.String(), "llm: write tool refused", "tool=snatcharr_trigger_run", "owner="+f.admin.ID.String(), "key_id="+keyID, "reason=not_opted_in")
	if runs := listRunsCount(t, f, token); runs != 0 {
		t.Fatalf("runs count after refused trigger_run = %d, want 0", runs)
	}
}

func listRunsCount(t *testing.T, f *mcpFixture, token string) int {
	t.Helper()
	raw := callToolStructured(t, f, token, "snatcharr_list_runs")
	var runs []map[string]any
	if err := json.Unmarshal(raw, &runs); err != nil {
		t.Fatalf("unmarshal list_runs: %v (%s)", err, string(raw))
	}
	return len(runs)
}

func assertLogContains(t *testing.T, logs string, wantSubstrings ...string) {
	t.Helper()
	for _, sub := range wantSubstrings {
		if !strings.Contains(logs, sub) {
			t.Fatalf("logs missing %q in:\n%s", sub, logs)
		}
	}
}

func TestMCPDestructiveToolConfirmFence(t *testing.T) {
	t.Parallel()
	f := newMCPFixture(t)
	enableLLM(t, f, domain.LLMWriteToolDeleteInstance, domain.LLMWriteToolTriggerRun, domain.LLMWriteToolCancelRun)
	inst, err := f.instSvc.Create(t.Context(), instances.Input{
		Kind: domain.KindRadarr, Name: "Radarr Delete", BaseURL: "http://radarr.internal:7878", APIKey: "radarr-secret-key", Enabled: true,
	})
	if err != nil {
		t.Fatalf("instSvc.Create: %v", err)
	}
	token := issueKey(t, f, "destroyer", domain.ScopeCoreRead, domain.ScopeCoreWrite)

	// ADR planted case 4: confirm absent and confirm false are refused before mutation.
	for _, args := range []map[string]any{
		{"instanceId": inst.ID.String()},
		{"instanceId": inst.ID.String(), "confirm": false},
	} {
		got := callToolWithParams(t, f, token, domain.LLMWriteToolDeleteInstance, args)
		if !got.IsError || got.ContentText != "confirmation required: pass confirm=true" {
			t.Fatalf("delete_instance(%v) = isError:%v text:%q", args, got.IsError, got.ContentText)
		}
		if _, gErr := f.instSvc.Get(t.Context(), inst.ID); gErr != nil {
			t.Fatalf("instance deleted without confirm=true: %v", gErr)
		}
	}
	assertLogContains(t, f.logBuf.String(), "llm: destructive tool unconfirmed", "tool=snatcharr_delete_instance")

	// Positive: confirm=true deletes the instance.
	delGot := callToolWithParams(t, f, token, domain.LLMWriteToolDeleteInstance, map[string]any{
		"instanceId": inst.ID.String(), "confirm": true,
	})
	if delGot.IsError {
		t.Fatalf("delete_instance(confirm=true) failed: %s", delGot.ContentText)
	}
	if _, gErr := f.instSvc.Get(t.Context(), inst.ID); gErr == nil {
		t.Fatal("instance still exists after delete_instance(confirm=true)")
	}
}

func TestMCPUpdatePolicyAndCancelRun(t *testing.T) {
	t.Parallel()
	f := newMCPFixture(t)
	enableLLM(t, f, domain.LLMWriteToolUpdatePolicy, domain.LLMWriteToolTriggerRun, domain.LLMWriteToolCancelRun)
	inst, err := f.instSvc.Create(t.Context(), instances.Input{
		Kind: domain.KindRadarr, Name: "Radarr Pol", BaseURL: "http://radarr.internal:7878", APIKey: "radarr-secret-key", Enabled: true,
	})
	if err != nil {
		t.Fatalf("instSvc.Create: %v", err)
	}
	token := issueKey(t, f, "pol-writer", domain.ScopeCoreRead, domain.ScopeCoreWrite)

	// Negative: invalid body fails validation in decodeOASBody.
	badPol := callToolWithParams(t, f, token, domain.LLMWriteToolUpdatePolicy, map[string]any{
		"instanceId": inst.ID.String(), "body": map[string]any{"missing_per_cycle": -1},
	})
	if !badPol.IsError {
		t.Fatal("update_policy with invalid body expected IsError=true")
	}

	// Positive: valid SnatchPolicy body updates policy.
	polGot := callToolWithParams(t, f, token, "snatcharr_get_policy", map[string]any{"instanceId": inst.ID.String()})
	var polBody map[string]any
	if err := json.Unmarshal(polGot.StructuredContent, &polBody); err != nil {
		t.Fatalf("decode get_policy: %v", err)
	}
	polBody["missing_per_cycle"] = 7
	goodPol := callToolWithParams(t, f, token, domain.LLMWriteToolUpdatePolicy, map[string]any{
		"instanceId": inst.ID.String(), "body": polBody,
	})
	if goodPol.IsError {
		t.Fatalf("update_policy valid failed: %s", goodPol.ContentText)
	}

	// Queue a run and cancel it with confirm=true.
	trig := callToolWithParams(t, f, token, domain.LLMWriteToolTriggerRun, map[string]any{
		"instanceId": inst.ID.String(), "body": map[string]any{"kind": "missing"},
	})
	var runOut struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(trig.StructuredContent, &runOut); err != nil {
		t.Fatalf("decode triggered run: %v", err)
	}
	cancelRes := callToolWithParams(t, f, token, domain.LLMWriteToolCancelRun, map[string]any{
		"runId": runOut.ID, "confirm": true,
	})
	if cancelRes.IsError {
		t.Fatalf("cancel_run(confirm=true) failed: %s", cancelRes.ContentText)
	}
}
