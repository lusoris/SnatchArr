// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Command settingsparity writes docs/settings-parity.tsv from config.Registry (ADR-0044),
// or with -check exits 1 when the committed table is out of date. Run from the repository root.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/lusoris/SnatchArr/api/internal/config"
)

const (
	docsPath = "docs/configuration.md"
	tsvPath  = "docs/settings-parity.tsv"
)

var errStale = errors.New(tsvPath + " is out of date; run make settings-parity")

func main() {
	check := flag.Bool("check", false, "fail when "+tsvPath+" differs from the registry")
	flag.Parse()
	if err := run(*check); err != nil {
		fmt.Fprintln(os.Stderr, "settingsparity:", err)
		os.Exit(1)
	}
}

func run(check bool) error {
	md, err := os.ReadFile(docsPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", docsPath, err)
	}
	want := config.ParityTSV(config.Registry(), config.DocsEnvs(md))
	if !check {
		// #nosec G306 G703 -- constant repository path to a public documentation file
		if werr := os.WriteFile(tsvPath, want, 0o644); werr != nil {
			return fmt.Errorf("write %s: %w", tsvPath, werr)
		}
		return nil
	}
	got, err := os.ReadFile(tsvPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", tsvPath, err)
	}
	if !bytes.Equal(got, want) {
		return errStale
	}
	return nil
}
