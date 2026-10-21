// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package llm_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/dlclients"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/llm"
	"github.com/lusoris/SnatchArr/api/internal/prowlarr"
	"github.com/lusoris/SnatchArr/api/internal/seerr"
	"github.com/lusoris/SnatchArr/api/internal/store/sqlcgen"
)

type r3cDestructiveIDs struct {
	clientID  uuid.UUID
	seerrID   uuid.UUID
	prowlID   uuid.UUID
	channelID uuid.UUID
	ruleID    uuid.UUID
}

type r3cDestructiveCase struct {
	name      string
	tool      string
	makeArgs  func(ids r3cDestructiveIDs) map[string]any
	checkHeld func(ctx context.Context, f *mcpFixture, ids r3cDestructiveIDs) error
}

func seedR3CDestructiveTargets(t *testing.T, f *mcpFixture) r3cDestructiveIDs {
	t.Helper()
	ctx := t.Context()
	dl, err := f.dlSvc.Create(ctx, dlclients.Input{
		Kind: domain.ClientQBittorrent, Name: "qbit-keep", BaseURL: "http://127.0.0.1:1",
		Username: "admin", Secret: "keep-dl-secret", Enabled: true, MaxActive: 5,
	})
	if err != nil {
		t.Fatalf("dlSvc.Create: %v", err)
	}
	sl, err := f.seerrSvc.Create(ctx, seerr.Input{
		Name: "seerr-keep", BaseURL: "http://127.0.0.1:1", APIKey: "keep-seerr-key", Enabled: true,
	})
	if err != nil {
		t.Fatalf("seerrSvc.Create: %v", err)
	}
	pl, err := f.prowlarrSvc.Create(ctx, prowlarr.Input{
		Name: "prowlarr-keep", BaseURL: "http://127.0.0.1:1", APIKey: "keep-prowlarr-key", Enabled: true,
	})
	if err != nil {
		t.Fatalf("prowlarrSvc.Create: %v", err)
	}
	ch, err := f.notifySvc.CreateChannel(ctx, domain.NotificationChannelInput{
		Name: "ntfy-keep", Kind: domain.ChannelNtfy, Enabled: true,
		Config: domain.ChannelConfig{ServerURL: "http://127.0.0.1:1", Topic: "keep"},
	})
	if err != nil {
		t.Fatalf("notifySvc.CreateChannel: %v", err)
	}
	rl, err := f.notifySvc.CreateRule(ctx, domain.NotificationRuleInput{
		Name: "rule-keep", EventKind: domain.RuleCircuitOpened, Threshold: 1,
		Enabled: true, ChannelIDs: []uuid.UUID{ch.ID},
	})
	if err != nil {
		t.Fatalf("notifySvc.CreateRule: %v", err)
	}
	return r3cDestructiveIDs{
		clientID: dl.ID, seerrID: sl.ID, prowlID: pl.ID, channelID: ch.ID, ruleID: rl.ID,
	}
}

func getR3CDestructiveCases() []r3cDestructiveCase {
	return []r3cDestructiveCase{
		{
			name: "delete_download_client confirm false",
			tool: domain.LLMWriteToolDeleteDownloadClient,
			makeArgs: func(ids r3cDestructiveIDs) map[string]any {
				return map[string]any{"clientId": ids.clientID.String(), "confirm": false}
			},
			checkHeld: func(ctx context.Context, f *mcpFixture, ids r3cDestructiveIDs) error {
				_, err := f.dlSvc.Get(ctx, ids.clientID)
				return err
			},
		},
		{
			name: "delete_seerr_link confirm absent",
			tool: domain.LLMWriteToolDeleteSeerrLink,
			makeArgs: func(ids r3cDestructiveIDs) map[string]any {
				return map[string]any{"linkId": ids.seerrID.String()}
			},
			checkHeld: func(ctx context.Context, f *mcpFixture, ids r3cDestructiveIDs) error {
				_, err := f.seerrSvc.Get(ctx, ids.seerrID)
				return err
			},
		},
		{
			name: "delete_prowlarr_link confirm false",
			tool: domain.LLMWriteToolDeleteProwlarrLink,
			makeArgs: func(ids r3cDestructiveIDs) map[string]any {
				return map[string]any{"linkId": ids.prowlID.String(), "confirm": false}
			},
			checkHeld: func(ctx context.Context, f *mcpFixture, ids r3cDestructiveIDs) error {
				_, err := f.prowlarrSvc.Get(ctx, ids.prowlID)
				return err
			},
		},
		{
			name: "delete_notification_channel confirm absent",
			tool: domain.LLMWriteToolDeleteNotificationChannel,
			makeArgs: func(ids r3cDestructiveIDs) map[string]any {
				return map[string]any{"channelId": ids.channelID.String()}
			},
			checkHeld: func(ctx context.Context, f *mcpFixture, ids r3cDestructiveIDs) error {
				_, err := f.notifySvc.GetChannel(ctx, ids.channelID)
				return err
			},
		},
		{
			name: "delete_notification_rule confirm false",
			tool: domain.LLMWriteToolDeleteNotificationRule,
			makeArgs: func(ids r3cDestructiveIDs) map[string]any {
				return map[string]any{"ruleId": ids.ruleID.String(), "confirm": false}
			},
			checkHeld: func(ctx context.Context, f *mcpFixture, ids r3cDestructiveIDs) error {
				_, err := f.notifySvc.GetRule(ctx, ids.ruleID)
				return err
			},
		},
	}
}

func TestMCPR3CDestructiveToolsRefuseWithoutConfirm(t *testing.T) {
	t.Parallel()
	f := newMCPFixture(t)
	enableLLM(t, f, testAllWriteTools...)
	token := issueKey(t, f, "r3c-destroy-tester", testAllScopes...)
	assertInitializeOK(t, f, token, f.srv.URL)
	ids := seedR3CDestructiveTargets(t, f)

	for _, tc := range getR3CDestructiveCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := callToolWithParams(t, f, token, tc.tool, tc.makeArgs(ids))
			if !got.IsError {
				t.Fatalf("%s without confirm expected IsError=true", tc.tool)
			}
			if got.ContentText != "confirmation required: pass confirm=true" {
				t.Fatalf("%s text = %q, want 'confirmation required: pass confirm=true'", tc.tool, got.ContentText)
			}
			if err := tc.checkHeld(t.Context(), f, ids); err != nil {
				t.Fatalf("%s state mutated without confirm: %v", tc.tool, err)
			}
		})
	}
}

type secretLeakCase struct {
	name    string
	tool    string
	args    map[string]any
	secrets []string
}

func getSecretLeakCases() []secretLeakCase {
	return []secretLeakCase{
		{
			name: "arr instance create redacts api_key",
			tool: domain.LLMWriteToolCreateInstance,
			args: map[string]any{"body": map[string]any{
				"name": "Sonarr Secret", "kind": "sonarr", "base_url": "http://127.0.0.1:1",
				"api_key": "arr-top-secret-key-999111", "enabled": true,
			}},
			secrets: []string{"arr-top-secret-key-999111"},
		},
		{
			name: "download client create redacts secret",
			tool: domain.LLMWriteToolCreateDownloadClient,
			args: map[string]any{"body": map[string]any{
				"name": "Qbit Secret", "kind": "qbittorrent", "base_url": "http://127.0.0.1:1",
				"username": "admin", "secret": "dl-client-top-secret-888222", "enabled": true,
			}},
			secrets: []string{"dl-client-top-secret-888222"},
		},
		{
			name: "seerr link create redacts api_key",
			tool: domain.LLMWriteToolCreateSeerrLink,
			args: map[string]any{"body": map[string]any{
				"name": "Seerr Secret", "base_url": "http://127.0.0.1:1",
				"api_key": "seerr-top-secret-key-777333", "enabled": true,
			}},
			secrets: []string{"seerr-top-secret-key-777333"},
		},
		{
			name: "prowlarr link create redacts api_key",
			tool: domain.LLMWriteToolCreateProwlarrLink,
			args: map[string]any{"body": map[string]any{
				"name": "Prowlarr Secret", "base_url": "http://127.0.0.1:1",
				"api_key": "prowlarr-top-secret-key-666444", "enabled": true,
			}},
			secrets: []string{"prowlarr-top-secret-key-666444"},
		},
		{
			name: "notification channel create redacts webhook_url and secret",
			tool: domain.LLMWriteToolCreateNotificationChannel,
			args: map[string]any{"body": map[string]any{
				"name": "Discord Secret", "kind": "discord", "enabled": true,
				"config": map[string]any{
					"webhook_url": "https://discord.example.internal/api/webhooks/secret-hook-555000",
					"token":       "notify-token-secret-333888",
					"secret":      "webhook-signing-secret-444999",
				},
			}},
			secrets: []string{
				"https://discord.example.internal/api/webhooks/secret-hook-555000",
				"secret-hook-555000",
				"notify-token-secret-333888",
				"webhook-signing-secret-444999",
			},
		},
	}
}

func TestMCPWriteToolsCreateNeverLeakSecrets(t *testing.T) {
	t.Parallel()
	f := newMCPFixture(t)
	enableLLM(t, f, testAllWriteTools...)
	token := issueKey(t, f, "secret-auditor", testAllScopes...)
	assertInitializeOK(t, f, token, f.srv.URL)

	for _, tc := range getSecretLeakCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := callToolWithParams(t, f, token, tc.tool, tc.args)
			if got.IsError {
				t.Fatalf("%s failed: %s", tc.tool, got.ContentText)
			}
			raw := string(got.StructuredContent)
			for _, sec := range tc.secrets {
				if strings.Contains(raw, sec) || strings.Contains(got.ContentText, sec) {
					t.Fatalf("%s leaked secret %q in result: structured=%s text=%s", tc.tool, sec, raw, got.ContentText)
				}
			}
		})
	}
}

func getR3CInvalidInputCases(ids r3cDestructiveIDs) []struct {
	name string
	tool string
	args map[string]any
} {
	return []struct {
		name string
		tool string
		args map[string]any
	}{
		{
			name: "create_download_client invalid kind",
			tool: domain.LLMWriteToolCreateDownloadClient,
			args: map[string]any{"body": map[string]any{"kind": "invalid_kind", "name": "x", "base_url": "http://127.0.0.1:1"}},
		},
		{
			name: "test_download_client_input invalid kind",
			tool: domain.LLMWriteToolTestDownloadClientInput,
			args: map[string]any{"body": map[string]any{"kind": "invalid_kind", "name": "x", "base_url": "http://127.0.0.1:1"}},
		},
		{
			name: "update_download_client invalid max_active",
			tool: domain.LLMWriteToolUpdateDownloadClient,
			args: map[string]any{"clientId": ids.clientID.String(), "body": map[string]any{
				"kind": "qbittorrent", "name": "qbit-keep", "base_url": "http://127.0.0.1:1", "max_active": -5,
			}},
		},
		{
			name: "create_seerr_link missing base_url",
			tool: domain.LLMWriteToolCreateSeerrLink,
			args: map[string]any{"body": map[string]any{"name": "only-name"}},
		},
		{
			name: "snatch_seerr_request zero requestId",
			tool: domain.LLMWriteToolSnatchSeerrRequest,
			args: map[string]any{"linkId": ids.seerrID.String(), "requestId": 0},
		},
		{
			name: "snatch_seerr_request negative requestId",
			tool: domain.LLMWriteToolSnatchSeerrRequest,
			args: map[string]any{"linkId": ids.seerrID.String(), "requestId": -3},
		},
		{
			name: "create_prowlarr_link missing name",
			tool: domain.LLMWriteToolCreateProwlarrLink,
			args: map[string]any{"body": map[string]any{"base_url": "http://127.0.0.1:1"}},
		},
		{
			name: "create_notification_channel invalid kind",
			tool: domain.LLMWriteToolCreateNotificationChannel,
			args: map[string]any{"body": map[string]any{"name": "bad", "kind": "unknown_kind", "config": map[string]any{}}},
		},
		{
			name: "create_notification_rule empty channel_ids",
			tool: domain.LLMWriteToolCreateNotificationRule,
			args: map[string]any{"body": map[string]any{"name": "bad", "event_kind": "circuit_opened", "channel_ids": []string{}}},
		},
	}
}

func TestMCPR3CWriteToolsInvalidBodyAndParams(t *testing.T) {
	t.Parallel()
	f := newMCPFixture(t)
	enableLLM(t, f, testAllWriteTools...)
	token := issueKey(t, f, "r3c-invalid-tester", testAllScopes...)
	assertInitializeOK(t, f, token, f.srv.URL)
	ids := seedR3CDestructiveTargets(t, f)

	for _, tc := range getR3CInvalidInputCases(ids) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := callToolWithParams(t, f, token, tc.tool, tc.args)
			if !got.IsError {
				t.Fatalf("%s(%v) expected IsError=true, got text: %q", tc.tool, tc.args, got.ContentText)
			}
		})
	}

	ctx := t.Context()
	if list, err := f.dlSvc.List(ctx); err != nil || len(list) != 1 || list[0].MaxActive != 5 {
		t.Fatalf("dlSvc state mutated after invalid calls: %+v, %v", list, err)
	}
	if list, err := f.seerrSvc.List(ctx); err != nil || len(list) != 1 {
		t.Fatalf("seerrSvc state mutated after invalid calls: %+v, %v", list, err)
	}
	if list, err := f.prowlarrSvc.List(ctx); err != nil || len(list) != 1 {
		t.Fatalf("prowlarrSvc state mutated after invalid calls: %+v, %v", list, err)
	}
	if runs := listRunsCount(t, f, token); runs != 0 {
		t.Fatalf("runs enqueued on invalid snatch_seerr_request = %d, want 0", runs)
	}
}

type scopeAndOptInCase struct {
	name       string
	tool       string
	heldScope  string
	wantScope  string
	sampleArgs map[string]any
}

func getScopeAndOptInCases() []scopeAndOptInCase {
	return []scopeAndOptInCase{
		{
			name:       "download_clients:write tool refused with download_clients:read only",
			tool:       domain.LLMWriteToolCreateDownloadClient,
			heldScope:  domain.ScopeDownloadClientsRead,
			wantScope:  domain.ScopeDownloadClientsWrite,
			sampleArgs: map[string]any{"body": map[string]any{"kind": "qbittorrent", "name": "q", "base_url": "http://127.0.0.1:1"}},
		},
		{
			name:       "requests:write tool refused with requests:read only",
			tool:       domain.LLMWriteToolCreateSeerrLink,
			heldScope:  domain.ScopeRequestsRead,
			wantScope:  domain.ScopeRequestsWrite,
			sampleArgs: map[string]any{"body": map[string]any{"name": "s", "base_url": "http://127.0.0.1:1"}},
		},
		{
			name:       "prowlarr:write tool refused with prowlarr:read only",
			tool:       domain.LLMWriteToolCreateProwlarrLink,
			heldScope:  domain.ScopeProwlarrRead,
			wantScope:  domain.ScopeProwlarrWrite,
			sampleArgs: map[string]any{"body": map[string]any{"name": "p", "base_url": "http://127.0.0.1:1"}},
		},
		{
			name:       "notifications:write tool refused with notifications:read only",
			tool:       domain.LLMWriteToolCreateNotificationChannel,
			heldScope:  domain.ScopeNotificationsRead,
			wantScope:  domain.ScopeNotificationsWrite,
			sampleArgs: map[string]any{"body": map[string]any{"name": "n", "kind": "ntfy", "config": map[string]any{}}},
		},
	}
}

func TestMCPR3CScopeAndOptInNegative(t *testing.T) {
	t.Parallel()
	for _, tc := range getScopeAndOptInCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newMCPFixture(t)
			enableLLM(t, f)
			readToken := issueKey(t, f, "read-only-key", tc.heldScope)
			res := postMCP(t, f, readToken, "", map[string]any{
				"jsonrpc": "2.0", "id": 7, "method": "tools/call",
				"params": map[string]any{"name": tc.tool, "arguments": tc.sampleArgs},
			})
			assertGateProblem(t, res, http.StatusForbidden, []string{`error="insufficient_scope"`, fmt.Sprintf(`scope=%q`, tc.wantScope)})

			writeToken := issueKey(t, f, "write-key", tc.heldScope, tc.wantScope)
			optRes := postMCP(t, f, writeToken, "", map[string]any{
				"jsonrpc": "2.0", "id": 8, "method": "tools/call",
				"params": map[string]any{"name": tc.tool, "arguments": tc.sampleArgs},
			})
			assertOptInRefused(t, optRes, 8)
		})
	}
}

func assertOptInRefused(t *testing.T, res mcpHTTPResult, wantID int) {
	t.Helper()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", res.StatusCode, string(res.Body))
	}
	var rpcErr struct {
		ID    int `json:"id"`
		Error struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(res.Body, &rpcErr); err != nil {
		t.Fatalf("decode JSON-RPC error: %v (%s)", err, string(res.Body))
	}
	if rpcErr.ID != wantID || rpcErr.Error.Code != llm.ErrCodeToolNotEnabled {
		t.Fatalf("rpcErr = %+v, want id=%d code=-32021", rpcErr, wantID)
	}
}

func writeRaw(t *testing.T, w http.ResponseWriter, b []byte) {
	t.Helper()
	if _, err := w.Write(b); err != nil {
		t.Errorf("write response: %v", err)
	}
}

type recordedReq struct {
	method string
	path   string
}

type stubRecorder struct {
	mu   sync.Mutex
	reqs []recordedReq
}

func (r *stubRecorder) record(method, path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reqs = append(r.reqs, recordedReq{method: method, path: path})
}

func (r *stubRecorder) has(method, pathPrefix string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, req := range r.reqs {
		if req.method == method && strings.HasPrefix(req.path, pathPrefix) {
			return true
		}
	}
	return false
}

type fakeSonarr struct {
	srv          *httptest.Server
	rec          *stubRecorder
	failStatus   atomic.Bool
	failDownload atomic.Bool
	failCommand  atomic.Bool
}

func newFakeSonarr(t *testing.T, wantKey string) *fakeSonarr {
	t.Helper()
	rec := &stubRecorder{}
	fs := &fakeSonarr{rec: rec}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/system/status", func(w http.ResponseWriter, r *http.Request) {
		rec.record(r.Method, r.URL.Path)
		if fs.failStatus.Load() {
			http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
			return
		}
		if r.Header.Get("X-Api-Key") != wantKey {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		writeRaw(t, w, []byte(`{"appName":"Sonarr","version":"4.0.10.2544"}`))
	})
	mux.HandleFunc("/api/v3/downloadclient", func(w http.ResponseWriter, r *http.Request) {
		rec.record(r.Method, r.URL.Path)
		if fs.failDownload.Load() {
			http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
			return
		}
		if r.Header.Get("X-Api-Key") != wantKey {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		writeRaw(t, w, []byte(`[{"id":1,"name":"qbit-discovered","implementation":"QBittorrent","implementationName":"QBittorrent","enable":true,"fields":[{"name":"host","value":"127.0.0.1"},{"name":"port","value":8080},{"name":"useSsl","value":false},{"name":"username","value":"admin"},{"name":"password","value":"qbit-secret"}]}]`))
	})
	mux.HandleFunc("/api/v3/series", func(w http.ResponseWriter, r *http.Request) {
		rec.record(r.Method, r.URL.Path)
		if r.Header.Get("X-Api-Key") != wantKey {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		writeRaw(t, w, []byte(`[{"id":100}]`))
	})
	mux.HandleFunc("/api/v3/command", func(w http.ResponseWriter, r *http.Request) {
		rec.record(r.Method, r.URL.Path)
		if fs.failCommand.Load() {
			http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
			return
		}
		if r.Header.Get("X-Api-Key") != wantKey {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		writeRaw(t, w, []byte(`{"id":100,"name":"EpisodeSearch","state":"completed"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	fs.srv = srv
	return fs
}

type fakeQBit struct {
	srv         *httptest.Server
	rec         *stubRecorder
	failAuth    atomic.Bool
	failVersion atomic.Bool
}

func newFakeQBit(t *testing.T) *fakeQBit {
	t.Helper()
	rec := &stubRecorder{}
	fq := &fakeQBit{rec: rec}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v2/auth/login", func(w http.ResponseWriter, r *http.Request) {
		rec.record(r.Method, r.URL.Path)
		if fq.failAuth.Load() {
			http.Error(w, "Fails.", http.StatusUnauthorized)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: "session-token"})
		writeRaw(t, w, []byte("Ok."))
	})
	mux.HandleFunc("/api/v2/app/version", func(w http.ResponseWriter, r *http.Request) {
		rec.record(r.Method, r.URL.Path)
		if fq.failVersion.Load() {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		writeRaw(t, w, []byte("v5.2.1"))
	})
	mux.HandleFunc("/api/v2/torrents/info", func(w http.ResponseWriter, r *http.Request) {
		rec.record(r.Method, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		writeRaw(t, w, []byte("[]"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	fq.srv = srv
	return fq
}

type fakeSeerr struct {
	srv          *httptest.Server
	rec          *stubRecorder
	failStatus   atomic.Bool
	failRequests atomic.Bool
	failSettings atomic.Bool
}

func newFakeSeerr(t *testing.T, wantKey, sonarrURL, sonarrKey string) *fakeSeerr {
	t.Helper()
	rec := &stubRecorder{}
	fs := &fakeSeerr{rec: rec}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/status", func(w http.ResponseWriter, r *http.Request) {
		rec.record(r.Method, r.URL.Path)
		if fs.failStatus.Load() {
			http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
			return
		}
		if r.Header.Get("X-Api-Key") != wantKey {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		writeRaw(t, w, []byte(`{"version":"2.5.1"}`))
	})
	mux.HandleFunc("/api/v1/request", func(w http.ResponseWriter, r *http.Request) {
		rec.record(r.Method, r.URL.Path)
		if fs.failRequests.Load() {
			http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
			return
		}
		if r.Header.Get("X-Api-Key") != wantKey {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		writeRaw(t, w, []byte(`{"pageInfo":{"pages":1,"results":1},"results":[{"id":77,"type":"tv","status":2,"is4k":false,"serverId":1,"requestedBy":{"displayName":"Alice"},"media":{"id":5,"mediaType":"tv","tvdbId":81189,"status":3}}]}`))
	})
	mux.HandleFunc("/api/v1/settings/sonarr", func(w http.ResponseWriter, r *http.Request) {
		rec.record(r.Method, r.URL.Path)
		if fs.failSettings.Load() {
			http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
			return
		}
		if r.Header.Get("X-Api-Key") != wantKey {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		u, err := url.Parse(sonarrURL)
		if err != nil {
			t.Fatalf("parse sonarr url: %v", err)
		}
		port, err := strconv.Atoi(u.Port())
		if err != nil {
			port = 8989
		}
		body := fmt.Sprintf(`[{"id":1,"name":"Sonarr","hostname":%q,"port":%d,"apiKey":%q,"useSsl":false,"baseUrl":"","is4k":false,"isDefault":true}]`, u.Hostname(), port, sonarrKey)
		writeRaw(t, w, []byte(body))
	})
	mux.HandleFunc("/api/v1/settings/radarr", func(w http.ResponseWriter, r *http.Request) {
		rec.record(r.Method, r.URL.Path)
		if r.Header.Get("X-Api-Key") != wantKey {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		writeRaw(t, w, []byte(`[{"id":2,"name":"Radarr","hostname":"127.0.0.1","port":7878,"apiKey":"radarr-key","useSsl":false,"baseUrl":"","is4k":false,"isDefault":true}]`))
	})
	mux.HandleFunc("/api/v1/tv/", func(w http.ResponseWriter, r *http.Request) {
		rec.record(r.Method, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		writeRaw(t, w, []byte(`{"name":"Breaking Bad"}`))
	})
	mux.HandleFunc("/api/v1/movie/", func(w http.ResponseWriter, r *http.Request) {
		rec.record(r.Method, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		writeRaw(t, w, []byte(`{"title":"Fight Club"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	fs.srv = srv
	return fs
}

type fakeProwlarr struct {
	srv        *httptest.Server
	rec        *stubRecorder
	failStatus atomic.Bool
	failIndex  atomic.Bool
}

func handleProwlarrJSON(t *testing.T, mux *http.ServeMux, rec *stubRecorder, path, wantKey, payload string) {
	t.Helper()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		rec.record(r.Method, r.URL.Path)
		if r.Header.Get("X-Api-Key") != wantKey {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		writeRaw(t, w, []byte(payload))
	})
}

func newFakeProwlarr(t *testing.T, wantKey string) *fakeProwlarr {
	t.Helper()
	rec := &stubRecorder{}
	fp := &fakeProwlarr{rec: rec}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/system/status", func(w http.ResponseWriter, r *http.Request) {
		rec.record(r.Method, r.URL.Path)
		if fp.failStatus.Load() {
			http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
			return
		}
		if r.Header.Get("X-Api-Key") != wantKey {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		writeRaw(t, w, []byte(`{"version":"1.15.0.4361"}`))
	})
	mux.HandleFunc("/api/v1/indexer", func(w http.ResponseWriter, r *http.Request) {
		rec.record(r.Method, r.URL.Path)
		if fp.failIndex.Load() {
			http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
			return
		}
		if r.Header.Get("X-Api-Key") != wantKey {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		writeRaw(t, w, []byte(`[{"id":1,"name":"Indexer One","enable":true}]`))
	})
	handleProwlarrJSON(t, mux, rec, "/api/v1/indexerstats", wantKey, `{"indexers":[]}`)
	handleProwlarrJSON(t, mux, rec, "/api/v1/indexerstatus", wantKey, "[]")
	handleProwlarrJSON(t, mux, rec, "/api/v1/applications", wantKey, "[]")
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	fp.srv = srv
	return fp
}

func assertNoTransportError(t *testing.T, text string) {
	t.Helper()
	lower := strings.ToLower(text)
	if strings.Contains(lower, "dial tcp") || strings.Contains(lower, "connection refused") {
		t.Fatalf("tool returned transport error instead of 401 failure: %s", text)
	}
}

func testPositiveDownloadClients(t *testing.T, f *mcpFixture, token, instID string, qb *fakeQBit, sn *fakeSonarr) {
	t.Helper()
	createRes := callToolWithParams(t, f, token, domain.LLMWriteToolCreateDownloadClient, map[string]any{
		"body": map[string]any{
			"name": "Qbit Pos", "kind": "qbittorrent", "base_url": qb.srv.URL,
			"username": "admin", "secret": "qbit-secret-123", "enabled": true, "max_active": 10,
		},
	})
	if createRes.IsError {
		t.Fatalf("create_download_client failed: %s", createRes.ContentText)
	}
	var created oas.DownloadClient
	if err := json.Unmarshal(createRes.StructuredContent, &created); err != nil {
		t.Fatalf("decode created download client: %v", err)
	}
	if created.Name != "Qbit Pos" {
		t.Fatalf("client name = %q, want 'Qbit Pos'", created.Name)
	}
	clientID := created.ID.String()

	testInRes := callToolWithParams(t, f, token, domain.LLMWriteToolTestDownloadClientInput, map[string]any{
		"body": map[string]any{
			"name": "Qbit TestIn", "kind": "qbittorrent", "base_url": qb.srv.URL,
			"username": "admin", "secret": "qbit-secret-123", "enabled": true,
		},
	})
	if testInRes.IsError {
		t.Fatalf("test_download_client_input failed: %s", testInRes.ContentText)
	}
	var snapIn oas.DownloadClientStatus
	if err := json.Unmarshal(testInRes.StructuredContent, &snapIn); err != nil || !snapIn.Reachable {
		t.Fatalf("test_download_client_input unreachable: %+v, %v", snapIn, err)
	}

	if res := callToolWithParams(t, f, token, domain.LLMWriteToolUpdateDownloadClient, map[string]any{
		"clientId": clientID,
		"body": map[string]any{
			"name": "Qbit Pos Renamed", "kind": "qbittorrent", "base_url": qb.srv.URL,
			"username": "admin", "enabled": true, "max_active": 15,
		},
	}); res.IsError {
		t.Fatalf("update_download_client failed: %s", res.ContentText)
	}

	testRes := callToolWithParams(t, f, token, domain.LLMWriteToolTestDownloadClient, map[string]any{
		"clientId": clientID,
	})
	if testRes.IsError {
		t.Fatalf("test_download_client failed: %s", testRes.ContentText)
	}
	var snap oas.DownloadClientStatus
	if err := json.Unmarshal(testRes.StructuredContent, &snap); err != nil || !snap.Reachable {
		t.Fatalf("test_download_client unreachable: %+v, %v", snap, err)
	}

	testPositiveDownloadClientDiscovery(t, f, token, instID, clientID, qb, sn)
}

func testPositiveDownloadClientDiscovery(t *testing.T, f *mcpFixture, token, instID, clientID string, qb *fakeQBit, sn *fakeSonarr) {
	t.Helper()
	discRes := callToolWithParams(t, f, token, domain.LLMWriteToolDiscoverDownloadClients, map[string]any{
		"instanceId": instID,
	})
	if discRes.IsError {
		t.Fatalf("discover_download_clients failed: %s", discRes.ContentText)
	}
	var disc oas.DiscoveryResult
	if err := json.Unmarshal(discRes.StructuredContent, &disc); err != nil || disc.Imported != 1 {
		t.Fatalf("discover_download_clients result = %+v, %v; want imported=1", disc, err)
	}

	if !qb.rec.has(http.MethodPost, "/api/v2/auth/login") || !qb.rec.has(http.MethodGet, "/api/v2/app/version") {
		t.Fatal("qbit stub did not receive expected login or version request")
	}
	if !sn.rec.has(http.MethodGet, "/api/v3/downloadclient") {
		t.Fatal("sonarr stub did not receive downloadclient request")
	}

	if res := callToolWithParams(t, f, token, domain.LLMWriteToolDeleteDownloadClient, map[string]any{
		"clientId": clientID, "confirm": true,
	}); res.IsError {
		t.Fatalf("delete_download_client failed: %s", res.ContentText)
	}
}

func seedResolvedSeerrRequest(t *testing.T, f *mcpFixture, linkID, instID uuid.UUID, reqID int) {
	t.Helper()
	now := f.clk.Now()
	entID := int64(100)
	err := f.st.Q().UpsertSeerrRequest(t.Context(), sqlcgen.UpsertSeerrRequestParams{
		LinkID:        linkID,
		RequestID:     int32(reqID),
		MediaType:     string(domain.SeerrMovie),
		TmdbID:        550,
		TvdbID:        0,
		Title:         "Fight Club",
		RequestStatus: 2,
		MediaStatus:   3,
		Is4k:          false,
		RequestedBy:   "alice",
		Seasons:       []int32{},
		InstanceID:    &instID,
		EntityID:      &entID,
		RequestedAt:   &now,
		LastSeenAt:    now,
	})
	if err != nil {
		t.Fatalf("UpsertSeerrRequest: %v", err)
	}
}

func testPositiveSeerrTools(t *testing.T, f *mcpFixture, token string, instID uuid.UUID, sr *fakeSeerr, sn *fakeSonarr) {
	t.Helper()
	linkID, linkUUID := testPositiveSeerrCrudAndSync(t, f, token, sr, sn)
	testPositiveSeerrImportAndSnatch(t, f, token, linkID, linkUUID, instID, sr)
}

func testPositiveSeerrCrudAndSync(t *testing.T, f *mcpFixture, token string, sr *fakeSeerr, sn *fakeSonarr) (string, uuid.UUID) {
	t.Helper()
	createRes := callToolWithParams(t, f, token, domain.LLMWriteToolCreateSeerrLink, map[string]any{
		"body": map[string]any{
			"name": "Seerr Pos", "base_url": sr.srv.URL, "api_key": "seerr-key-1234", "enabled": true,
		},
	})
	if createRes.IsError {
		t.Fatalf("create_seerr_link failed: %s", createRes.ContentText)
	}
	var created oas.SeerrLink
	if err := json.Unmarshal(createRes.StructuredContent, &created); err != nil {
		t.Fatalf("decode created seerr link: %v", err)
	}
	if created.Name != "Seerr Pos" {
		t.Fatalf("seerr link name = %q, want 'Seerr Pos'", created.Name)
	}
	linkID := created.ID.String()

	testInRes := callToolWithParams(t, f, token, domain.LLMWriteToolTestSeerrLinkInput, map[string]any{
		"body": map[string]any{
			"name": "Seerr TestIn", "base_url": sr.srv.URL, "api_key": "seerr-key-1234", "enabled": true,
		},
	})
	if testInRes.IsError {
		t.Fatalf("test_seerr_link_input failed: %s", testInRes.ContentText)
	}
	var probeIn oas.ProbeResult
	if err := json.Unmarshal(testInRes.StructuredContent, &probeIn); err != nil || probeIn.AppName != "Seerr" || probeIn.Version != "2.5.1" {
		t.Fatalf("test_seerr_link_input result: %+v, %v", probeIn, err)
	}

	if res := callToolWithParams(t, f, token, domain.LLMWriteToolUpdateSeerrLink, map[string]any{
		"linkId": linkID,
		"body":   map[string]any{"name": "Seerr Pos Renamed", "base_url": sr.srv.URL, "enabled": true},
	}); res.IsError {
		t.Fatalf("update_seerr_link failed: %s", res.ContentText)
	}

	testRes := callToolWithParams(t, f, token, domain.LLMWriteToolTestSeerrLink, map[string]any{"linkId": linkID})
	if testRes.IsError {
		t.Fatalf("test_seerr_link failed: %s", testRes.ContentText)
	}
	var probe oas.ProbeResult
	if err := json.Unmarshal(testRes.StructuredContent, &probe); err != nil || probe.AppName != "Seerr" {
		t.Fatalf("test_seerr_link result: %+v, %v", probe, err)
	}

	syncRes := callToolWithParams(t, f, token, domain.LLMWriteToolSyncSeerrLink, map[string]any{"linkId": linkID})
	if syncRes.IsError {
		t.Fatalf("sync_seerr_link failed: %s", syncRes.ContentText)
	}
	var syn oas.SeerrSyncResult
	if err := json.Unmarshal(syncRes.StructuredContent, &syn); err != nil || syn.Seen != 1 {
		t.Fatalf("sync_seerr_link seen = %d, want 1 (err=%v)", syn.Seen, err)
	}
	if !sr.rec.has(http.MethodGet, "/api/v1/request") {
		t.Fatal("seerr stub did not receive request query")
	}
	if !sn.rec.has(http.MethodGet, "/api/v3/series") {
		t.Fatal("sonarr stub did not receive series query during sync")
	}
	return linkID, created.ID
}

func testPositiveSeerrImportAndSnatch(t *testing.T, f *mcpFixture, token, linkID string, linkUUID, instID uuid.UUID, sr *fakeSeerr) {
	t.Helper()
	impRes := callToolWithParams(t, f, token, domain.LLMWriteToolImportSeerrInstances, map[string]any{"linkId": linkID})
	if impRes.IsError {
		t.Fatalf("import_seerr_instances failed: %s", impRes.ContentText)
	}
	var imp oas.SeerrImportResult
	if err := json.Unmarshal(impRes.StructuredContent, &imp); err != nil || len(imp.Created) != 1 {
		t.Fatalf("import_seerr_instances created = %+v, want 1 (err=%v)", imp.Created, err)
	}
	if !sr.rec.has(http.MethodGet, "/api/v1/settings/sonarr") {
		t.Fatal("seerr stub did not receive settings query")
	}

	seedResolvedSeerrRequest(t, f, linkUUID, instID, 77)
	snatchRes := callToolWithParams(t, f, token, domain.LLMWriteToolSnatchSeerrRequest, map[string]any{
		"linkId": linkID, "requestId": 77,
	})
	if snatchRes.IsError {
		t.Fatalf("snatch_seerr_request failed: %s", snatchRes.ContentText)
	}
	var run oas.Run
	if err := json.Unmarshal(snatchRes.StructuredContent, &run); err != nil || run.Status != oas.RunStatusQueued {
		t.Fatalf("snatch_seerr_request run status = %q, want 'queued' (err=%v)", run.Status, err)
	}

	if res := callToolWithParams(t, f, token, domain.LLMWriteToolDeleteSeerrLink, map[string]any{
		"linkId": linkID, "confirm": true,
	}); res.IsError {
		t.Fatalf("delete_seerr_link failed: %s", res.ContentText)
	}
}

func testPositiveProwlarrTools(t *testing.T, f *mcpFixture, token string, pl *fakeProwlarr) {
	t.Helper()
	createRes := callToolWithParams(t, f, token, domain.LLMWriteToolCreateProwlarrLink, map[string]any{
		"body": map[string]any{
			"name": "Prowlarr Pos", "base_url": pl.srv.URL, "api_key": "secret-prowlarr-key", "enabled": true,
		},
	})
	if createRes.IsError {
		t.Fatalf("create_prowlarr_link failed: %s", createRes.ContentText)
	}
	var created oas.ProwlarrLink
	if err := json.Unmarshal(createRes.StructuredContent, &created); err != nil {
		t.Fatalf("decode created prowlarr link: %v", err)
	}
	if created.Name != "Prowlarr Pos" {
		t.Fatalf("prowlarr link name = %q, want 'Prowlarr Pos'", created.Name)
	}
	linkID := created.ID.String()

	testInRes := callToolWithParams(t, f, token, domain.LLMWriteToolTestProwlarrLinkInput, map[string]any{
		"body": map[string]any{
			"name": "Prowlarr TestIn", "base_url": pl.srv.URL, "api_key": "secret-prowlarr-key", "enabled": true,
		},
	})
	if testInRes.IsError {
		t.Fatalf("test_prowlarr_link_input failed: %s", testInRes.ContentText)
	}
	var probeIn oas.ProbeResult
	if err := json.Unmarshal(testInRes.StructuredContent, &probeIn); err != nil || probeIn.AppName != "Prowlarr" || probeIn.Version != "1.15.0.4361" {
		t.Fatalf("test_prowlarr_link_input result: %+v, %v", probeIn, err)
	}

	if res := callToolWithParams(t, f, token, domain.LLMWriteToolUpdateProwlarrLink, map[string]any{
		"linkId": linkID,
		"body":   map[string]any{"name": "Prowlarr Pos Renamed", "base_url": pl.srv.URL, "enabled": true},
	}); res.IsError {
		t.Fatalf("update_prowlarr_link failed: %s", res.ContentText)
	}

	testRes := callToolWithParams(t, f, token, domain.LLMWriteToolTestProwlarrLink, map[string]any{"linkId": linkID})
	if testRes.IsError {
		t.Fatalf("test_prowlarr_link failed: %s", testRes.ContentText)
	}
	var probe oas.ProbeResult
	if err := json.Unmarshal(testRes.StructuredContent, &probe); err != nil || probe.AppName != "Prowlarr" {
		t.Fatalf("test_prowlarr_link result: %+v, %v", probe, err)
	}
	if !pl.rec.has(http.MethodGet, "/api/v1/system/status") {
		t.Fatal("prowlarr stub did not receive status query")
	}

	if res := callToolWithParams(t, f, token, domain.LLMWriteToolDeleteProwlarrLink, map[string]any{
		"linkId": linkID, "confirm": true,
	}); res.IsError {
		t.Fatalf("delete_prowlarr_link failed: %s", res.ContentText)
	}
}

func testPositiveNotificationChannels(t *testing.T, f *mcpFixture, token string) string {
	t.Helper()
	var ntfyHits atomic.Int32
	ntfySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		ntfyHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ntfySrv.Close)

	createCh := callToolWithParams(t, f, token, domain.LLMWriteToolCreateNotificationChannel, map[string]any{
		"body": map[string]any{
			"name": "Ntfy Pos", "kind": "ntfy", "enabled": true,
			"config": map[string]any{"server_url": ntfySrv.URL, "topic": "alerts", "token": "ntfy-tok"},
		},
	})
	if createCh.IsError {
		t.Fatalf("create_notification_channel failed: %s", createCh.ContentText)
	}
	var ch oas.NotificationChannel
	if err := json.Unmarshal(createCh.StructuredContent, &ch); err != nil {
		t.Fatalf("decode channel: %v", err)
	}
	if ch.Name != "Ntfy Pos" {
		t.Fatalf("channel name = %q, want 'Ntfy Pos'", ch.Name)
	}
	chID := ch.ID.String()

	if res := callToolWithParams(t, f, token, domain.LLMWriteToolUpdateNotificationChannel, map[string]any{
		"channelId": chID,
		"body": map[string]any{
			"name": "Ntfy Pos Renamed", "kind": "ntfy", "enabled": true,
			"config": map[string]any{"server_url": ntfySrv.URL, "topic": "alerts"},
		},
	}); res.IsError {
		t.Fatalf("update_notification_channel failed: %s", res.ContentText)
	}
	if res := callToolWithParams(t, f, token, domain.LLMWriteToolTestNotificationChannel, map[string]any{
		"channelId": chID,
	}); res.IsError {
		t.Fatalf("test_notification_channel failed: %s", res.ContentText)
	}
	if ntfyHits.Load() != 1 {
		t.Fatalf("ntfy test hits = %d, want 1", ntfyHits.Load())
	}
	return chID
}

func testPositiveNotificationRules(t *testing.T, f *mcpFixture, token, chID string) {
	t.Helper()
	createRl := callToolWithParams(t, f, token, domain.LLMWriteToolCreateNotificationRule, map[string]any{
		"body": map[string]any{
			"name": "Rule Pos", "event_kind": "circuit_opened", "threshold": 1,
			"enabled": true, "channel_ids": []string{chID},
		},
	})
	if createRl.IsError {
		t.Fatalf("create_notification_rule failed: %s", createRl.ContentText)
	}
	var rl oas.NotificationRule
	if err := json.Unmarshal(createRl.StructuredContent, &rl); err != nil {
		t.Fatalf("decode rule: %v", err)
	}
	rlID := rl.ID.String()

	if res := callToolWithParams(t, f, token, domain.LLMWriteToolUpdateNotificationRule, map[string]any{
		"ruleId": rlID,
		"body": map[string]any{
			"name": "Rule Pos Renamed", "event_kind": "circuit_opened", "threshold": 2,
			"enabled": true, "channel_ids": []string{chID},
		},
	}); res.IsError {
		t.Fatalf("update_notification_rule failed: %s", res.ContentText)
	}
	if res := callToolWithParams(t, f, token, domain.LLMWriteToolDeleteNotificationRule, map[string]any{
		"ruleId": rlID, "confirm": true,
	}); res.IsError {
		t.Fatalf("delete_notification_rule failed: %s", res.ContentText)
	}
	if res := callToolWithParams(t, f, token, domain.LLMWriteToolDeleteNotificationChannel, map[string]any{
		"channelId": chID, "confirm": true,
	}); res.IsError {
		t.Fatalf("delete_notification_channel failed: %s", res.ContentText)
	}
}

func TestMCPR3CWriteToolsPositive(t *testing.T) {
	t.Parallel()
	f := newMCPFixture(t)
	enableLLM(t, f, testAllWriteTools...)
	token := issueKey(t, f, "r3c-positive-tester", testAllScopes...)
	assertInitializeOK(t, f, token, f.srv.URL)

	fakeSonarr := newFakeSonarr(t, "secret-test-key")
	fakeQBit := newFakeQBit(t)
	fakeSeerr := newFakeSeerr(t, "seerr-key-1234", fakeSonarr.srv.URL, "secret-test-key")
	fakeProwlarr := newFakeProwlarr(t, "secret-prowlarr-key")

	inst, err := f.instSvc.Create(t.Context(), instances.Input{
		Kind: domain.KindSonarr, Name: "Sonarr R3C", BaseURL: fakeSonarr.srv.URL, APIKey: "secret-test-key", Enabled: true,
	})
	if err != nil {
		t.Fatalf("instSvc.Create: %v", err)
	}

	testPositiveDownloadClients(t, f, token, inst.ID.String(), fakeQBit, fakeSonarr)
	testPositiveSeerrTools(t, f, token, inst.ID, fakeSeerr, fakeSonarr)
	testPositiveProwlarrTools(t, f, token, fakeProwlarr)
	chID := testPositiveNotificationChannels(t, f, token)
	testPositiveNotificationRules(t, f, token, chID)
}

func testNegativeInstanceTools401(t *testing.T, f *mcpFixture, token string) {
	t.Helper()
	sonarr401 := newFakeSonarr(t, "expected-key")

	resInput := callToolWithParams(t, f, token, domain.LLMWriteToolTestInstanceInput, map[string]any{
		"body": map[string]any{
			"name": "Sonarr Neg", "kind": "sonarr", "base_url": sonarr401.srv.URL,
			"api_key": "wrong-key", "enabled": true,
		},
	})
	if !resInput.IsError {
		t.Fatal("test_instance_input with 401 expected IsError=true")
	}
	assertNoTransportError(t, resInput.ContentText)

	inst, err := f.instSvc.Create(t.Context(), instances.Input{
		Kind: domain.KindSonarr, Name: "Sonarr Neg", BaseURL: sonarr401.srv.URL, APIKey: "wrong-key", Enabled: true,
	})
	if err != nil {
		t.Fatalf("instSvc.Create: %v", err)
	}
	res := callToolWithParams(t, f, token, domain.LLMWriteToolTestInstance, map[string]any{
		"instanceId": inst.ID.String(),
	})
	if !res.IsError {
		t.Fatal("test_instance with 401 expected IsError=true")
	}
	assertNoTransportError(t, res.ContentText)
}

func testNegativeDownloadClientTools401(t *testing.T, f *mcpFixture, token string) {
	t.Helper()
	qbit401 := newFakeQBit(t)
	qbit401.failAuth.Store(true)

	resInput := callToolWithParams(t, f, token, domain.LLMWriteToolTestDownloadClientInput, map[string]any{
		"body": map[string]any{
			"name": "Qbit Neg", "kind": "qbittorrent", "base_url": qbit401.srv.URL,
			"username": "admin", "secret": "bad-secret", "enabled": true,
		},
	})
	var snapIn oas.DownloadClientStatus
	if err := json.Unmarshal(resInput.StructuredContent, &snapIn); err != nil {
		t.Fatalf("decode test_download_client_input: %v", err)
	}
	if snapIn.Reachable {
		t.Fatal("test_download_client_input expected Reachable=false on 401")
	}
	assertNoTransportError(t, snapIn.Error.Value)

	dl, err := f.dlSvc.Create(t.Context(), dlclients.Input{
		Kind: domain.ClientQBittorrent, Name: "Qbit Neg", BaseURL: qbit401.srv.URL,
		Username: "admin", Secret: "bad-secret", Enabled: true,
	})
	if err != nil {
		t.Fatalf("dlSvc.Create: %v", err)
	}
	res := callToolWithParams(t, f, token, domain.LLMWriteToolTestDownloadClient, map[string]any{
		"clientId": dl.ID.String(),
	})
	var snap oas.DownloadClientStatus
	if err := json.Unmarshal(res.StructuredContent, &snap); err != nil {
		t.Fatalf("decode test_download_client: %v", err)
	}
	if snap.Reachable {
		t.Fatal("test_download_client expected Reachable=false on 401")
	}
	assertNoTransportError(t, snap.Error.Value)
}

func testNegativeSeerrTools401(t *testing.T, f *mcpFixture, token string) {
	t.Helper()
	seerr401 := newFakeSeerr(t, "expected-key", "http://127.0.0.1:1", "k")

	resInput := callToolWithParams(t, f, token, domain.LLMWriteToolTestSeerrLinkInput, map[string]any{
		"body": map[string]any{
			"name": "Seerr Neg", "base_url": seerr401.srv.URL,
			"api_key": "wrong-key", "enabled": true,
		},
	})
	if !resInput.IsError {
		t.Fatal("test_seerr_link_input with 401 expected IsError=true")
	}
	assertNoTransportError(t, resInput.ContentText)

	link, err := f.seerrSvc.Create(t.Context(), seerr.Input{
		Name: "Seerr Neg", BaseURL: seerr401.srv.URL, APIKey: "wrong-key", Enabled: true,
	})
	if err != nil {
		t.Fatalf("seerrSvc.Create: %v", err)
	}
	res := callToolWithParams(t, f, token, domain.LLMWriteToolTestSeerrLink, map[string]any{
		"linkId": link.ID.String(),
	})
	if !res.IsError {
		t.Fatal("test_seerr_link with 401 expected IsError=true")
	}
	assertNoTransportError(t, res.ContentText)
}

func testNegativeProwlarrTools401(t *testing.T, f *mcpFixture, token string) {
	t.Helper()
	prowl401 := newFakeProwlarr(t, "expected-key")

	resInput := callToolWithParams(t, f, token, domain.LLMWriteToolTestProwlarrLinkInput, map[string]any{
		"body": map[string]any{
			"name": "Prowl Neg", "base_url": prowl401.srv.URL,
			"api_key": "wrong-key", "enabled": true,
		},
	})
	if !resInput.IsError {
		t.Fatal("test_prowlarr_link_input with 401 expected IsError=true")
	}
	assertNoTransportError(t, resInput.ContentText)

	link, err := f.prowlarrSvc.Create(t.Context(), prowlarr.Input{
		Name: "Prowl Neg", BaseURL: prowl401.srv.URL, APIKey: "wrong-key", Enabled: true,
	})
	if err != nil {
		t.Fatalf("prowlarrSvc.Create: %v", err)
	}
	res := callToolWithParams(t, f, token, domain.LLMWriteToolTestProwlarrLink, map[string]any{
		"linkId": link.ID.String(),
	})
	if !res.IsError {
		t.Fatal("test_prowlarr_link with 401 expected IsError=true")
	}
	assertNoTransportError(t, res.ContentText)
}

func testNegativeNotificationChannel401(t *testing.T, f *mcpFixture, token string) {
	t.Helper()
	srv401 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	t.Cleanup(srv401.Close)

	ch, err := f.notifySvc.CreateChannel(t.Context(), domain.NotificationChannelInput{
		Name: "ntfy-neg", Kind: domain.ChannelNtfy, Enabled: true,
		Config: domain.ChannelConfig{ServerURL: srv401.URL, Topic: "alerts"},
	})
	if err != nil {
		t.Fatalf("notifySvc.CreateChannel: %v", err)
	}
	res := callToolWithParams(t, f, token, domain.LLMWriteToolTestNotificationChannel, map[string]any{
		"channelId": ch.ID.String(),
	})
	if !res.IsError {
		t.Fatal("test_notification_channel with 401 expected IsError=true")
	}
	assertNoTransportError(t, res.ContentText)
}

func TestMCPR3CTestToolsNegativeUnauthorized(t *testing.T) {
	t.Parallel()
	f := newMCPFixture(t)
	enableLLM(t, f, testAllWriteTools...)
	token := issueKey(t, f, "neg-auth-tester", testAllScopes...)
	assertInitializeOK(t, f, token, f.srv.URL)

	testNegativeInstanceTools401(t, f, token)
	testNegativeDownloadClientTools401(t, f, token)
	testNegativeSeerrTools401(t, f, token)
	testNegativeProwlarrTools401(t, f, token)
	testNegativeNotificationChannel401(t, f, token)
}

func TestMCPOptionalBodyBoundary(t *testing.T) {
	t.Parallel()
	f := newMCPFixture(t)
	enableLLM(t, f, testAllWriteTools...)
	token := issueKey(t, f, "optional-body-tester", testAllScopes...)
	assertInitializeOK(t, f, token, f.srv.URL)

	cases := []struct {
		name      string
		tool      string
		args      map[string]any
		wantError bool
	}{
		{
			name:      "reset_state with omitted optional body succeeds",
			tool:      domain.LLMWriteToolResetState,
			args:      map[string]any{"confirm": true},
			wantError: false,
		},
		{
			name:      "reset_state with empty object optional body succeeds",
			tool:      domain.LLMWriteToolResetState,
			args:      map[string]any{"confirm": true, "body": map[string]any{}},
			wantError: false,
		},
		{
			name:      "import_configarr with omitted optional body reaches handler (no linked path)",
			tool:      domain.LLMWriteToolImportConfigarr,
			args:      map[string]any{},
			wantError: true,
		},
		{
			name:      "import_configarr with empty object optional body reaches handler (no linked path)",
			tool:      domain.LLMWriteToolImportConfigarr,
			args:      map[string]any{"body": map[string]any{}},
			wantError: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := callToolWithParams(t, f, token, tc.tool, tc.args)
			if got.IsError != tc.wantError {
				t.Fatalf("%s(%v) IsError = %v, want %v; text = %q", tc.tool, tc.args, got.IsError, tc.wantError, got.ContentText)
			}
		})
	}
}
