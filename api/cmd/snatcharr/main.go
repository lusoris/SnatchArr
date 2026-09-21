// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Command snatcharr runs the SnatchArr control plane: HTTP API + embedded UI, SSE,
// and the gRPC WorkerService the Rust snatch-worker leases runs from.
package main

import (
	"go.uber.org/fx"

	"github.com/lusoris/SnatchArr/api/internal/app"
	"github.com/lusoris/SnatchArr/api/internal/buildinfo"
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
	fx.New(
		fx.Supply(buildinfo.Info{Version: version, Commit: commit, Date: date}),
		app.Module,
	).Run()
}
