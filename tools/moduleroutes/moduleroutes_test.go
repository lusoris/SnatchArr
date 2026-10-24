// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package main

import (
	"bytes"
	"path/filepath"
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
