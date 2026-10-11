// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Command upstreamspec lets the contract tests check the hand-written API clients against the
// OpenAPI specs their upstream projects publish, without a spec entering this repository.
//
// The manifest `tools/upstreamspec/specs.yaml` pins each spec to one file at one commit of an
// upstream release, with the sha256 of that file.
//
//   - `upstreamspec fetch` downloads each pinned file into the cache directory, refuses a file
//     whose checksum differs from the manifest and names it, and writes one contract file per
//     spec (package contract).
//   - `upstreamspec guard` fails when a cache file is in the git index, under its own name or as
//     a copy, or when git or `.dockerignore` does not keep the cache directory out.
//   - `upstreamspec receipts` fails for a pinned spec whose contract test did not pass since the
//     last fetch: each passing test leaves a receipt with the pinned commit in the cache.
//
// The specs of Sonarr, Radarr and Lidarr are GPL-3.0 and this repository is EUPL-1.2, so neither
// the specs nor anything built from them is committed or copied into an image
// (docs/upstream-contracts.md).
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// deadline bounds one run: four downloads of about 300 kB each.
const deadline = 2 * time.Minute

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	err := run(ctx, os.Args[1:], os.Stdout, rawBase)
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, "upstreamspec:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer, base string) error {
	if len(args) == 0 {
		return errUsage
	}
	flags := flag.NewFlagSet("upstreamspec", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	manifest := flags.String("manifest", "tools/upstreamspec/specs.yaml", "manifest of pinned specs")
	cache := flags.String("cache", ".cache/upstream-specs", "cache directory")
	repo := flags.String("repo", ".", "repository root")
	if err := flags.Parse(args[1:]); err != nil {
		return fmt.Errorf("%w; %w", err, errUsage)
	}
	if flags.NArg() != 0 {
		return errUsage
	}
	pins, err := readManifest(*manifest)
	if err != nil {
		return err
	}
	switch args[0] {
	case "fetch":
		return fetcher{client: &http.Client{Timeout: deadline}, base: base, cache: *cache, out: out}.all(ctx, pins)
	case "guard":
		return guard(ctx, *repo, *cache, pins, out)
	case "receipts":
		return receipts(*cache, pins, out)
	default:
		return errUsage
	}
}
