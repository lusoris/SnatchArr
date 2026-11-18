//go:build semgrep_fixture

// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package fixture

import (
	"github.com/lusoris/SnatchArr/api/internal/buildinfo"
	"github.com/lusoris/SnatchArr/api/internal/useragent"
)

func identity(injected buildinfo.Info) []string {
	// ruleid: no-empty-buildinfo-literal
	empty := buildinfo.Info{}
	return []string{
		empty.Version,
		// ruleid: no-empty-buildinfo-literal
		useragent.Default(buildinfo.Info{}),
		// ok: no-empty-buildinfo-literal
		useragent.Default(injected),
		// ok: no-empty-buildinfo-literal
		useragent.Default(buildinfo.Info{Version: "1.2.3"}),
	}
}
