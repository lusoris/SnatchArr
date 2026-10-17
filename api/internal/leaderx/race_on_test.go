// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
//
// SPDX-License-Identifier: EUPL-1.2

//go:build race

package leaderx_test

// failoverRetries is the takeover bound in retry periods (see failoverBound). Under the race
// detector on a loaded host a takeover overran ten periods (2 s) in the pre-push race stage; fifty
// (10 s, as electionBound) still tells a bounded failover from none. The plain build keeps ten.
const failoverRetries = 50
