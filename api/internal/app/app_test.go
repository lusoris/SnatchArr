// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package app

import (
	"testing"

	"go.uber.org/fx"

	"github.com/lusoris/SnatchArr/api/internal/buildinfo"
)

// The control plane's fx graph resolves: every constructor, decorator and invoke finds its
// dependencies (as cmd/snatcharr assembles it), without running any of them.
func TestModuleGraphValidates(t *testing.T) {
	t.Parallel()
	if err := fx.ValidateApp(fx.Supply(buildinfo.Info{Version: "test"}), Module); err != nil {
		t.Fatalf("fx graph: %v", err)
	}
}
