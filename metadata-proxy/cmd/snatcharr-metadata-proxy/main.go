// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Command snatcharr-metadata-proxy runs the outbound CONNECT forward proxy and admin HTTP probes
// specified in ADR-0049.
package main

import (
	"go.uber.org/fx"

	"github.com/lusoris/SnatchArr/metadata-proxy/internal/app"
)

// Build metadata injected by -ldflags.
//
//nolint:gochecknoglobals // ldflags targets must be package-level variables
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	fx.New(
		fx.Supply(app.BuildInfo{Version: version, Commit: commit, Date: date}),
		app.Module,
	).Run()
}
