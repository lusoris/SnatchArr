// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package llm_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/lusoris/SnatchArr/api/internal/buildinfo"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/llm"
)

func lookupListRunsOp(t *testing.T) string {
	t.Helper()
	for _, spec := range llm.RegisteredToolsForTest() {
		if spec.Name == "snatcharr_list_runs" {
			return spec.OperationID
		}
	}
	t.Fatal("snatcharr_list_runs not found in registered tools")
	return ""
}

func newFixtureWithOps(t *testing.T, ops map[string]string) *mcpFixture {
	t.Helper()
	f := newMCPFixture(t)
	llmSvc, err := llm.NewWithSpecsAndOpsForTest(
		f.build, f.settingsSvc, f.security, f.handlers, f.modulesSvc, f.logger,
		llm.RegisteredToolsForTest(), ops,
	)
	if err != nil {
		t.Fatalf("NewWithSpecsAndOpsForTest: %v", err)
	}
	r := chi.NewRouter()
	llm.Mount(r, llmSvc)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	f.srv = srv
	return f
}

func assertCallSuccess(t *testing.T, f *mcpFixture, token string, id int) {
	t.Helper()
	res := postMCP(t, f, token, "", map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "snatcharr_list_runs",
			"arguments": map[string]any{},
		},
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("call status = %d, want 200", res.StatusCode)
	}
	var callResp struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Result  struct {
			IsError bool `json:"isError"` //nolint:tagliatelle // MCP JSON-RPC wire field
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(res.Body, &callResp); err != nil {
		t.Fatalf("unmarshal call response: %v (%s)", err, string(res.Body))
	}
	if callResp.ID != id || callResp.Error != nil || callResp.Result.IsError {
		t.Fatalf("call response = %+v, want success", callResp)
	}
}

func assertCallModuleDisabled(t *testing.T, f *mcpFixture, token, keyID string, id int) {
	t.Helper()
	res := postMCP(t, f, token, "", map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "snatcharr_list_runs",
			"arguments": map[string]any{},
		},
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("call status = %d, want 200", res.StatusCode)
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
		t.Fatalf("unmarshal error: %v (%s)", err, string(res.Body))
	}
	if rpcErr.ID != id {
		t.Fatalf("echoed id = %d, want %d", rpcErr.ID, id)
	}
	if rpcErr.Error.Code != llm.ErrCodeModuleDisabled || rpcErr.Error.Message != "module disabled" {
		t.Fatalf("error = %+v, want code=%d msg='module disabled'", rpcErr.Error, llm.ErrCodeModuleDisabled)
	}
	assertLogContains(t, f.logBuf.String(),
		"llm: module tool refused",
		"tool=snatcharr_list_runs",
		"module=watchaware",
		"owner="+f.admin.ID.String(),
		"key_id="+keyID,
		"reason=module_disabled",
	)
}

func TestMCPModuleGatingPositiveAndNegative(t *testing.T) {
	t.Parallel()
	opID := lookupListRunsOp(t)
	ops := map[string]string{opID: domain.ModuleWatchAware}

	tests := []struct {
		name          string
		moduleEnabled bool
		wantInList    bool
	}{
		{name: "positive module enabled: tool in list and call succeeds", moduleEnabled: true, wantInList: true},
		{name: "negative module disabled: tool absent and call refused", moduleEnabled: false, wantInList: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newFixtureWithOps(t, ops)
			enableLLM(t, f)
			if err := f.modulesSvc.SetEnabled(t.Context(), domain.ModuleWatchAware, tc.moduleEnabled); err != nil {
				t.Fatalf("SetEnabled: %v", err)
			}
			rawKey, err := f.apiKeys.Create(t.Context(), f.admin.ID, "mod-key-"+tc.name, nil, domain.ScopeCoreRead)
			if err != nil {
				t.Fatalf("apiKeys.Create: %v", err)
			}
			assertInitializeOK(t, f, rawKey.Secret, f.srv.URL)

			tools := callToolsList(t, f, rawKey.Secret)
			inList := slices.Contains(tools, "snatcharr_list_runs")
			if inList != tc.wantInList {
				t.Fatalf("snatcharr_list_runs in tools/list = %v, want %v", inList, tc.wantInList)
			}
			if !slices.Contains(tools, "snatcharr_list_instances") {
				t.Fatal("expected core tool snatcharr_list_instances to remain listed")
			}

			if tc.moduleEnabled {
				assertCallSuccess(t, f, rawKey.Secret, 100)
			} else {
				assertCallModuleDisabled(t, f, rawKey.Secret, rawKey.ID, 200)
			}
		})
	}
}

func TestMCPModuleGatingBoundaryToggle(t *testing.T) {
	t.Parallel()
	opID := lookupListRunsOp(t)
	f := newFixtureWithOps(t, map[string]string{opID: domain.ModuleWatchAware})
	enableLLM(t, f)

	rawKey, err := f.apiKeys.Create(t.Context(), f.admin.ID, "toggle-key", nil, domain.ScopeCoreRead)
	if err != nil {
		t.Fatalf("apiKeys.Create: %v", err)
	}
	assertInitializeOK(t, f, rawKey.Secret, f.srv.URL)

	// Step 1: Initial state (disabled).
	if err := f.modulesSvc.SetEnabled(t.Context(), domain.ModuleWatchAware, false); err != nil {
		t.Fatalf("SetEnabled off: %v", err)
	}
	tools1 := callToolsList(t, f, rawKey.Secret)
	if slices.Contains(tools1, "snatcharr_list_runs") {
		t.Fatal("step 1: expected snatcharr_list_runs absent from tools/list")
	}
	assertCallModuleDisabled(t, f, rawKey.Secret, rawKey.ID, 1)

	// Step 2: Toggle ON.
	if err := f.modulesSvc.SetEnabled(t.Context(), domain.ModuleWatchAware, true); err != nil {
		t.Fatalf("SetEnabled on: %v", err)
	}
	tools2 := callToolsList(t, f, rawKey.Secret)
	if !slices.Contains(tools2, "snatcharr_list_runs") {
		t.Fatal("step 2: expected snatcharr_list_runs present in tools/list")
	}
	assertCallSuccess(t, f, rawKey.Secret, 2)

	// Step 3: Toggle OFF.
	if err := f.modulesSvc.SetEnabled(t.Context(), domain.ModuleWatchAware, false); err != nil {
		t.Fatalf("SetEnabled off again: %v", err)
	}
	tools3 := callToolsList(t, f, rawKey.Secret)
	if slices.Contains(tools3, "snatcharr_list_runs") {
		t.Fatal("step 3: expected snatcharr_list_runs absent after toggle off")
	}
	assertCallModuleDisabled(t, f, rawKey.Secret, rawKey.ID, 3)
}

func TestMCPModuleConstructorHandsetValidation(t *testing.T) {
	t.Parallel()
	opID := lookupListRunsOp(t)
	ops := map[string]string{opID: domain.ModuleWatchAware}

	t.Run("disagreeing hand-set module returns error wrapping ErrInvalid", func(t *testing.T) {
		t.Parallel()
		spec := llm.ToolSpecForTest{
			Name:        "snatcharr_list_runs",
			OperationID: opID,
			Scope:       domain.ScopeCoreRead,
			Module:      "conflicting_module",
			ReadOnly:    true,
			Description: "Test spec.",
		}
		_, err := llm.NewWithSpecsAndOpsForTest(
			buildinfo.Info{Version: "test"}, nil, nil, nil, nil, nil,
			[]llm.ToolSpecForTest{spec}, ops,
		)
		if err == nil {
			t.Fatal("expected constructor error, got nil")
		}
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("expected ErrInvalid, got %v", err)
		}
	})

	t.Run("matching hand-set module succeeds", func(t *testing.T) {
		t.Parallel()
		spec := llm.ToolSpecForTest{
			Name:        "snatcharr_list_runs",
			OperationID: opID,
			Scope:       domain.ScopeCoreRead,
			Module:      domain.ModuleWatchAware,
			ReadOnly:    true,
			Description: "Test spec.",
		}
		svc, err := llm.NewWithSpecsAndOpsForTest(
			buildinfo.Info{Version: "test"}, nil, nil, nil, nil, nil,
			[]llm.ToolSpecForTest{spec}, ops,
		)
		if err != nil {
			t.Fatalf("unexpected constructor error: %v", err)
		}
		if svc == nil {
			t.Fatal("expected non-nil service")
		}
	})
}

type brokenModuleChecker struct {
	err error
}

func (b *brokenModuleChecker) IsEnabled(_ context.Context, _ string) (bool, error) {
	return false, b.err
}

func TestMCPModuleFailClosedOnError(t *testing.T) {
	t.Parallel()
	opID := lookupListRunsOp(t)
	ops := map[string]string{opID: domain.ModuleWatchAware}

	f := newMCPFixture(t)
	enableLLM(t, f)
	fake := &brokenModuleChecker{err: errors.New("simulated database failure")}
	llmSvc, err := llm.NewWithSpecsAndOpsForTest(
		f.build, f.settingsSvc, f.security, f.handlers, fake, f.logger,
		llm.RegisteredToolsForTest(), ops,
	)
	if err != nil {
		t.Fatalf("NewWithSpecsAndOpsForTest: %v", err)
	}
	r := chi.NewRouter()
	llm.Mount(r, llmSvc)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	f.srv = srv

	token := issueKey(t, f, "fail-closed-user", domain.ScopeCoreRead)

	res := postMCP(t, f, token, "", map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
		"params":  map[string]any{},
	})
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 Internal Server Error", res.StatusCode)
	}
	if !strings.HasPrefix(res.Header.Get("Content-Type"), "application/problem+json") {
		t.Fatalf("Content-Type = %q, want application/problem+json", res.Header.Get("Content-Type"))
	}
}
