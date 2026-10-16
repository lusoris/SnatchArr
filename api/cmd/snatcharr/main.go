// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Command snatcharr runs the SnatchArr control plane: HTTP API + embedded UI, SSE,
// and the gRPC WorkerService the Rust snatch-worker leases runs from.
package main

import (
	"context"
	"os"

	"go.uber.org/fx"

	"github.com/lusoris/SnatchArr/api/internal/app"
	"github.com/lusoris/SnatchArr/api/internal/buildinfo"
	"github.com/lusoris/SnatchArr/api/internal/healthprobe"
)

// Build metadata injected by -ldflags (see Dockerfile / GoReleaser).
//
//nolint:gochecknoglobals // ldflags targets must be package-level variables
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	// `snatcharr-api healthcheck` probes a running API instead of starting one (container
	// health check in a distroless image).
	if len(os.Args) > 1 && os.Args[1] == healthprobe.Command {
		os.Exit(healthprobe.Run(context.Background(), os.Getenv, os.Stderr))
	}
	fx.New(
		fx.Supply(buildinfo.Info{Version: version, Commit: commit, Date: date}),
		app.Module,
	).Run()
}
