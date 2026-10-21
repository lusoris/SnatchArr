// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package llm_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/lusoris/SnatchArr/api/internal/build/oas"
	"github.com/lusoris/SnatchArr/api/internal/buildinfo"
	"github.com/lusoris/SnatchArr/api/internal/domain"
	"github.com/lusoris/SnatchArr/api/internal/instances"
	"github.com/lusoris/SnatchArr/api/internal/llm"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
)

var testAllWriteTools = []string{
	domain.LLMWriteToolTriggerRun,
	domain.LLMWriteToolUpdatePolicy,
	domain.LLMWriteToolCancelRun,
	domain.LLMWriteToolDeleteInstance,
	domain.LLMWriteToolCreateInstance,
	domain.LLMWriteToolTestInstanceInput,
	domain.LLMWriteToolUpdateInstance,
	domain.LLMWriteToolTestInstance,
	domain.LLMWriteToolUpdateQueuePolicy,
	domain.LLMWriteToolPreviewRun,
	domain.LLMWriteToolDeleteInstanceEvents,
	domain.LLMWriteToolResetState,
	domain.LLMWriteToolCreateSchedule,
	domain.LLMWriteToolUpdateSchedule,
	domain.LLMWriteToolDeleteSchedule,
	domain.LLMWriteToolImportConfigarr,
	domain.LLMWriteToolUpdateSettings,
}

func callToolsListWithSchemas(t *testing.T, f *mcpFixture, token string) map[string]json.RawMessage {
	t.Helper()
	res := postMCP(t, f, token, "", map[string]any{
		"jsonrpc": "2.0", "id": 10, "method": "tools/list", "params": map[string]any{},
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("tools/list status = %d, body = %s", res.StatusCode, string(res.Body))
	}
	var rpc struct {
		Result struct {
			Tools []struct {
				Name        string          `json:"name"`
				InputSchema json.RawMessage `json:"inputSchema"` //nolint:tagliatelle // MCP JSON-RPC wire field
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(res.Body, &rpc); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}
	out := make(map[string]json.RawMessage, len(rpc.Result.Tools))
	for _, tl := range rpc.Result.Tools {
		out[tl.Name] = tl.InputSchema
	}
	return out
}

func assertSchemasEqual(t *testing.T, name string, gotRaw, wantRaw []byte) {
	t.Helper()
	var gotMap, wantMap map[string]any
	if err := json.Unmarshal(gotRaw, &gotMap); err != nil {
		t.Fatalf("unmarshal got schema for %s: %v", name, err)
	}
	if err := json.Unmarshal(wantRaw, &wantMap); err != nil {
		t.Fatalf("unmarshal want schema for %s: %v", name, err)
	}
	if !reflect.DeepEqual(gotMap, wantMap) {
		t.Fatalf("tool %s advertised schema != generated schema for operation:\ngot:  %s\nwant: %s",
			name, string(gotRaw), string(wantRaw))
	}
}

func TestMCPRegistryInputSchemasMatchGenerated(t *testing.T) {
	t.Parallel()
	f := newMCPFixture(t)
	enableLLM(t, f, testAllWriteTools...)
	token := issueKey(t, f, "schema-auditor",
		domain.ScopeCoreRead, domain.ScopeCoreWrite,
		domain.ScopeAdminRead, domain.ScopeAdminWrite,
		domain.ScopeDownloadClientsRead, domain.ScopeRequestsRead,
		domain.ScopeProwlarrRead, domain.ScopeNotificationsRead,
	)
	assertInitializeOK(t, f, token, f.srv.URL)

	advertised := callToolsListWithSchemas(t, f, token)
	for _, spec := range llm.RegisteredToolsForTest() {
		t.Run(spec.Name, func(t *testing.T) {
			t.Parallel()
			rawAdvertised, ok := advertised[spec.Name]
			if !ok {
				t.Fatalf("tool %s not advertised in tools/list", spec.Name)
			}
			wantSchema, err := llm.SchemaFor(spec.OperationID)
			if err != nil {
				t.Fatalf("SchemaFor(%s): %v", spec.OperationID, err)
			}
			wantBytes, err := json.Marshal(wantSchema)
			if err != nil {
				t.Fatalf("marshal wantSchema: %v", err)
			}
			assertSchemasEqual(t, spec.Name, rawAdvertised, wantBytes)

			var parsed map[string]any
			if err := json.Unmarshal(wantSchema, &parsed); err != nil {
				t.Fatalf("unmarshal generated schema: %v", err)
			}
			props, _ := parsed["properties"].(map[string]any)
			_, hasConfirm := props["confirm"]
			if spec.Destructive != hasConfirm {
				t.Fatalf("tool %s Destructive=%v, but generated schema hasConfirm=%v", spec.Name, spec.Destructive, hasConfirm)
			}
		})
	}
}

func TestMCPConstructorRejectsUnknownOperationID(t *testing.T) {
	t.Parallel()
	specs := []llm.ToolSpecForTest{
		{
			Name:        "snatcharr_unknown_op",
			OperationID: "nonExistentOperationID",
			Scope:       domain.ScopeCoreRead,
			ReadOnly:    true,
			Description: "Unknown operation.",
		},
	}
	_, err := llm.NewWithSpecsForTest(buildinfo.Info{Version: "test"}, nil, nil, nil, nil, specs)
	if err == nil {
		t.Fatal("expected error for unknown OperationID, got nil")
	}
}

func TestMCPAliasKeysRejected(t *testing.T) {
	t.Parallel()
	f := newMCPFixture(t)
	enableLLM(t, f, testAllWriteTools...)
	token := issueKey(t, f, "alias-tester", domain.ScopeCoreRead, domain.ScopeCoreWrite)
	assertInitializeOK(t, f, token, f.srv.URL)

	inst, err := f.instSvc.Create(t.Context(), instances.Input{
		Kind: domain.KindSonarr, Name: "Sonarr Alias", BaseURL: "http://127.0.0.1:1", APIKey: "secret-test-key", Enabled: true,
	})
	if err != nil {
		t.Fatalf("instSvc.Create: %v", err)
	}

	cases := []struct {
		name string
		tool string
		args map[string]any
	}{
		{
			name: "get_instance rejects extra alias key id alongside instanceId",
			tool: "snatcharr_get_instance",
			args: map[string]any{"instanceId": inst.ID.String(), "id": inst.ID.String()},
		},
		{
			name: "get_instance rejects alias id",
			tool: "snatcharr_get_instance",
			args: map[string]any{"id": inst.ID.String()},
		},
		{
			name: "get_instance rejects alias instance_id",
			tool: "snatcharr_get_instance",
			args: map[string]any{"instance_id": inst.ID.String()},
		},
		{
			name: "test_instance rejects alias id",
			tool: domain.LLMWriteToolTestInstance,
			args: map[string]any{"id": inst.ID.String()},
		},
		{
			name: "delete_instance rejects alias id",
			tool: domain.LLMWriteToolDeleteInstance,
			args: map[string]any{"id": inst.ID.String(), "confirm": true},
		},
		{
			name: "preview_run rejects alias id",
			tool: domain.LLMWriteToolPreviewRun,
			args: map[string]any{"id": inst.ID.String(), "kind": "missing"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := callToolWithParams(t, f, token, tc.tool, tc.args)
			if !got.IsError {
				t.Fatalf("%s(%v) expected IsError=true, got success", tc.tool, tc.args)
			}
		})
	}
}

func TestMCPRegistryInputSchemasDifferenceCheck(t *testing.T) {
	t.Parallel()
	schemaA := []byte(`{"type":"object","properties":{"a":{"type":"string"}}}`)
	schemaB := []byte(`{"type":"object","properties":{"b":{"type":"string"}}}`)
	var mapA, mapB map[string]any
	if err := json.Unmarshal(schemaA, &mapA); err != nil {
		t.Fatalf("unmarshal A: %v", err)
	}
	if err := json.Unmarshal(schemaB, &mapB); err != nil {
		t.Fatalf("unmarshal B: %v", err)
	}
	if reflect.DeepEqual(mapA, mapB) {
		t.Fatal("differing schemas unexpectedly considered equal")
	}
}

func TestMCPSettingsUpdateSecurityFence(t *testing.T) {
	t.Parallel()
	f := newMCPFixture(t)
	enableLLM(t, f, domain.LLMWriteToolUpdateSettings)
	token := issueKey(t, f, "fence-tester", domain.ScopeAdminRead, domain.ScopeAdminWrite)
	assertInitializeOK(t, f, token, f.srv.URL)

	updateArgs := map[string]any{
		"body": map[string]any{
			"history_retention_days": 45,
			"user_agent":             "SnatchArr-Custom/1.0",
			"global_hourly_cap":      150,
			"llm_enabled":            false,
			"llm_write_tools":        testAllWriteTools,
		},
	}
	withStoredPresets(t, f, token, updateArgs)
	got := callToolWithParams(t, f, token, domain.LLMWriteToolUpdateSettings, updateArgs)
	if got.IsError {
		t.Fatalf("update_settings failed: %s", got.ContentText)
	}

	stored, err := f.settingsSvc.Get(t.Context())
	if err != nil {
		t.Fatalf("settingsSvc.Get: %v", err)
	}
	if stored.HistoryRetentionDays != 45 {
		t.Errorf("HistoryRetentionDays = %d, want 45", stored.HistoryRetentionDays)
	}
	if stored.UserAgent != "SnatchArr-Custom/1.0" {
		t.Errorf("UserAgent = %q, want SnatchArr-Custom/1.0", stored.UserAgent)
	}
	if stored.GlobalHourlyCap != 150 {
		t.Errorf("GlobalHourlyCap = %d, want 150", stored.GlobalHourlyCap)
	}
	if !stored.LLMEnabled {
		t.Errorf("LLMEnabled was overwritten to false, want true preserved")
	}
	wantTools := []string{domain.LLMWriteToolUpdateSettings}
	if !reflect.DeepEqual(stored.LLMWriteTools, wantTools) {
		t.Errorf("LLMWriteTools = %v, want %v preserved", stored.LLMWriteTools, wantTools)
	}
}

type destructiveTestCase struct {
	name      string
	tool      string
	makeArgs  func(instID, runID, schedID uuid.UUID) map[string]any
	checkHeld func(ctx context.Context, f *mcpFixture, instID, runID, schedID uuid.UUID) error
}

func runDestructiveCase(t *testing.T, f *mcpFixture, token string, tc destructiveTestCase, instID, runID, schedID uuid.UUID) {
	t.Helper()
	args := tc.makeArgs(instID, runID, schedID)
	got := callToolWithParams(t, f, token, tc.tool, args)
	if !got.IsError {
		t.Fatalf("%s without confirm expected IsError=true", tc.tool)
	}
	if got.ContentText != "confirmation required: pass confirm=true" {
		t.Fatalf("%s text = %q, want 'confirmation required: pass confirm=true'", tc.tool, got.ContentText)
	}
	if err := tc.checkHeld(t.Context(), f, instID, runID, schedID); err != nil {
		t.Fatalf("%s state check failed: %v", tc.tool, err)
	}
}

func checkSchedulePresent(ctx context.Context, f *mcpFixture, schedID uuid.UUID) error {
	list, err := f.schedules.List(ctx)
	if err != nil {
		return fmt.Errorf("list schedules: %w", err)
	}
	for _, s := range list {
		if s.ID == schedID {
			return nil
		}
	}
	return fmt.Errorf("schedule %s not found in list", schedID)
}

func getDestructiveCases() []destructiveTestCase {
	return []destructiveTestCase{
		{
			name: "delete_instance without confirm",
			tool: domain.LLMWriteToolDeleteInstance,
			makeArgs: func(instID, _, _ uuid.UUID) map[string]any {
				return map[string]any{"instanceId": instID.String(), "confirm": false}
			},
			checkHeld: func(ctx context.Context, f *mcpFixture, instID, _, _ uuid.UUID) error {
				if _, err := f.instSvc.Get(ctx, instID); err != nil {
					return fmt.Errorf("instance missing: %w", err)
				}
				return nil
			},
		},
		{
			name: "delete_instance_events without confirm",
			tool: domain.LLMWriteToolDeleteInstanceEvents,
			makeArgs: func(instID, _, _ uuid.UUID) map[string]any {
				return map[string]any{"instanceId": instID.String()}
			},
			checkHeld: func(ctx context.Context, f *mcpFixture, instID, _, _ uuid.UUID) error {
				if _, err := f.instSvc.Get(ctx, instID); err != nil {
					return fmt.Errorf("instance missing: %w", err)
				}
				return nil
			},
		},
		{
			name: "cancel_run without confirm",
			tool: domain.LLMWriteToolCancelRun,
			makeArgs: func(_, runID, _ uuid.UUID) map[string]any {
				return map[string]any{"runId": runID.String(), "confirm": false}
			},
			checkHeld: func(ctx context.Context, f *mcpFixture, _, runID, _ uuid.UUID) error {
				r, err := f.st.Q().GetRun(ctx, runID)
				if err != nil || r.Status != "queued" {
					return fmt.Errorf("run status changed: %w (%s)", err, r.Status)
				}
				return nil
			},
		},
		{
			name: "reset_state without confirm",
			tool: domain.LLMWriteToolResetState,
			makeArgs: func(_, _, _ uuid.UUID) map[string]any {
				return map[string]any{"confirm": false}
			},
			checkHeld: func(_ context.Context, _ *mcpFixture, _, _, _ uuid.UUID) error {
				return nil
			},
		},
		{
			name: "delete_schedule without confirm",
			tool: domain.LLMWriteToolDeleteSchedule,
			makeArgs: func(_, _, schedID uuid.UUID) map[string]any {
				return map[string]any{"scheduleId": schedID.String()}
			},
			checkHeld: func(ctx context.Context, f *mcpFixture, _, _, schedID uuid.UUID) error {
				return checkSchedulePresent(ctx, f, schedID)
			},
		},
	}
}

func TestMCPDestructiveToolsRefuseWithoutConfirm(t *testing.T) {
	t.Parallel()
	f := newMCPFixture(t)
	enableLLM(t, f, testAllWriteTools...)
	token := issueKey(t, f, "destroy-tester", domain.ScopeCoreRead, domain.ScopeCoreWrite, domain.ScopeAdminRead, domain.ScopeAdminWrite)
	assertInitializeOK(t, f, token, f.srv.URL)

	inst, err := f.instSvc.Create(t.Context(), instances.Input{
		Kind: domain.KindSonarr, Name: "Sonarr Destruct", BaseURL: "http://127.0.0.1:1", APIKey: "secret-test-key", Enabled: true,
	})
	if err != nil {
		t.Fatalf("instSvc.Create: %v", err)
	}
	run, _, err := f.runs.EnqueueWithOptions(t.Context(), snatch.EnqueueOptions{
		InstanceID: inst.ID,
		Kind:       domain.SnatchMissing,
	})
	if err != nil {
		t.Fatalf("EnqueueWithOptions: %v", err)
	}
	sched, err := f.schedules.Create(t.Context(), domain.Schedule{
		Name: "Nightly", Days: []int{1}, Start: "02:00", End: "04:00", TZ: "UTC", Action: "pause", Enabled: true,
	})
	if err != nil {
		t.Fatalf("schedules.Create: %v", err)
	}

	for _, tc := range getDestructiveCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runDestructiveCase(t, f, token, tc, inst.ID, run.ID, sched.ID)
		})
	}
}

func TestMCPWriteToolsInvalidBody(t *testing.T) {
	t.Parallel()
	f := newMCPFixture(t)
	enableLLM(t, f, testAllWriteTools...)
	token := issueKey(t, f, "invalid-body-tester", domain.ScopeCoreRead, domain.ScopeCoreWrite, domain.ScopeAdminRead, domain.ScopeAdminWrite)
	assertInitializeOK(t, f, token, f.srv.URL)

	inst, err := f.instSvc.Create(t.Context(), instances.Input{
		Kind: domain.KindSonarr, Name: "Sonarr Valid", BaseURL: "http://127.0.0.1:1", APIKey: "secret-test-key", Enabled: true,
	})
	if err != nil {
		t.Fatalf("instSvc.Create: %v", err)
	}

	cases := []struct {
		name string
		tool string
		args map[string]any
	}{
		{
			name: "create_instance with invalid kind",
			tool: domain.LLMWriteToolCreateInstance,
			args: map[string]any{"body": map[string]any{"kind": "unsupported", "name": "bad"}},
		},
		{
			name: "test_instance_input with invalid kind",
			tool: domain.LLMWriteToolTestInstanceInput,
			args: map[string]any{"body": map[string]any{"kind": "unsupported", "name": "bad"}},
		},
		{
			name: "update_instance with invalid kind",
			tool: domain.LLMWriteToolUpdateInstance,
			args: map[string]any{"instanceId": inst.ID.String(), "body": map[string]any{"kind": "unsupported", "name": "bad"}},
		},
		{
			name: "update_policy with invalid values",
			tool: domain.LLMWriteToolUpdatePolicy,
			args: map[string]any{"instanceId": inst.ID.String(), "body": map[string]any{"missing_per_cycle": -10}},
		},
		{
			name: "update_queue_policy with invalid rules type",
			tool: domain.LLMWriteToolUpdateQueuePolicy,
			args: map[string]any{"instanceId": inst.ID.String(), "body": map[string]any{"rules": "not_an_object"}},
		},
		{
			name: "create_schedule with invalid days",
			tool: domain.LLMWriteToolCreateSchedule,
			args: map[string]any{"body": map[string]any{"name": "Bad", "days": []any{99}, "start": "bad", "end": "bad"}},
		},
		{
			name: "update_settings with 0 retention days",
			tool: domain.LLMWriteToolUpdateSettings,
			args: withStoredPresets(t, f, token, map[string]any{"body": map[string]any{"history_retention_days": 0, "user_agent": "x"}}),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := callToolWithParams(t, f, token, tc.tool, tc.args)
			if !got.IsError {
				t.Fatalf("%s with bad body expected IsError=true, got text: %q", tc.tool, got.ContentText)
			}
		})
	}
}

func writeSampleConfigarr(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yml")
	content := "sonarr:\n  sample:\n    base_url: http://127.0.0.1:1\n    api_key: secret-test-key\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write config.yml: %v", err)
	}
	return p
}

func testPositiveInstancesAndPolicies(t *testing.T, f *mcpFixture, token string) string {
	t.Helper()
	createRes := callToolWithParams(t, f, token, domain.LLMWriteToolCreateInstance, map[string]any{
		"body": map[string]any{
			"name": "Sonarr Pos", "kind": "sonarr", "base_url": "http://127.0.0.1:1",
			"api_key": "secret-test-key", "enabled": true,
		},
	})
	if createRes.IsError {
		t.Fatalf("create_instance failed: %s", createRes.ContentText)
	}
	var createdInst oas.Instance
	if err := json.Unmarshal(createRes.StructuredContent, &createdInst); err != nil {
		t.Fatalf("decode created instance: %v", err)
	}
	instID := createdInst.ID.String()

	testInputRes := callToolWithParams(t, f, token, domain.LLMWriteToolTestInstanceInput, map[string]any{
		"body": map[string]any{
			"name": "Sonarr TestIn", "kind": "sonarr", "base_url": "http://127.0.0.1:1",
			"api_key": "secret-test-key", "enabled": true,
		},
	})
	if testInputRes.IsError {
		t.Fatalf("test_instance_input failed: %s", testInputRes.ContentText)
	}
	if testRes := callToolWithParams(t, f, token, domain.LLMWriteToolTestInstance, map[string]any{
		"instanceId": instID,
	}); testRes.IsError {
		t.Fatalf("test_instance failed: %s", testRes.ContentText)
	}
	if updRes := callToolWithParams(t, f, token, domain.LLMWriteToolUpdateInstance, map[string]any{
		"instanceId": instID,
		"body": map[string]any{
			"name": "Sonarr Pos Renamed", "kind": "sonarr", "base_url": "http://127.0.0.1:1",
			"api_key": "secret-test-key", "enabled": true,
		},
	}); updRes.IsError {
		t.Fatalf("update_instance failed: %s", updRes.ContentText)
	}
	polGot := callToolWithParams(t, f, token, "snatcharr_get_policy", map[string]any{"instanceId": instID})
	var polBody map[string]any
	if err := json.Unmarshal(polGot.StructuredContent, &polBody); err != nil {
		t.Fatalf("decode get_policy: %v", err)
	}
	if updPol := callToolWithParams(t, f, token, domain.LLMWriteToolUpdatePolicy, map[string]any{
		"instanceId": instID, "body": polBody,
	}); updPol.IsError {
		t.Fatalf("update_policy failed: %s", updPol.ContentText)
	}
	qpGot := callToolWithParams(t, f, token, "snatcharr_get_queue_policy", map[string]any{"instanceId": instID})
	var qpBody map[string]any
	if err := json.Unmarshal(qpGot.StructuredContent, &qpBody); err != nil {
		t.Fatalf("decode get_queue_policy: %v", err)
	}
	qpBody["enabled"] = true
	if updQP := callToolWithParams(t, f, token, domain.LLMWriteToolUpdateQueuePolicy, map[string]any{
		"instanceId": instID, "body": qpBody,
	}); updQP.IsError {
		t.Fatalf("update_queue_policy failed: %s", updQP.ContentText)
	}
	return instID
}

func testPositiveRunsAndEvents(t *testing.T, f *mcpFixture, token, instID string) {
	t.Helper()
	trigRes := callToolWithParams(t, f, token, domain.LLMWriteToolTriggerRun, map[string]any{
		"instanceId": instID, "body": map[string]any{"kind": "missing"},
	})
	if trigRes.IsError {
		t.Fatalf("trigger_run failed: %s", trigRes.ContentText)
	}
	var trigRun struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(trigRes.StructuredContent, &trigRun); err != nil {
		t.Fatalf("decode trigRun: %v", err)
	}
	if cRes := callToolWithParams(t, f, token, domain.LLMWriteToolCancelRun, map[string]any{
		"runId": trigRun.ID, "confirm": true,
	}); cRes.IsError {
		t.Fatalf("cancel_run failed: %s", cRes.ContentText)
	}
	if pRes := callToolWithParams(t, f, token, domain.LLMWriteToolPreviewRun, map[string]any{
		"instanceId": instID, "kind": "missing",
	}); pRes.IsError {
		t.Fatalf("preview_run failed: %s", pRes.ContentText)
	}
	if dRes := callToolWithParams(t, f, token, domain.LLMWriteToolDeleteInstanceEvents, map[string]any{
		"instanceId": instID, "confirm": true,
	}); dRes.IsError {
		t.Fatalf("delete_instance_events failed: %s", dRes.ContentText)
	}
	if rRes := callToolWithParams(t, f, token, domain.LLMWriteToolResetState, map[string]any{
		"confirm": true,
	}); rRes.IsError {
		t.Fatalf("reset_state failed: %s", rRes.ContentText)
	}
}

func testPositiveSchedulesAndSettings(t *testing.T, f *mcpFixture, token, configPath, instID string) {
	t.Helper()
	createSchedRes := callToolWithParams(t, f, token, domain.LLMWriteToolCreateSchedule, map[string]any{
		"body": map[string]any{
			"name": "Weekday Pause", "days": []int{1, 2, 3, 4, 5},
			"start": "01:00", "end": "05:00", "tz": "UTC", "action": "pause", "enabled": true,
		},
	})
	if createSchedRes.IsError {
		t.Fatalf("create_schedule failed: %s", createSchedRes.ContentText)
	}
	var createdSched oas.Schedule
	if err := json.Unmarshal(createSchedRes.StructuredContent, &createdSched); err != nil {
		t.Fatalf("decode createdSched: %v", err)
	}
	schedID := createdSched.ID.String()

	if uSched := callToolWithParams(t, f, token, domain.LLMWriteToolUpdateSchedule, map[string]any{
		"scheduleId": schedID,
		"body": map[string]any{
			"name": "Weekday Pause Renamed", "days": []int{1, 2, 3, 4, 5},
			"start": "01:00", "end": "05:00", "tz": "UTC", "action": "pause", "enabled": true,
		},
	}); uSched.IsError {
		t.Fatalf("update_schedule failed: %s", uSched.ContentText)
	}
	if dSched := callToolWithParams(t, f, token, domain.LLMWriteToolDeleteSchedule, map[string]any{
		"scheduleId": schedID, "confirm": true,
	}); dSched.IsError {
		t.Fatalf("delete_schedule failed: %s", dSched.ContentText)
	}
	if impRes := callToolWithParams(t, f, token, domain.LLMWriteToolImportConfigarr, map[string]any{
		"body": map[string]any{"config_path": configPath},
	}); impRes.IsError {
		t.Fatalf("import_configarr failed: %s", impRes.ContentText)
	}
	if uSet := callToolWithParams(t, f, token, domain.LLMWriteToolUpdateSettings, withStoredPresets(t, f, token, map[string]any{
		"body": map[string]any{
			"history_retention_days": 60, "user_agent": "SnatchArr-Positive/1.0",
			"global_hourly_cap": 200, "llm_enabled": true, "llm_write_tools": testAllWriteTools,
		},
	})); uSet.IsError {
		t.Fatalf("update_settings failed: %s", uSet.ContentText)
	}
	if dInst := callToolWithParams(t, f, token, domain.LLMWriteToolDeleteInstance, map[string]any{
		"instanceId": instID, "confirm": true,
	}); dInst.IsError {
		t.Fatalf("delete_instance failed: %s", dInst.ContentText)
	}
}

func TestMCPWriteToolsPositive(t *testing.T) {
	t.Parallel()
	f := newMCPFixture(t)
	enableLLM(t, f, testAllWriteTools...)
	token := issueKey(t, f, "positive-tester", domain.ScopeCoreRead, domain.ScopeCoreWrite, domain.ScopeAdminRead, domain.ScopeAdminWrite)
	assertInitializeOK(t, f, token, f.srv.URL)

	configPath := writeSampleConfigarr(t)
	instID := testPositiveInstancesAndPolicies(t, f, token)
	testPositiveRunsAndEvents(t, f, token, instID)
	testPositiveSchedulesAndSettings(t, f, token, configPath, instID)
}

// withStoredPresets copies the stored priority presets into an update_settings
// body, so a test changes only the fields it names.
func withStoredPresets(t *testing.T, f *mcpFixture, token string, args map[string]any) map[string]any {
	t.Helper()
	got := callToolWithParams(t, f, token, "snatcharr_get_settings", map[string]any{})
	if got.IsError {
		t.Fatalf("get_settings failed: %s", got.ContentText)
	}
	var current map[string]json.RawMessage
	if err := json.Unmarshal([]byte(got.ContentText), &current); err != nil {
		t.Fatalf("decode get_settings: %v", err)
	}
	body, ok := args["body"].(map[string]any)
	if !ok {
		t.Fatalf("args carry no body object")
	}
	for _, key := range []string{"search_priority_preset", "download_priority_preset"} {
		preset, found := current[key]
		if !found {
			t.Fatalf("get_settings lacks %s", key)
		}
		body[key] = preset
	}
	return args
}
