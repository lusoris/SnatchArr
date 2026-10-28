// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

type positiveTestCase struct {
	name          string
	yaml          string
	wantOp        string
	wantRequired  []string
	wantDef       string
	wantConfirm   bool
	checkProperty string
	checkFormat   string
	checkEnum     []string
	checkPattern  string
	checkMaxLen   int
	checkDesc     string
	checkDefault  any
}

func TestGeneratePositive(t *testing.T) {
	t.Parallel()
	cases := []positiveTestCase{
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
		{
			name: "query param with ref enum schema and description override",
			yaml: `
paths:
  /logs:
    get:
      operationId: listLogs
      parameters:
        - name: level
          in: query
          description: Log level filter
          schema:
            $ref: "#/components/schemas/LogLevel"
components:
  schemas:
    LogLevel:
      type: string
      description: Schema level description
      enum: [debug, info, warn, error]
`,
			wantOp:        "listLogs",
			checkProperty: "level",
			checkEnum:     []string{"debug", "info", "warn", "error"},
			checkDesc:     "Log level filter",
		},
		{
			name: "inline enum pattern maxLength default kept",
			yaml: `
paths:
  /items:
    get:
      operationId: listItems
      parameters:
        - name: sort
          in: query
          schema:
            type: string
            enum: [asc, desc]
            pattern: "^[a-z]+$"
            maxLength: 10
            default: asc
`,
			wantOp:        "listItems",
			checkProperty: "sort",
			checkEnum:     []string{"asc", "desc"},
			checkPattern:  "^[a-z]+$",
			checkMaxLen:   10,
			checkDefault:  "asc",
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
			assertGeneratedSchema(t, parsed, tc)
		})
	}
}

func assertGeneratedSchema(t *testing.T, parsed map[string]any, tc positiveTestCase) {
	t.Helper()
	if addProps, ok := parsed["additionalProperties"].(bool); !ok || addProps {
		t.Fatalf("expected additionalProperties=false, got %v", parsed["additionalProperties"])
	}
	if len(tc.wantRequired) > 0 {
		rawReq, ok := parsed["required"].([]any)
		if !ok {
			t.Fatalf("expected required list, got %v", parsed["required"])
		}
		gotReq := make([]string, len(rawReq))
		for i, v := range rawReq {
			gotReq[i] = v.(string)
		}
		if !slices.Equal(gotReq, tc.wantRequired) {
			t.Fatalf("required = %v, want %v", gotReq, tc.wantRequired)
		}
	}
	if tc.wantDef != "" {
		defs, ok := parsed["$defs"].(map[string]any)
		if !ok || defs[tc.wantDef] == nil {
			t.Fatalf("expected $defs[%q], got %v", tc.wantDef, parsed["$defs"])
		}
	}
	props, ok := parsed["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties map, got %v", parsed["properties"])
	}
	if tc.wantConfirm && props["confirm"] == nil {
		t.Fatal("expected confirm property for destructive operation")
	}
	if tc.checkProperty != "" {
		pObj, ok := props[tc.checkProperty].(map[string]any)
		if !ok {
			t.Fatalf("missing property %q", tc.checkProperty)
		}
		if tc.checkFormat != "" && pObj["format"] != tc.checkFormat {
			t.Fatalf("property %q format = %v, want %v", tc.checkProperty, pObj["format"], tc.checkFormat)
		}
		if len(tc.checkEnum) > 0 {
			rawEnum, ok := pObj["enum"].([]any)
			if !ok {
				t.Fatalf("property %q missing enum list", tc.checkProperty)
			}
			gotEnum := make([]string, len(rawEnum))
			for i, v := range rawEnum {
				gotEnum[i] = fmt.Sprint(v)
			}
			if !slices.Equal(gotEnum, tc.checkEnum) {
				t.Fatalf("property %q enum = %v, want %v", tc.checkProperty, gotEnum, tc.checkEnum)
			}
		}
		if tc.checkPattern != "" && pObj["pattern"] != tc.checkPattern {
			t.Fatalf("property %q pattern = %v, want %v", tc.checkProperty, pObj["pattern"], tc.checkPattern)
		}
		if tc.checkMaxLen > 0 {
			if maxLenVal, ok := pObj["maxLength"].(float64); !ok || int(maxLenVal) != tc.checkMaxLen {
				t.Fatalf("property %q maxLength = %v, want %v", tc.checkProperty, pObj["maxLength"], tc.checkMaxLen)
			}
		}
		if tc.checkDesc != "" && pObj["description"] != tc.checkDesc {
			t.Fatalf("property %q description = %v, want %v", tc.checkProperty, pObj["description"], tc.checkDesc)
		}
		if tc.checkDefault != nil && pObj["default"] != tc.checkDefault {
			t.Fatalf("property %q default = %v, want %v", tc.checkProperty, pObj["default"], tc.checkDefault)
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
			name: "unknown param schema ref fails",
			yaml: `
paths:
  /logs:
    get:
      operationId: listLogs
      parameters:
        - name: level
          in: query
          schema:
            $ref: "#/components/schemas/NonExistentParamSchema"
components:
  schemas: {}
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
	cases := []struct {
		name   string
		yaml   string
		wantOp string
	}{
		{
			name: "ping without params or body",
			yaml: `
paths:
  /ping:
    get:
      operationId: ping
      summary: Ping without params or body
      responses:
        "200":
          description: Pong
`,
			wantOp: "ping",
		},
		{
			name: "boundary parameter schemas with items and limits",
			yaml: `
paths:
  /boundary:
    get:
      operationId: boundaryOp
      parameters:
        - name: noSchema
          in: query
        - name: arrayParam
          in: query
          schema:
            type: array
            items:
              type: string
            minLength: 0
            maxLength: 0
            exclusiveMinimum: 0
            exclusiveMaximum: 10
`,
			wantOp: "boundaryOp",
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
				t.Fatalf("unmarshal: %v", err)
			}
			raw, ok := m[tc.wantOp]
			if !ok {
				t.Fatalf("missing operation %q", tc.wantOp)
			}
			var s jsonschema.Schema
			if err := json.Unmarshal(raw, &s); err != nil {
				t.Fatalf("unmarshal into jsonschema.Schema: %v", err)
			}
			if _, err := s.Resolve(&jsonschema.ResolveOptions{ValidateDefaults: true}); err != nil {
				t.Fatalf("Resolve boundary schema: %v", err)
			}
		})
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
