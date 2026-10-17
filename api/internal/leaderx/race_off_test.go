// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

//go:build !race

package leaderx_test

// failoverRetries is the takeover bound in retry periods (see failoverBound); the race build
// allows more (race_on_test.go).
const failoverRetries = 10
