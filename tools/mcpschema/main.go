// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Command mcpschema generates self-contained JSON Schemas from OpenAPI for MCP tools.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func main() {
	code, err := run(os.Args[1:], os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mcpschema:", err)
	}
	os.Exit(code)
}

func run(args []string, _, stderr io.Writer) (int, error) {
	fs := flag.NewFlagSet("mcpschema", flag.ContinueOnError)
	fs.SetOutput(stderr)
	inPath := fs.String("in", "api/openapi/openapi.yaml", "Path to api/openapi/openapi.yaml")
	outPath := fs.String("out", "api/internal/llm/schemas_gen.json", "Path to output schemas_gen.json")
	if err := fs.Parse(args); err != nil {
		return 2, fmt.Errorf("parse flags: %w", err)
	}

	cleanIn := filepath.Clean(*inPath)
	// #nosec G304 -- path provided via CLI flag
	src, err := os.ReadFile(cleanIn)
	if err != nil {
		return 1, fmt.Errorf("read %s: %w", cleanIn, err)
	}

	outBytes, err := Generate(src)
	if err != nil {
		return 1, fmt.Errorf("generate schemas: %w", err)
	}

	cleanOut := filepath.Clean(*outPath)
	if err := os.MkdirAll(filepath.Dir(cleanOut), 0o750); err != nil {
		return 1, fmt.Errorf("mkdir %s: %w", filepath.Dir(cleanOut), err)
	}
	if err := os.WriteFile(cleanOut, outBytes, 0o600); err != nil {
		return 1, fmt.Errorf("write %s: %w", cleanOut, err)
	}
	return 0, nil
}
