// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package prowlarr

import (
	"go.uber.org/fx"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/buildinfo"
	"github.com/lusoris/SnatchArr/api/internal/prowlarrclient"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
	"github.com/lusoris/SnatchArr/api/internal/useragent"
)

// Module provides the service and its planner gate.
var Module = fx.Module("snatcharr.prowlarr",
	fx.Provide(
		New,
		func(info buildinfo.Info, ua *useragent.Source) prowlarrclient.Client {
			o := arrclient.DefaultOptionsFor(info)
			return prowlarrclient.NewWithSource(o.Timeout, o.UserAgent, ua)
		},
		fx.Annotate(func(s *Service) snatch.Gate { return s }, fx.ResultTags(snatch.GateGroup)),
	),
)
