// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package runtimesettings

import (
	"context"
	"time"

	"go.uber.org/fx"

	gconfig "github.com/golusoris/golusoris/core/config"
	"github.com/golusoris/golusoris/core/crypto"

	"github.com/lusoris/SnatchArr/api/internal/config"
	"github.com/lusoris/SnatchArr/api/internal/store"
)

// OptionsParams specifies dependencies for constructing effective Options.
type OptionsParams struct {
	fx.In
	Config    *gconfig.Config
	Base      config.BaseOptions
	Store     *store.Store
	Encryptor *crypto.Encryptor
}

// ProvideEffectiveOptions provides the merged effective config.Options to fx. It runs no
// migration: db.migrate.auto stays the deployment's choice, and a schema without the
// runtime_settings table yields no overrides (see LoadOverrides).
func ProvideEffectiveOptions(p OptionsParams) (config.Options, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return LoadEffectiveOptions(ctx, p.Config, p.Store, p.Encryptor)
}

// Module provides the runtimesettings service and effective Options to the fx graph.
var Module = fx.Module(
	"snatcharr.runtimesettings",
	fx.Provide(ProvideEffectiveOptions),
	fx.Provide(New),
)
