// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"bytes"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestCheckFixtures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		fixture    string
		wantExit   int
		wantReason string
	}{
		{
			name:       "clean fixture passes",
			fixture:    "testdata/clean.yaml",
			wantExit:   0,
			wantReason: "",
		},
		{
			name:       "real openapi specification passes",
			fixture:    "../../api/openapi/openapi.yaml",
			wantExit:   0,
			wantReason: "",
		},
		{
			name:       "planted unknown module fails",
			fixture:    "testdata/unknown-module.yaml",
			wantExit:   1,
			wantReason: "unregistered module id \"unknown_mod\"",
		},
		{
			name:       "planted missing x-module on non-core tag fails",
			fixture:    "testdata/missing-x-module.yaml",
			wantExit:   1,
			wantReason: "missing x-module for non-core tag \"watchaware\"",
		},
		{
			name:       "planted bad x-module type fails",
			fixture:    "testdata/bad-type.yaml",
			wantExit:   1,
			wantReason: "x-module must be a non-empty string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			path := filepath.Clean(tt.fixture)
			code, err := run([]string{"check", "-in", path}, &stdout, &stderr)
			if code != tt.wantExit {
				t.Fatalf("run(check) exit code = %d, want %d (err: %v, out: %s)", code, tt.wantExit, err, stdout.String())
			}
			if tt.wantReason != "" {
				out := stdout.String()
				if !strings.Contains(out, tt.wantReason) {
					t.Fatalf("run(check) output %q does not contain expected reason %q", out, tt.wantReason)
				}
			}
		})
	}
}

func TestCheckBoundaryCases(t *testing.T) {
	t.Parallel()
	t.Run("empty x-module string fails", func(t *testing.T) {
		t.Parallel()
		specYAML := []byte(`openapi: 3.1.0
info:
  title: Empty X-Module API
  version: 0.1.0
paths:
  /test:
    get:
      operationId: getTest
      x-module: ""
      responses:
        '200':
          description: OK
`)
		regSet := map[string]struct{}{"llm": {}}
		findings, err := CheckSpec(specYAML, regSet)
		if err != nil {
			t.Fatalf("CheckSpec unexpected error: %v", err)
		}
		if len(findings) != 1 {
			t.Fatalf("expected 1 finding, got %d", len(findings))
		}
		if findings[0].Reason != "x-module must be a non-empty string" {
			t.Fatalf("unexpected reason: %q", findings[0].Reason)
		}
	})

	t.Run("missing operationId with x-module in gen fails", func(t *testing.T) {
		t.Parallel()
		specYAML := []byte(`openapi: 3.1.0
info:
  title: No OpID API
  version: 0.1.0
paths:
  /test:
    get:
      x-module: "llm"
      responses:
        '200':
          description: OK
`)
		_, err := GenerateCode(specYAML)
		if err == nil {
			t.Fatal("expected GenerateCode error for missing operationId, got nil")
		}
	})
}

func TestCheckAllPathItemOperations(t *testing.T) {
	t.Parallel()
	specYAML := []byte(`openapi: 3.1.0
info:
  title: All Operations API
  version: 0.1.0
paths:
  /widgets:
    get:
      operationId: getWidget
      x-module: ""
    put:
      operationId: putWidget
      x-module: ""
    post:
      operationId: postWidget
      x-module: ""
    delete:
      operationId: deleteWidget
      x-module: ""
    options:
      operationId: optionsWidget
      x-module: ""
    head:
      operationId: headWidget
      x-module: ""
    patch:
      operationId: patchWidget
      x-module: ""
    trace:
      operationId: traceWidget
      x-module: ""
    parameters: []
    summary: Widget operations
    servers: []
    $ref: "#/components/pathItems/Widget"
`)
	findings, err := CheckSpec(specYAML, map[string]struct{}{"llm": {}})
	if err != nil {
		t.Fatalf("CheckSpec unexpected error: %v", err)
	}
	if len(findings) != 8 {
		t.Fatalf("visited %d operations, want 8: %v", len(findings), findings)
	}
	for _, finding := range findings {
		if finding.Reason != "x-module must be a non-empty string" {
			t.Errorf("unexpected finding for %s: %q", finding.Method, finding.Reason)
		}
	}
}

func TestGenerateCodeAllPathItemOperations(t *testing.T) {
	t.Parallel()
	specYAML := []byte(`openapi: 3.1.0
info:
  title: All Generated Operations API
  version: 0.1.0
paths:
  /widgets:
    get:
      operationId: getWidget
      x-module: get_module
    put:
      operationId: putWidget
      x-module: put_module
    post:
      operationId: postWidget
      x-module: post_module
    delete:
      operationId: deleteWidget
      x-module: delete_module
    options:
      operationId: optionsWidget
      x-module: options_module
    head:
      operationId: headWidget
      x-module: head_module
    patch:
      operationId: patchWidget
      x-module: patch_module
    trace:
      operationId: traceWidget
      x-module: trace_module
  /core:
    get:
      operationId: getCore
`)
	generated, err := GenerateCode(specYAML)
	if err != nil {
		t.Fatalf("GenerateCode unexpected error: %v", err)
	}
	for _, method := range []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			pattern := `(?m)^\s*"` + method + `Widget":\s+"` + method + `_module",$`
			matched, err := regexp.Match(pattern, generated)
			if err != nil {
				t.Fatalf("match generated mapping: %v", err)
			}
			if !matched {
				t.Errorf("generated code missing mapping for %sWidget to %s_module:\n%s", method, method, generated)
			}
		})
	}
	t.Run("missing x-module is omitted", func(t *testing.T) {
		t.Parallel()
		if bytes.Contains(generated, []byte("getCore")) {
			t.Errorf("generated code includes operation without x-module:\n%s", generated)
		}
	})
}

func TestGenerateCodeEmptyModule(t *testing.T) {
	t.Parallel()
	specYAML := []byte(`openapi: 3.1.0
info:
  title: Empty Generated Module API
  version: 0.1.0
paths:
  /widgets:
    get:
      operationId: getWidget
      x-module: ""
`)
	_, err := GenerateCode(specYAML)
	if err == nil || err.Error() != "operation has invalid x-module: must be non-empty string" {
		t.Fatalf("GenerateCode error = %v, want invalid x-module error", err)
	}
}

func TestGenerateCode(t *testing.T) {
	t.Parallel()
	t.Run("real openapi generates empty map", func(t *testing.T) {
		t.Parallel()
		var stdout, stderr bytes.Buffer
		tmpOut := filepath.Join(t.TempDir(), "moduleops_gen.go")
		code, err := run([]string{"gen", "-in", "../../api/openapi/openapi.yaml", "-out", tmpOut}, &stdout, &stderr)
		if code != 0 || err != nil {
			t.Fatalf("run(gen) failed: code=%d err=%v stderr=%s", code, err, stderr.String())
		}
	})

	t.Run("clean fixture generates populated map", func(t *testing.T) {
		t.Parallel()
		var stdout, stderr bytes.Buffer
		tmpOut := filepath.Join(t.TempDir(), "moduleops_gen.go")
		code, err := run([]string{"gen", "-in", "testdata/clean.yaml", "-out", tmpOut}, &stdout, &stderr)
		if code != 0 || err != nil {
			t.Fatalf("run(gen) failed: code=%d err=%v stderr=%s", code, err, stderr.String())
		}
	})
}

func TestRunCommandFlags(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	code, err := run([]string{}, &stdout, &stderr)
	if code != 2 || err == nil {
		t.Fatalf("expected usage error (code 2), got %d (%v)", code, err)
	}

	code, err = run([]string{"invalid-subcommand"}, &stdout, &stderr)
	if code != 2 || err == nil {
		t.Fatalf("expected usage error (code 2), got %d (%v)", code, err)
	}
}
