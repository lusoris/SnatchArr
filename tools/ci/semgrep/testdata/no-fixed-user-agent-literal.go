//go:build semgrep_fixture

// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package fixture

import (
	"github.com/lusoris/SnatchArr/api/internal/buildinfo"
	"github.com/lusoris/SnatchArr/api/internal/useragent"
)

// ruleid: no-fixed-user-agent-literal
const fixedDefaultUA = "SnatchArr/1.0 (https://github.com/lusoris/SnatchArr)"

func userAgents(info buildinfo.Info) []string {
	return []string{
		// ruleid: no-fixed-user-agent-literal
		fixedDefaultUA,
		// ruleid: no-fixed-user-agent-literal
		"SnatchArr/2.0",
		// ruleid: no-fixed-user-agent-literal
		`SnatchArr/0.1 (+https://github.com/lusoris/SnatchArr)`,
		// ok: no-fixed-user-agent-literal
		useragent.Default(info),
		// ok: no-fixed-user-agent-literal
		"SnatchArr/dev (+https://github.com/lusoris/SnatchArr)",
		// ok: no-fixed-user-agent-literal
		buildinfo.Name + "/" + info.Version,
		// ok: no-fixed-user-agent-literal
		"CustomAgent/1.0",
	}
}
