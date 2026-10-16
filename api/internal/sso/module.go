// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package sso

import "go.uber.org/fx"

// Module provides the SSO service to the fx graph.
var Module = fx.Module("snatcharr.sso",
	fx.Provide(NewService),
)
