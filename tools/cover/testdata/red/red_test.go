// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package red holds the planted failing test of tools/cover/gotest_test.sh.
package red

import "testing"

func TestPlantedRed(t *testing.T) {
	t.Error("planted failure: this line must reach the gate log")
}
