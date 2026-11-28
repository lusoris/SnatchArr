// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package libraryprocessing

import "go.uber.org/fx"

// Module provides the library processing rule Service in the fx graph.
var Module = fx.Module("snatcharr.libraryprocessing",
	fx.Provide(NewService),
)
