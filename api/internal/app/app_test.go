// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package app

import (
	"testing"

	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/golusoris/golusoris/leader/gate"

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

// TestLeaderLockNameDefault checks the APP_LEADER_NAME default the app decorates into the gate.
func TestLeaderLockNameDefault(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty -> snatcharr", in: "", want: "snatcharr"},
		{name: "set -> kept", in: "custom-lock", want: "custom-lock"},
		{name: "one byte -> kept", in: "x", want: "x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var resolved gate.Options
			app := fxtest.New(t,
				fx.Supply(gate.Options{Name: tc.in}),
				fx.Decorate(defaultLeaderName),
				fx.Populate(&resolved),
			)
			app.RequireStart()
			app.RequireStop()
			if resolved.Name != tc.want {
				t.Fatalf("resolved Name = %q, want %q", resolved.Name, tc.want)
			}
		})
	}
}
