// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

package retention

import (
	"os"
	"testing"

	"github.com/lusoris/SnatchArr/api/internal/store/storetest"
)

// TestMain shares one Postgres container across the package's integration tests.
func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }
