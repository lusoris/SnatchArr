// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Command moduleroutes generates and checks OpenAPI operation module mappings.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	code, err := run(os.Args[1:], os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "moduleroutes:", err)
	}
	os.Exit(code)
}

func run(args []string, stdout, stderr io.Writer) (int, error) {
	if len(args) == 0 {
		return 2, errors.New("expected subcommand 'gen' or 'check'")
	}
	switch args[0] {
	case "gen":
		return runGen(args[1:], stderr)
	case "check":
		return runCheck(args[1:], stdout, stderr)
	default:
		return 2, fmt.Errorf("unknown subcommand %q, expected 'gen' or 'check'", args[0])
	}
}

func runGen(args []string, stderr io.Writer) (int, error) {
	fs := flag.NewFlagSet("gen", flag.ContinueOnError)
	fs.SetOutput(stderr)
	inPath := fs.String("in", "api/openapi/openapi.yaml", "Path to api/openapi/openapi.yaml")
	outPath := fs.String("out", "api/internal/build/moduleops/moduleops_gen.go", "Path to output Go file")
	if err := fs.Parse(args); err != nil {
		return 2, fmt.Errorf("parse flags: %w", err)
	}
	return executeGen(*inPath, *outPath)
}

func runCheck(args []string, stdout, stderr io.Writer) (int, error) {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	inPath := fs.String("in", "api/openapi/openapi.yaml", "Path to api/openapi/openapi.yaml")
	if err := fs.Parse(args); err != nil {
		return 2, fmt.Errorf("parse flags: %w", err)
	}
	return executeCheck(*inPath, stdout)
}
