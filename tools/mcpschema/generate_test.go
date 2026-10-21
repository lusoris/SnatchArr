// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func TestGeneratePositive(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		yaml          string
		wantOp        string
		wantRequired  []string
		wantDef       string
		wantConfirm   bool
		checkProperty string
		checkFormat   string
	}{
		{
			name: "ref resolved and required body",
			yaml: `
paths:
  /widgets:
    post:
      operationId: createWidget
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/WidgetInput"
components:
  schemas:
    WidgetInput:
      type: object
      properties:
        name:
          type: string
`,
			wantOp:       "createWidget",
			wantRequired: []string{"body"},
			wantDef:      "WidgetInput",
		},
		{
			name: "path parameter with format uuid",
			yaml: `
paths:
  /widgets/{widgetId}:
    get:
      operationId: getWidget
      parameters:
        - name: widgetId
          in: path
          required: true
          schema:
            type: string
            format: uuid
`,
			wantOp:        "getWidget",
			wantRequired:  []string{"widgetId"},
			checkProperty: "widgetId",
			checkFormat:   "uuid",
		},
		{
			name: "destructive operation has confirm",
			yaml: `
paths:
  /widgets/{widgetId}:
    delete:
      operationId: deleteInstance
      x-destructive: true
      parameters:
        - name: widgetId
          in: path
          required: true
          schema:
            type: string
            format: uuid
`,
			wantOp:        "deleteInstance",
			wantRequired:  []string{"widgetId"},
			wantConfirm:   true,
			checkProperty: "widgetId",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := Generate([]byte(tc.yaml))
			if err != nil {
				t.Fatalf("Generate: %v", err)
			}
			var m map[string]json.RawMessage
			if err := json.Unmarshal(out, &m); err != nil {
				t.Fatalf("unmarshal generated JSON: %v", err)
			}
			raw, ok := m[tc.wantOp]
			if !ok {
				t.Fatalf("missing operation %q in %v", tc.wantOp, m)
			}
			var parsed map[string]any
			if err := json.Unmarshal(raw, &parsed); err != nil {
				t.Fatalf("unmarshal operation schema: %v", err)
			}
			assertGeneratedSchema(t, parsed, tc.wantRequired, tc.wantDef, tc.wantConfirm, tc.checkProperty, tc.checkFormat)
		})
	}
}

func assertGeneratedSchema(t *testing.T, parsed map[string]any, wantReq []string, wantDef string, wantConfirm bool, prop, format string) {
	t.Helper()
	if addProps, ok := parsed["additionalProperties"].(bool); !ok || addProps {
		t.Fatalf("expected additionalProperties=false, got %v", parsed["additionalProperties"])
	}
	if len(wantReq) > 0 {
		rawReq, ok := parsed["required"].([]any)
		if !ok {
			t.Fatalf("expected required list, got %v", parsed["required"])
		}
		gotReq := make([]string, len(rawReq))
		for i, v := range rawReq {
			gotReq[i] = v.(string)
		}
		if !slices.Equal(gotReq, wantReq) {
			t.Fatalf("required = %v, want %v", gotReq, wantReq)
		}
	}
	if wantDef != "" {
		defs, ok := parsed["$defs"].(map[string]any)
		if !ok || defs[wantDef] == nil {
			t.Fatalf("expected $defs[%q], got %v", wantDef, parsed["$defs"])
		}
	}
	props, ok := parsed["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties map, got %v", parsed["properties"])
	}
	if wantConfirm && props["confirm"] == nil {
		t.Fatal("expected confirm property for destructive operation")
	}
	if prop != "" {
		pObj, ok := props[prop].(map[string]any)
		if !ok {
			t.Fatalf("missing property %q", prop)
		}
		if format != "" && pObj["format"] != format {
			t.Fatalf("property %q format = %v, want %v", prop, pObj["format"], format)
		}
	}
}

func TestGenerateNegative(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		yaml    string
		wantErr error
	}{
		{
			name: "unknown schema ref fails",
			yaml: `
paths:
  /test:
    post:
      operationId: testOp
      requestBody:
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/NonExistent"
`,
			wantErr: ErrInvalidRef,
		},
		{
			name: "missing operationId fails",
			yaml: `
paths:
  /test:
    get:
      summary: No operation ID
      responses:
        "200":
          description: OK
`,
			wantErr: ErrMissingOpID,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Generate([]byte(tc.yaml))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestGenerateBoundary(t *testing.T) {
	t.Parallel()
	yaml := `
paths:
  /ping:
    get:
      operationId: ping
      summary: Ping without params or body
      responses:
        "200":
          description: Pong
`
	out, err := Generate([]byte(yaml))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	raw, ok := m["ping"]
	if !ok {
		t.Fatal("missing operation 'ping'")
	}
	var s jsonschema.Schema
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("unmarshal into jsonschema.Schema: %v", err)
	}
	if _, err := s.Resolve(&jsonschema.ResolveOptions{ValidateDefaults: true}); err != nil {
		t.Fatalf("Resolve boundary schema: %v", err)
	}
}

func TestGenerateDeterministic(t *testing.T) {
	t.Parallel()
	path := filepath.Join("..", "..", "api", "openapi", "openapi.yaml")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("cannot read %s: %v", path, err)
	}
	out1, err := Generate(src)
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}
	out2, err := Generate(src)
	if err != nil {
		t.Fatalf("second generate: %v", err)
	}
	if string(out1) != string(out2) {
		t.Fatal("Generate output is non-deterministic")
	}
}
