// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

// Package green holds the passing test of tools/cover/gotest_test.sh.
package green

import "testing"

func TestPlantedGreen(t *testing.T) {
	if 1+1 != 2 {
		t.Error("arithmetic")
	}
}
