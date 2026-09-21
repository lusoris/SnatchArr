// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package snatch

import (
	"go.uber.org/fx"

	"github.com/lusoris/SnatchArr/api/internal/store"
)

// storeParam keeps the planner constructor's parameter list readable.
type storeParam struct {
	fx.In
	Store *store.Store
}
