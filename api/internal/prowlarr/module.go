// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package prowlarr

import (
	"go.uber.org/fx"

	"github.com/lusoris/SnatchArr/api/internal/arrclient"
	"github.com/lusoris/SnatchArr/api/internal/prowlarrclient"
	"github.com/lusoris/SnatchArr/api/internal/snatch"
)

// Module provides the service and its planner gate.
var Module = fx.Module("snatcharr.prowlarr",
	fx.Provide(
		New,
		func() prowlarrclient.Client {
			o := arrclient.DefaultOptions()
			return prowlarrclient.New(o.Timeout, o.UserAgent)
		},
		fx.Annotate(func(s *Service) snatch.Gate { return s }, fx.ResultTags(snatch.GateGroup)),
	),
)
